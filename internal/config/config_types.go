package config

// Sub-config types for structured access.
// These provide clean grouping without breaking existing flat field access.
// Future migration: callers can switch from cfg.TelegramBotToken to cfg.Telegram.BotToken.

// TelegramConfig holds Telegram Bot settings
type TelegramConfig struct {
	BotToken string
	ChatID   string
}

// MoviePilotConfig holds MoviePilot API settings
type MoviePilotConfig struct {
	URL          string
	APIKey       string
	DownloadPath string
}

// EmbyConfig holds Emby/Jellyfin API settings
type EmbyConfig struct {
	URL    string
	APIKey string
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port       string
	Host       string
	WebhookURL string
}

// SecurityConfig holds security-related settings
type SecurityConfig struct {
	EnableAPIAuth    bool
	APIKeys          map[string]string
	EnableRateLimit  bool
	EnableIPBlocking bool
	RateLimitReq     int // requests per window
	RateLimitWindow  int // window duration in seconds
	MaxFailed        int // failed attempts before IP block
	BlockDuration    int // block duration in minutes
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string
	Color  bool
	Prefix string
}

// PTSitesConfig holds PT site passkeys
type PTSitesConfig struct {
	HDSkyPasskey  string
	ZhuQuePasskey string
	ZhuQueRSSKey1 string
	ZhuQueRSSKey2 string
	MTeamPasskey  string
	MTeamRSSUID   string
	MTeamRSSSign  string
}
