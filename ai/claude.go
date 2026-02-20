// Package ai provides Claude AI integration for intelligent features
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ClaudeClient handles communication with Claude API
type ClaudeClient struct {
	apiKey     string
	apiURL     string
	model      string
	maxTokens  int
	httpClient *http.Client
	cache      *ResponseCache
	mu         sync.RWMutex
	enabled    bool
}

// ClaudeMessage represents a message in the conversation
type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ClaudeRequest represents the API request structure
type ClaudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []ClaudeMessage `json:"messages"`
	System    string          `json:"system,omitempty"`
}

// ClaudeResponse represents the API response structure
type ClaudeResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ResponseCache caches AI responses
type ResponseCache struct {
	data map[string]*CacheEntry
	mu   sync.RWMutex
	ttl  time.Duration
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Response  string
	Timestamp time.Time
	HitCount  int
}

// NewResponseCache creates a new response cache
func NewResponseCache(ttl time.Duration) *ResponseCache {
	cache := &ResponseCache{
		data: make(map[string]*CacheEntry),
		ttl:  ttl,
	}
	// Start cleanup goroutine
	go cache.cleanup()
	return cache
}

// Get retrieves a cached response
func (c *ResponseCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.data[key]
	if !exists || time.Since(entry.Timestamp) > c.ttl {
		return "", false
	}
	entry.HitCount++
	return entry.Response, true
}

// Set stores a response in cache
func (c *ResponseCache) Set(key, response string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = &CacheEntry{
		Response:  response,
		Timestamp: time.Now(),
		HitCount:  0,
	}
}

// cleanup removes expired entries
func (c *ResponseCache) cleanup() {
	ticker := time.NewTicker(time.Minute * 10)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.data {
			if now.Sub(entry.Timestamp) > c.ttl {
				delete(c.data, key)
			}
		}
		c.mu.Unlock()
	}
}

// NewClaudeClient creates a new Claude API client
func NewClaudeClient(apiKey string) *ClaudeClient {
	if apiKey == "" {
		return &ClaudeClient{enabled: false}
	}
	return &ClaudeClient{
		apiKey: apiKey,
		apiURL: "https://api.anthropic.com/v1/messages",
		model:  "claude-3-5-haiku-20241022", // Cost-effective model
		maxTokens: 4096,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache:   NewResponseCache(time.Hour * 24),
		enabled: true,
	}
}

// IsEnabled returns whether the client is enabled
func (c *ClaudeClient) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// Send sends a message to Claude and returns the response
func (c *ClaudeClient) Send(userMessage string, systemPrompt string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("Claude client is not enabled")
	}

	// Check cache
	cacheKey := systemPrompt + "|||" + userMessage
	if cached, hit := c.cache.Get(cacheKey); hit {
		return cached, nil
	}

	messages := []ClaudeMessage{
		{Role: "user", Content: userMessage},
	}

	request := ClaudeRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Messages:  messages,
	}

	if systemPrompt != "" {
		request.System = systemPrompt
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var response ClaudeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("Claude API error: %s", response.Error.Message)
	}

	if len(response.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	result := strings.TrimSpace(response.Content[0].Text)

	// Cache the response
	c.cache.Set(cacheKey, result)

	return result, nil
}

// SendWithConversation sends a message with conversation history
func (c *ClaudeClient) SendWithConversation(messages []ClaudeMessage, systemPrompt string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("Claude client is not enabled")
	}

	request := ClaudeRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Messages:  messages,
	}

	if systemPrompt != "" {
		request.System = systemPrompt
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var response ClaudeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("Claude API error: %s", response.Error.Message)
	}

	if len(response.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	return strings.TrimSpace(response.Content[0].Text), nil
}

// GetModel returns the current model name
func (c *ClaudeClient) GetModel() string {
	return c.model
}

// SetModel changes the model (e.g., "claude-3-5-sonnet-20241022")
func (c *ClaudeClient) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
}

// GetCacheStats returns cache statistics
func (c *ClaudeClient) GetCacheStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	totalHits := 0
	for _, entry := range c.cache.data {
		totalHits += entry.HitCount
	}

	return map[string]interface{}{
		"entries":   len(c.cache.data),
		"totalHits": totalHits,
		"ttlHours":  c.cache.ttl.Hours(),
	}
}
