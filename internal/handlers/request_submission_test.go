package handlers

import (
	"os"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/services"
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
