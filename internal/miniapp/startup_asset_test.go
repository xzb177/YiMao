package miniapp

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestMiniAppWaitsForTelegramSDKBeforeAuthenticatedRequests(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/miniapp", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, contract := range []string{
		"const telegramReady=new Promise",
		"resolveTelegramReady?.()",
		"await telegramReady",
		"tg=telegramWebApp()||tg",
		"if(!tg)return",
		"resolveTelegramReady?.();resolveTelegramReady=null",
		`onload="initTelegramWebApp()" onerror="resolveTelegramReady?.()"`,
	} {
		if !strings.Contains(body, contract) {
			t.Fatalf("Mini App must wait for Telegram SDK before authenticated requests: missing %q", contract)
		}
	}
}

func TestMiniAppServesEmbeddedTelegramSDK(t *testing.T) {
	handler := NewServer(Deps{}).Handler()

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
	if contentTypeOptions := response.Header().Get("X-Content-Type-Options"); contentTypeOptions != "nosniff" {
		t.Fatalf("X-Content-Type-Options=%q, want nosniff", contentTypeOptions)
	}
	body := response.Body.Bytes()
	if len(body) < 10000 || !strings.Contains(string(body), "Telegram.WebApp") {
		t.Fatalf("embedded Telegram SDK is missing or truncated: bytes=%d", len(body))
	}
	digest := sha256.Sum256(body)
	if got := hex.EncodeToString(digest[:]); got != "3549138a7934039fe7dfd1291a4ee739bd2b705a614308053a8b08a87d85c451" {
		t.Fatalf("Telegram SDK SHA-256=%s, want the reviewed upstream asset", got)
	}
}

func TestMiniAppTelegramSDKSupportsHEADWithoutBody(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/miniapp/telegram-web-app.js", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("HEAD body bytes=%d, want 0", response.Body.Len())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("Content-Type=%q, want JavaScript", contentType)
	}
}

func TestMiniAppTelegramSDKRejectsUnsupportedMethods(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, "/miniapp/telegram-web-app.js", nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
			if allow := response.Header().Get("Allow"); allow != "GET, HEAD" {
				t.Fatalf("Allow=%q, want GET, HEAD", allow)
			}
		})
	}
}

func TestMiniAppTelegramSDKRejectsSimilarPaths(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	for _, path := range []string{
		"/miniapp/telegram-web-app.js/",
		"/miniapp/telegram-web-app.js.map",
		"/miniapp/telegram-web-app.js/extra",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status=%d, want %d", response.Code, http.StatusNotFound)
			}
		})
	}
}
