package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds application configuration
type Config struct {
	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// MoviePilot
	MoviePilotURL    string
	MoviePilotAPIKey string

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
	ServerPort      string
	ServerHost      string
	WebhookURL      string

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
	MaxSessionAge   int // in hours
	MaxSessions     int

	// Admins
	Admins          map[int64]string // admin ID -> name
}

// Load loads configuration from environment variables and files
func Load() (*Config, error) {
	cfg := &Config{
		TelegramBotToken:    getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:      getEnv("TELEGRAM_CHAT_ID", ""),
		MoviePilotURL:       getEnv("MOVIEPILOT_URL", ""),
		MoviePilotAPIKey:    getEnv("MOVIEPILOT_API_KEY", ""),
		EmbyURL:             getEnv("EMBY_URL", ""),
		EmbyAPIKey:          getEnv("EMBY_API_KEY", ""),
		TMDBAPIKey:          getEnv("TMDB_API_KEY", ""),
		AnthropicAPIKey:     getEnv("ANTHROPIC_API_KEY", ""),
		ZhipuAPIKey:         getEnv("ZHIPU_API_KEY", ""),
		OpenAIAPIKey:        getEnv("OPENAI_API_KEY", ""),
		ServerPort:          getEnv("PORT", "8080"),
		ServerHost:          getEnv("HOST", "0.0.0.0"),
		WebhookURL:          getEnv("WEBHOOK_URL", ""),
		DataDir:             getEnv("DATA_DIR", "/app/data"),
		MaxSessionAge:       getEnvInt("MAX_SESSION_AGE", 24),
		MaxSessions:         getEnvInt("MAX_SESSIONS", 1000),
		EnableAI:            getEnvBool("ENABLE_AI", true),
		EnableTrending:      getEnvBool("ENABLE_TRENDING", true),
		EnableHotTV:         getEnvBool("ENABLE_HOT_TV", true),
		EnableNewMovies:     getEnvBool("ENABLE_NEW_MOVIES", true),
		EnableRandom:        getEnvBool("ENABLE_RANDOM", true),
		Admins:              make(map[int64]string),
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
		fmt.Printf("Warning: failed to load admins: %v\n", err)
	}

	// Validate required config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate validates the configuration
func (c *Config) validate() error {
	if c.TelegramBotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if c.MoviePilotURL == "" {
		return fmt.Errorf("MOVIEPILOT_URL is required")
	}
	if c.MoviePilotAPIKey == "" {
		return fmt.Errorf("MOVIEPILOT_API_KEY is required")
	}
	return nil
}

// loadAdmins loads admin configuration from file
func (c *Config) loadAdmins() error {
	data, err := os.ReadFile(c.AdminFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default admin file
			defaultAdmins := map[string]int64{
				"admin": 0, // Placeholder
			}
			if err := c.saveAdmins(defaultAdmins); err != nil {
				return err
			}
			return nil
		}
		return err
	}

	var admins map[string]int64
	if err := json.Unmarshal(data, &admins); err != nil {
		return err
	}

	// Invert to ID -> name
	c.Admins = make(map[int64]string)
	for name, id := range admins {
		c.Admins[id] = name
	}

	return nil
}

// saveAdmins saves admin configuration to file
func (c *Config) saveAdmins(admins map[string]int64) error {
	data, err := json.MarshalIndent(admins, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.AdminFile, data, 0644)
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

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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
