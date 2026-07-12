package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/richmessage"
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
	lockTTLMin    int // searching_at 自愈锁 TTL（分钟），与重搜周期解耦（坑B），默认 60

	// 出源喜报里「立即求片」按钮的回调串构造函数（由 handler 注入，避免本包反向依赖 callback 包）。
	buildRequestButton func(item *WishItem) (text string, callbackData string)

	stopCh   chan struct{}
	stopOnce sync.Once

	mu sync.Mutex // 串行化过期扫描判定（读改写 last_expiry_sweep_at）

	// #2 群内公示去重：同一 canonical key 当天只公示一次「N 人在等的片出源了」，
	// 避免多个用户都许愿同一部片时、各自 FOUND 触发重复群公示。
	// 进程内即可（公示是尽力而为，重启后最多多发一次，不影响主路径）。
	announcedMu sync.Mutex
	announced   map[string]bool // key: canonicalKey|YYYY-MM-DD
}

// NewWishScheduler 创建许愿池调度器。
func NewWishScheduler(
	wish *WishService,
	moviepilot *MoviePilotClient,
	telegram *TelegramClient,
	groupChatID int64,
	intervalHours, expireDays, minSeeders, lockTTLMinutes int,
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
	if lockTTLMinutes <= 0 {
		lockTTLMinutes = 60
	}
	return &WishScheduler{
		wish:          wish,
		moviepilot:    moviepilot,
		telegram:      telegram,
		groupChatID:   groupChatID,
		intervalHours: intervalHours,
		expireDays:    expireDays,
		minSeeders:    minSeeders,
		lockTTLMin:    lockTTLMinutes,
		stopCh:        make(chan struct{}),
		announced:     make(map[string]bool),
	}
}

// SetRequestButtonBuilder 注入「立即求片」按钮构造（由 main.go 用 callback 包实现）。
func (s *WishScheduler) SetRequestButtonBuilder(f func(item *WishItem) (string, string)) {
	s.buildRequestButton = f
}

// Start 启动单个后台 task：每分钟 tick，错峰重搜 + 过期扫描（漏跑则启动时 catch-up 补跑）。
func (s *WishScheduler) Start() {
	// 启动 catch-up：若上次过期扫描距今已超周期（含停机错过的情况），立即补跑一次。
	s.maybeRunExpirySweep(time.Now())
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[WishScheduler] run panic: %v", r)
			}
		}()
		s.run()
	}()
	logger.Info("[wish] DailyRescan task 已启动（interval=%dh expire=%dd minSeeders=%d lockTTL=%dmin）",
		s.intervalHours, s.expireDays, s.minSeeders, s.lockTTLMin)
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

// tick 每分钟执行一次：①错峰重搜本分钟该搜的条目 ②距上次过期扫描 >=周期 就补跑（不再依赖整点）。
func (s *WishScheduler) tick(now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[wish] tick panic recovered: %v", r)
		}
	}()

	s.rescanThisMinute(now)

	// 过期扫描漏跑修复：以持久化 last_expiry_sweep_at 为唯一判据，
	// 「距上次扫描 >= 24h 就补跑」，定时器只负责驱动检查、不再以「03:00 那一分钟」为唯一触发点。
	s.maybeRunExpirySweep(now)
}

// expirySweepInterval 过期扫描周期（24h）。
const expirySweepInterval = 24 * time.Hour

// maybeRunExpirySweep 若距上次过期扫描已达周期（或从未跑过）则跑一次并持久化时间戳。
// 启动 catch-up 与每分钟 tick 共用此逻辑，保证停机错过的扫描会被补跑。
func (s *WishScheduler) maybeRunExpirySweep(now time.Time) {
	s.mu.Lock()
	last, ok, err := s.wish.GetLastExpirySweep()
	if err != nil {
		s.mu.Unlock()
		logger.Info("[wish] 读取上次过期扫描时间失败，跳过本次判定: %v", err)
		return
	}
	// 从未跑过 → 视为需要补跑；否则按周期判定。
	due := !ok || now.Sub(last) >= expirySweepInterval
	if !due {
		s.mu.Unlock()
		return
	}
	// 先持久化时间戳再释放锁，避免多个 tick 并发重复跑（同进程 mu 串行化即可）。
	if serr := s.wish.SetLastExpirySweep(now); serr != nil {
		s.mu.Unlock()
		logger.Info("[wish] 持久化过期扫描时间失败，跳过本次补跑: %v", serr)
		return
	}
	s.mu.Unlock()

	s.runExpirySweep()
}

// rescanThisMinute 认领「本分钟该搜」且自愈锁可用的条目并逐个重搜（坑2 错峰）。
// 错峰过滤已下沉到 ClaimSearchableItems 的 SQL（search_offset_minute=本分钟），
// 这里直接拿到的就是本分钟该搜的批次，无需再在内存过滤/解锁（消除 B1 的空写 churn 与饿死）。
func (s *WishScheduler) rescanThisMinute(now time.Time) {
	nowMinute := now.Hour()*60 + now.Minute()

	claimed, err := s.wish.ClaimSearchableItems(nowMinute, s.lockTTLMin, 200)
	if err != nil {
		logger.Info("[wish] 认领待搜条目失败: %v", err)
		return
	}
	for _, item := range claimed {
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
//
// 坑A 通知失败分流：
//   - 明确不可达（退群/封禁/用户不存在 → 403/Forbidden/blocked/kicked）→ ORPHANED（终态，不再试）。
//   - 网络抖动/超时/5xx 等临时错误 → 保持 FOUND，不标终态，下个调度窗口重试，绝不丢愿。
func (s *WishScheduler) notifyFound(item *WishItem) {
	reach := s.reachability(item.UserID)
	if reach == reachOrphaned {
		if err := s.wish.MarkOrphaned(item.ID); err != nil {
			logger.Info("[wish] id=%d 置 ORPHANED 失败: %v", item.ID, err)
		}
		return
	}
	if reach == reachTransientErr {
		// 可达性检查本身网络抖动：保守保留 FOUND，下次再试，不误判退群。
		logger.Info("[wish] id=%d 可达性检查临时失败，保留 FOUND 待下次重试", item.ID)
		return
	}

	// 坑C：发送前再查一次最新状态，若已被取消/改动（非 FOUND）则不发，避免对已变更条目误推。
	if latest, err := s.wish.GetByID(item.ID); err != nil {
		logger.Info("[wish] id=%d 发送前状态复查失败，保守跳过本次通知: %v", item.ID, err)
		return
	} else if latest == nil || latest.State != WishStateFound {
		logger.Info("[wish] id=%d 发送前状态已变更，跳过通知", item.ID)
		return
	}

	// Build Rich Message
	rmData := richmessage.WishFoundData{
		Title:       item.Title,
		Year:        item.Year,
		MediaType:   item.MediaType,
		Season:      item.Season,
		FoundDetail: item.FoundDetail,
	}
	richMsg := richmessage.BuildWishFoundCard(rmData)

	// Build plain text fallback
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
	plainText := b.String()

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

	// Try Rich Message, fall back to plain text
	if _, err := s.telegram.SendRichMessage(item.UserID, richMsg.Markdown, keyboard); err != nil {
		logger.Info("[wish] Rich Message failed for user %d: %v, falling back to plain text", item.UserID, err)
		if _, err := s.telegram.SendMessage(item.UserID, plainText, "", keyboard); err != nil {
			// 坑A：按错误类型分流，避免网络抖动误判为退群而永久丢愿。
			if isUnreachableErr(err) {
				logger.Info("[wish] id=%d 发送出源喜报被拒（用户不可达）→ ORPHANED: %v", item.ID, err)
				if merr := s.wish.MarkOrphaned(item.ID); merr != nil {
					logger.Info("[wish] id=%d 置 ORPHANED 失败: %v", item.ID, merr)
				}
			} else {
				// 网络/超时/5xx 等临时错误：保持 FOUND，下次重试，不标终态。
				logger.Info("[wish] id=%d 发送出源喜报临时失败，保留 FOUND 待重试: %v", item.ID, err)
			}
			return
		}
	}

	// 通知发出成功 → FOUND→NOTIFIED（写 notified_at 启动 TTL）。
	if _, err := s.wish.MarkNotified(item.ID); err != nil {
		logger.Info("[wish] id=%d MarkNotified 失败: %v", item.ID, err)
	}

	// 众筹：除发起人外，通知该 canonical 的其余所有 wisher（可达过滤，PM 尽力而为）。
	// 不影响状态机——状态机只认 wish_items 的发起人（item.UserID）；其余 wisher 仅是额外 PM。
	s.notifyOtherWishers(item)

	// #2 群内公示（主路径，尽力而为）：在群里发「《X》出源了，N 人在等 🎉」。
	// 即便上面的个人私信全靠 PM，群内公示也保证「等车的人」能在群里看到。
	// 同一 canonical key 当天只公示一次（announced 去重）。
	s.announceWishFoundToGroup(item)
}

// notifyOtherWishers 对该 canonical 的「除发起人外」的所有 wisher 做 PM 通知（坑5 可达过滤）。
// 退群/封禁用户跳过 PM（但仍计入总数，CountWishers 只升不降）；网络抖动保守保留（仍尝试发）。
// PM 失败仅记日志，不影响发起人主流程与群内公示。
func (s *WishScheduler) notifyOtherWishers(item *WishItem) {
	if item == nil || s.telegram == nil {
		return
	}
	ck := CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
	all := s.wish.ListWishers(ck)
	if len(all) == 0 {
		return
	}

	// 可达过滤（纯函数 + 注入可达判定，便于单测）：剔除发起人（已单独走带按钮的喜报），
	// 退群/封禁的不 PM，但人数不减。
	targets := filterWisherTargets(all, item.UserID, func(uid int64) bool {
		return s.reachability(uid) != reachOrphaned
	})

	msg := s.buildOtherWisherMessage(item)
	for _, uid := range targets {
		if _, err := s.telegram.SendMessage(uid, msg, "", nil); err != nil {
			// PM 发不出（未私聊过 / 封禁 / 抖动）仅记日志，不影响主流程与群内公示。
			logger.Info("[wish] id=%d 通知其他 wisher user=%d 失败（不影响主流程）: %v", item.ID, uid, err)
		}
	}
	if len(targets) > 0 {
		logger.Info("[wish] id=%d 已额外通知 %d 位等待者（共 %d 人在等）", item.ID, len(targets), len(all))
	}
}

// buildOtherWisherMessage 给「非发起人 wisher」的出源 PM 文案（不带「立即求片」按钮——
// 该按钮绑定 wish_items 且只允许发起人点；其余 wisher 自行 /search 或等发起人入库）。
func (s *WishScheduler) buildOtherWisherMessage(item *WishItem) string {
	var b strings.Builder
	b.WriteString("🎉 你也在等的影片出源啦！\n\n")
	b.WriteString(fmt.Sprintf("🎬 %s", strings.TrimSpace(item.Title)))
	if item.Year > 0 {
		b.WriteString(fmt.Sprintf(" (%d)", item.Year))
	}
	if item.MediaType == "tv" && item.Season > 0 {
		b.WriteString(fmt.Sprintf(" 第%d季", item.Season))
	}
	b.WriteString("\n\n已有人发起求片，入库后即可观看～如想自己求片可用 /search 重新搜索。")
	return b.String()
}

// filterWisherTargets 是「可达过滤」纯函数（便于单测，不依赖网络）：
//   - 剔除 originator（发起人单独走带按钮的喜报，避免重复 PM）。
//   - reachable(uid)==false（退群/封禁）的跳过 @/PM；但**不从计数减**（计数在 CountWishers 处独立统计）。
//   - 保持原顺序、去重（同一 uid 只出现一次）。
func filterWisherTargets(all []int64, originator int64, reachable func(int64) bool) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, uid := range all {
		if uid == 0 || uid == originator || seen[uid] {
			continue
		}
		seen[uid] = true
		if reachable != nil && !reachable(uid) {
			continue
		}
		out = append(out, uid)
	}
	return out
}

// announceWishFoundToGroup 在群里公示「某许愿片出源了，N 人在等」（#2 三层之主通知）。
// 仅在配置了群 chatID（且为群组）时发送；同一 canonical key 当天去重，发送失败仅记日志。
func (s *WishScheduler) announceWishFoundToGroup(item *WishItem) {
	if item == nil || s.telegram == nil {
		return
	}
	// 只在群组里公示（chatID < -100 表示超级群组）。
	if s.groupChatID == 0 || s.groupChatID >= -100 {
		return
	}

	// 当天去重 key。
	dayKey := s.wishAnnounceKey(item, time.Now())
	s.announcedMu.Lock()
	if s.announced[dayKey] {
		s.announcedMu.Unlock()
		return
	}
	s.announced[dayKey] = true
	s.announcedMu.Unlock()

	// 统计「在等」总人数（CountWishers = wish_wishers 该 canonical 的总数，只升不降，退群也算）。
	// 众筹计数后，同一部片即便 wish_items 仍只去重一条，wish_wishers 已累计所有许愿者，
	// 故这里如实展示 N 人在等。N<=1 时不显示人数（避免「1 人在等」的尴尬，退化为「到货了」）。
	ck := CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season)
	n := s.wish.CountWishers(ck)
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "有人许愿的片"
	}
	var b strings.Builder
	if n > 1 {
		b.WriteString(fmt.Sprintf("🎉 《%s》发现可用资源，已有 %d 人关注！", title, n))
	} else {
		b.WriteString(fmt.Sprintf("🎉 《%s》发现可用资源！", title))
	}
	if item.MediaType == "tv" && item.Season > 0 {
		b.WriteString(fmt.Sprintf("（第%d季）", item.Season))
	}
	b.WriteString("\n可通过搜索发起求片，最终结果以审核和入库通知为准。")

	if _, err := s.telegram.SendMessage(s.groupChatID, b.String(), "", nil); err != nil {
		// 公示失败：撤销去重标记，下次同片 FOUND 可再试；不影响个人 PM 主流程。
		s.announcedMu.Lock()
		delete(s.announced, dayKey)
		s.announcedMu.Unlock()
		logger.Info("[wish] 群内公示发送失败 id=%d: %v", item.ID, err)
	}
}

// wishAnnounceKey 生成「canonical + 日期」去重 key（同片当天只公示一次）。
// 复用 CanonicalKey 保证与去重/计数维度严格对齐。
func (s *WishScheduler) wishAnnounceKey(item *WishItem, now time.Time) string {
	day := now.Format("2006-01-02")
	return CanonicalKey(item.TmdbID, item.ImdbID, item.MediaType, item.Season) + "|" + day
}

// notifyExpired 私信发起人「等了 N 天没找到已自动取消」（坑4）。
func (s *WishScheduler) notifyExpired(item *WishItem) {
	if s.reachability(item.UserID) == reachOrphaned {
		return // 明确不可达就不打扰，状态已是 EXPIRED。
	}
	msg := fmt.Sprintf("😔 你许愿的《%s》等了 %d 天还没找到源，已自动从许愿池移除。\n如仍想看，可重新 /wish 加入。",
		item.Title, s.expireDays)
	if _, err := s.telegram.SendMessage(item.UserID, msg, "", nil); err != nil {
		logger.Info("[wish] id=%d 发送过期通知失败: %v", item.ID, err)
	}
}

// reachState 表示可达性三态判定（坑A：区分明确退群 vs 网络抖动）。
type reachState int

const (
	reachOK           reachState = iota // 明确可达
	reachOrphaned                       // 明确不可达（退群/封禁）→ 应置 ORPHANED
	reachTransientErr                   // 临时错误（网络/超时）→ 不可判定，保守保留待重试
)

// reachability 用 getChatMember 检查用户可达性（坑5 + 坑A）。
// 未配置群 chatID 时无法检查群成员，退化为「假定可达」（发送时再按错误分流）。
func (s *WishScheduler) reachability(userID int64) reachState {
	if s.groupChatID == 0 {
		return reachOK
	}
	status, err := s.telegram.GetChatMemberStatus(s.groupChatID, userID)
	if err != nil {
		// 坑A：区分「明确不可达」与「网络抖动」。
		if isUnreachableErr(err) {
			logger.Info("[wish] getChatMember 明确不可达 user=%d: %v", userID, err)
			return reachOrphaned
		}
		logger.Info("[wish] getChatMember 临时失败 user=%d: %v（保守保留待重试）", userID, err)
		return reachTransientErr
	}
	switch status {
	case "left", "kicked":
		return reachOrphaned
	default:
		return reachOK
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

// isUnreachableErr 判断发送/可达性检查的错误是否表示「用户明确不可达」（坑A）。
// 命中条件（→ ORPHANED 终态）：
//   - Telegram API 403 Forbidden（bot 被用户封禁 / 未启动会话）。
//   - 400 且描述含 user not found / chat not found / user is deactivated 等（用户不存在/注销）。
//   - 描述含 blocked / kicked / deactivated 等关键词。
//
// 其余（超时、连接失败、5xx、429 限流等）一律视为临时错误 → 保留待重试，绝不丢愿。
func isUnreachableErr(err error) bool {
	if err == nil {
		return false
	}
	// 优先看结构化的 Telegram 错误码。
	if te, ok := err.(*types.TelegramError); ok {
		if te.Code == 403 {
			return true
		}
		// 400 需结合描述：部分 400 是「用户不存在/注销」（不可达），部分是参数问题（不强判）。
		msg := strings.ToLower(te.Message)
		if te.Code == 400 && (strings.Contains(msg, "user not found") ||
			strings.Contains(msg, "chat not found") ||
			strings.Contains(msg, "user is deactivated") ||
			strings.Contains(msg, "peer_id_invalid")) {
			return true
		}
		if strings.Contains(msg, "blocked") ||
			strings.Contains(msg, "kicked") ||
			strings.Contains(msg, "deactivated") {
			return true
		}
		return false
	}
	// 兜底：从错误文本里识别明确不可达关键词。
	msg := strings.ToLower(err.Error())
	for _, k := range []string{"forbidden", "blocked by the user", "user is blocked",
		"bot was blocked", "user not found", "chat not found", "user is deactivated"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
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
