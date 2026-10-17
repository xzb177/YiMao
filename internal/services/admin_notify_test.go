package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 审计第 2 条后半：每次密码重置都要通知超管。先验证通知通道本身可用。
func TestNotifyRootAdminSendsToRoot(t *testing.T) {
	dir := t.TempDir()
	svc := NewAdminService(dir)
	if err := svc.AddAdminWithRole(1001, "root", AdminRoleRoot); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddAdminWithRole(1002, "normal", AdminRoleNormal); err != nil {
		t.Fatal(err)
	}

	var gotChatID int64
	var gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		gotChatID = payload.ChatID
		gotText = payload.Text
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1001,"type":"private"}}}`))
	}))
	defer server.Close()

	tg := NewTelegramClient("1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	tg.SetBaseURLForTest(server.URL, server.Client())

	svc.NotifyRootAdmin(tg, "test notification")
	if gotChatID != 1001 {
		t.Fatalf("notification went to chat %d, want root admin 1001", gotChatID)
	}
	if gotText != "test notification" {
		t.Fatalf("notification text = %q", gotText)
	}
}

// 审计第 2 条门禁：重置他人密码只允许超管（IsRootAdmin），普通管理员不行。
func TestIsRootAdminDistinguishesRoles(t *testing.T) {
	dir := t.TempDir()
	svc := NewAdminService(dir)
	if err := svc.AddAdminWithRole(1001, "root", AdminRoleRoot); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddAdminWithRole(1002, "normal", AdminRoleNormal); err != nil {
		t.Fatal(err)
	}

	if !svc.IsAdmin(1002) {
		t.Fatal("1002 should be an admin")
	}
	if svc.IsRootAdmin(1002) {
		t.Fatal("normal admin must NOT be root (otherwise it could reset others' passwords)")
	}
	if !svc.IsRootAdmin(1001) {
		t.Fatal("1001 should be root admin")
	}
	if svc.IsRootAdmin(9999) {
		t.Fatal("stranger must not be root admin")
	}
}

// 无超管/无服务时必须是 no-op，不能 panic。
func TestNotifyRootAdminNoop(t *testing.T) {
	var nilSvc *AdminService
	nilSvc.NotifyRootAdmin(nil, "x")

	dir := t.TempDir()
	svc := NewAdminService(dir) // no admins at all
	svc.NotifyRootAdmin(nil, "x")

	if svc.IsRootAdmin(999) {
		t.Fatal("nobody should be root admin here")
	}
}
