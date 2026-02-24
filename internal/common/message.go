package common

import (
	"fmt"

	"emby-telegram-bot/internal/services"
)

// Message formatting constants
const (
	MaxOverviewLength = 300
	MaxTitleLength    = 100
)

// MessageBuilder provides common message building patterns
type MessageBuilder struct {
	builder *services.MessageBuilder
}

// NewMessageBuilder creates a new message builder wrapper
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		builder: services.NewMessageBuilder(),
	}
}

// BuildMediaDetail builds a standard media detail message
func (m *MessageBuilder) BuildMediaDetail(title, year string, rating float64, mediaType, overview string, tmdbID int) string {
	m.builder.Bold(title).Newline()
	m.builder.Textf("📅 %s年  ⭐ %.1f分  🏷️ %s", year, rating, mediaType).Newline()
	m.builder.Newline()

	// Truncate overview if needed
	if overview != "" {
		truncated := TruncateText(overview, MaxOverviewLength)
		m.builder.Italic("📖 剧情简介").Newline()
		m.builder.Text(truncated).Newline()
		m.builder.Newline()
	}

	m.builder.Textf("🆔 TMDB ID: %d", tmdbID)

	return m.builder.Build()
}

// TruncateText truncates text to max length, preserving word boundaries
func TruncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	// For Chinese text, use rune count
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}

	return string(runes[:maxLen]) + "..."
}

// FormatMediaType returns the icon and label for a media type
func FormatMediaType(mediaType string) (icon, label string) {
	switch mediaType {
	case "movie", "MOV", "电影":
		return "🎬", "电影"
	case "tv", "TV", "电视剧", "剧集":
		return "📺", "剧集"
	default:
		return "🎬", mediaType
	}
}
