package services

import (
	"net/http/httptest"
	"testing"
)

func TestGetClientIPTrustedProxyChain(t *testing.T) {
	security := NewSecurityService()
	cases := []struct {
		name, remote, xff, realIP, want string
	}{
		{"direct IPv4", "203.0.113.9:4321", "", "", "203.0.113.9"},
		{"direct IPv6", "[2001:db8::9]:4321", "", "", "2001:db8::9"},
		{"trusted proxy client", "127.0.0.1:4321", "203.0.113.8", "", "203.0.113.8"},
		{"trusted IPv6 proxy client", "[::1]:4321", "2001:db8::8", "", "2001:db8::8"},
		{"rightmost non-trusted wins", "127.0.0.1:4321", "198.51.100.66, 203.0.113.8", "", "203.0.113.8"},
		{"trusted hops stripped", "127.0.0.1:4321", "198.51.100.5, 127.0.0.1, ::1", "", "198.51.100.5"},
		{"malformed chain falls back", "127.0.0.1:4321", "203.0.113.8, garbage", "", "127.0.0.1"},
		{"real IP is not a trusted chain", "127.0.0.1:4321", "", "2001:db8::7", "127.0.0.1"},
		{"invalid real IP falls back", "127.0.0.1:4321", "", "garbage", "127.0.0.1"},
		{"untrusted peer ignores headers", "198.51.100.10:4321", "203.0.113.8", "192.0.2.1", "198.51.100.10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", tc.xff)
			req.Header.Set("X-Real-IP", tc.realIP)
			if got := security.getClientIP(req); got != tc.want {
				t.Fatalf("getClientIP()=%q want=%q", got, tc.want)
			}
		})
	}
}
