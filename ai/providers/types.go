package providers

import (
	"context"
	"time"
)

// Provider defines the interface for AI chat providers
type Provider interface {
	// Send sends a chat request and returns a complete response
	Send(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// Stream sends a chat request and returns a channel of streaming chunks
	Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)

	// Name returns the provider's name
	Name() string

	// IsEnabled returns whether the provider is configured and enabled
	IsEnabled() bool

	// MaxTokens returns the maximum tokens this provider supports
	MaxTokens() int
}

// Message represents a chat message
type Message struct {
	Role         string // "user", "assistant", "system"
	Content      string
	Reasoning    string // Optional reasoning content (for thinking models)
	Timestamp    time.Time
	TokenEstimate int
}

// ChatRequest represents a request to an AI provider
type ChatRequest struct {
	Messages     []Message
	SystemPrompt string
	MaxTokens    int
	Temperature  float64
	Stream       bool

	// Optional: Provider-specific options
	Options map[string]any
}

// ChatResponse represents a response from an AI provider
type ChatResponse struct {
	Content      string
	Reasoning    string // Thinking/reasoning content if available
	FinishReason string
	Usage        Usage
	Model        string
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// StreamChunk represents a chunk of streaming response
type StreamChunk struct {
	Content      string
	Reasoning    string // Thinking/reasoning content chunk
	Done         bool
	Err          error

	// Internal: accumulated content for this chunk
	AccumulatedContent    string
	AccumulatedReasoning  string
}

// EstimateTokens estimates the token count for a text
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	runes := []rune(text)
	charCount := len(runes)

	tokens := charCount / 3
	if tokens < 1 {
		tokens = 1
	}

	return tokens
}
