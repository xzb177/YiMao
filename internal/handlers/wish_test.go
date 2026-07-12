package handlers

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

type fakeWishSubmitter struct {
	result services.SubmissionResult
	err    error
	got    services.RequestSubmission
	calls  int
}

func (f *fakeWishSubmitter) SubmitResult(in services.RequestSubmission) (services.SubmissionResult, error) {
	f.calls++
	f.got = in
	return f.result, f.err
}

func notifiedWish(t *testing.T, userID int64) (*services.WishService, *services.WishItem) {
	t.Helper()
	wish, err := services.NewWishService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := wish.AddWish(&services.WishItem{UserID: userID, TmdbID: 42, MediaType: "tv", Title: "测试剧", Year: 2026, Season: 3})
	if err != nil || !res.Created {
		t.Fatalf("AddWish: result=%+v err=%v", res, err)
	}
	items, err := wish.ListByUser(userID)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListByUser: count=%d err=%v", len(items), err)
	}
	created := items[0]
	claimed, err := wish.ClaimSearchableItems(created.SearchOffsetMinute, 60, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimSearchableItems: count=%d err=%v", len(claimed), err)
	}
	if changed, err := wish.MarkFound(created.ID, "source"); err != nil || !changed {
		t.Fatalf("MarkFound: changed=%v err=%v", changed, err)
	}
	if changed, err := wish.MarkNotified(created.ID); err != nil || !changed {
		t.Fatalf("MarkNotified: changed=%v err=%v", changed, err)
	}
	item, err := wish.GetByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return wish, item
}

func wishRequestContext(userID, wishID int64) *callback.Context {
	return &callback.Context{UserID: userID, Callback: &callback.Callback{Params: map[string]string{"id": fmt.Sprint(wishID)}}}
}

// TestIsConfidentTitleMatch 覆盖 B6：多结果时判定首条是否高置信，决定是否提示用户精确重搜。
func TestIsConfidentTitleMatch(t *testing.T) {
	cases := []struct {
		query, title string
		want         bool
	}{
		{"沙丘", "沙丘", true},
		{"DUNE", "Dune", true}, // 忽略大小写
		{"  沙丘 ", "沙丘", true},  // 忽略首尾空白
		{"沙丘", "沙丘 2", true},   // 标题包含查询
		{"沙丘 2", "沙丘", true},   // 查询包含标题
		{"沙丘", "盗梦空间", false},  // 完全不相关
		{"", "沙丘", false},      // 空查询
		{"沙丘", "", false},      // 空标题
	}
	for _, c := range cases {
		if got := isConfidentTitleMatch(c.query, c.title); got != c.want {
			t.Errorf("isConfidentTitleMatch(%q,%q)=%v want %v", c.query, c.title, got, c.want)
		}
	}
}

func TestWishHandleCreatedFulfillsFromTypedResult(t *testing.T) {
	wish, item := notifiedWish(t, 100)
	submitter := &fakeWishSubmitter{result: services.SubmissionResult{Status: services.SubmissionCreated}}
	h := NewWishHandler(wish, nil, nil, nil, nil, nil)
	h.SetRequestSubmissionService(submitter)

	resp, err := h.Handle(wishRequestContext(100, item.ID))
	if err != nil || resp.CallbackMsg != "请求已提交" {
		t.Fatalf("Handle: resp=%+v err=%v", resp, err)
	}
	got, _ := wish.GetByID(item.ID)
	if got.State != services.WishStateFulfilled {
		t.Fatalf("state=%s want FULFILLED", got.State)
	}
	if submitter.got.TelegramID != 100 || submitter.got.TmdbID != 42 || submitter.got.MediaTitle != "测试剧" || submitter.got.MediaYear != 2026 || submitter.got.MediaType != services.MediaTypeTV || submitter.got.Season != 3 || submitter.got.Origin != "wish" || !submitter.got.UseQuota {
		t.Fatalf("submission fields mismatch: %+v", submitter.got)
	}
}

func TestWishHandleStateUsesTypedResultNotCallbackMessage(t *testing.T) {
	wish, item := notifiedWish(t, 101)
	submitter := &fakeWishSubmitter{result: services.SubmissionResult{Status: services.SubmissionDuplicateOwn}}
	h := NewWishHandler(wish, nil, nil, nil, nil, nil)
	h.SetRequestSubmissionService(submitter)

	resp, err := h.Handle(wishRequestContext(101, item.ID))
	if err != nil || resp.CallbackMsg == "请求已提交" {
		t.Fatalf("Handle: resp=%+v err=%v", resp, err)
	}
	got, _ := wish.GetByID(item.ID)
	if got.State != services.WishStateNotified {
		t.Fatalf("duplicate own must remain retryable, state=%s", got.State)
	}
}

func TestWishHandleFailureKeepsWish(t *testing.T) {
	wish, item := notifiedWish(t, 102)
	submitter := &fakeWishSubmitter{err: errors.New("storage unavailable")}
	h := NewWishHandler(wish, nil, nil, nil, nil, nil)
	h.SetRequestSubmissionService(submitter)

	resp, err := h.Handle(wishRequestContext(102, item.ID))
	if err == nil || resp == nil || !resp.ShowAlert {
		t.Fatalf("Handle: resp=%+v err=%v", resp, err)
	}
	got, _ := wish.GetByID(item.ID)
	if got.State != services.WishStateNotified {
		t.Fatalf("failed submission changed state to %s", got.State)
	}
}

func TestWishHandleRejectsNonOwnerBeforeSubmission(t *testing.T) {
	wish, item := notifiedWish(t, 103)
	submitter := &fakeWishSubmitter{result: services.SubmissionResult{Status: services.SubmissionCreated}}
	h := NewWishHandler(wish, nil, nil, nil, nil, nil)
	h.SetRequestSubmissionService(submitter)

	resp, err := h.Handle(wishRequestContext(999, item.ID))
	if err != nil || resp == nil || !resp.ShowAlert || submitter.calls != 0 {
		t.Fatalf("Handle: resp=%+v err=%v calls=%d", resp, err, submitter.calls)
	}
	got, _ := wish.GetByID(item.ID)
	if got.State != services.WishStateNotified {
		t.Fatalf("non-owner changed state to %s", got.State)
	}
}

func TestWishHandleDuplicateOtherCarpoolsAndFulfills(t *testing.T) {
	wish, item := notifiedWish(t, 104)
	submitter := &fakeWishSubmitter{result: services.SubmissionResult{Status: services.SubmissionDuplicateOther}}
	carpool := services.NewCarpoolService(t.TempDir())
	h := NewWishHandler(wish, nil, nil, nil, nil, nil)
	h.SetRequestSubmissionService(submitter)
	h.SetCarpoolService(carpool)

	resp, err := h.Handle(wishRequestContext(104, item.ID))
	if err != nil || resp == nil {
		t.Fatalf("Handle: resp=%+v err=%v", resp, err)
	}
	got, _ := wish.GetByID(item.ID)
	if got.State != services.WishStateFulfilled {
		t.Fatalf("state=%s want FULFILLED", got.State)
	}
	users := carpool.Get(item.TmdbID, item.MediaType)
	if len(users) != 1 || users[0] != item.UserID {
		t.Fatalf("carpool users=%v", users)
	}
}
