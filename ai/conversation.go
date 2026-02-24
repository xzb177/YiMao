package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"emby-telegram-bot/ai/providers"
)

// Message is an alias for providers.Message
type Message = providers.Message

// Conversation represents a single conversation session
type Conversation struct {
	ID            string
	UserID        int64
	ChatID        int64
	ChatType      string // "private", "group", "supergroup"
	Messages      []Message
	Context       map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
	maxTokens     int
	mu            sync.RWMutex
}

// NewConversation creates a new conversation
func NewConversation(userID, chatID int64, chatType string) *Conversation {
	return &Conversation{
		ID:        generateConversationID(userID, chatID),
		UserID:    userID,
		ChatID:    chatID,
		ChatType:  chatType,
		Messages:  make([]Message, 0),
		Context:   make(map[string]any),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		maxTokens: 16000, // Default context limit
	}
}

// AddMessage adds a message to the conversation
func (c *Conversation) AddMessage(role, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Messages = append(c.Messages, Message{
		Role:         role,
		Content:      content,
		Timestamp:    time.Now(),
		TokenEstimate: providers.EstimateTokens(content),
	})
	c.UpdatedAt = time.Now()
}

// GetMessages returns the conversation messages
func (c *Conversation) GetMessages() []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	msgs := make([]Message, len(c.Messages))
	copy(msgs, c.Messages)
	return msgs
}

// GetLastN returns the last N messages
func (c *Conversation) GetLastN(n int) []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.Messages) == 0 {
		return []Message{}
	}

	start := 0
	if len(c.Messages) > n {
		start = len(c.Messages) - n
	}

	// Return a copy
	msgs := make([]Message, len(c.Messages)-start)
	copy(msgs, c.Messages[start:])
	return msgs
}

// GetTokenEstimate returns the estimated token count of the conversation
func (c *Conversation) GetTokenEstimate() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := 0
	for _, m := range c.Messages {
		total += providers.EstimateTokens(m.Content)
	}
	return total
}

// NeedsCompaction returns true if the conversation exceeds the token limit
func (c *Conversation) NeedsCompaction() bool {
	return c.GetTokenEstimate() > c.maxTokens
}

// Compact compacts the conversation to fit within the token limit
// It keeps system messages and the most recent messages
func (c *Conversation) Compact() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	totalTokens := 0
	for _, m := range c.Messages {
		totalTokens += providers.EstimateTokens(m.Content)
	}

	// If under limit, no need to compact
	if totalTokens <= c.maxTokens {
		return nil
	}

	// Keep system messages and most recent messages
	var kept []Message
	var toRemove []Message

	// First pass: identify system messages (always keep)
	for _, m := range c.Messages {
		if m.Role == "system" {
			kept = append(kept, m)
		} else {
			toRemove = append(toRemove, m)
		}
	}

	// Second pass: add recent messages until we approach the limit
	// Start from the end of toRemove
	for i := len(toRemove) - 1; i >= 0; i-- {
		msg := toRemove[i]
		tokens := providers.EstimateTokens(msg.Content)

		// Calculate current total
		currentTotal := 0
		for _, m := range kept {
			currentTotal += providers.EstimateTokens(m.Content)
		}

		if currentTotal+tokens < c.maxTokens {
			// Insert at the beginning of kept (to maintain order)
			kept = append([]Message{msg}, kept...)
		} else {
			// We've reached the limit
			break
		}
	}

	c.Messages = kept
	c.UpdatedAt = time.Now()

	return nil
}

// SetContext sets a context value
func (c *Conversation) SetContext(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Context == nil {
		c.Context = make(map[string]any)
	}
	c.Context[key] = value
}

// GetContext gets a context value
func (c *Conversation) GetContext(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.Context[key]
	return val, ok
}

// Clear clears all messages from the conversation
func (c *Conversation) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Messages = make([]Message, 0)
	c.UpdatedAt = time.Now()
}

// ToChatRequest converts the conversation to a ChatRequest
func (c *Conversation) ToChatRequest(systemPrompt string, maxTokens int, temperature float64) *ChatRequest {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &ChatRequest{
		Messages:     c.GetMessages(),
		SystemPrompt: systemPrompt,
		MaxTokens:    maxTokens,
		Temperature:  temperature,
		Stream:       c.ChatType != "private", // Only stream in non-private chats
	}
}

// ConversationManager manages multiple conversations
type ConversationManager struct {
	conversations map[string]*Conversation
	store         *Store
	mu            sync.RWMutex
	maxTokens     int
}

// NewConversationManager creates a new conversation manager
func NewConversationManager(store *Store) *ConversationManager {
	return &ConversationManager{
		conversations: make(map[string]*Conversation),
		store:         store,
		maxTokens:     16000,
	}
}

// GetOrCreate retrieves or creates a conversation for the given user and chat
func (m *ConversationManager) GetOrCreate(ctx context.Context, userID, chatID int64, chatType string) (*Conversation, error) {
	convID := generateConversationID(userID, chatID)

	// Check in-memory cache first
	m.mu.RLock()
	conv, exists := m.conversations[convID]
	m.mu.RUnlock()

	if exists {
		return conv, nil
	}

	// Try to load from store
	if m.store != nil {
		record, err := m.store.GetConversation(convID)
		if err == nil && record != nil {
			// Load messages from store
			msgRecords, err := m.store.GetMessages(convID, 0)
			if err == nil {
				conv := &Conversation{
					ID:        record.ID,
					UserID:    record.UserID,
					ChatID:    record.ChatID,
					ChatType:  record.ChatType,
					Messages:  make([]Message, 0),
					Context:   make(map[string]any),
					CreatedAt: time.Unix(record.CreatedAt, 0),
					UpdatedAt: time.Unix(record.UpdatedAt, 0),
					maxTokens: m.maxTokens,
				}

				// Convert message records
				for _, mr := range msgRecords {
					conv.Messages = append(conv.Messages, Message{
						Role:         mr.Role,
						Content:      mr.Content,
						Timestamp:    time.Unix(mr.Timestamp, 0),
						TokenEstimate: mr.Tokens,
					})
				}

				// Cache in memory
				m.mu.Lock()
				m.conversations[convID] = conv
				m.mu.Unlock()

				return conv, nil
			}
		}
	}

	// Create new conversation
	conv = NewConversation(userID, chatID, chatType)
	conv.maxTokens = m.maxTokens

	// Save to store
	if m.store != nil {
		if err := m.store.CreateConversation(conv.ID, userID, chatID, chatType); err != nil {
			// Log but don't fail - conversation will still work in memory
			fmt.Printf("[ConversationManager] Failed to save conversation: %v\n", err)
		}
	}

	// Cache in memory
	m.mu.Lock()
	m.conversations[convID] = conv
	m.mu.Unlock()

	return conv, nil
}

// Get retrieves a conversation by ID
func (m *ConversationManager) Get(convID string) *Conversation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.conversations[convID]
}

// Save saves a conversation's messages to the store
func (m *ConversationManager) Save(convID string) error {
	if m.store == nil {
		return nil
	}

	m.mu.RLock()
	_, exists := m.conversations[convID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("conversation not found: %s", convID)
	}

	// Update timestamp
	if err := m.store.UpdateConversationTimestamp(convID); err != nil {
		return err
	}

	// Note: Messages are saved as they're added via AddAndSaveMessage
	return nil
}

// AddAndSaveMessage adds a message to the conversation and saves it to the store
func (m *ConversationManager) AddAndSaveMessage(convID, role, content string) error {
	m.mu.RLock()
	conv, exists := m.conversations[convID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("conversation not found: %s", convID)
	}

	// Add to in-memory conversation
	conv.AddMessage(role, content)

	// Save to store
	if m.store != nil {
		tokens := providers.EstimateTokens(content)
		if err := m.store.AddMessage(convID, role, content, tokens); err != nil {
			return fmt.Errorf("failed to save message: %w", err)
		}
	}

	return nil
}

// Clear clears a conversation
func (m *ConversationManager) Clear(convID string) error {
	m.mu.RLock()
	conv, exists := m.conversations[convID]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	conv.Clear()

	return nil
}

// Remove removes a conversation from memory
func (m *ConversationManager) Remove(convID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.conversations, convID)
}

// CompactIfNeeded compacts a conversation if it exceeds the token limit
func (m *ConversationManager) CompactIfNeeded(convID string) error {
	m.mu.RLock()
	conv, exists := m.conversations[convID]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	if conv.NeedsCompaction() {
		return conv.Compact()
	}

	return nil
}

// GetAllActiveIDs returns all active conversation IDs
func (m *ConversationManager) GetAllActiveIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.conversations))
	for id := range m.conversations {
		ids = append(ids, id)
	}
	return ids
}

// Cleanup removes conversations that haven't been updated in a while
func (m *ConversationManager) Cleanup(olderThan time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	for id, conv := range m.conversations {
		if conv.UpdatedAt.Before(cutoff) {
			delete(m.conversations, id)
		}
	}
}

// generateConversationID generates a unique conversation ID
func generateConversationID(userID, chatID int64) string {
	return fmt.Sprintf("conv:%d:%d", userID, chatID)
}

// GenerateSessionID generates a unique session ID for streaming
func GenerateSessionID() string {
	return uuid.New().String()
}
