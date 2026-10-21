package miniapp

import (
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

// 审计 P1-3（用户侧文案）：终态失败的求片在小程序里必须如实显示
// 「提交失败」并归入已完结分组，不再显示「同步重试/处理中」。
func TestUserRequestStatus_TerminalFailed(t *testing.T) {
	status, text, group := userRequestStatus(&services.ReviewRequest{Status: "failed"})
	if status != "failed" || text != "提交失败" || group != "done" {
		t.Fatalf("got (%q, %q, %q), want (failed, 提交失败, done)", status, text, group)
	}

	status, text, group = publicReviewStatus(&services.ReviewRequest{Status: "failed"})
	if status != "failed" || text != "提交失败" || group != "done" {
		t.Fatalf("public: got (%q, %q, %q), want (failed, 提交失败, done)", status, text, group)
	}
}

// 洗版 failed 继续走既有 wash_failed 口径，不被求片终态映射影响。
func TestPublicReviewStatus_WashFailedUnchanged(t *testing.T) {
	status, text, _ := publicReviewStatus(&services.ReviewRequest{
		Status:       services.WashStatusFailed,
		BusinessType: services.BusinessTypeWash,
	})
	if status != "wash_failed" || text != "洗版待人工处理" {
		t.Fatalf("got (%q, %q), want (wash_failed, 洗版待人工处理)", status, text)
	}
}
