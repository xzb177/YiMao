package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现「拼车 +1」入库出源通知（Batch A #3 入库侧 + #2 三层通知改造）。
//
// #2 三层通知模型（群内公示为主）：
//   1) 主通知 = 群内公示：在群里发「《X》到货了，N 人等到了 🎉」——这条 100% 可见，是主路径，
//      即便所有 @ / PM 都失败，等车的人也能在群里看到。
//   2) @ 尽力而为：对「可达」的 +1 用户用 tg://user 链接 @，不作为送达保证；
//      明确退群/封禁（getChatMember=left/kicked）的用户不进 @ 列表，只计入「还有 N 人」。
//   3) PM 兜底：对已建私聊的用户额外私信（带去 Emby 观看的链接，如已配置）；
//      私信发不出（403/blocked）不影响群内公示与 @。
//
// 长度保护：沿用 carpoolMaxMentions，@ 列表最多展示 N 人，其余并入「还有 M 人」，避免撑爆 4096。

// extractTMDBID 尽力从 enhancedInfo 或 webhook payload 的 ProviderIds 中取 TMDB ID。
// 取不到返回空串（notifyCarpoolMembers 会据此跳过）。
func (s *WebhookService) extractTMDBID(enhanced *EmbyEnhancedInfo, payload EmbyWebhookPayload) string {
	if enhanced != nil && enhanced.TMDBID != "" {
		return enhanced.TMDBID
	}
	if payload.Item != nil && payload.Item.ProviderIds != nil {
		if tid, ok := payload.Item.ProviderIds["tmdb"]; ok && tid != "" {
			return tid
		}
		if tid, ok := payload.Item.ProviderIds["Tmdb"]; ok && tid != "" {
			return tid
		}
	}
	return ""
}

// carpoolMaxMentions 单条 @ 通知里最多展示的用户数（防止接近 4096 字符上限被 Telegram 截断/拒绝）。
// 超出部分用「还有 N 人」概述，既不丢人也不撑爆消息。
const carpoolMaxMentions = 20

// notifyCarpoolMembers 在入库后发出源通知（#2 三层）并清空记录。
// tmdbIDStr: TMDB ID（字符串，来自 EmbyEnhancedInfo.TMDBID）
// mediaType: "movie" 或 "tv"
// title:     片名（用于群内公示文案；为空时退化为「你拼车想看的片」）。
//
// 失败不丢人：先取列表、群内公示发送成功后才清空（GetThenClear 语义），公示失败保留记录、不整条重发，
// 下次入库（或人工）仍可重试，避免「清了又发失败 → 永久丢拼车」。
func (s *WebhookService) notifyCarpoolMembers(tmdbIDStr string, mediaType string, title string) {
	if s.carpool == nil {
		return
	}
	if tmdbIDStr == "" {
		return
	}
	// 只在群组里公示 @（chatID < -100 表示超级群组）
	if s.chatID == 0 || s.chatID >= -100 {
		return
	}

	tmdbID := 0
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
	if tmdbID == 0 {
		return
	}

	// 先只读取（不清空），群内公示成功后再清空，避免失败丢人。
	userIDs := s.carpool.Get(tmdbID, mediaType)
	if len(userIDs) == 0 {
		return
	}

	titleText := strings.TrimSpace(title)
	if titleText == "" {
		titleText = "你拼车想看的片"
	}

	// 第 2 层：按可达性筛 @ 列表。明确退群/封禁的用户不进 @ 列表（只计入「还有 N 人」）。
	// getChatMember 临时失败（网络抖动）按「可达」处理，避免误把人踢出 @ 列表。
	var reachable []int64
	unreachable := 0
	for _, uid := range userIDs {
		if s.carpoolUserReachableInGroup(uid) {
			reachable = append(reachable, uid)
		} else {
			unreachable++
		}
	}

	total := len(userIDs)

	// 构建 @ 文案：仅 @ 可达用户，超过上限部分并入「还有 N 人」。
	shown := reachable
	overflow := 0
	if len(shown) > carpoolMaxMentions {
		overflow = len(shown) - carpoolMaxMentions
		shown = shown[:carpoolMaxMentions]
	}
	// 不可达用户也并入「还有 N 人」概述（人数照计，名字不显示）。
	overflow += unreachable

	var mentions []string
	for _, uid := range shown {
		name := s.carpoolMentionName(uid)
		mentions = append(mentions, fmt.Sprintf("[%s](tg://user?id=%d)", name, uid))
	}

	// 第 1 层（主路径）：群内公示。即便 mentions 为空（全员不可达）也照发，保证 100% 可见。
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🎉 《%s》到货了，%d 人等到了！\n", escapeCarpoolMentionText(titleText), total))
	if len(mentions) > 0 {
		b.WriteString(strings.Join(mentions, " "))
	}
	if overflow > 0 {
		if len(mentions) > 0 {
			b.WriteString(" ")
		}
		b.WriteString(fmt.Sprintf("还有 %d 人", overflow))
	}

	// 使用 Markdown 解析以便 tg://user 链接生效。
	if _, err := s.telegram.SendMessage(s.chatID, b.String(), "Markdown", nil); err != nil {
		// 群内公示失败：保留拼车记录、不清空、不整条重发，避免永久丢人；下次入库可重试。
		logger.Info("[Carpool] 群内公示发送失败（保留拼车记录待重试）tmdbID=%d type=%s: %v", tmdbID, mediaType, err)
		return
	}

	// 第 3 层（兜底）：对可达用户额外私信带 direct link。发不出仅记日志，不影响群内公示与清空。
	s.sendCarpoolDirectPMs(reachable, titleText)

	// 群内公示成功后才清空该片拼车记录。
	s.carpool.GetAndClear(tmdbID, mediaType)
	logger.Info("[Carpool] 已群内公示 %d 位拼车用户（可达 %d / 不可达 %d）tmdbID=%d type=%s",
		total, len(reachable), unreachable, tmdbID, mediaType)
}

// carpoolUserReachableInGroup 判断拼车用户在群里是否「明确可达」（#2 第 2 层）。
//   - 未配置群 chatID：无法判定，按可达处理（让群内公示照发）。
//   - getChatMember 明确返回 left/kicked，或错误判定为「明确不可达」→ 不可达。
//   - 网络抖动等临时错误 → 保守按可达处理（避免误剔除）。
func (s *WebhookService) carpoolUserReachableInGroup(userID int64) bool {
	if s.chatID == 0 {
		return true
	}
	status, err := s.telegram.GetChatMemberStatus(s.chatID, userID)
	if err != nil {
		if isUnreachableErr(err) {
			return false
		}
		// 临时错误：保守按可达（不误剔除）。
		return true
	}
	switch status {
	case "left", "kicked":
		return false
	default:
		return true
	}
}

// sendCarpoolDirectPMs 第 3 层兜底：对可达用户逐个尝试私信，带去 Emby 观看的链接（如配置）。
// 发不出（未私聊过 / 封禁）仅记日志，绝不影响群内公示。
func (s *WebhookService) sendCarpoolDirectPMs(userIDs []int64, title string) {
	if len(userIDs) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎬 你拼车想看的《%s》到货啦，去看看吧～", title))
	// direct link：当前仅在已配置 Emby 地址时附上站点入口
	if link := s.carpoolDirectLink(); link != "" {
		sb.WriteString("\n👉 " + link)
	}
	msg := sb.String()
	for _, uid := range userIDs {
		if _, err := s.telegram.SendMessage(uid, msg, "", nil); err != nil {
			// 私信发不出：仅记日志，不影响群内公示（第 1 层已保证可见）。
			logger.Info("[Carpool] PM 兜底发送失败（不影响群内公示）user=%d: %v", uid, err)
		}
	}
}

// carpoolDirectLink 返回兜底私信里的「去观看」链接。
// 保守处理：仅返回已配置的 Emby 站点入口（去掉末尾斜杠）；未配置则返回空串。
func (s *WebhookService) carpoolDirectLink() string {
	url := strings.TrimRight(strings.TrimSpace(s.embyURL), "/")
	return url
}

// carpoolMentionName 返回用于 @ 链接的可点击文本：优先真实昵称，取不到退回「拼车用户」。
// 对 Markdown 链接文本里的特殊字符做转义，避免 [] () 破坏链接解析。
func (s *WebhookService) carpoolMentionName(userID int64) string {
	name := ""
	if s.userMapping != nil {
		name = strings.TrimSpace(s.userMapping.GetTelegramUsername(userID))
	}
	if name == "" {
		return "拼车用户"
	}
	return escapeCarpoolMentionText(name)
}

// escapeCarpoolMentionText 转义 Markdown(v1) 链接文本里会破坏 [..](..) 结构的字符。
func escapeCarpoolMentionText(s string) string {
	r := strings.NewReplacer(
		"[", "（",
		"]", "）",
		"(", "（",
		")", "）",
		"`", "'",
		"*", "",
		"_", " ",
	)
	return r.Replace(s)
}

func reviewMatchesLibraryAdd(rv *ReviewRequest, tmdbID int, mediaType string, season int) bool {
	if rv == nil || rv.Status != "approved" || rv.TmdbID != tmdbID {
		return false
	}
	rvType := string(rv.MediaType)
	if rvType == "" {
		rvType = "movie"
	}
	if rvType != mediaType {
		return false
	}
	if mediaType == "tv" {
		// Unknown webhook seasons may only complete legacy, non-season-scoped
		// reviews. A known season must match exactly.
		return rv.Season == season
	}
	return true
}

func (s *WebhookService) completeWashOnLibraryAdd(tmdbIDStr, mediaType string, season int, itemID string) {
	if s.review == nil || strings.TrimSpace(tmdbIDStr) == "" || strings.TrimSpace(itemID) == "" {
		return
	}
	tmdbID := 0
	if _, err := fmt.Sscanf(tmdbIDStr, "%d", &tmdbID); err != nil || tmdbID == 0 {
		logger.Info("[洗版自动完成] TMDB ID 解析失败: %s", tmdbIDStr)
		return
	}

	var matched []*ReviewRequest
	for _, review := range s.review.GetApprovedWashRequests() {
		if !reviewMatchesLibraryAdd(review, tmdbID, mediaType, season) {
			continue
		}
		// A work order that already exhausted automatic verification must not
		// trigger another Emby lookup on every library event.
		if s.review.WashVerificationExhausted(review.RequestID) {
			logger.Info("[洗版自动完成] 已达自动核验上限，交由人工处理 request=%s title=%s", review.RequestID, review.MediaTitle)
			continue
		}
		matched = append(matched, review)
	}
	if len(matched) == 0 {
		return
	}

	var currentSources []string
	var err error
	if mediaType == "tv" {
		currentSources, err = s.CaptureEmbyWashBaseline(tmdbID, MediaTypeTV, season)
	} else {
		currentSources, err = s.fetchEmbyMediaSourcePaths(itemID)
	}
	if err != nil {
		logger.Info("[洗版自动完成] Emby MediaSource 核验失败 item=%s: %v", itemID, err)
		return
	}
	for _, review := range matched {
		completed, err := s.review.CompleteWashAutomatically(review.RequestID, currentSources)
		if err != nil {
			logger.Info("[洗版自动完成] 暂不完成 request=%s: %v", review.RequestID, err)
			continue
		}
		logger.Info("[洗版自动完成] 已完成 request=%s title=%s sources=%d", completed.RequestID, completed.MediaTitle, len(currentSources))
	}
}

// notifyRequesterOnLibraryAdd 入库后私聊通知求片用户。
// 查找匹配 TMDB ID + mediaType + season 的审核请求，向求片人发送「已入库」私信。
func (s *WebhookService) notifyRequesterOnLibraryAdd(tmdbIDStr string, mediaType string, title string, season int) {
	if s.review == nil {
		logger.Info("[入库通知] review service 未注入，跳过")
		return
	}
	if tmdbIDStr == "" {
		logger.Info("[入库通知] TMDB ID 为空，跳过")
		return
	}

	tmdbID := 0
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
	if tmdbID == 0 {
		logger.Info("[入库通知] TMDB ID 解析失败: %s", tmdbIDStr)
		return
	}
	logger.Info("[入库通知] 查找匹配: tmdbID=%d, type=%s, title=%s", tmdbID, mediaType, title)

	// 查找所有匹配的已批准审核请求
	s.review.mu.RLock()
	var matched []*ReviewRequest
	for _, rv := range s.review.reviews {
		if !reviewMatchesLibraryAdd(rv, tmdbID, mediaType, season) {
			continue
		}
		matched = append(matched, cloneReview(rv))
	}
	s.review.mu.RUnlock()

	if len(matched) == 0 {
		logger.Info("[入库通知] 未找到匹配的已批准请求: tmdbID=%d, type=%s", tmdbID, mediaType)
		return
	}

	titleText := strings.TrimSpace(title)
	if titleText == "" {
		titleText = "你求的片"
	}

	// 去重：同一用户只通知一次
	notified := make(map[int64]bool)
	for _, rv := range matched {
		if notified[rv.TelegramID] {
			continue
		}
		if !s.review.IsLibraryNotificationPending(rv.RequestID) {
			continue
		}

		mediaEmoji := "🎬"
		if mediaType == "tv" {
			mediaEmoji = "📺"
		}
		msg := fmt.Sprintf("🎉 你求的%s《%s》已入库！\n\n快去 Emby 看吧～", mediaEmoji, titleText)
		if _, err := s.telegram.SendMessage(rv.TelegramID, msg, "", nil); err != nil {
			logger.Info("[入库通知] 私聊求片用户失败 user=%d: %v", rv.TelegramID, err)
		} else {
			record := CompletionRecord{RequestID: rv.RequestID, TelegramID: rv.TelegramID, Title: rv.MediaTitle, Year: rv.MediaYear, MediaType: string(rv.MediaType), Season: rv.Season, CompletedAt: time.Now()}
			if err := s.review.RecordLibraryCompletion(record, s.fulfillmentStats); err != nil {
				logger.Info("[入库通知] 保存完成状态失败 request=%s: %v", rv.RequestID, err)
				continue
			}
			notified[rv.TelegramID] = true
			logger.Info("[入库通知] 已通知求片用户 %d: %s", rv.TelegramID, titleText)
		}
	}
}
