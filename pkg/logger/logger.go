package logger

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Sensitive patterns that should be masked in logs
var sensitivePatterns = []*regexp.Regexp{
	// API keys
	regexp.MustCompile(`(["']?api_?key["']?\s*[:=]\s*["']?)([^"'\s,}]+)(["']?)`),
	regexp.MustCompile(`(Authorization\s*:\s*Bearer\s+)([^\s]+)`),
	regexp.MustCompile(`(token["']?\s*[:=]\s*["']?)([^"'\s,}]+)(["']?)`),

	// Passwords
	regexp.MustCompile(`(["']?password["']?\s*[:=]\s*["']?)([^"'\s,}]+)(["']?)`),
	regexp.MustCompile(`(pwd\s*[:=]\s*)([^\s,}]+)`),

	// Bot tokens (Telegram)
	regexp.MustCompile(`(\d+:[A-Za-z0-9_-]{35})([^\w]|$)`),
	regexp.MustCompile(`(["']?bot_?token["']?\s*[:=]\s*["']?)(\d+:[A-Za-z0-9_-]{35,})(["']?)`),

	// JWT tokens
	regexp.MustCompile(`(eyJ[^.\s]+)\.([^.\s]+)\.([^.\s]+)`),
}

// Sanitize sanitizes log messages by masking sensitive information
func Sanitize(msg string) string {
	result := msg

	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the first few chars and mask the rest
			parts := pattern.FindStringSubmatch(match)
			if len(parts) >= 3 {
				prefix := parts[1]
				sensitive := parts[2]
				suffix := ""
				if len(parts) >= 4 {
					suffix = parts[3]
				}

				// Mask sensitive part: show first 4 chars + asterisks + last 4 chars
				if len(sensitive) > 8 {
					masked := sensitive[:4] + strings.Repeat("*", len(sensitive)-8) + sensitive[len(sensitive)-4:]
					return prefix + masked + suffix
				} else if len(sensitive) > 0 {
					return prefix + strings.Repeat("*", len(sensitive)) + suffix
				}
			}
			// Fallback: replace with asterisks
			return strings.Repeat("*", len(match))
		})
	}

	return result
}

// Sanitizef formats and sanitizes a log message
func Sanitizef(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return Sanitize(msg)
}

// SanitizePayload sanitizes a map/string payload for logging
func SanitizePayload(payload interface{}) string {
	var msg string
	switch v := payload.(type) {
	case string:
		msg = v
	case []byte:
		msg = string(v)
	default:
		msg = fmt.Sprintf("%+v", v)
	}
	return Sanitize(msg)
}

// LogLevel represents the severity level of a log message
type LogLevel int

const (
	// DEBUG level for detailed debugging information
	DEBUG LogLevel = iota
	// INFO level for general informational messages
	INFO
	// WARN level for warning messages
	WARN
	// ERROR level for error messages
	ERROR
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging with levels
type Logger struct {
	mu       sync.Mutex
	level    LogLevel
	prefix   string
	logger   *log.Logger
	useColor bool
}

// Default logger instance
var defaultLogger = &Logger{
	level:    INFO,
	prefix:   "",
	logger:   log.New(os.Stdout, "", log.LstdFlags),
	useColor: false,
}

// SetLevel sets the global log level
func SetLevel(level LogLevel) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// GetLevel returns the current global log level
func GetLevel() LogLevel {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	return defaultLogger.level
}

// SetPrefix sets the global log prefix
func SetPrefix(prefix string) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.prefix = prefix
}

// SetColor enables or disables colored output
func SetColor(enabled bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.useColor = enabled
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	defaultLogger.log(DEBUG, format, args...)
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	defaultLogger.log(INFO, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	defaultLogger.log(WARN, format, args...)
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	defaultLogger.log(ERROR, format, args...)
}

// WithPrefix returns a new logger with the given prefix
func WithPrefix(prefix string) *Logger {
	return &Logger{
		level:    defaultLogger.level,
		prefix:   prefix,
		logger:   defaultLogger.logger,
		useColor: defaultLogger.useColor,
	}
}

// log logs a message at the specified level
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	// Build the log message
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	levelStr := level.String()

	// Add color if enabled
	if l.useColor {
		levelStr = l.colorizeLevel(level)
	}

	// Build prefix with module
	prefix := ""
	if l.prefix != "" {
		prefix = fmt.Sprintf("[%s] ", l.prefix)
	}

	// Format the message and sanitize
	message := fmt.Sprintf(format, args...)
	message = Sanitize(message)

	// Output: [timestamp] LEVEL [prefix] message
	l.logger.Printf("[%s] %s [%s]%s\n", timestamp, levelStr, prefix, message)
}

// colorizeLevel adds ANSI color codes to the level string
func (l *Logger) colorizeLevel(level LogLevel) string {
	const (
		reset  = "\033[0m"
		gray   = "\033[90m"
		green  = "\033[32m"
		yellow = "\033[33m"
		red    = "\033[31m"
	)

	switch level {
	case DEBUG:
		return gray + "DEBUG" + reset
	case INFO:
		return green + "INFO" + reset
	case WARN:
		return yellow + "WARN" + reset
	case ERROR:
		return red + "ERROR" + reset
	default:
		return level.String()
	}
}

// Debug logs a debug message with the logger's prefix
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Info logs an info message with the logger's prefix
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warn logs a warning message with the logger's prefix
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

// Error logs an error message with the logger's prefix
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// InitLogger initializes the global logger with the given settings
func InitLogger(level LogLevel, prefix string, useColor bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	defaultLogger.level = level
	defaultLogger.prefix = prefix
	defaultLogger.useColor = useColor
}

// ParseLevel parses a string into a LogLevel
func ParseLevel(s string) LogLevel {
	switch s {
	case "DEBUG", "debug":
		return DEBUG
	case "INFO", "info":
		return INFO
	case "WARN", "warn", "WARNING", "warning":
		return WARN
	case "ERROR", "error":
		return ERROR
	default:
		return INFO
	}
}
