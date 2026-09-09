package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

func TestManagementRoutesAreAuthenticatedAndReadOnly(t *testing.T) {
	cfg := &config.Config{ServerHost: "127.0.0.1", ServerPort: "8080"}
	security := services.NewSecurityService()
	security.SetAPIKeys(map[string]string{"valid-key": "test"})
	security.SetConfig(100, 60, 100, 30)
	deps := &Dependencies{
		SessionMgr:   session.NewManager(time.Hour, 10),
		AdminService: services.NewAdminService(t.TempDir()),
	}
	srv := New(cfg, nil, deps, security)

	cases := []struct {
		name, method, path, key, adminID string
		want                             int
	}{
		{"stats no key", http.MethodGet, "/api/stats", "", "", http.StatusUnauthorized},
		{"admins wrong key", http.MethodGet, "/api/admins", "wrong", "", http.StatusUnauthorized},
		{"stats read", http.MethodGet, "/api/stats", "valid-key", "", http.StatusOK},
		{"admins read", http.MethodGet, "/api/admins", "valid-key", "", http.StatusOK},
		{"admins post forged identity", http.MethodPost, "/api/admins", "valid-key", "12345", http.StatusForbidden},
		{"admins delete forged identity", http.MethodDelete, "/api/admins/67890", "valid-key", "12345", http.StatusForbidden},
		{"summary write forged identity", http.MethodPost, "/api/summary", "valid-key", "12345", http.StatusForbidden},
		{"stats wrong method", http.MethodPost, "/api/stats", "valid-key", "", http.StatusMethodNotAllowed},
		{"admins wrong method", http.MethodPut, "/api/admins", "valid-key", "", http.StatusMethodNotAllowed},
		{"admin member read unavailable", http.MethodGet, "/api/admins/67890", "valid-key", "", http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			req.RemoteAddr = "198.51.100.10:1234"
			if tc.key != "" {
				req.Header.Set("X-API-Key", tc.key)
			}
			if tc.adminID != "" {
				req.Header.Set("X-Admin-User-ID", tc.adminID)
			}
			rr := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}
