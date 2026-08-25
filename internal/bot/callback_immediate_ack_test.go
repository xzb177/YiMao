package bot

import (
	"testing"

	"github.com/xzb177/yimao/internal/callback"
)

// Approval and wash-completion callbacks run authoritative preflight work
// (Emby availability, MoviePilot duplicate lookup, subscription creation) that
// can outlast Telegram's callback window, so they must be acknowledged before
// the handler runs. Cheap navigation callbacks keep the default behaviour of
// answering with the handler's own toast text.
func TestCallbackNeedsImmediateAck(t *testing.T) {
	for _, action := range []callback.Action{"review_approve", "rv_a", "review_complete_wash", "review_retry_wash"} {
		if !callbackNeedsImmediateAck(action) {
			t.Fatalf("action %q must be acknowledged before its handler runs", action)
		}
	}
	for _, action := range []callback.Action{"review_reject", "rv_r", "my_requests", "review_list", "search", ""} {
		if callbackNeedsImmediateAck(action) {
			t.Fatalf("action %q must keep the deferred answer path", action)
		}
	}
}
