package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MessageEditor handles sending and editing Telegram messages
type MessageEditor struct {
	botToken  string
	httpClient *http.Client
	mu        sync.RWMutex

	// Message tracking for editing
	lastMessages map[int64]int64 // chatID -> messageID
}

// NewMessageEditor creates a new message editor
func NewMessageEditor() *MessageEditor {
	return &MessageEditor{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		lastMessages: make(map[int64]int64),
	}
}

// SetBotToken sets the bot token
func (e *MessageEditor) SetBotToken(token string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.botToken = token
}

// SendMessage sends a new message
func (e *MessageEditor) SendMessage(chatID int64, text string, keyboard [][]map[string]string) (int64, error) {
	return e.sendOrEditMessage(chatID, 0, text, keyboard, false)
}

// EditMessage edits an existing message
func (e *MessageEditor) EditMessage(chatID, messageID int64, text string, keyboard [][]map[string]string) error {
	_, err := e.sendOrEditMessage(chatID, messageID, text, keyboard, true)
	return err
}

// sendOrEditMessage sends a new message or edits an existing one
func (e *MessageEditor) sendOrEditMessage(chatID, messageID int64, text string, keyboard [][]map[string]string, edit bool) (int64, error) {
	e.mu.RLock()
	botToken := e.botToken
	e.mu.RUnlock()

	if botToken == "" {
		return 0, fmt.Errorf("bot token not set")
	}

	// Check message length and split if needed
	messages := e.splitLongMessage(text)

	var replyMarkup interface{}
	if len(keyboard) > 0 {
		replyMarkup = map[string]interface{}{
			"inline_keyboard": keyboard,
		}
	}

	var resultMessageID int64

	for i, msgText := range messages {
		var url string
		var payload map[string]interface{}

		if edit && messageID > 0 && i == 0 {
			// Edit the original message with the first part
			url = fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", botToken)
			payload = map[string]interface{}{
				"chat_id":    strconv.FormatInt(chatID, 10),
				"message_id": messageID,
				"text":       msgText,
			}
			if replyMarkup != nil {
				payload["reply_markup"] = replyMarkup
			}
		} else if edit && messageID > 0 && i > 0 {
			// Send additional parts as new messages
			url = fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
			payload = map[string]interface{}{
				"chat_id": strconv.FormatInt(chatID, 10),
				"text":    msgText,
			}
		} else {
			// Send new message
			url = fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
			payload = map[string]interface{}{
				"chat_id": strconv.FormatInt(chatID, 10),
				"text":    msgText,
			}
			if replyMarkup != nil {
				payload["reply_markup"] = replyMarkup
			}
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal payload: %w", err)
		}

		log.Printf("[Editor] Sending message to chat %d, edit=%v, text_len=%d", chatID, edit, len(msgText))

		resp, err := e.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("[Editor] Request failed: %v", err)
			return 0, fmt.Errorf("failed to send request: %w", err)
		}
		defer func() {
			io.Copy(io.Discard, resp.Body) // Drain body before closing
			resp.Body.Close()
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("[Editor] Failed to read response: %v", err)
			return 0, fmt.Errorf("failed to read response: %w", err)
		}

		log.Printf("[Editor] Response status: %d, body: %s", resp.StatusCode, string(body))

		if resp.StatusCode != http.StatusOK {
			log.Printf("[Editor] API error: %s", string(body))
			return 0, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		// Parse response to get message ID
		var result struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
			Result      struct {
				MessageID int64 `json:"message_id"`
			} `json:"result"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			log.Printf("[Editor] Failed to parse response: %v, body: %s", err, string(body))
			// Don't continue on parse error, return the error instead
			return 0, fmt.Errorf("failed to parse API response: %w", err)
		}

		if !result.OK {
			return 0, fmt.Errorf("API error: %s", result.Description)
		}

		resultMessageID = result.Result.MessageID
	}

	// Track the message ID for future edits
	if resultMessageID > 0 {
		e.mu.Lock()
		e.lastMessages[chatID] = resultMessageID
		e.mu.Unlock()
	}

	return resultMessageID, nil
}

// DeleteMessage deletes a message
func (e *MessageEditor) DeleteMessage(chatID, messageID int64) error {
	e.mu.RLock()
	botToken := e.botToken
	e.mu.RUnlock()

	if botToken == "" {
		return fmt.Errorf("bot token not set")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteMessage", botToken)
	payload := map[string]interface{}{
		"chat_id":    strconv.FormatInt(chatID, 10),
		"message_id": messageID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := e.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) // Drain body before closing
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Remove from tracking
	e.mu.Lock()
	delete(e.lastMessages, chatID)
	e.mu.Unlock()

	return nil
}

// GetLastMessageID gets the last sent message ID for a chat
func (e *MessageEditor) GetLastMessageID(chatID int64) int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastMessages[chatID]
}

// AnswerCallback answers a callback query
func (e *MessageEditor) AnswerCallback(callbackID, text string, showAlert bool) error {
	e.mu.RLock()
	botToken := e.botToken
	e.mu.RUnlock()

	if botToken == "" {
		return fmt.Errorf("bot token not set")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", botToken)
	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}

	if text != "" {
		payload["text"] = text
		payload["show_alert"] = showAlert
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := e.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) // Drain body before closing
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// splitLongMessage splits a long message into smaller chunks
// Telegram limit is 4096 characters for text messages
func (e *MessageEditor) splitLongMessage(text string) []string {
	const maxLen = 4000 // Leave some margin

	if len(text) <= maxLen {
		return []string{text}
	}

	var messages []string
	lines := e.splitLines(text)

	currentMsg := ""
	for _, line := range lines {
		if len(currentMsg)+len(line)+1 <= maxLen {
			if currentMsg != "" {
				currentMsg += "\n"
			}
			currentMsg += line
		} else {
			if currentMsg != "" {
				messages = append(messages, currentMsg)
			}
			currentMsg = line
		}
	}

	if currentMsg != "" {
		messages = append(messages, currentMsg)
	}

	return messages
}

// splitLines splits text into lines while preserving code blocks
func (e *MessageEditor) splitLines(text string) []string {
	var lines []string
	var currentLine strings.Builder

	inCodeBlock := false
	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Check for code block
		if r == '`' && i+2 < len(runes) && runes[i+1] == '`' && runes[i+2] == '`' {
			inCodeBlock = !inCodeBlock
			currentLine.WriteRune(r)
			currentLine.WriteRune('`')
			currentLine.WriteRune('`')
			i += 2
			continue
		}

		if r == '\n' {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
		} else {
			currentLine.WriteRune(r)
		}
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	return lines
}

// SendMediaMessage sends a message with a photo
func (e *MessageEditor) SendMediaMessage(chatID int64, photoURL, caption string, keyboard [][]map[string]string) (int64, error) {
	e.mu.RLock()
	botToken := e.botToken
	e.mu.RUnlock()

	if botToken == "" {
		return 0, fmt.Errorf("bot token not set")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", botToken)
	payload := map[string]interface{}{
		"chat_id":  strconv.FormatInt(chatID, 10),
		"photo":    photoURL,
		"caption":  caption,
	}

	if len(keyboard) > 0 {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": keyboard,
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := e.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if !result.OK {
		return 0, fmt.Errorf("API error")
	}

	return result.Result.MessageID, nil
}

// EditMediaMessage edits a media message
func (e *MessageEditor) EditMediaMessage(chatID, messageID int64, photoURL, caption string, keyboard [][]map[string]string) error {
	e.mu.RLock()
	botToken := e.botToken
	e.mu.RUnlock()

	if botToken == "" {
		return fmt.Errorf("bot token not set")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageMedia", botToken)
	payload := map[string]interface{}{
		"chat_id":    strconv.FormatInt(chatID, 10),
		"message_id": messageID,
		"media": map[string]interface{}{
			"type":  "photo",
			"media": photoURL,
			"caption": caption,
		},
	}

	if len(keyboard) > 0 {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": keyboard,
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := e.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) // Drain body before closing
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
