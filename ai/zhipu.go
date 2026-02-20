// Package ai provides Zhipu AI (ChatGLM) integration
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ZhipuClient represents the Zhipu AI API client
type ZhipuClient struct {
	apiKey     string
	apiURL     string
	model      string
	maxTokens  int
	httpClient *http.Client
	cache      *ResponseCache
	mu         sync.RWMutex
	enabled    bool
}

// ZhipuRequest represents the API request structure
type ZhipuRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// ZhipuResponse represents the API response structure
// 智谱 AI API 返回格式参考：https://open.bigmodel.cn/doc/api#chat
// GLM-5 使用 reasoning_content 字段返回内容
type ZhipuResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role            string `json:"role"`
			Content         string `json:"content"`
			ReasoningContent string `json:"reasoning_content"` // GLM-5 使用此字段
		} `json:"message"`
		Delta struct {
			Content         string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// NewZhipuClient creates a new Zhipu AI client
func NewZhipuClient(apiKey string) *ZhipuClient {
	if apiKey == "" {
		return &ZhipuClient{enabled: false}
	}
	return &ZhipuClient{
		apiKey: apiKey,
		apiURL: "https://open.bigmodel.cn/api/paas/v4/chat/completions", // 标准端点
		model:  "glm-4-flash", // 使用 GLM-4-Flash (快速且免费)
		maxTokens: 8192, // 增加到 8K 以获得更高质量回复
		httpClient: &http.Client{
			Timeout: 45 * time.Second, // 增加超时以支持更复杂任务
		},
		cache:   NewResponseCache(time.Hour * 24),
		enabled: true,
	}
}

// IsEnabled returns whether the client is enabled
func (c *ZhipuClient) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// Send sends a message to Zhipu AI and returns the response
func (c *ZhipuClient) Send(userMessage string, systemPrompt string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("Zhipu client is not enabled")
	}

	// Safe string truncation for multi-byte characters
	maxDisplayLen := 50
	displayMsg := userMessage
	if len(userMessage) > maxDisplayLen {
		// Truncate at rune boundary to avoid splitting multi-byte characters
		runes := []rune(userMessage)
		if len(runes) > maxDisplayLen {
			displayMsg = string(runes[:maxDisplayLen]) + "..."
		}
	}
	fmt.Printf("[ZhipuAI] Sending message: %s\n", displayMsg)

	// Check cache
	cacheKey := systemPrompt + "|||" + userMessage
	if cached, hit := c.cache.Get(cacheKey); hit {
		fmt.Printf("[ZhipuAI] Cache hit\n")
		return cached, nil
	}

	messages := []Message{
		{Role: "user", Content: userMessage},
	}

	if systemPrompt != "" {
		messages = append([]Message{{Role: "system", Content: systemPrompt}}, messages...)
	}

	request := ZhipuRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   c.maxTokens,
		Temperature: 0.8, // 提高创意性
		TopP:        0.95, // 提高多样性
		Stream:      false,
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
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[ZhipuAI] Request failed: %v", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[ZhipuAI] Failed to read response: %v", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[ZhipuAI] Response status: %d", resp.StatusCode)

	var response ZhipuResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("Zhipu AI error: %s", response.Error.Message)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("empty response from Zhipu AI")
	}

	// GLM-5 使用 reasoning_content 字段，普通模型使用 content 字段
	// 优先使用 reasoning_content，如果为空则使用 content
	result := strings.TrimSpace(response.Choices[0].Message.ReasoningContent)
	if result == "" {
		result = strings.TrimSpace(response.Choices[0].Message.Content)
	}
	if result == "" {
		return "", fmt.Errorf("empty content in Zhipu AI response")
	}

	// Cache the response
	c.cache.Set(cacheKey, result)

	return result, nil
}

// SendWithConversation sends a message with conversation history
func (c *ZhipuClient) SendWithConversation(messages []Message, systemPrompt string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("Zhipu client is not enabled")
	}

	if systemPrompt != "" {
		messages = append([]Message{{Role: "system", Content: systemPrompt}}, messages...)
	}

	request := ZhipuRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   c.maxTokens,
		Temperature: 0.8, // 提高创意性
		TopP:        0.95, // 提高多样性
		Stream:      false,
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
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[ZhipuAI] Request failed: %v", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[ZhipuAI] Failed to read response: %v", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[ZhipuAI] Response status: %d", resp.StatusCode)

	var response ZhipuResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("Zhipu AI error: %s", response.Error.Message)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("empty response from Zhipu AI")
	}

	// GLM-5 使用 reasoning_content 字段，普通模型使用 content 字段
	// 优先使用 reasoning_content，如果为空则使用 content
	result := strings.TrimSpace(response.Choices[0].Message.ReasoningContent)
	if result == "" {
		result = strings.TrimSpace(response.Choices[0].Message.Content)
	}
	if result == "" {
		return "", fmt.Errorf("empty content in Zhipu AI response")
	}

	return result, nil
}

// GetModel returns the current model name
func (c *ZhipuClient) GetModel() string {
	return c.model
}

// SetModel changes the model
func (c *ZhipuClient) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
}
