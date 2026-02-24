package ai

import (
	"context"
	"time"

	"emby-telegram-bot/ai/providers"
)

// Provider is an alias for the providers.Provider interface
type Provider = providers.Provider

// ChatRequest is an alias for providers.ChatRequest
type ChatRequest = providers.ChatRequest

// ChatResponse is an alias for providers.ChatResponse
type ChatResponse = providers.ChatResponse

// StreamChunk is an alias for providers.StreamChunk
type StreamChunk = providers.StreamChunk

// ProviderMessage is an alias for providers.Message
type ProviderMessage = providers.Message

// StreamProcessor handles processing of streaming chunks
type StreamProcessor struct {
	chunkBuffer    string
	reasoningBuffer string
	lastFlush      time.Time
	minChunkSize   int
	flushInterval  time.Duration
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor(minChunkSize int, flushInterval time.Duration) *StreamProcessor {
	if minChunkSize <= 0 {
		minChunkSize = 30 // Default: minimum 30 characters before flush
	}
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond // Default: flush every 500ms
	}

	return &StreamProcessor{
		minChunkSize:  minChunkSize,
		flushInterval: flushInterval,
		lastFlush:     time.Now(),
	}
}

// ProcessChunk processes a stream chunk and returns whether content should be flushed
func (p *StreamProcessor) ProcessChunk(chunk *StreamChunk) (shouldFlush bool, content string, reasoning string) {
	if chunk.Err != nil || chunk.Done {
		// Flush on error or completion
		content = p.chunkBuffer + chunk.Content
		reasoning = p.reasoningBuffer + chunk.Reasoning
		p.chunkBuffer = ""
		p.reasoningBuffer = ""
		return true, content, reasoning
	}

	// Accumulate content
	p.chunkBuffer += chunk.Content
	p.reasoningBuffer += chunk.Reasoning

	// Check if we should flush based on size or time
	shouldFlush = len(p.chunkBuffer) >= p.minChunkSize ||
		time.Since(p.lastFlush) >= p.flushInterval

	if shouldFlush {
		content = p.chunkBuffer
		reasoning = p.reasoningBuffer
		p.chunkBuffer = ""
		p.reasoningBuffer = ""
		p.lastFlush = time.Now()
		return true, content, reasoning
	}

	return false, "", ""
}

// Flush returns any remaining buffered content
func (p *StreamProcessor) Flush() (content string, reasoning string) {
	content = p.chunkBuffer
	reasoning = p.reasoningBuffer
	p.chunkBuffer = ""
	p.reasoningBuffer = ""
	return content, reasoning
}

// ProviderRegistry manages multiple AI providers with fallback support
type ProviderRegistry struct {
	providers []Provider
	primary   Provider
}

// NewProviderRegistry creates a new provider registry
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make([]Provider, 0),
	}
}

// Register adds a provider to the registry
func (r *ProviderRegistry) Register(provider Provider) {
	r.providers = append(r.providers, provider)
	if r.primary == nil && provider.IsEnabled() {
		r.primary = provider
	}
}

// SetPrimary sets the primary provider
func (r *ProviderRegistry) SetPrimary(name string) bool {
	for _, p := range r.providers {
		if p.Name() == name && p.IsEnabled() {
			r.primary = p
			return true
		}
	}
	return false
}

// GetPrimary returns the primary provider
func (r *ProviderRegistry) GetPrimary() Provider {
	return r.primary
}

// GetEnabled returns all enabled providers
func (r *ProviderRegistry) GetEnabled() []Provider {
	var enabled []Provider
	for _, p := range r.providers {
		if p.IsEnabled() {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// GetByName returns a provider by name
func (r *ProviderRegistry) GetByName(name string) Provider {
	for _, p := range r.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// SendWithFallback attempts to send using primary, falls back to other enabled providers
func (r *ProviderRegistry) SendWithFallback(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Try primary first
	if r.primary != nil {
		resp, err := r.primary.Send(ctx, req)
		if err == nil {
			return resp, nil
		}
	}

	// Try other enabled providers
	for _, p := range r.providers {
		if p == r.primary || !p.IsEnabled() {
			continue
		}
		resp, err := p.Send(ctx, req)
		if err == nil {
			return resp, nil
		}
	}

	return nil, ErrNoProviderAvailable
}

// StreamWithFallback attempts to stream using primary, falls back to other enabled providers
func (r *ProviderRegistry) StreamWithFallback(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	// Try primary first
	if r.primary != nil {
		ch, err := r.primary.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
	}

	// Try other enabled providers
	for _, p := range r.providers {
		if p == r.primary || !p.IsEnabled() {
			continue
		}
		ch, err := p.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
	}

	return nil, ErrNoProviderAvailable
}

// Errors
var (
	ErrNoProviderAvailable = &ProviderError{Msg: "no AI provider available"}
	ErrProviderDisabled    = &ProviderError{Msg: "provider is disabled"}
	ErrContextExceeded     = &ProviderError{Msg: "context length exceeded"}
)

// ProviderError represents an error from a provider
type ProviderError struct {
	Msg string
	Err error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// EstimateTokens estimates the token count for a text (alias for providers.EstimateTokens)
func EstimateTokens(text string) int {
	return providers.EstimateTokens(text)
}

// EstimateMessagesTokens estimates total tokens for a slice of messages
func EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += providers.EstimateTokens(m.Content)
		if m.Reasoning != "" {
			total += providers.EstimateTokens(m.Reasoning)
		}
	}
	return total
}
