package richmessage

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// RichMessage represents the InputRichMessage structure for Telegram Bot API 10.1
type RichMessage struct {
	Markdown string `json:"markdown"`
}

// Builder helps build rich messages
type Builder struct {
	content strings.Builder
}

// NewBuilder creates a new rich message builder
func NewBuilder() *Builder {
	return &Builder{}
}

func escapeMarkdownInline(text string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func escapeTableCell(text string) string {
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	return escapeMarkdownInline(text)
}

func escapeTableCells(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = escapeTableCell(v)
	}
	return out
}

// Heading adds a heading (level 1-6)
func (b *Builder) Heading(text string, level int) *Builder {
	// Validate level
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}

	b.content.WriteString(strings.Repeat("#", level))
	b.content.WriteString(" ")
	b.content.WriteString(escapeMarkdownInline(text))
	b.content.WriteString("\n\n")
	return b
}

// Paragraph adds a paragraph
func (b *Builder) Paragraph(text string) *Builder {
	b.content.WriteString(escapeMarkdownInline(text))
	b.content.WriteString("\n\n")
	return b
}

// BoldParagraph adds a bold paragraph
func (b *Builder) BoldParagraph(text string) *Builder {
	b.content.WriteString("**")
	b.content.WriteString(escapeMarkdownInline(text))
	b.content.WriteString("**\n\n")
	return b
}

// Italic adds an italic paragraph
func (b *Builder) Italic(text string) *Builder {
	b.content.WriteString("*")
	b.content.WriteString(escapeMarkdownInline(text))
	b.content.WriteString("*\n\n")
	return b
}

// Table adds a table
func (b *Builder) Table(headers []string, rows [][]string) *Builder {
	// Validate headers
	if len(headers) == 0 {
		return b
	}

	// Header row
	b.content.WriteString("| ")
	b.content.WriteString(strings.Join(escapeTableCells(headers), " | "))
	b.content.WriteString(" |\n")

	// Separator row
	b.content.WriteString("|")
	for range headers {
		b.content.WriteString("------|")
	}
	b.content.WriteString("\n")

	// Data rows
	for _, row := range rows {
		b.content.WriteString("| ")
		b.content.WriteString(strings.Join(escapeTableCells(row), " | "))
		b.content.WriteString(" |\n")
	}

	b.content.WriteString("\n")
	return b
}

// Details adds a collapsible section (using details/summary HTML tags)
// Note: summary and content are HTML-escaped to prevent XSS
func (b *Builder) Details(summary string, content string, isOpen bool) *Builder {
	if isOpen {
		b.content.WriteString("<details open><summary>")
	} else {
		b.content.WriteString("<details><summary>")
	}

	// HTML escape summary to prevent XSS
	b.content.WriteString(html.EscapeString(summary))
	b.content.WriteString("</summary>\n\n")

	// HTML escape content to prevent XSS
	b.content.WriteString(html.EscapeString(content))
	b.content.WriteString("\n\n</details>\n\n")
	return b
}

// Divider adds a divider
func (b *Builder) Divider() *Builder {
	b.content.WriteString("---\n\n")
	return b
}

// Build builds the rich message
func (b *Builder) Build() RichMessage {
	return RichMessage{Markdown: b.content.String()}
}

// ToJSON converts to JSON
func (b *Builder) ToJSON() (string, error) {
	msg := b.Build()
	data, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Reset resets the builder
func (b *Builder) Reset() {
	b.content.Reset()
}

// SendRichMessage sends a rich message via Telegram Bot API
func SendRichMessage(botToken string, chatID int64, msg RichMessage) error {
	// Validate inputs
	if botToken == "" {
		return fmt.Errorf("bot token is empty")
	}
	if chatID == 0 {
		return fmt.Errorf("chat ID is zero")
	}
	if msg.Markdown == "" {
		return fmt.Errorf("rich message markdown is empty")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendRichMessage", botToken)

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"rich_message": msg,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response to check success
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response error: %w", err)
	}

	if ok, okExists := result["ok"].(bool); !okExists || !ok {
		description, _ := result["description"].(string)
		return fmt.Errorf("API returned error: %s", description)
	}

	return nil
}
