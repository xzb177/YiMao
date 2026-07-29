package services

import (
	"os"
	"testing"
)

func TestBuildStartKeyboardAddsMiniAppOnlyForHTTPSURL(t *testing.T) {
	t.Setenv("MINIAPP_URL", "https://example.com/miniapp")
	keyboard := BuildStartKeyboardWithOptions(false, false)
	var found bool
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.WebApp != nil {
				found = button.WebApp.URL == "https://example.com/miniapp" && button.CallbackData == "" && button.URL == ""
			}
		}
	}
	if !found {
		t.Fatal("valid HTTPS Mini App button was not added")
	}
}

func TestBuildStartKeyboardRejectsNonHTTPSMiniAppURL(t *testing.T) {
	old, ok := os.LookupEnv("MINIAPP_URL")
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv("MINIAPP_URL", old)
		} else {
			_ = os.Unsetenv("MINIAPP_URL")
		}
	})
	_ = os.Setenv("MINIAPP_URL", "http://localhost:8080/miniapp")
	keyboard := BuildStartKeyboardWithOptions(false, false)
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.WebApp != nil {
				t.Fatal("non-HTTPS Mini App URL was exposed")
			}
		}
	}
}
