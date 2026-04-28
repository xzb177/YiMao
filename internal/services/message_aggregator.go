package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/types"
	"github.com/xzb177/yimao/pkg/logger"
)

// MessageAggregator aggregates multiple messages before sending
type MessageAggregator struct {
	queues    map[int64]*messageQueue
	mu        sync.RWMutex
	flushInt  time.Duration
	maxWait   time.Duration
	maxBatch  int
	telegram  *TelegramClient
	stopChan  chan struct{}
}

type messageQueue struct {
	messages []queuedMessage
	mu       sync.Mutex
	flushTimer *time.Timer
	lastFlush time.Time
}

type queuedMessage struct {
	text      string
	parseMode string
	keyboard  *types.TelegramInlineKeyboard
	callback  func(*types.TelegramMessage, error)
	priority  int // 0=low, 1=normal, 2=high
}

// AggregatorConfig holds aggregator configuration
type AggregatorConfig struct {
	FlushInterval time.Duration // Maximum time to wait before flushing
	MaxWait       time.Duration // Maximum time to wait for first message
	MaxBatchSize  int           // Maximum messages to batch
}

// DefaultAggregatorConfig returns default configuration
func DefaultAggregatorConfig() AggregatorConfig {
	return AggregatorConfig{
		FlushInterval: 2 * time.Second,
		MaxWait:       500 * time.Millisecond,
		MaxBatchSize:  5,
	}
}

// NewMessageAggregator creates a new message aggregator
func NewMessageAggregator(telegram *TelegramClient, config AggregatorConfig) *MessageAggregator {
	if config.FlushInterval == 0 {
		config.FlushInterval = 2 * time.Second
	}
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = 5
	}

	ma := &MessageAggregator{
		queues:   make(map[int64]*messageQueue),
		flushInt: config.FlushInterval,
		maxWait:  config.MaxWait,
		maxBatch: config.MaxBatchSize,
		telegram: telegram,
		stopChan: make(chan struct{}),
	}
	go ma.flushLoop()
	return ma
}

// Stop stops the aggregator
func (ma *MessageAggregator) Stop() {
	close(ma.stopChan)
	ma.flushAll()
}

// SendMessage queues a message for sending
// If priority is high (2), sends immediately without batching
func (ma *MessageAggregator) SendMessage(chatID int64, text string, parseMode string, keyboard *types.TelegramInlineKeyboard, priority int) (*types.TelegramMessage, error) {
	if priority == 2 {
		// High priority - send immediately
		return ma.telegram.SendMessage(chatID, text, parseMode, keyboard)
	}

	// Queue the message
	ma.mu.Lock()
	q, exists := ma.queues[chatID]
	if !exists {
		q = &messageQueue{
			messages:   make([]queuedMessage, 0, ma.maxBatch),
			lastFlush:  time.Now(),
		}
		ma.queues[chatID] = q
	}
	ma.mu.Unlock()

	q.mu.Lock()
	q.messages = append(q.messages, queuedMessage{
		text:      text,
		parseMode: parseMode,
		keyboard:  keyboard,
		priority:  priority,
	})

	// Check if we should flush immediately
	msgCount := len(q.messages)
	shouldFlush := msgCount >= ma.maxBatch

	// Reset or create flush timer
	if q.flushTimer != nil {
		q.flushTimer.Stop()
	}

	if shouldFlush {
		q.mu.Unlock()
		go ma.flushQueue(chatID)
	} else {
		q.flushTimer = time.AfterFunc(ma.flushInt, func() {
			ma.flushQueue(chatID)
		})
		q.mu.Unlock()
	}

	return &types.TelegramMessage{MessageID: 0}, nil // Return placeholder
}

// SendAsync sends a message asynchronously with callback
func (ma *MessageAggregator) SendAsync(chatID int64, text string, parseMode string, keyboard *types.TelegramInlineKeyboard, callback func(*types.TelegramMessage, error)) {
	ma.mu.Lock()
	q, exists := ma.queues[chatID]
	if !exists {
		q = &messageQueue{
			messages:   make([]queuedMessage, 0, ma.maxBatch),
			lastFlush:  time.Now(),
		}
		ma.queues[chatID] = q
	}
	ma.mu.Unlock()

	q.mu.Lock()
	q.messages = append(q.messages, queuedMessage{
		text:      text,
		parseMode: parseMode,
		keyboard:  keyboard,
		callback:  callback,
		priority:  1,
	})
	msgCount := len(q.messages)
	shouldFlush := msgCount >= ma.maxBatch

	if q.flushTimer != nil {
		q.flushTimer.Stop()
	}

	if shouldFlush {
		q.mu.Unlock()
		go ma.flushQueue(chatID)
	} else {
		q.flushTimer = time.AfterFunc(ma.flushInt, func() {
			ma.flushQueue(chatID)
		})
		q.mu.Unlock()
	}
}

// flushQueue flushes messages for a specific chat
func (ma *MessageAggregator) flushQueue(chatID int64) {
	ma.mu.RLock()
	q, exists := ma.queues[chatID]
	ma.mu.RUnlock()

	if !exists {
		return
	}

	q.mu.Lock()
	if len(q.messages) == 0 {
		q.mu.Unlock()
		return
	}

	// Copy messages and clear queue
	messages := make([]queuedMessage, len(q.messages))
	copy(messages, q.messages)
	q.messages = q.messages[:0]
	q.lastFlush = time.Now()
	if q.flushTimer != nil {
		q.flushTimer.Stop()
		q.flushTimer = nil
	}
	q.mu.Unlock()

	// Send aggregated message
	if len(messages) == 1 {
		// Single message - send as-is
		msg := messages[0]
		result, err := ma.telegram.SendMessage(chatID, msg.text, msg.parseMode, msg.keyboard)
		if msg.callback != nil {
			msg.callback(result, err)
		}
		return
	}

	// Multiple messages - aggregate them
	aggregatedText := ma.aggregateMessages(messages)
	_, err := ma.telegram.SendMessage(chatID, aggregatedText, "", nil)

	// Call callbacks
	for _, msg := range messages {
		if msg.callback != nil {
			msg.callback(nil, err)
		}
	}

	logger.Info("[MessageAggregator] Flushed %d messages for chat %d", len(messages), chatID)
}

// aggregateMessages combines multiple messages into one
func (ma *MessageAggregator) aggregateMessages(messages []queuedMessage) string {
	var sb strings.Builder

	sb.WriteString("📬 汇总消息\n\n")

	for i, msg := range messages {
		// Truncate long messages
		text := msg.text
		if len(text) > 200 {
			text = text[:200] + "..."
		}

		if i < len(messages)-1 {
			sb.WriteString(fmt.Sprintf("• %s\n\n", text))
		} else {
			sb.WriteString(fmt.Sprintf("• %s", text))
		}
	}

	return sb.String()
}

// flushLoop periodically flushes all queues
func (ma *MessageAggregator) flushLoop() {
	ticker := time.NewTicker(ma.flushInt)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ma.flushStaleQueues()
		case <-ma.stopChan:
			return
		}
	}
}

// flushStaleQueues flushes queues that haven't been flushed recently
func (ma *MessageAggregator) flushStaleQueues() {
	ma.mu.RLock()
	chatIDs := make([]int64, 0, len(ma.queues))
	for chatID := range ma.queues {
		chatIDs = append(chatIDs, chatID)
	}
	ma.mu.RUnlock()

	for _, chatID := range chatIDs {
		ma.flushQueue(chatID)
	}
}

// flushAll flushes all remaining messages
func (ma *MessageAggregator) flushAll() {
	ma.mu.RLock()
	chatIDs := make([]int64, 0, len(ma.queues))
	for chatID := range ma.queues {
		chatIDs = append(chatIDs, chatID)
	}
	ma.mu.RUnlock()

	for _, chatID := range chatIDs {
		ma.flushQueue(chatID)
	}
}

// Stats returns aggregator statistics
func (ma *MessageAggregator) Stats() map[string]interface{} {
	ma.mu.RLock()
	defer ma.mu.RUnlock()

	totalMessages := 0
	for _, q := range ma.queues {
		q.mu.Lock()
		totalMessages += len(q.messages)
		q.mu.Unlock()
	}

	return map[string]interface{}{
		"active_queues": len(ma.queues),
		"pending_messages": totalMessages,
		"flush_interval": ma.flushInt.String(),
		"max_batch_size": ma.maxBatch,
	}
}
