package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// StreamHandler manages the streaming response workflow
type StreamHandler struct {
	processor    *StreamProcessor
	chunkChan    <-chan StreamChunk
	done         chan struct{}
	mu           sync.Mutex
	accumulated  strings.Builder
	reasoningAcc strings.Builder
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(chunkChan <-chan StreamChunk, minChunkSize int, flushInterval time.Duration) *StreamHandler {
	return &StreamHandler{
		processor:   NewStreamProcessor(minChunkSize, flushInterval),
		chunkChan:   chunkChan,
		done:        make(chan struct{}),
		accumulated: strings.Builder{},
	}
}

// HandleStream processes the stream with callbacks
func (h *StreamHandler) HandleStream(
	ctx context.Context,
	onContent func(content string) error,
	onReasoning func(content string) error,
	onComplete func(fullContent, fullReasoning string) error,
	onError func(err error) error,
) error {
	defer close(h.done)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case chunk, ok := <-h.chunkChan:
			if !ok {
				// Channel closed
				fullContent, fullReasoning := h.processor.Flush()
				if fullContent != "" || fullReasoning != "" {
					h.accumulated.WriteString(fullContent)
					h.reasoningAcc.WriteString(fullReasoning)
				}
				if onComplete != nil {
					return onComplete(h.accumulated.String(), h.reasoningAcc.String())
				}
				return nil
			}

			if chunk.Err != nil {
				if onError != nil {
					return onError(chunk.Err)
				}
				return chunk.Err
			}

			shouldFlush, content, reasoning := h.processor.ProcessChunk(&chunk)

			if shouldFlush {
				h.mu.Lock()
				if content != "" {
					h.accumulated.WriteString(content)
				}
				if reasoning != "" {
					h.reasoningAcc.WriteString(reasoning)
				}
				h.mu.Unlock()

				// Call callbacks
				if content != "" && onContent != nil {
					if err := onContent(h.accumulated.String()); err != nil {
						return err
					}
				}
				if reasoning != "" && onReasoning != nil {
					if err := onReasoning(h.reasoningAcc.String()); err != nil {
						return err
					}
				}

				if chunk.Done {
					if onComplete != nil {
						return onComplete(h.accumulated.String(), h.reasoningAcc.String())
					}
					return nil
				}
			} else if chunk.Done {
				fullContent, fullReasoning := h.processor.Flush()
				if fullContent != "" {
					h.accumulated.WriteString(fullContent)
				}
				if fullReasoning != "" {
					h.reasoningAcc.WriteString(fullReasoning)
				}
				if onComplete != nil {
					return onComplete(h.accumulated.String(), h.reasoningAcc.String())
				}
				return nil
			}
		}
	}
}

// GetAccumulated returns the currently accumulated content
func (h *StreamHandler) GetAccumulated() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.accumulated.String()
}

// GetReasoning returns the currently accumulated reasoning
func (h *StreamHandler) GetReasoning() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reasoningAcc.String()
}

// IsDone returns whether the stream is complete
func (h *StreamHandler) IsDone() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

// Abort stops the stream processing
func (h *StreamHandler) Abort() {
	close(h.done)
}

// ChunkCoalescer combines multiple small chunks into larger ones
type ChunkCoalescer struct {
	buffer       strings.Builder
	minSize      int
	maxWait      time.Duration
	lastFlush    time.Time
	flushTimer   *time.Timer
	mu           sync.Mutex
}

// NewChunkCoalescer creates a new chunk coalescer
func NewChunkCoalescer(minSize int, maxWait time.Duration) *ChunkCoalescer {
	c := &ChunkCoalescer{
		minSize:   minSize,
		maxWait:   maxWait,
		lastFlush: time.Now(),
	}
	// Create a timer that won't fire until Reset is called
	c.flushTimer = time.AfterFunc(time.Hour, func() {})
	c.flushTimer.Stop()
	return c
}

// Add adds a chunk to the buffer and returns content if buffer should be flushed
func (c *ChunkCoalescer) Add(chunk string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if chunk == "" {
		return "", false
	}

	c.buffer.WriteString(chunk)

	// Reset flush timer on each chunk
	c.flushTimer.Reset(c.maxWait)

	// Flush if buffer exceeds min size
	if c.buffer.Len() >= c.minSize {
		return c.flush(), true
	}

	return "", false
}

// Flush flushes the buffer and returns the accumulated content
func (c *ChunkCoalescer) Flush() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushTimer.Stop()
	return c.flush()
}

func (c *ChunkCoalescer) flush() string {
	content := c.buffer.String()
	c.buffer.Reset()
	c.lastFlush = time.Now()
	return content
}

// Debouncer ensures updates are not sent too frequently
type Debouncer struct {
	minInterval time.Duration
	lastUpdate  time.Time
	mu          sync.Mutex
	pending     bool
	pendingData string
	timer       *time.Timer
	callback    func(string)
}

// NewDebouncer creates a new debouncer
func NewDebouncer(minInterval time.Duration, callback func(string)) *Debouncer {
	d := &Debouncer{
		minInterval: minInterval,
		lastUpdate:  time.Now(),
		callback:    callback,
	}
	return d
}

// Add adds data to be debounced
func (d *Debouncer) Add(data string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.pending = true
	d.pendingData = data

	// Reset or create timer
	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.minInterval, d.flush)
}

// Flush immediately flushes pending data
func (d *Debouncer) Flush() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.flush()
}

func (d *Debouncer) flush() {
	if d.pending && d.callback != nil {
		d.callback(d.pendingData)
	}
	d.pending = false
	d.pendingData = ""
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

// StreamManager manages multiple concurrent streaming sessions
type StreamManager struct {
	sessions map[string]*StreamHandler
	mu       sync.RWMutex
}

// NewStreamManager creates a new stream manager
func NewStreamManager() *StreamManager {
	return &StreamManager{
		sessions: make(map[string]*StreamHandler),
	}
}

// StartStream starts a new streaming session
func (m *StreamManager) StartStream(sessionID string, chunkChan <-chan StreamChunk) *StreamHandler {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop any existing session for this ID
	if existing, ok := m.sessions[sessionID]; ok {
		existing.Abort()
	}

	handler := NewStreamHandler(chunkChan, 30, 500*time.Millisecond)
	m.sessions[sessionID] = handler

	return handler
}

// GetStream retrieves an active streaming session
func (m *StreamManager) GetStream(sessionID string) *StreamHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[sessionID]
}

// StopStream stops and removes a streaming session
func (m *StreamManager) StopStream(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if handler, ok := m.sessions[sessionID]; ok {
		handler.Abort()
		delete(m.sessions, sessionID)
	}
}

// Cleanup removes completed sessions
func (m *StreamManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, handler := range m.sessions {
		if handler.IsDone() {
			delete(m.sessions, id)
		}
	}
}

// StreamReader is a helper for reading from a stream channel with timeout
func StreamReader(ctx context.Context, chunkChan <-chan StreamChunk, timeout time.Duration) ([]string, error) {
	var chunks []string

	for {
		select {
		case <-ctx.Done():
			return chunks, ctx.Err()

		case chunk, ok := <-chunkChan:
			if !ok {
				return chunks, nil
			}

			if chunk.Err != nil {
				return chunks, chunk.Err
			}

			if chunk.Content != "" {
				chunks = append(chunks, chunk.Content)
			}

			if chunk.Done {
				return chunks, nil
			}

		case <-time.After(timeout):
			// Timeout - return what we have so far
			return chunks, fmt.Errorf("stream read timeout after %v", timeout)
		}
	}
}

// StreamToChannelAdapter converts a streaming function to a channel-based interface
func StreamToChannelAdapter(
	ctx context.Context,
	streamFunc func(string) error,
) (chan<- StreamChunk, <-chan error) {
	chunkChan := make(chan StreamChunk, 16)
	errChan := make(chan error, 1)

	go func() {
		defer close(errChan)

		for chunk := range chunkChan {
			if chunk.Err != nil {
				errChan <- chunk.Err
				return
			}

			if chunk.Content != "" {
				if err := streamFunc(chunk.Content); err != nil {
					errChan <- err
					return
				}
			}

			if chunk.Done {
				return
			}
		}
	}()

	return chunkChan, errChan
}

// TeeStream splits a stream channel to multiple consumers
func TeeStream(source <-chan StreamChunk, count int) []<-chan StreamChunk {
	outputs := make([]chan StreamChunk, count)
	for i := range outputs {
		outputs[i] = make(chan StreamChunk, 16)
	}

	go func() {
		defer func() {
			for _, ch := range outputs {
				close(ch)
			}
		}()

		for chunk := range source {
			for _, ch := range outputs {
				select {
				case ch <- chunk:
				case <-time.After(time.Second):
					// Drop if consumer is blocked
				}
			}
		}
	}()

	// Convert to return type
	result := make([]<-chan StreamChunk, count)
	for i, ch := range outputs {
		result[i] = ch
	}
	return result
}
