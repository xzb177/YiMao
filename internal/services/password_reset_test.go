package services

import (
	"strings"
	"testing"
)

// 审计第 2 条：ResetUserPassword 的 SQL 不排除 is_superuser，普通管理员能重置
// MoviePilot 超管密码。脚本必须探测 is_superuser 列并拒绝超管。
func TestBuildResetPasswordScriptRejectsSuperuser(t *testing.T) {
	script := buildResetPasswordScript("/config/user.db", "root", "newpass123")

	for _, want := range []string{
		"is_superuser",
		"SUPERUSER_RESET_DENIED",
		"PRAGMA table_info",
		"USER_NOT_FOUND",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("reset script missing %q:\n%s", want, script)
		}
	}
	// 用户名必须走 JSON 转义注入脚本，防止注入。
	if strings.Contains(script, "username = root") {
		t.Fatalf("username not properly escaped in script:\n%s", script)
	}
}

func TestClassifyResetPasswordResult(t *testing.T) {
	if err := classifyResetPasswordResult("OK", "alice"); err != nil {
		t.Fatalf("OK should map to nil, got %v", err)
	}
	if err := classifyResetPasswordResult("USER_NOT_FOUND", "alice"); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("USER_NOT_FOUND should map to not-found error, got %v", err)
	}
	err := classifyResetPasswordResult("SUPERUSER_RESET_DENIED", "root")
	if err == nil {
		t.Fatal("SUPERUSER_RESET_DENIED should map to an error")
	}
	if !strings.Contains(err.Error(), "超管") {
		t.Fatalf("superuser denial should mention 超管, got %v", err)
	}
	if err := classifyResetPasswordResult("weird output", "alice"); err == nil {
		t.Fatal("unknown status should map to an error")
	}
}
