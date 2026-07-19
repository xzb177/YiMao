package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequireLoopbackURL(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:18080", "http://localhost:18080", "http://[::1]:18080"} {
		if err := requireLoopbackURL(value); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	for _, value := range []string{"https://127.0.0.1:18080", "http://example.com:18080", "http://user:pass@127.0.0.1:18080"} {
		if err := requireLoopbackURL(value); err == nil {
			t.Errorf("expected %s to be rejected", value)
		}
	}
}

func TestRequireHTTPServiceURL(t *testing.T) {
	for _, value := range []string{"http://moviepilot:3000", "https://mp.example.test"} {
		if err := requireHTTPServiceURL(value); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	for _, value := range []string{"ftp://moviepilot", "http://user:pass@moviepilot", "not-a-url"} {
		if err := requireHTTPServiceURL(value); err == nil {
			t.Errorf("expected %s to be rejected", value)
		}
	}
}

func TestRequestJSONDoesNotLeakEndpointOnFailure(t *testing.T) {
	secret := "123456:secret-token"
	_, _, err := requestJSON(time.Millisecond, http.MethodGet, "http://127.0.0.1:1/bot"+secret, nil, nil)
	if err == nil {
		t.Fatal("expected request failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("request error leaked a credential-bearing endpoint")
	}
}

func TestCheckAppHealthAndMoviePilotReadOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","dependencies":{"moviepilot":"ok"}}`))
		case "/api/v1/system/setting/APP":
			if r.Header.Get("X-API-Key") != "moviepilot-test-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"app":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := smokeConfig{baseURL: server.URL, moviePilotURL: server.URL, moviePilotKey: "moviepilot-test-key", requestTimeout: time.Second}
	if result := checkAppHealth(&cfg); result.Status != "pass" {
		t.Fatalf("checkAppHealth = %#v", result)
	}
	if result := checkMoviePilotReadOnly(&cfg); result.Status != "pass" {
		t.Fatalf("checkMoviePilotReadOnly = %#v", result)
	}
}

func TestCheckAPIAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-API-Key") != "staging-key-123456" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sessions":0,"total_size":0}`))
	}))
	defer server.Close()

	result := checkAPIAuth(&smokeConfig{baseURL: server.URL, apiKey: "staging-key-123456", requestTimeout: time.Second})
	if result.Status != "pass" {
		t.Fatalf("checkAPIAuth = %#v", result)
	}
}

func TestSmokeExitCodeRejectsRequiredSkippedChecks(t *testing.T) {
	if got := smokeExitCode(smokeReport{Skipped: 1}, true); got != 1 {
		t.Fatalf("required skipped check exit code = %d, want 1", got)
	}
	if got := smokeExitCode(smokeReport{Skipped: 1}, false); got != 0 {
		t.Fatalf("optional skipped check exit code = %d, want 0", got)
	}
	if got := smokeExitCode(smokeReport{Failed: 1}, false); got != 1 {
		t.Fatalf("failed check exit code = %d, want 1", got)
	}
}
