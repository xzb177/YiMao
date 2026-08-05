package miniapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniAppLoadsTelegramSDKFromSameOrigin(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/miniapp", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `src="/miniapp/telegram-web-app.js"`) {
		t.Fatal("Mini App must load the Telegram SDK from the same origin")
	}
	if strings.Contains(body, "https://telegram.org/js/telegram-web-app.js") {
		t.Fatal("Mini App must not depend on the external Telegram SDK at startup")
	}
}

func TestMiniAppServesEmbeddedTelegramSDK(t *testing.T) {
	handler := NewServer(Deps{}).Handler()

	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/miniapp/telegram-web-app.js", nil))
	if postResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d, want %d", postResponse.Code, http.StatusMethodNotAllowed)
	}
	if allow := postResponse.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want GET, HEAD", allow)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/miniapp/telegram-web-app.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("Content-Type=%q, want JavaScript", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("Cache-Control=%q, want no-store", cacheControl)
	}
	body := response.Body.String()
	if len(body) < 10000 || !strings.Contains(body, "Telegram.WebApp") {
		t.Fatalf("embedded Telegram SDK is missing or truncated: bytes=%d", len(body))
	}
}
