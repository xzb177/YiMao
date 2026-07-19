package handlers

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

type fakeRequestSubmitter struct {
	calls  []services.RequestSubmission
	result services.SubmissionResult
	err    error
}

func (f *fakeRequestSubmitter) SubmitResult(in services.RequestSubmission) (services.SubmissionResult, error) {
	f.calls = append(f.calls, in)
	return f.result, f.err
}

func TestRequestHandlerSubmissionResultMapping(t *testing.T) {
	h := &RequestHandler{}
	review := &services.ReviewRequest{MediaTitle: "测试影片", Status: "approved"}
	tests := []struct {
		name         string
		result       services.SubmissionResult
		wantCallback string
		wantText     string
	}{
		{"duplicate own", services.SubmissionResult{Status: services.SubmissionDuplicateOwn, Review: review}, "请勿重复提交", "已通过审核"},
		{"duplicate other", services.SubmissionResult{Status: services.SubmissionDuplicateOther, Review: review}, "已加入拼车", "不重复扣配额"},
		{"not bound", services.SubmissionResult{Status: services.SubmissionNotBound}, "需要绑定账号", "请先绑定账号"},
		{"quota", services.SubmissionResult{Status: services.SubmissionQuotaExceeded}, "今日求片次数已用完", "求片次数用完"},
		{"unknown", services.SubmissionResult{}, "操作失败", "求片没有提交成功"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.mapSubmissionResult(tt.result, 42, 123, "movie", false)
			if got.CallbackMsg != tt.wantCallback || !strings.Contains(got.Text, tt.wantText) || !got.ShowAlert {
				t.Fatalf("response=%+v", got)
			}
		})
	}
}

func TestRequestSubmitterCapturesTVSeasonAndQuotaIntent(t *testing.T) {
	fake := &fakeRequestSubmitter{result: services.SubmissionResult{Status: services.SubmissionCreated, Review: &services.ReviewRequest{MediaTitle: "剧集", MediaType: services.MediaTypeTV, Season: 2, QuotaCost: 3}}}
	var submitter requestSubmitter = fake
	in := services.RequestSubmission{TmdbID: 123, MediaType: services.MediaTypeTV, Season: 2, UseQuota: true}
	result, err := submitter.SubmitResult(in)
	if err != nil || len(fake.calls) != 1 || fake.calls[0].Season != 2 || !fake.calls[0].UseQuota {
		t.Fatalf("calls=%+v result=%+v err=%v", fake.calls, result, err)
	}
	if result.Review.QuotaCost != 3 {
		t.Fatalf("TV quota cost=%d", result.Review.QuotaCost)
	}
}

func TestRequestHandlersUseUnifiedSubmissionService(t *testing.T) {
	data, err := os.ReadFile("request.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if got := strings.Count(source, "h.submissionService.SubmitResult("); got != 2 {
		t.Fatalf("SubmitResult call count=%d, want 2", got)
	}
	for _, forbidden := range []string{"h.reviewService.HasActiveSimilarRequest", "h.reviewService.FindActiveSimilarRequest", "h.quotaService.UseQuota", "h.quotaService.RestoreQuota", "h.reviewService.CreateRequest", "go h.notifyAdminsForReview(review)"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("legacy submission path remains: %s", forbidden)
		}
	}
}

func TestSubmissionServiceMissingFailsClosed(t *testing.T) {
	h := &RequestHandler{}
	if h.submissionService != nil {
		t.Fatal("unexpected submitter")
	}
	got := serviceConfigurationError()
	if got.CallbackMsg != "暂时不可用" || !got.ShowAlert {
		t.Fatalf("response=%+v", got)
	}
}

func TestFullShowRequestRequiresConfirmationBeforeSubmission(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	sess := manager.GetOrCreate(42)
	sess.Set("user_name", "测试用户")
	sess.SetSearchResults([]session.SearchItem{{ID: "303", Title: "测试剧集", Year: 2026, Type: "tv"}}, 1, "测试")

	mapping := services.NewUserMappingService(t.TempDir())
	if err := mapping.AddMapping(42, 7, "tester"); err != nil {
		t.Fatal(err)
	}
	review := services.NewReviewService(t.TempDir(), false)
	submitter := &fakeRequestSubmitter{}
	h := NewRequestHandler(manager, nil, nil, nil, nil, nil, mapping, nil, review)
	h.submissionService = submitter

	resp, err := h.Handle(&callback.Context{
		UserID: 42,
		Callback: &callback.Callback{Action: callback.ActionRequest, Params: map[string]string{
			"id": "303", "type": "tv", "season": "0",
		}},
	})
	if err != nil || resp == nil || resp.Keyboard == nil {
		t.Fatalf("Handle resp=%#v err=%v", resp, err)
	}
	if len(submitter.calls) != 0 {
		t.Fatalf("full-show request submitted before confirmation: %+v", submitter.calls)
	}
	if !strings.Contains(resp.Text, "全部季度") || !strings.Contains(resp.Text, "不是单独一季") {
		t.Fatalf("confirmation copy=%q", resp.Text)
	}
	callbacks := keyboardCallbacks(resp.Keyboard)
	if callbacks["request:id:303:type:tv:season:0:confirm:1"] != "✅ 确认求全部季度" {
		t.Fatalf("confirmation callback missing: %#v", callbacks)
	}
}
