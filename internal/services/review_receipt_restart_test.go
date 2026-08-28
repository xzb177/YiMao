package services

import "testing"

func TestRequesterReceiptCoordinatesSurviveServiceRestart(t *testing.T) {
	dir := t.TempDir()
	first := NewReviewService(dir, false)
	req := &ReviewRequest{RequestID: "restart-receipt", TelegramID: 42, MediaTitle: "Example", Status: "pending"}
	if err := first.CreateRequest(req); err != nil {
		t.Fatal(err)
	}
	if err := first.SetRequesterReceipt(req.RequestID, -1001, 9876); err != nil {
		t.Fatal(err)
	}
	second := NewReviewService(dir, false)
	got, ok := second.GetRequest(req.RequestID)
	if !ok {
		t.Fatal("request missing after reload")
	}
	if got.RequesterChatID != -1001 || got.RequesterReceiptMsgID != 9876 {
		t.Fatalf("receipt coordinates lost after reload: chat=%d message=%d", got.RequesterChatID, got.RequesterReceiptMsgID)
	}
}
