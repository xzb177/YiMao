package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 亲审 R2：DNS 重绑定 TOCTOU。场景：校验时 DNS 返回公网 IP（通过），
// 连接瞬间 DNS 被换成内网 IP——没有 pinning 时，连接会打到内网地址。
// pinning 要求：连接必须打到校验过的那个 IP。
func TestPinnedDialerIgnoresDNSRebinding(t *testing.T) {
	oldLookup, oldDial := imageLookupIP, imageDialTCP
	defer func() { imageLookupIP, imageDialTCP = oldLookup, oldDial }()

	calls := 0
	imageLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		calls++
		if calls == 1 {
			return []net.IP{net.ParseIP("203.0.113.7")}, nil // 校验时：公网
		}
		return []net.IP{net.ParseIP("10.9.9.9")}, nil // 连接时：攻击者已重绑定
	}
	var dialed []string
	imageDialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, errors.New("stop here")
	}

	dc := pinnedImageDialContext()
	dc(context.Background(), "tcp", "cdn.example.com:443")
	dc(context.Background(), "tcp", "cdn.example.com:443") // 同一连接器内再次拨号

	if calls != 1 {
		t.Fatalf("expected exactly 1 DNS lookup (pinned), got %d", calls)
	}
	if len(dialed) != 2 {
		t.Fatalf("expected 2 dials, got %d", len(dialed))
	}
	for _, a := range dialed {
		if a != "203.0.113.7:443" {
			t.Fatalf("dialed %s, want pinned 203.0.113.7:443 (rebinding not blocked)", a)
		}
	}
}

// 解析出内网 IP 时，拨号前直接拒绝。
func TestPinnedDialerRefusesNonPublicIP(t *testing.T) {
	oldLookup, oldDial := imageLookupIP, imageDialTCP
	defer func() { imageLookupIP, imageDialTCP = oldLookup, oldDial }()
	imageLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.5")}, nil
	}
	dialed := false
	imageDialTCP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("must not dial")
	}
	if _, err := pinnedImageDialContext()(context.Background(), "tcp", "evil.example.com:443"); err == nil {
		t.Fatal("expected refusal for non-public IP")
	}
	if dialed {
		t.Fatal("must not dial when resolution is non-public")
	}
}

// 端到端接线：pinned dialer 参与的完整下载路径可用（httptest + 允许内网）。
func TestDownloadImageWithPinnedDialer(t *testing.T) {
	oldAllow, oldLookup := allowPrivateImageHosts, imageLookupIP
	allowPrivateImageHosts = true
	defer func() { allowPrivateImageHosts, imageLookupIP = oldAllow, oldLookup }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-image-bytes"))
	}))
	defer srv.Close()
	imageLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	data, err := downloadImageWithLimit(&http.Client{}, srv.URL+"/p.jpg", nil)
	if err != nil {
		t.Fatalf("download through pinned dialer failed: %v", err)
	}
	if string(data) != "fake-image-bytes" {
		t.Fatalf("unexpected body %q", data)
	}
}
