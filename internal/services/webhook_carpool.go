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

// notifyCarpoolMembers 在入库后 @ 拼车用户并清空记录。
// tmdbIDStr: TMDB ID（字符串，来自 EmbyEnhancedInfo.TMDBID）
// mediaType: "movie" 或 "tv"
//
// @ 写法：项目现有通知里没有现成的用户 @ 写法（入库通知都是纯文本/图片，不 @ 人），
// 因此按需求采用 Markdown 链接 [名字](tg://user?id=<id>) 形式 @ 用户。
// 为避免名字解析失败，这里统一用「用户」作为可点击文本（点击即可跳转到对应用户）。
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

	userIDs := s.carpool.GetAndClear(tmdbID, mediaType)
	if len(userIDs) == 0 {
		return
	}

	// 构建 @ 文案：用 tg://user?id=<id> 的 Markdown 链接。
	var mentions []string
	for _, uid := range userIDs {
		mentions = append(mentions, fmt.Sprintf("[拼车用户](tg://user?id=%d)", uid))
	}

	message := fmt.Sprintf("🚗 你拼车想看的片到货啦！\n%s", strings.Join(mentions, " "))

	// 使用 Markdown 解析以便 tg://user 链接生效。
	if _, err := s.telegram.SendMessage(s.chatID, message, "Markdown", nil); err != nil {
		logger.Info("[Carpool] 发送入库 @ 通知失败 tmdbID=%d type=%s: %v", tmdbID, mediaType, err)
	} else {
		logger.Info("[Carpool] 已 @ %d 位拼车用户 tmdbID=%d type=%s", len(userIDs), tmdbID, mediaType)
	}
}
