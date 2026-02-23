package logger

import (
	"fmt"
	"regexp"
	"strings"
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
