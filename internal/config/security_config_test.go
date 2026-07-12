package config

import (
	"strings"
	"testing"
)

func TestParseAPIKeysJSON(t *testing.T) {
	keys, err := parseAPIKeys(`{"0123456789abcdef":"cron"}`)
	if err != nil || keys["0123456789abcdef"] != "cron" { t.Fatalf("keys=%v err=%v", keys, err) }
}

func TestParseAPIKeysRejectsUnsafeFormats(t *testing.T) {
	for _, raw := range []string{`short:desc`, `{"short":"desc"}`, `{"0123456789abcdef":""}`} {
		if _, err := parseAPIKeys(raw); err == nil { t.Fatalf("expected error for %q", raw) }
	}
}

func TestValidateAPIAuthFailsClosedWithoutKeys(t *testing.T) {
	c := &Config{TelegramBotToken: strings.Repeat("x", 30), MoviePilotURL: "http://mp", MoviePilotAPIKey: strings.Repeat("k", 10), EnableAPIAuth: true, APIKeys: map[string]string{}}
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "API_KEYS") { t.Fatalf("unexpected error: %v", err) }
}
