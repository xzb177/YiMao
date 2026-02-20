package response

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Tracker tracks the status of operations
type Tracker struct {
	operations map[string]*Operation
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// Operation represents a tracked operation
type Operation struct {
	ID          string
	Type        string
	UserID      int64
	ChatID      int64
	Status      OperationStatus
	Progress    float64
	Message     string
	Error       error
	StartedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
	Metadata    map[string]interface{}
	mu          sync.RWMutex
}

// OperationStatus represents the status of an operation
type OperationStatus string

const (
	StatusPending    OperationStatus = "pending"
	StatusRunning    OperationStatus = "running"
	StatusCompleted  OperationStatus = "completed"
	StatusFailed     OperationStatus = "failed"
	StatusCancelled  OperationStatus = "cancelled"
	StatusTimeout    OperationStatus = "timeout"
)

// NewTracker creates a new operation tracker
func NewTracker() *Tracker {
	ctx, cancel := context.WithCancel(context.Background())

	t := &Tracker{
		operations: make(map[string]*Operation),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start cleanup goroutine
	go t.cleanupLoop()

	return t
}

// Create creates a new operation
func (t *Tracker) Create(opType string, userID, chatID int64) *Operation {
	op := &Operation{
		ID:        GenerateRequestID(),
		Type:      opType,
		UserID:    userID,
		ChatID:    chatID,
		Status:    StatusPending,
		Progress:  0,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	t.mu.Lock()
	t.operations[op.ID] = op
	t.mu.Unlock()

	return op
}

// Get retrieves an operation by ID
func (t *Tracker) Get(id string) (*Operation, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	op, exists := t.operations[id]
	return op, exists
}

// Update updates an operation's status and message
func (t *Tracker) Update(id string, status OperationStatus, progress float64, message string) error {
	t.mu.RLock()
	op, exists := t.operations[id]
	t.mu.RUnlock()

	if !exists {
		return fmt.Errorf("operation not found: %s", id)
	}

	op.mu.Lock()
	defer op.mu.Unlock()

	op.Status = status
	op.Progress = progress
	op.Message = message
	op.UpdatedAt = time.Now()

	if status == StatusCompleted || status == StatusFailed || status == StatusCancelled {
		op.CompletedAt = time.Now()
	}

	return nil
}

// SetError sets an error for an operation
func (t *Tracker) SetError(id string, err error) error {
	t.mu.RLock()
	op, exists := t.operations[id]
	t.mu.RUnlock()

	if !exists {
		return fmt.Errorf("operation not found: %s", id)
	}

	op.mu.Lock()
	defer op.mu.Unlock()

	op.Error = err
	op.Status = StatusFailed
	op.UpdatedAt = time.Now()
	op.CompletedAt = time.Now()

	return nil
}

// SetMetadata sets metadata for an operation
func (t *Tracker) SetMetadata(id string, key string, value interface{}) error {
	t.mu.RLock()
	op, exists := t.operations[id]
	t.mu.RUnlock()

	if !exists {
		return fmt.Errorf("operation not found: %s", id)
	}

	op.mu.Lock()
	defer op.mu.Unlock()

	if op.Metadata == nil {
		op.Metadata = make(map[string]interface{})
	}
	op.Metadata[key] = value

	return nil
}

// GetMetadata gets metadata from an operation
func (t *Tracker) GetMetadata(id, key string) (interface{}, bool) {
	t.mu.RLock()
	op, exists := t.operations[id]
	t.mu.RUnlock()

	if !exists {
		return nil, false
	}

	op.mu.RLock()
	defer op.mu.RUnlock()

	if op.Metadata == nil {
		return nil, false
	}

	val, ok := op.Metadata[key]
	return val, ok
}

// ListByUser lists all operations for a user
func (t *Tracker) ListByUser(userID int64) []*Operation {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*Operation
	for _, op := range t.operations {
		if op.UserID == userID {
			result = append(result, op)
		}
	}

	return result
}

// ListByStatus lists all operations with a given status
func (t *Tracker) ListByStatus(status OperationStatus) []*Operation {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*Operation
	for _, op := range t.operations {
		if op.Status == status {
			result = append(result, op)
		}
	}

	return result
}

// Cancel cancels an operation
func (t *Tracker) Cancel(id string) error {
	return t.Update(id, StatusCancelled, 0, "Operation cancelled")
}

// Remove removes an operation from the tracker
func (t *Tracker) Remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.operations, id)
}

// cleanupLoop periodically cleans up old operations
func (t *Tracker) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.cleanup()
		case <-t.ctx.Done():
			return
		}
	}
}

// cleanup removes old completed/failed operations
func (t *Tracker) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := time.Hour // Keep operations for 1 hour after completion

	for id, op := range t.operations {
		op.mu.RLock()
		isFinished := op.Status == StatusCompleted || op.Status == StatusFailed || op.Status == StatusCancelled
		age := now.Sub(op.UpdatedAt)
		op.mu.RUnlock()

		if isFinished && age > cutoff {
			delete(t.operations, id)
		}
	}
}

// Shutdown gracefully shuts down the tracker
func (t *Tracker) Shutdown() {
	t.cancel()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.operations = make(map[string]*Operation)
}

// Operation methods

// GetStatus returns the current status
func (o *Operation) GetStatus() OperationStatus {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.Status
}

// GetProgress returns the current progress (0-100)
func (o *Operation) GetProgress() float64 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.Progress
}

// GetMessage returns the current message
func (o *Operation) GetMessage() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.Message
}

// GetError returns the error if any
func (o *Operation) GetError() error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.Error
}

// IsFinished returns true if the operation is finished
func (o *Operation) IsFinished() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.Status == StatusCompleted || o.Status == StatusFailed || o.Status == StatusCancelled || o.Status == StatusTimeout
}

// GetDuration returns the duration of the operation
func (o *Operation) GetDuration() time.Duration {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.CompletedAt.IsZero() {
		return time.Since(o.StartedAt)
	}
	return o.CompletedAt.Sub(o.StartedAt)
}

// ToResponse converts the operation to a Response
func (o *Operation) ToResponse() *Response {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var respType ResponseType
	var title string
	var message string
	var severity Severity

	switch o.Status {
	case StatusCompleted:
		respType = ResponseTypeSuccess
		title = "✅ 操作完成"
		message = o.Message
	case StatusFailed:
		respType = ResponseTypeError
		title = "❌ 操作失败"
		message = o.Message
		severity = SeverityMedium
	case StatusCancelled:
		respType = ResponseTypeWarning
		title = "⚠️ 操作已取消"
		message = o.Message
	case StatusTimeout:
		respType = ResponseTypeError
		title = "⏰ 操作超时"
		message = o.Message
		severity = SeverityMedium
	case StatusRunning:
		respType = ResponseTypeProgress
		title = "⏳ 处理中"
		message = o.Message
	default:
		respType = ResponseTypeInfo
		title = "📋 操作状态"
		message = o.Message
	}

	details := ""
	if o.Progress > 0 && o.Status != StatusCompleted {
		details = fmt.Sprintf("进度: %.0f%%", o.Progress)
	}

	return NewBuilder().
		WithType(respType).
		WithSeverity(severity).
		WithTitle(title).
		WithMessage(message).
		WithDetails(details).
		WithUserContext(o.UserID, o.ChatID).
		WithRequestID(o.ID).
		Build()
}

// Context provides context for an operation
type Context struct {
	operation *Operation
	tracker   *Tracker
}

// NewContext creates a new operation context
func NewContext(tracker *Tracker, op *Operation) *Context {
	return &Context{
		operation: op,
		tracker:   tracker,
	}
}

// ID returns the operation ID
func (c *Context) ID() string {
	return c.operation.ID
}

// Update updates the operation status
func (c *Context) Update(status OperationStatus, progress float64, message string) error {
	return c.tracker.Update(c.operation.ID, status, progress, message)
}

// SetProgress updates just the progress
func (c *Context) SetProgress(progress float64) error {
	c.operation.mu.Lock()
	c.operation.Progress = progress
	c.operation.UpdatedAt = time.Now()
	c.operation.mu.Unlock()
	return nil
}

// SetMessage updates just the message
func (c *Context) SetMessage(message string) error {
	c.operation.mu.Lock()
	c.operation.Message = message
	c.operation.UpdatedAt = time.Now()
	c.operation.mu.Unlock()
	return nil
}

// Complete marks the operation as completed
func (c *Context) Complete(message string) error {
	return c.tracker.Update(c.operation.ID, StatusCompleted, 100, message)
}

// Fail marks the operation as failed
func (c *Context) Fail(message string, err error) error {
	c.tracker.SetError(c.operation.ID, fmt.Errorf("%s: %w", message, err))
	return nil
}

// SetMetadata sets metadata
func (c *Context) SetMetadata(key string, value interface{}) error {
	return c.tracker.SetMetadata(c.operation.ID, key, value)
}

// GetMetadata gets metadata
func (c *Context) GetMetadata(key string) (interface{}, bool) {
	return c.tracker.GetMetadata(c.operation.ID, key)
}

// Operation returns the underlying operation
func (c *Context) Operation() *Operation {
	return c.operation
}

// ToResponse converts to a Response
func (c *Context) ToResponse() *Response {
	return c.operation.ToResponse()
}
