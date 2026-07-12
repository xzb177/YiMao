package services

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// AlertService 系统告警服务。
// 关键错误（MP API 连续失败、stuck 未处理、DB 写入失败）时发 Telegram 私信给管理员。
// 内置去重：同一告警 key 在 cooldown 内只发一次，避免管理员被刷屏。
type AlertService struct {
	telegram *TelegramClient
	adminID  int64
	mu       sync.Mutex
	// lastSent 记录每个告警 key 上次发送时间，用于去重
	lastSent map[string]time.Time
	cooldown time.Duration
}

// NewAlertService 创建告警服务。
// adminID: 管理员 Telegram 用户 ID。
// cooldown: 同一告警的最小发送间隔（默认 30 分钟）。
func NewAlertService(telegram *TelegramClient, adminID int64, cooldown time.Duration) *AlertService {
	if cooldown <= 0 {
		cooldown = 30 * time.Minute
	}
	return &AlertService{
		telegram: telegram,
		adminID:  adminID,
		lastSent: make(map[string]time.Time),
		cooldown: cooldown,
	}
}

// Warn 发送告警。同一 key 在 cooldown 内只发一次。
func (s *AlertService) Warn(key, title, detail string) {
	if s == nil || s.telegram == nil || s.adminID == 0 {
		return
	}

	s.mu.Lock()
	if last, ok := s.lastSent[key]; ok && time.Since(last) < s.cooldown {
		s.mu.Unlock()
		return
	}
	s.lastSent[key] = time.Now()
	s.mu.Unlock()

	// Telegram 只接收稳定、无技术细节的提示；原始 detail 仅写入脱敏日志。
	text := fmt.Sprintf("⚠️ 系统告警\n\n📌 %s\n\n系统检测到异常，请查看服务日志。", title)
	if _, err := s.telegram.SendMessage(s.adminID, text, "", nil); err != nil {
		logger.Info("[Alert] 发送告警失败: key=%s, err=%v, detail=%s", key, err, sanitizeAlertDetail(detail))
	} else {
		logger.Info("[Alert] 已通知管理员: key=%s, detail=%s", key, sanitizeAlertDetail(detail))
	}
}

func sanitizeAlertDetail(detail string) string {
	detail = strings.ReplaceAll(detail, "\n", " ")
	if len(detail) > 500 {
		detail = detail[:500] + "…"
	}
	// 遮盖 URL query，避免 token/api_key 等凭据落日志。
	re := regexp.MustCompile(`([?&][^=\s]+)=([^&\s]+)`)
	return re.ReplaceAllString(detail, "$1=<redacted>")
}

// Warnf 格式化发送告警（兼容旧调用方式）。
func (s *AlertService) Warnf(key, title, detailFormat string, args ...interface{}) {
	s.Warn(key, title, fmt.Sprintf(detailFormat, args...))
}

// Clear 清除某个告警的冷却状态（例如问题修复后可立即恢复告警能力）。
func (s *AlertService) Clear(key string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.lastSent, key)
	s.mu.Unlock()
}
