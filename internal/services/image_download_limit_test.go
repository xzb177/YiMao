package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 审计第 12 条：两条图片下载路径上限不一致（1MB / 5MB）且都是静默截断，
// 截断的坏图还会被缓存下来。修复后：统一 10MB 上限，超限明确报错、不缓存。
func TestDownloadImageWithLimit(t *testing.T) {
	serve := func(size int64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
			chunk := make([]byte, 1<<20)
			var written int64
			for written < size {
				n := int64(len(chunk))
				if written+n > size {
					n = size - written
				}
				_, _ = w.Write(chunk[:n])
				written += n
			}
		}))
	}

	t.Run("within limit succeeds", func(t *testing.T) {
		srv := serve(3 << 20) // 3MB：旧 1MB 路径会截断，现在必须完整
		defer srv.Close()
		data, err := downloadImageWithLimit(srv.Client(), srv.URL, nil)
		if err != nil {
			t.Fatalf("3MB image should download, got: %v", err)
		}
		if int64(len(data)) != 3<<20 {
			t.Fatalf("got %d bytes, want full %d", len(data), 3<<20)
		}
	})

	t.Run("over limit errors, not truncated", func(t *testing.T) {
		srv := serve(12 << 20)
		defer srv.Close()
		_, err := downloadImageWithLimit(srv.Client(), srv.URL, nil)
		if err == nil {
			t.Fatal("12MB image must error, not silently truncate")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error should mention the limit, got: %v", err)
		}
	})

	t.Run("over limit is not cached", func(t *testing.T) {
		srv := serve(12 << 20)
		defer srv.Close()
		cache := NewImageCache(t.TempDir(), time.Hour)
		if _, err := cache.DownloadOrFetch(srv.URL+"/big.jpg", nil); err == nil {
			t.Fatal("DownloadOrFetch must error on oversize image")
		}
		if cached := cache.Get(srv.URL + "/big.jpg"); cached != nil {
			t.Fatalf("oversize image must not be cached, got %d bytes", len(cached))
		}
	})

	t.Run("bad status errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if _, err := downloadImageWithLimit(srv.Client(), srv.URL, nil); err == nil {
			t.Fatal("404 must error")
		}
	})
}
