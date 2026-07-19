package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type smokeConfig struct {
	baseURL        string
	telegramToken  string
	expectedBot    string
	chatID         string
	moviePilotURL  string
	moviePilotKey  string
	apiKey         string
	requireChat    bool
	requestTimeout time.Duration
}

type checkResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type smokeReport struct {
	SchemaVersion int           `json:"schema_version"`
	Environment   string        `json:"environment"`
	StartedAt     string        `json:"started_at"`
	FinishedAt    string        `json:"finished_at"`
	Passed        int           `json:"passed"`
	Failed        int           `json:"failed"`
	Skipped       int           `json:"skipped"`
	Checks        []checkResult `json:"checks"`
}

func main() {
	started := time.Now().UTC()
	cfg, err := loadConfig()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "staging guard failed: %v\n", err)
		os.Exit(2)
	}

	checks := []func(*smokeConfig) checkResult{
		checkAppHealth,
		checkAPIAuth,
		checkTelegramIdentity,
		checkTelegramWebhook,
		checkTelegramCommands,
		checkMoviePilotReadOnly,
		checkTelegramSendDelete,
	}
	report := smokeReport{
		SchemaVersion: 1,
		Environment:   "staging",
		StartedAt:     started.Format(time.RFC3339),
	}
	for _, check := range checks {
		result := check(&cfg)
		report.Checks = append(report.Checks, result)
		switch result.Status {
		case "pass":
			report.Passed++
		case "skip":
			report.Skipped++
		default:
			report.Failed++
		}
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		os.Exit(2)
	}
	os.Exit(smokeExitCode(report, cfg.requireChat))
}

func smokeExitCode(report smokeReport, requireChat bool) int {
	if report.Failed > 0 || (requireChat && report.Skipped > 0) {
		return 1
	}
	return 0
}

func loadConfig() (smokeConfig, error) {
	if !envBool("STAGING_CONFIRM_ISOLATED") {
		return smokeConfig{}, fmt.Errorf("STAGING_CONFIRM_ISOLATED must be true")
	}
	cfg := smokeConfig{
		baseURL:        envOr("STAGING_BASE_URL", "http://127.0.0.1:18080"),
		telegramToken:  strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		expectedBot:    strings.TrimPrefix(strings.TrimSpace(os.Getenv("STAGING_EXPECTED_BOT_USERNAME")), "@"),
		chatID:         strings.TrimSpace(os.Getenv("STAGING_SMOKE_CHAT_ID")),
		moviePilotURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("MOVIEPILOT_URL")), "/"),
		moviePilotKey:  strings.TrimSpace(os.Getenv("MOVIEPILOT_API_KEY")),
		requireChat:    envBool("STAGING_REQUIRE_CHAT"),
		requestTimeout: 8 * time.Second,
	}
	if len(cfg.telegramToken) < 30 || len(cfg.moviePilotKey) < 10 {
		return smokeConfig{}, fmt.Errorf("staging Telegram or MoviePilot credential is too short")
	}
	if cfg.telegramToken == "" || cfg.expectedBot == "" || cfg.moviePilotURL == "" || cfg.moviePilotKey == "" {
		return smokeConfig{}, fmt.Errorf("staging Telegram identity and MoviePilot credentials are required")
	}
	var keys map[string]string
	if err := json.Unmarshal([]byte(os.Getenv("API_KEYS")), &keys); err != nil || len(keys) == 0 {
		return smokeConfig{}, fmt.Errorf("API_KEYS must contain at least one valid JSON key")
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	cfg.apiKey = ordered[0]
	if cfg.requireChat && cfg.chatID == "" {
		return smokeConfig{}, fmt.Errorf("STAGING_SMOKE_CHAT_ID is required")
	}
	if err := requireLoopbackURL(cfg.baseURL); err != nil {
		return smokeConfig{}, err
	}
	if err := requireHTTPServiceURL(cfg.moviePilotURL); err != nil {
		return smokeConfig{}, err
	}
	return cfg, nil
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
