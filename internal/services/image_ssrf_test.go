package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 审计 A5：图片回退下载必须拒绝非公网地址。

func TestValidateOutboundImageURL(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/x.jpg",
		"http://localhost/x.jpg", // 解析到环回
		"http://10.0.0.5/x.jpg",
		"http://192.168.1.1/x.jpg",
		"http://172.16.0.1/x.jpg",
		"http://169.254.169.254/latest/meta-data", // 链路本地（云元数据）
		"http://[::1]/x.jpg",
		"file:///etc/passwd",
		"ftp://example.com/x.jpg",
		"",
	}
	for _, u := range blocked {
		if err := validateOutboundImageURL(u); err == nil {
			t.Errorf("应拒绝 %q，实际通过", u)
		}
	}
	// 公网字面 IP 不做 DNS，直接放行（不打外网）。
	if err := validateOutboundImageURL("https://8.8.8.8/x.jpg"); err != nil {
		t.Errorf("公网 IP 应放行，实际 err=%v", err)
	}
}

// 审计 A5：跨主机重定向必须剥掉 X-Emby-Token 等敏感头。
func TestRedirectStripsSensitiveHeaders(t *testing.T) {
	var gotToken string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Emby-Token")
		_, _ = w.Write([]byte("img"))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x.jpg", http.StatusFound)
	}))
	defer redirector.Close()

	old := allowPrivateImageHosts
	allowPrivateImageHosts = true
	defer func() { allowPrivateImageHosts = old }()

	data, err := downloadImageWithLimit(redirector.Client(),
		redirector.URL+"/start.jpg", map[string]string{"X-Emby-Token": "SECRET123"})
	if err != nil {
		t.Fatalf("重定向下载应成功，err=%v", err)
	}
	if len(data) == 0 {
		t.Fatal("应下载到内容")
	}
	if gotToken != "" {
		t.Fatalf("X-Emby-Token 被带到了跳转后的主机: %q", gotToken)
	}
}

// 审计 A5：重定向到内网地址必须被拦下。
func TestRedirectToPrivateBlocked(t *testing.T) {
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("img"))
	}))
	defer inner.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inner.URL+"/x.jpg", http.StatusFound)
	}))
	defer redirector.Close()

	// allowPrivateImageHosts=false（默认）：127.0.0.1 必须被拦。
	if _, err := downloadImageWithLimit(redirector.Client(), redirector.URL+"/start.jpg", nil); err == nil {
		t.Fatal("重定向到 127.0.0.1 应被拒绝，实际通过")
	} else if !strings.Contains(err.Error(), "non-public") && !strings.Contains(err.Error(), "not public") {
		t.Fatalf("拒绝原因不符合预期: %v", err)
	}
}

// 审计 A5：起始 URL 就是内网地址，直接拒绝。
func TestDownloadImageRejectsPrivateStartURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("img"))
	}))
	defer srv.Close()
	if _, err := downloadImageWithLimit(srv.Client(), srv.URL+"/x.jpg", nil); err == nil {
		t.Fatal("内网起始 URL 应被拒绝，实际通过")
	}
}
