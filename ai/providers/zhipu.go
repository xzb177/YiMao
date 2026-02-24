// Package providers implements AI provider interfaces
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ResponseCache is defined here to avoid import cycles
// (Same implementation as in claude.go)

// ZhipuProvider implements the Provider interface for Zhipu AI (ChatGLM)
type ZhipuProvider struct {
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
	Model       string            `json:"model"`
	Messages    []ZhipuMessage    `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	TopP        float64           `json:"top_p,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
}

// ZhipuMessage represents a message in the Zhipu API format
type ZhipuMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ZhipuResponse represents the API response structure
type ZhipuResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role            string `json:"role"`
			Content         string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
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

// NewZhipuProvider creates a new Zhipu AI provider
func NewZhipuProvider(apiKey string) *ZhipuProvider {
	if apiKey == "" {
		return &ZhipuProvider{enabled: false}
	}
	return &ZhipuProvider{
		apiKey: apiKey,
		apiURL: "https://open.bigmodel.cn/api/paas/v4/chat/completions",
		model:  "glm-4-flash",
		maxTokens: 8192,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		cache:   NewResponseCache(time.Hour * 24),
		enabled: true,
	}
}

// Name returns the provider name
func (p *ZhipuProvider) Name() string {
	return "zhipu"
}

// IsEnabled returns whether the provider is enabled
func (p *ZhipuProvider) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled
}

// MaxTokens returns the maximum tokens supported
func (p *ZhipuProvider) MaxTokens() int {
	return p.maxTokens
}

// Send sends a non-streaming request
func (p *ZhipuProvider) Send(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if !p.IsEnabled() {
		return nil, fmt.Errorf("zhipu provider is not enabled")
	}

	// Build messages from request
	messages := make([]ZhipuMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, ZhipuMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append([]ZhipuMessage{{Role: "system", Content: req.SystemPrompt}}, messages...)
	}

	// Check cache for simple requests
	if len(messages) <= 2 {
		cacheKey := p.buildCacheKey(messages)
		if cached, hit := p.cache.Get(cacheKey); hit {
			return &ChatResponse{
				Content: cached,
				Usage: Usage{
					TotalTokens: EstimateTokens(cached),
				},
			}, nil
		}
	}

	zhipuReq := ZhipuRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   p.maxTokens,
		Temperature: req.Temperature,
		TopP:        0.95,
		Stream:      false,
	}

	if zhipuReq.Temperature == 0 {
		zhipuReq.Temperature = 0.8
	}

	jsonData, err := json.Marshal(zhipuReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[Zhipu] Request failed: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Zhipu] Failed to read response: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[Zhipu] Response status: %d", resp.StatusCode)

	var response ZhipuResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("zhipu error: %s", response.Error.Message)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("empty response from zhipu")
	}

	// Handle reasoning_content for GLM-5 models
	content := strings.TrimSpace(response.Choices[0].Message.ReasoningContent)
	if content == "" {
		content = strings.TrimSpace(response.Choices[0].Message.Content)
	}
	if content == "" {
		return nil, fmt.Errorf("empty content in zhipu response")
	}

	// Cache simple responses
	if len(messages) <= 2 {
		p.cache.Set(p.buildCacheKey(messages), content)
	}

	return &ChatResponse{
		Content: content,
		Usage: Usage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
		Model:        p.model,
		FinishReason: response.Choices[0].FinishReason,
	}, nil
}

// Stream sends a streaming request
func (p *ZhipuProvider) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	if !p.IsEnabled() {
		return nil, fmt.Errorf("zhipu provider is not enabled")
	}

	// Build messages from request
	messages := make([]ZhipuMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, ZhipuMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append([]ZhipuMessage{{Role: "system", Content: req.SystemPrompt}}, messages...)
	}

	zhipuReq := ZhipuRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   p.maxTokens,
		Temperature: req.Temperature,
		TopP:        0.95,
		Stream:      true,
	}

	if zhipuReq.Temperature == 0 {
		zhipuReq.Temperature = 0.8
	}

	jsonData, err := json.Marshal(zhipuReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[Zhipu] Stream request failed: %v", err)
		return nil, fmt.Errorf("failed to send stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("stream request failed with status: %d", resp.StatusCode)
	}

	chunkChan := make(chan StreamChunk, 16)

	go p.processStream(ctx, resp.Body, chunkChan)

	return chunkChan, nil
}

// processStream processes the SSE stream from Zhipu API
func (p *ZhipuProvider) processStream(ctx context.Context, body io.ReadCloser, chunkChan chan<- StreamChunk) {
	defer close(chunkChan)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			chunkChan <- StreamChunk{Err: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Check for SSE data prefix
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// Extract JSON data
		data := strings.TrimPrefix(line, "data: ")

		// Check for stream end
		if data == "[DONE]" {
			chunkChan <- StreamChunk{Done: true}
			return
		}

		// Parse SSE chunk
		var response ZhipuResponse
		if err := json.Unmarshal([]byte(data), &response); err != nil {
			log.Printf("[Zhipu] Failed to parse stream chunk: %v", err)
			continue
		}

		if response.Error != nil {
			chunkChan <- StreamChunk{Err: fmt.Errorf("zhipu stream error: %s", response.Error.Message)}
			return
		}

		if len(response.Choices) == 0 {
			continue
		}

		choice := response.Choices[0]

		// Handle delta content
		content := choice.Delta.Content
		reasoning := choice.Delta.ReasoningContent

		// For GLM-5, use reasoning_content if available
		if reasoning != "" {
			content = reasoning
		}

		chunk := StreamChunk{
			Content:   content,
			Reasoning: reasoning,
		}

		if choice.FinishReason != "" {
			chunk.Done = true
		}

		chunkChan <- chunk
	}

	if err := scanner.Err(); err != nil {
		chunkChan <- StreamChunk{Err: fmt.Errorf("stream scan error: %w", err)}
	}
}

// SetModel changes the model
func (p *ZhipuProvider) SetModel(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = model
}

// GetModel returns the current model
func (p *ZhipuProvider) GetModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// buildCacheKey creates a cache key from messages
func (p *ZhipuProvider) buildCacheKey(messages []ZhipuMessage) string {
	var parts []string
	for _, m := range messages {
		parts = append(parts, m.Role+":"+m.Content)
	}
	return strings.Join(parts, "|||")
}
