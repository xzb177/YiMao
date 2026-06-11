// Package ai provides AI client implementations
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ClaudeClient handles Claude API interactions
type ClaudeClient struct {
	apiKey     string
	baseURL    string
	model      string
	enabled    bool
	httpClient *http.Client
	mu         sync.RWMutex
	cache      *ResponseCache
}

// NewClaudeClient creates a new Claude client.
// Supports OpenAI-compatible proxies via OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL env vars.
// When OPENAI_BASE_URL is set, uses OpenAI chat/completions format instead of Anthropic native format.
func NewClaudeClient(apiKey string) *ClaudeClient {
	// Check for OpenAI-compatible proxy first (higher priority)
	openaiKey := os.Getenv("OPENAI_API_KEY")
	openaiBase := os.Getenv("OPENAI_BASE_URL")
	openaiModel := os.Getenv("OPENAI_MODEL")

	if openaiKey != "" && openaiBase != "" {
		// OpenAI-compatible mode
		if openaiModel == "" {
			openaiModel = "claude-sonnet-4-5"
		}
		// Ensure base URL ends with /chat/completions
		baseURL := strings.TrimRight(openaiBase, "/")
		if !strings.HasSuffix(baseURL, "/chat/completions") {
			baseURL = baseURL + "/chat/completions"
		}
		return &ClaudeClient{
			apiKey:  openaiKey,
			baseURL: baseURL,
			model:   openaiModel,
			enabled: true,
			httpClient: &http.Client{
				Timeout: 60 * time.Second,
			},
			cache: NewResponseCache(30 * time.Minute),
		}
	}

	// Fallback to native Anthropic API
	if apiKey == "" {
		return &ClaudeClient{enabled: false}
	}

	return &ClaudeClient{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1/messages",
		model:   "claude-3-5-sonnet-20241022",
		enabled: true,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: NewResponseCache(30 * time.Minute),
	}
}

// IsEnabled returns whether the client is enabled
func (c *ClaudeClient) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// Send sends a message to Claude and returns the response.
// Automatically detects OpenAI-compatible proxy vs native Anthropic API based on baseURL.
func (c *ClaudeClient) Send(userMessage string, systemPrompt string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("Claude client is not enabled")
	}

	// Check cache
	cacheKey := BuildCacheKey(systemPrompt, []Message{{Role: "user", Content: userMessage}})
	if cached, ok := c.cache.Get(cacheKey); ok {
		return cached, nil
	}

	var response string
	var err error

	// Detect OpenAI-compatible proxy by checking if baseURL contains "chat/completions"
	if strings.Contains(c.baseURL, "chat/completions") {
		response, err = c.sendOpenAI(userMessage, systemPrompt)
	} else {
		response, err = c.sendAnthropic(userMessage, systemPrompt)
	}

	if err != nil {
		return "", err
	}

	c.cache.Set(cacheKey, response)
	return response, nil
}

// sendOpenAI sends request using OpenAI-compatible chat/completions format
func (c *ClaudeClient) sendOpenAI(userMessage string, systemPrompt string) (string, error) {
	messages := []Message{}
	if systemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: userMessage})

	requestBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 4096,
		"messages":   messages,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: status %d, response: %s", resp.StatusCode, string(body))
	}

	// Parse OpenAI response format
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	return result.Choices[0].Message.Content, nil
}

// sendAnthropic sends request using native Anthropic API format
func (c *ClaudeClient) sendAnthropic(userMessage string, systemPrompt string) (string, error) {
	messages := []Message{
		{Role: "user", Content: userMessage},
	}

	requestBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages":   messages,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(jsonBody))
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: status %d, response: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	return result.Content[0].Text, nil
}

// ZhipuClient handles Zhipu AI API interactions
type ZhipuClient struct {
	apiKey     string
	baseURL    string
	model      string
	enabled    bool
	httpClient *http.Client
	mu         sync.RWMutex
	cache      *ResponseCache
}

// NewZhipuClient creates a new Zhipu AI client
func NewZhipuClient(apiKey string) *ZhipuClient {
	if apiKey == "" {
		// Try to get from environment
		apiKey = os.Getenv("ZHIPU_API_KEY")
		if apiKey == "" {
			// Try to read from .env file
			if data, err := os.ReadFile("/root/YiMao/.env"); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, "ZHIPU_API_KEY=") {
						apiKey = strings.TrimSpace(strings.TrimPrefix(line, "ZHIPU_API_KEY="))
						break
					}
				}
			}
		}
	}

	if apiKey == "" {
		return &ZhipuClient{enabled: false}
	}

	return &ZhipuClient{
		apiKey:  apiKey,
		baseURL: "https://open.bigmodel.cn/api/paas/v4/chat/completions",
		model:   "glm-5.1",
		enabled: true,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: NewResponseCache(30 * time.Minute),
	}
}

// IsEnabled returns whether the client is enabled
func (z *ZhipuClient) IsEnabled() bool {
	z.mu.RLock()
	defer z.mu.RUnlock()
	return z.enabled
}

// Send sends a message to Zhipu AI and returns the response
func (z *ZhipuClient) Send(userMessage string, systemPrompt string) (string, error) {
	if !z.IsEnabled() {
		return "", fmt.Errorf("Zhipu client is not enabled")
	}

	// Check cache
	cacheKey := BuildCacheKey(systemPrompt, []Message{{Role: "user", Content: userMessage}})
	if cached, ok := z.cache.Get(cacheKey); ok {
		return cached, nil
	}

	// Prepare request
	messages := []Message{}
	if systemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: userMessage})

	requestBody := map[string]interface{}{
		"model":    z.model,
		"messages": messages,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", z.baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+z.apiKey)

	// Send request
	resp, err := z.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: status %d, response: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	response := result.Choices[0].Message.Content
	z.cache.Set(cacheKey, response)
	return response, nil
}

// GetStats returns client statistics
func (z *ZhipuClient) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"enabled": z.IsEnabled(),
		"model":   z.model,
	}
	if z.cache != nil {
		stats["cache"] = z.cache.GetStats()
	}
	return stats
}

// GetStats returns client statistics
func (c *ClaudeClient) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"enabled": c.IsEnabled(),
		"model":   c.model,
	}
	if c.cache != nil {
		stats["cache"] = c.cache.GetStats()
	}
	return stats
}
