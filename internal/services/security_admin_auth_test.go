package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagementMiddlewareAlwaysRequiresAPIKey(t *testing.T) {
	security := NewSecurityService()
	security.SetAPIKeys(map[string]string{"valid-key": "test"})
	security.EnableAPIAuth(false) // legacy global flag must not disable management auth
	h := security.ManagementMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	cases := []struct {
		name, remote, xff, key string
		want                   int
	}{
		{"direct loopback without key", "127.0.0.1:1000", "", "", http.StatusUnauthorized},
		{"proxied external with loopback remote", "127.0.0.1:1000", "203.0.113.8", "", http.StatusUnauthorized},
		{"spoofed forwarded loopback", "203.0.113.8:1000", "127.0.0.1", "", http.StatusUnauthorized},
		{"invalid key", "127.0.0.1:1000", "", "wrong", http.StatusUnauthorized},
		{"valid key", "127.0.0.1:1000", "203.0.113.8", "valid-key", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/summary", http.NoBody)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.key != "" {
				req.Header.Set("X-API-Key", tc.key)
			}
			rr := httptest.NewRecorder()
			h(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status=%d want=%d", rr.Code, tc.want)
			}
		})
	}
}

func TestManagementMiddlewareFailsClosedWithoutConfiguredKeys(t *testing.T) {
	security := NewSecurityService()
	h := security.ManagementMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/api/admins", http.NoBody)
	req.RemoteAddr = "127.0.0.1:1000"
	req.Header.Set("X-API-Key", "anything")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}
