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

// ResponseCache caches AI responses to reduce API calls
type ResponseCache struct {
	data      map[string]*CacheEntry
	mu        sync.RWMutex
	ttl       time.Duration
	stopChan  chan struct{}
	stopped   sync.Once
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
		data:     make(map[string]*CacheEntry),
		ttl:      ttl,
		stopChan: make(chan struct{}),
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

// cleanup removes expired entries periodically
func (c *ResponseCache) cleanup() {
	ticker := time.NewTicker(time.Minute * 10)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for key, entry := range c.data {
				if now.Sub(entry.Timestamp) > c.ttl {
					delete(c.data, key)
				}
			}
			c.mu.Unlock()
		case <-c.stopChan:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (c *ResponseCache) Stop() {
	c.stopped.Do(func() {
		close(c.stopChan)
	})
}

// ClaudeProvider implements the Provider interface for Anthropic Claude
type ClaudeProvider struct {
	apiKey     string
	apiURL     string
	model      string
	maxTokens  int
	httpClient *http.Client
	cache      *ResponseCache
	mu         sync.RWMutex
	enabled    bool
}

// ClaudeMessage represents a message in the Claude API format
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
	Stream    bool            `json:"stream,omitempty"`
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
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ClaudeStreamEvent represents a single event in the streaming response
type ClaudeStreamEvent struct {
	Type         string `json:"type"`
	Message      *struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message,omitempty"`
	Delta         *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	Usage        *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Error        *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(apiKey string) *ClaudeProvider {
	if apiKey == "" {
		return &ClaudeProvider{enabled: false}
	}
	return &ClaudeProvider{
		apiKey: apiKey,
		apiURL: "https://api.anthropic.com/v1/messages",
		model:  "claude-3-5-haiku-20241022",
		maxTokens: 4096,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		cache:   NewResponseCache(time.Hour * 24),
		enabled: true,
	}
}

// Name returns the provider name
func (p *ClaudeProvider) Name() string {
	return "claude"
}

// IsEnabled returns whether the provider is enabled
func (p *ClaudeProvider) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled
}

// MaxTokens returns the maximum tokens supported
func (p *ClaudeProvider) MaxTokens() int {
	return p.maxTokens
}

// Send sends a non-streaming request
func (p *ClaudeProvider) Send(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if !p.IsEnabled() {
		return nil, fmt.Errorf("claude provider is not enabled")
	}

	// Build messages from request
	messages := make([]ClaudeMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, ClaudeMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Check cache for simple requests
	if len(messages) <= 2 {
		cacheKey := p.buildCacheKey(messages, req.SystemPrompt)
		if cached, hit := p.cache.Get(cacheKey); hit {
			return &ChatResponse{
				Content: cached,
				Usage: Usage{
					TotalTokens: EstimateTokens(cached),
				},
			}, nil
		}
	}

	claudeReq := ClaudeRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages:  messages,
		System:    req.SystemPrompt,
		Stream:    false,
	}

	if claudeReq.MaxTokens == 0 {
		claudeReq.MaxTokens = p.maxTokens
	}

	jsonData, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[Claude] Request failed: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Claude] Failed to read response: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[Claude] Response status: %d", resp.StatusCode)

	var response ClaudeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("claude error: %s", response.Error.Message)
	}

	if len(response.Content) == 0 {
		return nil, fmt.Errorf("empty response from claude")
	}

	content := strings.TrimSpace(response.Content[0].Text)

	// Cache simple responses
	if len(messages) <= 2 {
		p.cache.Set(p.buildCacheKey(messages, req.SystemPrompt), content)
	}

	return &ChatResponse{
		Content: content,
		Usage: Usage{
			PromptTokens:     response.Usage.InputTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.InputTokens + response.Usage.OutputTokens,
		},
		Model:        p.model,
		FinishReason: response.StopReason,
	}, nil
}

// Stream sends a streaming request
func (p *ClaudeProvider) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	if !p.IsEnabled() {
		return nil, fmt.Errorf("claude provider is not enabled")
	}

	// Build messages from request
	messages := make([]ClaudeMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, ClaudeMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	claudeReq := ClaudeRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages:  messages,
		System:    req.SystemPrompt,
		Stream:    true,
	}

	if claudeReq.MaxTokens == 0 {
		claudeReq.MaxTokens = p.maxTokens
	}

	jsonData, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[Claude] Stream request failed: %v", err)
		return nil, fmt.Errorf("failed to send stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("stream request failed with status %d: %s", resp.StatusCode, string(body))
	}

	chunkChan := make(chan StreamChunk, 16)

	go p.processStream(ctx, resp.Body, chunkChan)

	return chunkChan, nil
}

// processStream processes the SSE stream from Claude API
func (p *ClaudeProvider) processStream(ctx context.Context, body io.ReadCloser, chunkChan chan<- StreamChunk) {
	defer close(chunkChan)
	defer body.Close()

	scanner := newSSEScanner(body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			chunkChan <- StreamChunk{Err: ctx.Err()}
			return
		default:
		}

		data := scanner.Bytes()
		if len(data) == 0 {
			continue
		}

		// Parse stream event
		var event ClaudeStreamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			log.Printf("[Claude] Failed to parse stream event: %v", err)
			continue
		}

		if event.Error != nil {
			chunkChan <- StreamChunk{Err: fmt.Errorf("claude stream error: %s", event.Error.Message)}
			return
		}

		switch event.Type {
		case "content_block_delta":
			// Content chunk
			if event.Delta != nil && event.Delta.Text != "" {
				chunkChan <- StreamChunk{
					Content: event.Delta.Text,
				}
			}

		case "message_stop":
			// Stream complete
			chunkChan <- StreamChunk{Done: true}
			return

		case "error":
			if event.Error != nil {
				chunkChan <- StreamChunk{Err: fmt.Errorf("claude stream error: %s", event.Error.Message)}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		chunkChan <- StreamChunk{Err: fmt.Errorf("stream scan error: %w", err)}
	}
}

// SetModel changes the model
func (p *ClaudeProvider) SetModel(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = model
}

// GetModel returns the current model
func (p *ClaudeProvider) GetModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// buildCacheKey creates a cache key from messages and system prompt
func (p *ClaudeProvider) buildCacheKey(messages []ClaudeMessage, systemPrompt string) string {
	var parts []string
	for _, m := range messages {
		parts = append(parts, m.Role+":"+m.Content)
	}
	key := strings.Join(parts, "|||")
	if systemPrompt != "" {
		key = systemPrompt + "|||" + key
	}
	return key
}

// sseScanner handles scanning Server-Sent Events
type sseScanner struct {
	scanner *bufio.Scanner
	buffer  bytes.Buffer
}

func newSSEScanner(reader io.Reader) *sseScanner {
	return &sseScanner{
		scanner: bufio.NewScanner(reader),
	}
}

func (s *sseScanner) Scan() bool {
	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Check for data prefix
		if strings.HasPrefix(line, "data: ") {
			// Clear buffer for new event
			s.buffer.Reset()
			s.buffer.WriteString(strings.TrimPrefix(line, "data: "))
			return true
		}
	}
	return false
}

func (s *sseScanner) Bytes() []byte {
	return s.buffer.Bytes()
}

func (s *sseScanner) Err() error {
	return s.scanner.Err()
}
