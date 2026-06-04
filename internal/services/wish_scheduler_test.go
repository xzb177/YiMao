package services

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

// TestIsUnreachableErr 覆盖坑A 通知失败分流的纯函数逻辑：
//   - 403 / blocked / user not found → 明确不可达（ORPHANED）。
//   - 超时 / 5xx / 429 / 连接失败 → 临时错误（保留可重试）。
func TestIsUnreachableErr(t *testing.T) {
	unreachable := []error{
		&types.TelegramError{Code: 403, Message: "Forbidden: bot was blocked by the user"},
		&types.TelegramError{Code: 403, Message: "Forbidden: user is deactivated"},
		&types.TelegramError{Code: 400, Message: "Bad Request: chat not found"},
		&types.TelegramError{Code: 400, Message: "Bad Request: user not found"},
		errors.New("Forbidden: bot was blocked by the user"),
		errors.New("telegram: user is deactivated"),
	}
	for _, e := range unreachable {
		if !isUnreachableErr(e) {
			t.Errorf("expected unreachable for %v", e)
		}
	}

	transient := []error{
		nil,
		&types.TelegramError{Code: 500, Message: "Internal Server Error"},
		&types.TelegramError{Code: 502, Message: "Bad Gateway"},
		&types.TelegramError{Code: 429, Message: "Too Many Requests: retry after 5"},
		&types.TelegramError{Code: 400, Message: "Bad Request: message text is empty"},
		errors.New("request failed: dial tcp: i/o timeout"),
		fmt.Errorf("context deadline exceeded"),
		errors.New("connection reset by peer"),
	}
	for _, e := range transient {
		if isUnreachableErr(e) {
			t.Errorf("expected transient (retryable) for %v", e)
		}
	}
}
