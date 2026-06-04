package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// 本文件实现 Batch B #6 许愿池的「单个 DailyRescan task」（坑2）+ 重搜命中判定 + 通知。
//
// 严格照 docs 附录 v2：
//   - 单个 DailyRescan task（不是每条目一个 timer），每分钟 tick 一次，按 hash(id)%1440 错峰（坑2）。
//   - searching_at 锁 + 自愈由 WishService.ClaimSearchableItems 负责（坑6）。
//   - 命中判定接 moviepilot SearchAllResources（GetSiteResources 聚合），seeders>=WISH_MIN_SEEDERS 视为命中（坑3 不设质量门槛）。
//   - 命中详情里标注疑似枪版/无中字/分辨率（从候选标题解析，坑3），用户自决。
//   - 通知前 getChatMember 检查可达（坑5），不可达 → ORPHANED。
//   - 超期最终重搜 + EXPIRED + 私信（坑4）。
//   - FOUND 触发通知前不自动求片，只发带「🎬 立即求片」按钮的喜报（出源后仅通知）。
//
// 重搜「命中判定」取舍说明：moviepilot 没有按 tmdb_id 精确搜资源的接口，只能按关键词（标题）跨站搜。
// 为避免「错误触发求片」（宁可漏报不误报），这里只把 seeders>=阈值 当作命中信号，
// 由 FOUND→NOTIFIED 全程「仅通知 + 人工 ack」兜底——即便命中判定偶有误报，也绝不会自动下载。

// WishNotifier 抽象通知发送，便于在调度里复用 telegram + 群可达检查。
// 这里直接持有 *TelegramClient，不另造接口（复用现有消息发送机制）。

// WishScheduler 是许愿池的后台重搜调度器。
type WishScheduler struct {
	wish        *WishService
	moviepilot  *MoviePilotClient
	telegram    *TelegramClient
	groupChatID int64 // 用于 getChatMember 可达性检查（坑5）；0 表示未配置群，跳过检查

	intervalHours int
	expireDays    int
	minSeeders    int

	// 出源喜报里「立即求片」按钮的回调串构造函数（由 handler 注入，避免本包反向依赖 callback 包）。
	buildRequestButton func(item *WishItem) (text string, callbackData string)

	stopCh   chan struct{}
	stopOnce sync.Once

	mu            sync.Mutex
	lastExpiryDay int // 记录上次跑过期扫描的「年内第几天」，保证每天只扫一次（坑4）
}

// NewWishScheduler 创建许愿池调度器。
func NewWishScheduler(
	wish *WishService,
	moviepilot *MoviePilotClient,
	telegram *TelegramClient,
	groupChatID int64,
	intervalHours, expireDays, minSeeders int,
) *WishScheduler {
	if intervalHours <= 0 {
		intervalHours = 24
	}
	if expireDays <= 0 {
		expireDays = 30
	}
	if minSeeders <= 0 {
		minSeeders = 1
	}
	return &WishScheduler{
		wish:          wish,
		moviepilot:    moviepilot,
		telegram:      telegram,
		groupChatID:   groupChatID,
		intervalHours: intervalHours,
		expireDays:    expireDays,
		minSeeders:    minSeeders,
		stopCh:        make(chan struct{}),
		lastExpiryDay: -1,
	}
}

// SetRequestButtonBuilder 注入「立即求片」按钮构造（由 main.go 用 callback 包实现）。
func (s *WishScheduler) SetRequestButtonBuilder(f func(item *WishItem) (string, string)) {
	s.buildRequestButton = f
}

// Start 启动单个后台 task：每分钟 tick，错峰重搜 + 每天一次过期扫描。
func (s *WishScheduler) Start() {
	go s.run()
	logger.Info("[wish] DailyRescan task 已启动（interval=%dh expire=%dd minSeeders=%d）",
		s.intervalHours, s.expireDays, s.minSeeders)
}

// Stop 停止调度。可重复调用。
func (s *WishScheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *WishScheduler) run() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

// tick 每分钟执行一次：①错峰重搜本分钟该搜的条目 ②每天一次过期扫描。
func (s *WishScheduler) tick(now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[wish] tick panic recovered: %v", r)
		}
	}()

	s.rescanThisMinute(now)

	// 每天 03:00 跑一次过期扫描（避开整点高峰；只跑一次靠 lastExpiryDay 去重）。
	if now.Hour() == 3 && now.Minute() == 0 {
		s.mu.Lock()
		day := now.YearDay()
		shouldRun := s.lastExpiryDay != day
		if shouldRun {
			s.lastExpiryDay = day
		}
		s.mu.Unlock()
		if shouldRun {
			s.runExpirySweep()
		}
	}
}

// rescanThisMinute 认领「本分钟该搜」且锁可用的条目并逐个重搜（坑2 错峰）。
func (s *WishScheduler) rescanThisMinute(now time.Time) {
	nowMinute := now.Hour()*60 + now.Minute()

	// 认领一批锁可用的条目（ClaimSearchableItems 已做 searching_at 锁 + 自愈）。
	// 取较大 limit，再在内存里用 SearchOffsetMinutes 过滤出「本分钟该搜」的，避免一次性搜太多。
	claimed, err := s.wish.ClaimSearchableItems(s.intervalHours, 200)
	if err != nil {
		logger.Info("[wish] 认领待搜条目失败: %v", err)
		return
	}
	for _, item := range claimed {
		if SearchOffsetMinutes(item.ID) != nowMinute {
			// 不是本分钟该搜的：立即解锁，等它自己的时隙。
			if err := s.wish.ReleaseSearchLock(item.ID); err != nil {
				logger.Info("[wish] id=%d 解锁失败: %v", item.ID, err)
			}
			continue
		}
		s.researchOne(item, false)
	}
}

// researchOne 对单个条目做一次重搜。final=true 表示这是过期前的「最终重搜」。
func (s *WishScheduler) researchOne(item *WishItem, final bool) {
	detail, hit := s.searchHit(item)
	if !hit {
		if final {
			// 最终重搜仍无源 → EXPIRED + 私信发起人（坑4）。
			if err := s.wish.MarkExpired(item.ID); err != nil {
				logger.Info("[wish] id=%d 置 EXPIRED 失败: %v", item.ID, err)
				return
			}
			s.notifyExpired(item)
			return
		}
		// 普通重搜未命中：解锁，保持 SEARCHING 等下个窗口。
		if err := s.wish.ReleaseSearchLock(item.ID); err != nil {
			logger.Info("[wish] id=%d 解锁失败: %v", item.ID, err)
		}
		return
	}

	// 命中：SEARCHING→FOUND（前置状态校验防并发重复推）。
	changed, err := s.wish.MarkFound(item.ID, detail)
	if err != nil {
		logger.Info("[wish] id=%d MarkFound 失败: %v", item.ID, err)
		return
	}
	if !changed {
		return // 已被其它路径处理，避免重复通知。
	}

	// 坑7：FOUND 触发前再查一遍现有订阅/求片，命中则直接 FULFILLED，不发通知（防撞两条任务）。
	if s.alreadySubscribed(item) {
		if _, err := s.wish.MarkFulfilled(item.ID); err != nil {
			logger.Info("[wish] id=%d 已有订阅，置 FULFILLED 失败: %v", item.ID, err)
		} else {
			logger.Info("[wish] id=%d 命中时发现已有订阅/求片，直接 FULFILLED，不重复通知", item.ID)
		}
		return
	}

	// 重新读最新条目（带 found_detail）后通知。
	fresh, err := s.wish.GetByID(item.ID)
	if err != nil || fresh == nil {
		fresh = item
		fresh.FoundDetail = detail
	}
	s.notifyFound(fresh)
}

// searchHit 接 moviepilot 跨站搜索做命中判定。
// 返回 (命中详情含质量标注, 是否命中)。
// 出错或资源不可用时一律返回未命中（保守：宁可漏报也不误报，绝不错误触发后续流程）。
func (s *WishScheduler) searchHit(item *WishItem) (string, bool) {
	if s.moviepilot == nil {
		return "", false
	}
	keyword := strings.TrimSpace(item.Title)
	if keyword == "" {
		return "", false
	}

	resources, err := s.moviepilot.SearchAllResources(keyword, 1)
	if err != nil {
		logger.Info("[wish] id=%d 重搜失败（视为未命中）: %v", item.ID, err)
		return "", false
	}

	// 找做种数最高、且 >= 阈值的候选。
	var best *TorrentResource
	var bestSite string
	for site, list := range resources {
		for i := range list {
			r := &list[i]
			if r.Seeders < s.minSeeders {
				continue
			}
			if best == nil || r.Seeders > best.Seeders {
				best = r
				bestSite = site
			}
		}
	}
	if best == nil {
		return "", false
	}

	// 组装命中详情 + 质量标注（坑3：不拦，只标注，用户自决）。
	tags := parseQualityTags(best.Title)
	detail := fmt.Sprintf("%s · %s · 做种 %d", bestSite, best.Title, best.Seeders)
	if len(tags) > 0 {
		detail += " ⚠️ " + strings.Join(tags, "/")
	}
	logger.Info("[wish] id=%d 命中: %s", item.ID, detail)
	return detail, true
}

// alreadySubscribed 复用 FindExistingSubscription 查现有订阅/求片（坑7）。
func (s *WishScheduler) alreadySubscribed(item *WishItem) bool {
	if s.moviepilot == nil || item.TmdbID == 0 {
		return false
	}
	mt := MediaTypeMovie
	if item.MediaType == "tv" {
		mt = MediaTypeTV
	}
	_, found, err := s.moviepilot.FindExistingSubscription(item.TmdbID, mt, item.Season)
	if err != nil {
		// 查询失败时保守返回 false（继续走通知，由人工 ack 兜底，不会自动下载）。
		logger.Info("[wish] id=%d FindExistingSubscription 失败（按未订阅处理）: %v", item.ID, err)
		return false
	}
	return found
}

// notifyFound 发出源喜报（带「🎬 立即求片」按钮）。先做坑5 可达性检查。
func (s *WishScheduler) notifyFound(item *WishItem) {
	if !s.reachable(item.UserID) {
		if err := s.wish.MarkOrphaned(item.ID); err != nil {
			logger.Info("[wish] id=%d 置 ORPHANED 失败: %v", item.ID, err)
		}
		return
	}

	var b strings.Builder
	b.WriteString("🎉 你许愿的影片出源啦！\n\n")
	b.WriteString(fmt.Sprintf("🎬 %s", item.Title))
	if item.Year > 0 {
		b.WriteString(fmt.Sprintf(" (%d)", item.Year))
	}
	if item.MediaType == "tv" && item.Season > 0 {
		b.WriteString(fmt.Sprintf(" 第%d季", item.Season))
	}
	b.WriteString("\n")
	if item.FoundDetail != "" {
		b.WriteString("\n📦 " + item.FoundDetail + "\n")
	}
	b.WriteString("\n点下面按钮即可发起求片（需要你确认）。")

	var keyboard *types.TelegramInlineKeyboard
	if s.buildRequestButton != nil {
		text, data := s.buildRequestButton(item)
		if text != "" && data != "" {
			keyboard = &types.TelegramInlineKeyboard{
				InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
					{{Text: text, CallbackData: data}},
				},
			}
		}
	}

	if _, err := s.telegram.SendMessage(item.UserID, b.String(), "", keyboard); err != nil {
		logger.Info("[wish] id=%d 发送出源喜报失败: %v", item.ID, err)
		return
	}

	// 通知发出成功 → FOUND→NOTIFIED（写 notified_at 启动 TTL）。
	if _, err := s.wish.MarkNotified(item.ID); err != nil {
		logger.Info("[wish] id=%d MarkNotified 失败: %v", item.ID, err)
	}
}

// notifyExpired 私信发起人「等了 N 天没找到已自动取消」（坑4）。
func (s *WishScheduler) notifyExpired(item *WishItem) {
	if !s.reachable(item.UserID) {
		return // 不可达就不打扰，状态已是 EXPIRED。
	}
	msg := fmt.Sprintf("😔 你许愿的《%s》等了 %d 天还没找到源，已自动从许愿池移除。\n如仍想看，可重新 /wish 加入。",
		item.Title, s.expireDays)
	if _, err := s.telegram.SendMessage(item.UserID, msg, "", nil); err != nil {
		logger.Info("[wish] id=%d 发送过期通知失败: %v", item.ID, err)
	}
}

// reachable 用 getChatMember 检查用户是否可达（坑5）。
// 未配置群 chatID 时无法检查群成员，退化为「假定可达」（私信失败时上层日志记录）。
func (s *WishScheduler) reachable(userID int64) bool {
	if s.groupChatID == 0 {
		return true
	}
	status, err := s.telegram.GetChatMemberStatus(s.groupChatID, userID)
	if err != nil {
		logger.Info("[wish] getChatMember 检查失败 user=%d: %v（按不可达处理）", userID, err)
		return false
	}
	switch status {
	case "left", "kicked":
		return false
	default:
		return true
	}
}

// runExpirySweep 每天一次：对超期无源条目做「最终重搜」，仍无则过期（坑4）。
func (s *WishScheduler) runExpirySweep() {
	candidates, err := s.wish.ListExpiryCandidates(s.expireDays)
	if err != nil {
		logger.Info("[wish] 过期扫描查询失败: %v", err)
		return
	}
	logger.Info("[wish] 过期扫描：%d 条候选做最终重搜", len(candidates))
	for _, item := range candidates {
		s.researchOne(item, true)
	}
}

// parseQualityTags 从候选标题解析疑似质量问题标注（坑3）。
func parseQualityTags(title string) []string {
	t := strings.ToUpper(title)
	var tags []string

	// 疑似枪版 / 录屏。
	for _, k := range []string{"CAM", "TS", "TC", "HDTS", "HDCAM", "TELESYNC", "枪版"} {
		if strings.Contains(t, k) {
			tags = append(tags, "疑似枪版")
			break
		}
	}
	// 分辨率标注。
	switch {
	case strings.Contains(t, "2160P") || strings.Contains(t, "4K") || strings.Contains(t, "UHD"):
		tags = append(tags, "4K")
	case strings.Contains(t, "1080P"):
		tags = append(tags, "1080P")
	case strings.Contains(t, "720P"):
		tags = append(tags, "720P")
	}
	// 中字判断：含明确中字标记则不标注；否则提示「可能无中字」。
	hasChs := strings.Contains(t, "CHS") || strings.Contains(t, "CHT") ||
		strings.Contains(t, "GB") || strings.Contains(t, "ZH") ||
		strings.Contains(title, "中字") || strings.Contains(title, "中文") ||
		strings.Contains(title, "简") || strings.Contains(title, "繁")
	if !hasChs {
		tags = append(tags, "可能无中字")
	}
	return tags
}
