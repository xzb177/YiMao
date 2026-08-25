package services

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A retried POST must resend the identical body. Before GetBody-based cloning
// the second attempt shipped Content-Length with an empty body and MoviePilot
// answered "ContentLength=N with Body length 0".
func TestRetryHTTPResendsFullBodyOnRetry(t *testing.T) {
	var attempts atomic.Int32
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	payload := `{"name":"test-title","tmdbid":1433321}`
	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 0
	resp, err := RetryHTTP(context.Background(), server.Client(), req, cfg)
	if err != nil {
		t.Fatalf("RetryHTTP returned error: %v", err)
	}
	defer resp.Body.Close()

	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts=%d, want 2", got)
	}
	for i, body := range bodies {
		if body != payload {
			t.Fatalf("attempt %d body=%q, want %q", i+1, body, payload)
		}
	}
}

// unrewindableBody has no GetBody counterpart, so the request may only be sent
// once; a second attempt would send an incomplete payload.
type unrewindableBody struct{ r io.Reader }

func (u *unrewindableBody) Read(p []byte) (int, error) { return u.r.Read(p) }
func (u *unrewindableBody) Close() error               { return nil }

func TestRetryHTTPDoesNotRetryUnrewindableBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	const payload = `{"tmdbid":1}`
	req.Body = &unrewindableBody{r: strings.NewReader(payload)}
	req.ContentLength = int64(len(payload))
	req.GetBody = nil

	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 0
	resp, err := RetryHTTP(context.Background(), server.Client(), req, cfg)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("RetryHTTP must fail instead of replaying an unrewindable body")
	}
	if !strings.Contains(err.Error(), "cannot be replayed") {
		t.Fatalf("error = %v, want an explicit non-replayable body error", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, want exactly 1 (no retry)", got)
	}
}

// Bodyless GET requests stay retryable.
func TestRetryHTTPStillRetriesBodylessRequests(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 0
	resp, err := RetryHTTP(context.Background(), server.Client(), req, cfg)
	if err != nil {
		t.Fatalf("RetryHTTP returned error: %v", err)
	}
	defer resp.Body.Close()
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts=%d, want 3", got)
	}
}
