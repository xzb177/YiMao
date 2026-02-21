// Package main provides security monitoring for media content
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MediaSecurityChecker monitors media content for Emby links
type MediaSecurityChecker struct {
	botToken string
	mu       sync.RWMutex
}

// NewMediaSecurityChecker creates a new security checker
func NewMediaSecurityChecker() *MediaSecurityChecker {
	return &MediaSecurityChecker{}
}

// SetBotToken sets the bot token for accessing Telegram API
func (m *MediaSecurityChecker) SetBotToken(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.botToken = token
}

// CheckUpdate checks an update for media with Emby links
// Returns (shouldDelete, warningMessage)
func (m *MediaSecurityChecker) CheckUpdate(rawJSON []byte) (bool, string) {
	m.mu.RLock()
	token := m.botToken
	m.mu.RUnlock()

	if token == "" {
		log.Printf("[Security] No bot token set")
		return false, ""
	}

	// Parse the update to check for media
	var update struct {
		Message *struct {
			MessageID int64  `json:"message_id"`
			Chat      struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			Photo []struct {
				FileID string `json:"file_id"`
			} `json:"photo"`
			Video *struct {
				FileID string `json:"file_id"`
			} `json:"video"`
			Document *struct {
				FileID   string `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"document"`
			Animation *struct {
				FileID string `json:"file_id"`
			} `json:"animation"`
			Caption string `json:"caption"`
		} `json:"message"`
	}

	if err := json.Unmarshal(rawJSON, &update); err != nil {
		log.Printf("[Security] JSON decode error: %v", err)
		return false, ""
	}

	if update.Message == nil {
		log.Printf("[Security] No message in update")
		return false, ""
	}

	log.Printf("[Security] Checking message %d in chat %d (type: %s)",
		update.Message.MessageID, update.Message.Chat.ID, update.Message.Chat.Type)

	// Only check group/supergroup messages
	if update.Message.Chat.Type == "private" {
		log.Printf("[Security] Skipping private chat message")
		return false, ""
	}

	chatID := update.Message.Chat.ID
	messageID := update.Message.MessageID

	// Check caption first (faster)
	if update.Message.Caption != "" {
		log.Printf("[Security] Caption found: %q", update.Message.Caption)
		if m.containsEmbyLink(update.Message.Caption) {
			log.Printf("[Security] Emby link found in caption!")
			// Delete immediately
			go m.deleteMessage(chatID, messageID)
			return true, "Caption contains Emby link"
		}
	} else {
		log.Printf("[Security] No caption found")
	}

	// Check if there's media content that needs OCR
	var fileID string
	var mediaType string

	if len(update.Message.Photo) > 0 {
		fileID = update.Message.Photo[len(update.Message.Photo)-1].FileID
		mediaType = "图片"
		log.Printf("[Security] Photo detected, starting OCR check...")
	} else if update.Message.Video != nil {
		fileID = update.Message.Video.FileID
		mediaType = "视频"
		log.Printf("[Security] Video detected, starting OCR check...")
	} else if update.Message.Document != nil {
		fileID = update.Message.Document.FileID
		mediaType = "文档"
		log.Printf("[Security] Document detected, starting OCR check...")
	} else if update.Message.Animation != nil {
		fileID = update.Message.Animation.FileID
		mediaType = "动图"
		log.Printf("[Security] Animation detected, starting OCR check...")
	} else {
		log.Printf("[Security] No media content found to check")
		return false, ""
	}

	// Perform OCR asynchronously
	go m.performOCRCheck(chatID, messageID, fileID, mediaType, update.Message.Caption)

	return false, ""
}

// containsEmbyLink checks if text contains Emby links
func (m *MediaSecurityChecker) containsEmbyLink(text string) bool {
	textLower := strings.ToLower(text)

	patterns := []string{
		"emby.oceancloud.asia",
		"emby.oceancloud",
		":8096",
		":8920",
	}

	for _, pattern := range patterns {
		if strings.Contains(textLower, pattern) {
			return true
		}
	}

	// Check URL + port pattern
	if strings.Contains(textLower, "http") {
		if strings.Contains(textLower, ":8096") || strings.Contains(textLower, ":8920") {
			return true
		}
	}

	return false
}

// performOCRCheck performs OCR on media and deletes if Emby link found
func (m *MediaSecurityChecker) performOCRCheck(chatID int64, messageID int64, fileID, mediaType, caption string) {
	// Get file URL
	fileURL, err := m.getFileURL(fileID)
	if err != nil {
		log.Printf("[Security] Failed to get file URL: %v", err)
		return
	}

	// Perform OCR
	text, err := m.performOCR(fileURL)
	if err != nil {
		log.Printf("[Security] OCR failed for %s: %v", mediaType, err)
		return
	}

	// Add caption to OCR text
	if caption != "" {
		text += " " + caption
	}

	log.Printf("[Security] OCR extracted %d chars from %s", len(text), mediaType)

	// Check for Emby links
	if m.containsEmbyLink(text) {
		log.Printf("[Security] Emby link found in media! Deleting message %d", messageID)

		// Delete the message
		m.deleteMessage(chatID, messageID)

		// Send warning
		m.sendWarning(chatID, mediaType)

		// Notify admin
		m.notifyAdmin(chatID, messageID, mediaType, text)
	}
}

// getFileURL gets the download URL for a file
func (m *MediaSecurityChecker) getFileURL(fileID string) (string, error) {
	m.mu.RLock()
	token := m.botToken
	m.mu.RUnlock()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getFile", token)
	payload := map[string]string{
		"file_id": fileID,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	json.Unmarshal(body, &result)

	if !result.OK {
		return "", fmt.Errorf("getFile failed")
	}

	return fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, result.Result.FilePath), nil
}

// performOCR performs OCR on an image using OCR.space API
func (m *MediaSecurityChecker) performOCR(imageURL string) (string, error) {
	ocrURL := "https://api.ocr.space/parse/image"

	formData := fmt.Sprintf("url=%s&language=chs&isOverlayRequired=false&scale=true", imageURL)

	req, err := http.NewRequest("POST", ocrURL, strings.NewReader(formData))
	if err != nil {
		return "", fmt.Errorf("failed to create OCR request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		IsErroredOnProcessing bool `json:"IsErroredOnProcessing"`
		ParsedTexts            []struct {
			ParsedText string `json:"ParsedText"`
		} `json:"ParsedResults"`
		ErrorMessage string `json:"ErrorMessage"`
	}
	json.Unmarshal(body, &result)

	if result.IsErroredOnProcessing {
		return "", fmt.Errorf("OCR error: %s", result.ErrorMessage)
	}

	var textBuilder strings.Builder
	for _, r := range result.ParsedTexts {
		textBuilder.WriteString(r.ParsedText)
	}

	return textBuilder.String(), nil
}

// deleteMessage deletes a message
func (m *MediaSecurityChecker) deleteMessage(chatID, messageID int64) {
	m.mu.RLock()
	token := m.botToken
	m.mu.RUnlock()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		log.Printf("[Security] Delete request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Printf("[Security] Successfully deleted message %d", messageID)
	} else {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Security] Delete failed with status %d: %s", resp.StatusCode, string(body))
	}
}

// sendWarning sends a warning message to the chat
func (m *MediaSecurityChecker) sendWarning(chatID int64, mediaType string) {
	m.mu.RLock()
	token := m.botToken
	m.mu.RUnlock()

	warning := fmt.Sprintf("🚨 **安全警告**\n\n")
	warning += fmt.Sprintf("检测到%s中包含服务器链接，该消息已被删除！\n\n", mediaType)
	warning += "⚠️ 严禁分享包含服务器链接的图片/视频！"

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       warning,
		"parse_mode": "Markdown",
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonData)))
	if resp != nil {
		io.Copy(io.Discard, resp.Body) // Drain body before closing
		resp.Body.Close()
	}
	_ = err // Ignore error in warning function
}

// notifyAdmin notifies admin about security breach
func (m *MediaSecurityChecker) notifyAdmin(chatID int64, messageID int64, mediaType, content string) {
	// Notify admin (夏夜 - ID: 5779291957)
	adminID := int64(5779291957)

	m.mu.RLock()
	token := m.botToken
	m.mu.RUnlock()

	msg := fmt.Sprintf("🚨 **链接泄露警报**\n\n")
	msg += fmt.Sprintf("📍 群组: %d\n", chatID)
	msg += fmt.Sprintf("📝 消息: %d\n", messageID)
	msg += fmt.Sprintf("📎 类型: %s\n", mediaType)
	// Safely truncate OCR content
	ocrPreview := SafeStringSlice(content, Min(200, len(content)))
	msg += fmt.Sprintf("🔍 OCR内容: %s\n", ocrPreview)
	msg += "\n✅ 已自动删除该消息"

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id": adminID,
		"text":    msg,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonData)))
	if resp != nil {
		io.Copy(io.Discard, resp.Body) // Drain body before closing
		resp.Body.Close()
	}
	_ = err // Ignore error in notification function

	log.Printf("[Security] Notified admin about media link leak")
}
