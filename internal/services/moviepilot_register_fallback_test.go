package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 审计第 5 条：RegisterUser 注册报"已存在"后回退取现有用户，不校验密码就返回，
// 调用方（/link）随即绑定——攻击者用 /link victim 任意密码 即可绑走别人的账号。
// 修复后：回退分支必须先经 LoginUser 校验密码。
func newRegisterFallbackTestServer(t *testing.T, correctPassword string) *httptest.Server {
	t.Helper()
	victimJSON := `{"success":true,"data":[{"name":"victim","email":"victim@x","id":42,"is_active":true}]}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/user"):
			// 注册报"已存在"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"message":"user already exists"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/user"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(victimJSON))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/login/access-token"):
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.Form.Get("username") == "victim" && r.Form.Get("password") == correctPassword {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"tok123","token_type":"bearer"}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// 错误密码：回退分支必须拒绝，不能返回现有用户。
func TestRegisterUserFallbackRequiresPassword(t *testing.T) {
	server := newRegisterFallbackTestServer(t, "correct-horse")
	defer server.Close()

	c := NewMoviePilotClient(server.URL, "testkey", "")
	_, err := c.RegisterUser("victim", "wrong-password", "victim@x")
	if err == nil {
		t.Fatal("RegisterUser returned existing user WITHOUT password verification")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Fatalf("error should mention password verification, got: %v", err)
	}
}

// 正确密码：回退分支校验通过后才返回现有用户。
func TestRegisterUserFallbackAcceptsCorrectPassword(t *testing.T) {
	server := newRegisterFallbackTestServer(t, "correct-horse")
	defer server.Close()

	c := NewMoviePilotClient(server.URL, "testkey", "")
	user, err := c.RegisterUser("victim", "correct-horse", "victim@x")
	if err != nil {
		t.Fatalf("RegisterUser with correct password should succeed, got: %v", err)
	}
	if user == nil || user.ID != 42 || user.Username != "victim" {
		t.Fatalf("unexpected user returned: %+v", user)
	}
}
