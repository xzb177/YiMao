package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Config holds all application configuration
type Config struct {
	// Server
	ServerHost string
	ServerPort int
	ReadTimeout  int
	WriteTimeout int

	// Telegram
	TgBotToken       string
	TgChatID         string
	TgWebhookURL     string
	TgAdminIDs       map[int64]string
	TgRateLimit      int
	TgRateBurst      int

	// Jellyseerr
	JellyseerrURL    string
	JellyseerrAPIKey string

	// Paths
	DataDir        string
	LogDir         string
	PIDFile        string

	// Features
	DebugMode      bool
	EnableMonitor  bool
	EnableMetrics  bool

	mu            sync.RWMutex
	watchers      []chan struct{}
}

var (
	globalConfig *Config
	once         sync.Once
)

// Load loads configuration from environment variables and .env file
func Load() (*Config, error) {
	var loadErr error
	once.Do(func() {
		globalConfig = &Config{
			ServerHost:      "0.0.0.0",
			ServerPort:      8080,
			ReadTimeout:    30,
			WriteTimeout:   30,
			TgRateLimit:    30,
			TgRateBurst:    10,
			DataDir:        "./data",
			LogDir:         "/tmp",
			PIDFile:        "/tmp/emby-bot.pid",
			TgAdminIDs:     make(map[int64]string),
		}

		// Load from .env file if exists
		if err := globalConfig.loadFromEnvFile(); err != nil {
			loadErr = fmt.Errorf("failed to load .env file: %w", err)
		}

		// Override with environment variables
		globalConfig.loadFromOSEnv()

		// Validate
		if err := globalConfig.Validate(); err != nil {
			loadErr = err
		}

		// Create directories
		os.MkdirAll(globalConfig.DataDir, 0755)
		os.MkdirAll(globalConfig.LogDir, 0755)
	})

	return globalConfig, loadErr
}

// Get returns the global configuration
func Get() *Config {
	if globalConfig == nil {
		_, _ = Load()
	}
	return globalConfig
}

// loadFromEnvFile loads configuration from .env file
func (c *Config) loadFromEnvFile() error {
	file, err := os.Open(".env")
	if err != nil {
		if os.IsNotExist(err) {
			return nil // .env file is optional
		}
		return err
	}
	defer file.Close()

	// Parse .env file line by line
	// Format: KEY=value or KEY=value # comment
	var content strings.Builder
Scanner := bufio.NewScanner(file)
	for Scanner.Scan() {
		line := Scanner.Text()
		// Remove comments
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			content.WriteString(line + "\n")
		}
	}

	// Parse key=value pairs
	for _, line := range strings.Split(content.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'"`)

		os.Setenv(key, value)
	}

	return Scanner.Err()
}

// loadFromOSEnv loads configuration from OS environment variables
func (c *Config) loadFromOSEnv() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if v := os.Getenv("SERVER_HOST"); v != "" {
		c.ServerHost = v
	}
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.ServerPort = p
		}
	}
	if v := os.Getenv("READ_TIMEOUT"); v != "" {
		if t, err := strconv.Atoi(v); err == nil {
			c.ReadTimeout = t
		}
	}
	if v := os.Getenv("WRITE_TIMEOUT"); v != "" {
		if t, err := strconv.Atoi(v); err == nil {
			c.WriteTimeout = t
		}
	}

	c.TgBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	c.TgChatID = os.Getenv("TELEGRAM_CHAT_ID")
	c.TgWebhookURL = os.Getenv("TELEGRAM_WEBHOOK_URL")

	if v := os.Getenv("ADMINS"); v != "" {
		c.TgAdminIDs = parseAdmins(v)
	}

	if v := os.Getenv("TG_RATE_LIMIT"); v != "" {
		if r, err := strconv.Atoi(v); err == nil {
			c.TgRateLimit = r
		}
	}

	c.JellyseerrURL = os.Getenv("JELLYSEERR_URL")
	c.JellyseerrAPIKey = os.Getenv("JELLYSEERR_API_KEY")

	if v := os.Getenv("DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("LOG_DIR"); v != "" {
		c.LogDir = v
	}
	if v := os.Getenv("PID_FILE"); v != "" {
		c.PIDFile = v
	}

	if v := os.Getenv("DEBUG_MODE"); v != "" {
		c.DebugMode = v == "true" || v == "1"
	}
	if v := os.Getenv("ENABLE_MONITOR"); v != "" {
		c.EnableMonitor = v == "true" || v == "1"
	}
	if v := os.Getenv("ENABLE_METRICS"); v != "" {
		c.EnableMetrics = v == "true" || v == "1"
	}
}

// parseAdmins parses admin list from string
func parseAdmins(s string) map[int64]string {
	admins := make(map[int64]string)
	if s == "" {
		return admins
	}

	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			if id, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64); err == nil {
				admins[id] = strings.TrimSpace(parts[1])
			}
		}
	}
	return admins
}

// Validate validates the configuration
func (c *Config) Validate() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.TgBotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if c.TgChatID == "" {
		return fmt.Errorf("TELEGRAM_CHAT_ID is required")
	}
	if c.JellyseerrURL == "" {
		return fmt.Errorf("JELLYSEERR_URL is required")
	}
	if c.JellyseerrAPIKey == "" {
		return fmt.Errorf("JELLYSEERR_API_KEY is required")
	}

	return nil
}

// GetServerAddr returns the server address
func (c *Config) GetServerAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}

// GetTelegramToken returns the Telegram bot token
func (c *Config) GetTelegramToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TgBotToken
}

// IsAdmin checks if a user is an admin
func (c *Config) IsAdmin(userID int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.TgAdminIDs[userID]
	return exists
}

// GetAdmins returns the admin list
func (c *Config) GetAdmins() map[int64]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[int64]string, len(c.TgAdminIDs))
	for k, v := range c.TgAdminIDs {
		result[k] = v
	}
	return result
}

// AddAdmin adds an admin
func (c *Config) AddAdmin(userID int64, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TgAdminIDs[userID] = name
}

// RemoveAdmin removes an admin
func (c *Config) RemoveAdmin(userID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.TgAdminIDs, userID)
}

// Watch watches for config file changes and notifies watchers
func (c *Config) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					c.mu.Lock()
					c.loadFromEnvFile()
					c.loadFromOSEnv()
					c.mu.Unlock()

					// Notify watchers
					for _, ch := range c.watchers {
						select {
						case ch <- struct{}{}:
						default:
						}
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Printf("Config watcher error: %v\n", err)
			}
		}
	}()

	return watcher.Add(".env")
}

// Subscribe returns a channel that receives notifications on config changes
func (c *Config) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.watchers = append(c.watchers, ch)
	c.mu.Unlock()
	return ch
}
