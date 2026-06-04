package services

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现「拼车 +1」入库 @ 通知（Batch A #3 的入库侧）。
//
// 入库通知发送后，如果该片有拼车列表，则在群里 @ 这些用户，然后清空该片拼车记录。

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
// 超出部分用「等 N 人」概述，既不丢人也不撑爆消息。
const carpoolMaxMentions = 20

// notifyCarpoolMembers 在入库后 @ 拼车用户并清空记录。
// tmdbIDStr: TMDB ID（字符串，来自 EmbyEnhancedInfo.TMDBID）
// mediaType: "movie" 或 "tv"
//
// B4 修复要点：
//   - 长度保护：最多 @ carpoolMaxMentions 人，其余用「等 N 人」概述，避免接近 4096 上限被截断。
//   - 真实昵称：能从 userMapping 取到名字就用真实昵称，取不到才退回「拼车用户」（链接仍可点跳转）。
//   - 失败不丢人：先取列表、发送成功后才清空（GetThenClear 语义），发送失败保留记录、不整条重发，
//     下次入库（或人工）仍可重试，避免「清了又发失败 → 永久丢拼车」。
func (s *WebhookService) notifyCarpoolMembers(tmdbIDStr string, mediaType string) {
	if s.carpool == nil {
		return
	}
	if tmdbIDStr == "" {
		return
	}
	// 只在群组里 @（chatID < -100 表示超级群组）
	if s.chatID == 0 || s.chatID >= -100 {
		return
	}

	tmdbID := 0
	fmt.Sscanf(tmdbIDStr, "%d", &tmdbID)
	if tmdbID == 0 {
		return
	}

	// 先只读取（不清空），发送成功后再清空，避免失败丢人。
	userIDs := s.carpool.Get(tmdbID, mediaType)
	if len(userIDs) == 0 {
		return
	}

	// 构建 @ 文案：用 tg://user?id=<id> 的 Markdown 链接；可点击文本优先用真实昵称。
	total := len(userIDs)
	shown := userIDs
	overflow := 0
	if total > carpoolMaxMentions {
		shown = userIDs[:carpoolMaxMentions]
		overflow = total - carpoolMaxMentions
	}

	var mentions []string
	for _, uid := range shown {
		name := s.carpoolMentionName(uid)
		mentions = append(mentions, fmt.Sprintf("[%s](tg://user?id=%d)", name, uid))
	}

	message := fmt.Sprintf("🚗 你拼车想看的片到货啦！\n%s", strings.Join(mentions, " "))
	if overflow > 0 {
		message += fmt.Sprintf(" 等 %d 人", overflow)
	}

	// 使用 Markdown 解析以便 tg://user 链接生效。
	if _, err := s.telegram.SendMessage(s.chatID, message, "Markdown", nil); err != nil {
		// 失败：保留拼车记录、不清空、不整条重发，避免永久丢人；下次入库可重试。
		logger.Info("[Carpool] 发送入库 @ 通知失败（保留拼车记录待重试）tmdbID=%d type=%s: %v", tmdbID, mediaType, err)
		return
	}

	// 发送成功后才清空该片拼车记录。
	s.carpool.GetAndClear(tmdbID, mediaType)
	logger.Info("[Carpool] 已 @ %d 位拼车用户 tmdbID=%d type=%s", total, tmdbID, mediaType)
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
