package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xzb177/yimao/pkg/logger"
)

// Config holds application configuration
type Config struct {
	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// MoviePilot
	MoviePilotURL      string
	MoviePilotAPIKey   string
	DownloadSavePath   string // Download save path for subscriptions (optional)

	// Emby (optional)
	EmbyURL    string
	EmbyAPIKey string

	// TMDB
	TMDBAPIKey string

	// AI Services
	AnthropicAPIKey string
	ZhipuAPIKey     string
	OpenAIAPIKey    string

	// Server
	ServerPort string
	ServerHost string
	WebhookURL string

	// Storage
	DataDir         string
	QuotaFile       string
	AdminFile       string
	PrefFile        string
	FeedbackFile    string
	AdminProfileDir string

	// Features
	EnableAI        bool
	EnableTrending  bool
	EnableHotTV     bool
	EnableNewMovies bool
	EnableRandom    bool

	// Limits
	MaxSessionAge int // in hours
	MaxSessions   int

	// Admins
	Admins map[int64]string // admin ID -> name

	// Security
	EnableAPIAuth    bool
	APIKeys          map[string]string // key -> description
	EnableRateLimit  bool
	EnableIPBlocking bool

	// Security Limits
	RateLimitRequests int // requests per window
	RateLimitWindow   int // window duration in seconds
	MaxFailedAttempts int // failed attempts before IP block
	BlockDuration     int // block duration in minutes

	// Notification Format
	NotificationFormat string // "simple" or "detailed"

	// Review/Resubscribe
	EnableAutoResubscribe bool

	// Logging
	LogLevel  string // "debug", "info", "warn", "error"
	LogColor  bool   // Enable colored log output
	LogPrefix string // Log prefix for module identification

	// PT Site Passkeys for RSS feeds
	HDSkyPasskey  string // HD-Sky passkey
	ZhuQuePasskey string // ZhuQue passkey (or "key1/key2" for new format)
	ZhuQueRSSKey1 string // ZhuQue RSS key part 1 (new format)
	ZhuQueRSSKey2 string // ZhuQue RSS key part 2 (new format)
	MTeamPasskey  string // M-Team passkey (legacy format)
	MTeamRSSUID   string // M-Team RSS user ID (new format)
	MTeamRSSSign  string // M-Team RSS signature (new format, dynamic)
}

// Load loads configuration from environment variables and files
func Load() (*Config, error) {
	cfg := &Config{
		TelegramBotToken: getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:   getEnv("TELEGRAM_CHAT_ID", ""),
		MoviePilotURL:      getEnv("MOVIEPILOT_URL", ""),
		MoviePilotAPIKey:   getEnv("MOVIEPILOT_API_KEY", ""),
		DownloadSavePath:   getEnv("DOWNLOAD_SAVE_PATH", ""), // Optional download save path
		EmbyURL:          getEnv("EMBY_URL", ""),
		EmbyAPIKey:       getEnv("EMBY_API_KEY", ""),
		TMDBAPIKey:       getEnv("TMDB_API_KEY", ""),
		// Support both ANTHROPIC_API_KEY and CLAUDE_API_KEY (for compatibility)
		AnthropicAPIKey:       getEnvFirst("ANTHROPIC_API_KEY", "CLAUDE_API_KEY", ""),
		ZhipuAPIKey:           getEnv("ZHIPU_API_KEY", ""),
		OpenAIAPIKey:          getEnv("OPENAI_API_KEY", ""),
		ServerPort:            getEnv("PORT", "8080"),
		ServerHost:            getEnv("HOST", "0.0.0.0"),
		WebhookURL:            getEnv("WEBHOOK_URL", ""),
		DataDir:               getEnv("DATA_DIR", "/app/data"),
		MaxSessionAge:         getEnvInt("MAX_SESSION_AGE", 24),
		MaxSessions:           getEnvInt("MAX_SESSIONS", 1000),
		EnableAI:              getEnvBool("ENABLE_AI", true),
		EnableTrending:        getEnvBool("ENABLE_TRENDING", true),
		EnableHotTV:           getEnvBool("ENABLE_HOT_TV", true),
		EnableNewMovies:       getEnvBool("ENABLE_NEW_MOVIES", true),
		EnableRandom:          getEnvBool("ENABLE_RANDOM", true),
		Admins:                make(map[int64]string),
		EnableAPIAuth:         getEnvBool("ENABLE_API_AUTH", false),
		APIKeys:               make(map[string]string),
		EnableRateLimit:       getEnvBool("ENABLE_RATE_LIMIT", true),
		EnableIPBlocking:      getEnvBool("ENABLE_IP_BLOCKING", true),
		RateLimitRequests:     getEnvInt("RATE_LIMIT_REQUESTS", 60),
		RateLimitWindow:       getEnvInt("RATE_LIMIT_WINDOW", 60), // seconds
		MaxFailedAttempts:     getEnvInt("MAX_FAILED_ATTEMPTS", 5),
		BlockDuration:         getEnvInt("BLOCK_DURATION", 30),           // minutes
		NotificationFormat:    getEnv("NOTIFICATION_FORMAT", "detailed"), // "simple" or "detailed"
		EnableAutoResubscribe: getEnvBool("ENABLE_AUTO_RESUBSCRIBE", false),
		// Logging
		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogColor:  getEnvBool("LOG_COLOR", false),
		LogPrefix: getEnv("LOG_PREFIX", "YiMao"),
		// PT Site Passkeys
		HDSkyPasskey:          getEnv("HDSKY_PASSKEY", ""),
		ZhuQuePasskey:         getEnv("ZHUQUE_PASSKEY", ""),
		ZhuQueRSSKey1:         getEnv("ZHUQUE_RSS_KEY1", ""),
		ZhuQueRSSKey2:         getEnv("ZHUQUE_RSS_KEY2", ""),
		MTeamPasskey:          getEnv("MTEAM_PASSKEY", ""),
		MTeamRSSUID:           getEnv("MTEAM_RSS_UID", ""),
		MTeamRSSSign:          getEnv("MTEAM_RSS_SIGN", ""),
	}

	// Set file paths
	cfg.QuotaFile = filepath.Join(cfg.DataDir, "user_quotas.json")
	cfg.AdminFile = filepath.Join(cfg.DataDir, "admins.json")
	cfg.PrefFile = filepath.Join(cfg.DataDir, "preferences.json")
	cfg.FeedbackFile = filepath.Join(cfg.DataDir, "feedback.json")
	cfg.AdminProfileDir = filepath.Join(cfg.DataDir, "admin_profiles")

	// Load admins from file
	if err := cfg.loadAdmins(); err != nil {
		// Non-fatal, continue with empty map
		logger.Warn("failed to load admins: %v", err)
	}

	// Also load admins from ADMIN_USER_IDS environment variable (comma-separated)
	// This allows setting admins purely via env vars without creating a JSON file.
	if envAdmins := getEnv("ADMIN_USER_IDS", ""); envAdmins != "" {
		for _, idStr := range strings.Split(envAdmins, ",") {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}
			var id int64
			if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil && id > 0 {
				if _, exists := cfg.Admins[id]; !exists {
					cfg.Admins[id] = "Admin"
					logger.Info("Admin added from env: %d", id)
				}
			}
		}
	}

	// Validate required config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate validates the configuration
func (c *Config) validate() error {
	// Required sensitive fields - no default allowed
	if c.TelegramBotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	// Validate token format (basic check)
	if len(c.TelegramBotToken) < 30 {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN appears invalid (too short)")
	}

	if c.MoviePilotURL == "" {
		return fmt.Errorf("MOVIEPILOT_URL is required")
	}
	if c.MoviePilotAPIKey == "" {
		return fmt.Errorf("MOVIEPILOT_API_KEY is required")
	}
	// Validate API key format
	if len(c.MoviePilotAPIKey) < 10 {
		return fmt.Errorf("MOVIEPILOT_API_KEY appears invalid (too short)")
	}

	// If Emby is configured, validate both URL and key
	if c.EmbyURL != "" && c.EmbyAPIKey == "" {
		return fmt.Errorf("EMBY_API_KEY is required when EMBY_URL is set")
	}

	// Validate limits
	if c.MaxSessions < 1 || c.MaxSessions > 10000 {
		return fmt.Errorf("MAX_SESSIONS must be between 1 and 10000")
	}
	if c.MaxSessionAge < 1 || c.MaxSessionAge > 168 { // max 1 week
		return fmt.Errorf("MAX_SESSION_AGE must be between 1 and 168 hours")
	}

	return nil
}

// loadAdmins loads admin configuration from file
// Supports two formats:
// 1. New: {"admins": {"123456": "Name", "789012": "Name2"}}
// 2. Legacy: {"admin1": 123456, "admin2": 789012} (name -> id)
func (c *Config) loadAdmins() error {
	data, err := os.ReadFile(c.AdminFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Don't create default file - AdminService handles it
			return nil
		}
		return err
	}

	// Try new format first: {"admins": {"id": "name", ...}}
	var newFormat struct {
		Admins map[string]string `json:"admins"`
	}
	if err := json.Unmarshal(data, &newFormat); err == nil {
		// Accept empty admin map (valid format, just no admins yet)
		c.Admins = make(map[int64]string)
		for idStr, name := range newFormat.Admins {
			var id int64
			if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil {
				c.Admins[id] = name
			}
		}
		return nil
	}

	// Try legacy format: {"name1": id1, "name2": id2}
	var legacyAdmins map[string]int64
	if err := json.Unmarshal(data, &legacyAdmins); err == nil {
		// Invert to ID -> name
		c.Admins = make(map[int64]string)
		for name, id := range legacyAdmins {
			c.Admins[id] = name
		}
		return nil
	}

	// If both formats fail, try flat object like {"admin": 0}
	var flatFormat map[string]json.RawMessage
	if err := json.Unmarshal(data, &flatFormat); err == nil {
		c.Admins = make(map[int64]string)
		for key, val := range flatFormat {
			// Skip non-numeric values
			var id int64
			if err := json.Unmarshal(val, &id); err == nil && id > 0 {
				c.Admins[id] = key
			}
		}
		if len(c.Admins) > 0 {
			return nil
		}
	}

	return fmt.Errorf("invalid admin file format")
}

// saveAdmins saves admin configuration to file (legacy format, not recommended)
// Use AdminService.AddAdmin/RemoveAdmin instead
func (c *Config) saveAdmins(admins map[string]int64) error {
	data, err := json.MarshalIndent(admins, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.AdminFile, data, 0600)
}

// IsAdmin checks if a user is an admin
func (c *Config) IsAdmin(userID int64) bool {
	_, exists := c.Admins[userID]
	return exists
}

// GetAdminName returns the admin's name
func (c *Config) GetAdminName(userID int64) string {
	return c.Admins[userID]
}

// ---- Structured accessors ----
// These return sub-configs that group related fields.
// New code should prefer these over direct field access.

// Telegram returns Telegram-specific configuration
func (c *Config) Telegram() TelegramConfig {
	return TelegramConfig{BotToken: c.TelegramBotToken, ChatID: c.TelegramChatID}
}

// MoviePilot returns MoviePilot-specific configuration
func (c *Config) MoviePilot() MoviePilotConfig {
	return MoviePilotConfig{URL: c.MoviePilotURL, APIKey: c.MoviePilotAPIKey, DownloadPath: c.DownloadSavePath}
}

// Emby returns Emby-specific configuration
func (c *Config) Emby() EmbyConfig {
	return EmbyConfig{URL: c.EmbyURL, APIKey: c.EmbyAPIKey}
}

// AI returns AI service configuration
func (c *Config) AI() AIConfig {
	return AIConfig{AnthropicKey: c.AnthropicAPIKey, ZhipuKey: c.ZhipuAPIKey, OpenAIKey: c.OpenAIAPIKey, Enabled: c.EnableAI}
}

// Server returns server configuration
func (c *Config) Server() ServerConfig {
	return ServerConfig{Port: c.ServerPort, Host: c.ServerHost, WebhookURL: c.WebhookURL}
}

// Security returns security configuration
func (c *Config) Security() SecurityConfig {
	return SecurityConfig{
		EnableAPIAuth: c.EnableAPIAuth, APIKeys: c.APIKeys,
		EnableRateLimit: c.EnableRateLimit, EnableIPBlocking: c.EnableIPBlocking,
		RateLimitReq: c.RateLimitRequests, RateLimitWindow: c.RateLimitWindow,
		MaxFailed: c.MaxFailedAttempts, BlockDuration: c.BlockDuration,
	}
}

// Logging returns logging configuration
func (c *Config) Logging() LoggingConfig {
	return LoggingConfig{Level: c.LogLevel, Color: c.LogColor, Prefix: c.LogPrefix}
}

// PTSites returns PT site passkey configuration
func (c *Config) PTSites() PTSitesConfig {
	return PTSitesConfig{
		HDSkyPasskey: c.HDSkyPasskey, ZhuQuePasskey: c.ZhuQuePasskey,
		ZhuQueRSSKey1: c.ZhuQueRSSKey1, ZhuQueRSSKey2: c.ZhuQueRSSKey2,
		MTeamPasskey: c.MTeamPasskey, MTeamRSSUID: c.MTeamRSSUID, MTeamRSSSign: c.MTeamRSSSign,
	}
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvFirst tries multiple environment variable names in order, returns first non-empty value
func getEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var i int
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}
