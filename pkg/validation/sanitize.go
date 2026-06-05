package validation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	// Common malicious patterns
	sqlInjectionPattern  = regexp.MustCompile(`(?i)(union\s+select|drop\s+table|delete\s+from|insert\s+into|--|;|\/\*|\*\/)`)
	scriptPattern        = regexp.MustCompile(`(?i)<script|javascript:|onerror|onload|onclick`)
	pathTraversalPattern = regexp.MustCompile(`\.\.\/|\.\.\\`)
)

// MaxLengths defines maximum allowed lengths for various inputs
const (
	MaxUsernameLength = 100
	MaxPasswordLength = 200
	MaxSearchLength   = 200
	MaxCallbackLength = 500
	MaxTextLength     = 4096 // Telegram message limit
)

// SanitizeInput removes potentially malicious characters from input
func SanitizeInput(input string) string {
	if input == "" {
		return ""
	}

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove control characters except newlines and tabs
	var result strings.Builder
	for _, r := range input {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// SanitizeUsername sanitizes username input
func SanitizeUsername(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

	username = SanitizeInput(username)

	// Limit length
	if len(username) > MaxUsernameLength {
		username = username[:MaxUsernameLength]
	}

	// Check for malicious patterns
	if sqlInjectionPattern.MatchString(username) {
		return "", fmt.Errorf("username contains invalid characters")
	}
	if scriptPattern.MatchString(username) {
		return "", fmt.Errorf("username contains invalid characters")
	}

	return username, nil
}

// SanitizePassword sanitizes password (more lenient - allows special chars)
func SanitizePassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	password = SanitizeInput(password)

	// Limit length (hashing will handle this, but we trim for logging/safety)
	if len(password) > MaxPasswordLength {
		password = password[:MaxPasswordLength]
	}

	// Only remove null bytes and control characters for passwords
	// Don't check for SQL injection since password values are parameterized

	return password, nil
}

// SanitizeSearchQuery sanitizes search queries
func SanitizeSearchQuery(query string) string {
	if query == "" {
		return ""
	}

	query = SanitizeInput(query)

	// Limit length
	if len(query) > MaxSearchLength {
		query = query[:MaxSearchLength]
	}

	// Remove potential path traversal
	query = pathTraversalPattern.ReplaceAllString(query, "")

	return query
}

// SanitizeCallbackData sanitizes callback data
func SanitizeCallbackData(data string) string {
	if data == "" {
		return ""
	}

	data = SanitizeInput(data)

	// Limit length
	if len(data) > MaxCallbackLength {
		data = data[:MaxCallbackLength]
	}

	return data
}

// SanitizeMessageText sanitizes message text
func SanitizeMessageText(text string) string {
	if text == "" {
		return ""
	}

	text = SanitizeInput(text)

	// Limit length to Telegram's max
	if len(text) > MaxTextLength {
		text = text[:MaxTextLength]
	}

	return text
}

// ValidateUsername checks if username meets minimum requirements
func ValidateUsername(username string) error {
	username = SanitizeInput(username)

	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	if len(username) < 2 {
		return fmt.Errorf("username must be at least 2 characters")
	}

	if len(username) > MaxUsernameLength {
		return fmt.Errorf("username too long (max %d characters)", MaxUsernameLength)
	}

	return nil
}

// ContainsMaliciousPatterns checks if input contains malicious patterns
func ContainsMaliciousPatterns(input string) bool {
	if input == "" {
		return false
	}

	inputLower := strings.ToLower(input)

	// Check for common attack patterns
	maliciousPatterns := []string{
		"<script",
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
		"eval(",
		"alert(",
		"union select",
		"drop table",
		"delete from",
		"insert into",
		"update set",
		"../",
		"..\\",
	}

	for _, pattern := range maliciousPatterns {
		if strings.Contains(inputLower, pattern) {
			return true
		}
	}

	return false
}

// SafeTruncate safely truncates a string to max length
func SafeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Try to truncate at a word boundary
	if maxLen > 10 {
		lastSpace := strings.LastIndex(s[:maxLen], " ")
		if lastSpace > maxLen/2 {
			return s[:lastSpace] + "..."
		}
	}

	return s[:maxLen] + "..."
}
