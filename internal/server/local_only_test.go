package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalOnlyHandlerRejectsRemoteAndIgnoresForwardedHeaders(t *testing.T) {
	called := false
	h := localOnlyHandler(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admins", http.NoBody)
	req.RemoteAddr = "203.0.113.9:4567"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusForbidden || called {
		t.Fatalf("code=%d called=%v", rr.Code, called)
	}
}

func TestLocalOnlyHandlerAcceptsIPv4AndIPv6Loopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:1234", "[::1]:1234"} {
		t.Run(addr, func(t *testing.T) {
			called := false
			h := localOnlyHandler(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/api/stats", http.NoBody)
			req.RemoteAddr = addr
			rr := httptest.NewRecorder()
			h(rr, req)
			if rr.Code != http.StatusNoContent || !called {
				t.Fatalf("addr=%s code=%d called=%v", addr, rr.Code, called)
			}
		})
	}
}
