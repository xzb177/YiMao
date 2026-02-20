package response

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Handler manages response creation and delivery
type Handler struct {
	responses map[string]*Response // RequestID -> Response
	mu        sync.RWMutex

	// Progress tracking
	progress map[string]*ProgressState

	// Local configuration
	defaultLanguage string
	timezone        *time.Location

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// ProgressState tracks the state of a long-running operation
type ProgressState struct {
	RequestID    string
	Operation    string
	CurrentStep  int
	TotalSteps   int
	StartedAt    time.Time
	LastUpdate   time.Time
	Status       string
	Message      string
	Error        error
	mu           sync.RWMutex
	subscribers  []chan *ProgressUpdate
}

// ProgressUpdate represents a progress update
type ProgressUpdate struct {
	RequestID   string
	Operation   string
	Current     int
	Total       int
	Message     string
	Status      string
	Error       error
	Percentage  float64
	ETA         time.Duration
}

// NewHandler creates a new response handler
func NewHandler() *Handler {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Handler{
		responses:       make(map[string]*Response),
		progress:        make(map[string]*ProgressState),
		defaultLanguage: "zh",
		timezone:        time.UTC,
		ctx:             ctx,
		cancel:          cancel,
	}

	// Start cleanup goroutine
	go h.cleanupLoop()

	return h
}

// SetDefaultLanguage sets the default language for responses
func (h *Handler) SetDefaultLanguage(lang string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.defaultLanguage = lang
}

// SetTimezone sets the timezone for time displays
func (h *Handler) SetTimezone(tz *time.Location) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timezone = tz
}

// CreateResponse creates a new response with the given type and message
func (h *Handler) CreateResponse(respType ResponseType, message string) *Response {
	return NewBuilder().
		WithType(respType).
		WithMessage(message).
		Build()
}

// CreateResponseWithContext creates a response with user context
func (h *Handler) CreateResponseWithContext(respType ResponseType, message string, userID, chatID int64) *Response {
	return NewBuilder().
		WithType(respType).
		WithMessage(message).
		WithUserContext(userID, chatID).
		Build()
}

// StoreResponse stores a response for later retrieval
func (h *Handler) StoreResponse(requestID string, resp *Response) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Set request ID in response
	resp.Context.RequestID = requestID

	h.responses[requestID] = resp

	// Auto-cleanup after 5 minutes
	go func() {
		time.Sleep(5 * time.Minute)
		h.mu.Lock()
		delete(h.responses, requestID)
		h.mu.Unlock()
	}()
}

// GetResponse retrieves a stored response
func (h *Handler) GetResponse(requestID string) (*Response, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	resp, exists := h.responses[requestID]
	return resp, exists
}

// CreateErrorResponse creates an error response from an error
func (h *Handler) CreateErrorResponse(err error, context string) *Response {
	if err == nil {
		return nil
	}

	builder := NewBuilder().
		WithType(ResponseTypeError).
		WithSeverity(SeverityMedium).
		WithTitle("❌ 操作失败")

	// Add context
	if context != "" {
		builder.WithMessage(context)
	} else {
		builder.WithMessage("无法完成您的请求")
	}

	// Add error details
	builder.WithDetails(err.Error())

	return builder.Build()
}

// StartProgress starts tracking a progress operation
func (h *Handler) StartProgress(requestID, operation string, totalSteps int) *ProgressState {
	h.mu.Lock()
	defer h.mu.Unlock()

	state := &ProgressState{
		RequestID:   requestID,
		Operation:   operation,
		TotalSteps:  totalSteps,
		CurrentStep: 0,
		StartedAt:   time.Now(),
		LastUpdate:  time.Now(),
		Status:      "in_progress",
		subscribers: make([]chan *ProgressUpdate, 0),
	}

	h.progress[requestID] = state

	return state
}

// GetProgress retrieves the current progress state
func (h *Handler) GetProgress(requestID string) (*ProgressState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	state, exists := h.progress[requestID]
	return state, exists
}

// UpdateProgress updates a progress operation
func (h *Handler) UpdateProgress(requestID, message string, currentStep int) {
	h.mu.RLock()
	state, exists := h.progress[requestID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.CurrentStep = currentStep
	state.Message = message
	state.LastUpdate = time.Now()

	update := &ProgressUpdate{
		RequestID:  requestID,
		Operation:  state.Operation,
		Current:    currentStep,
		Total:      state.TotalSteps,
		Message:    message,
		Status:     state.Status,
		Percentage: float64(currentStep) / float64(state.TotalSteps) * 100,
	}

	if state.StartedAt.Before(time.Now()) && currentStep > 0 {
		elapsed := time.Since(state.StartedAt).Seconds()
		avgTime := elapsed / float64(currentStep)
		remaining := float64(state.TotalSteps-currentStep) * avgTime
		update.ETA = time.Duration(remaining) * time.Second
	}

	// Notify subscribers
	for _, ch := range state.subscribers {
		select {
		case ch <- update:
		default:
			// Channel full, skip
		}
	}
}

// CompleteProgress marks a progress operation as complete
func (h *Handler) CompleteProgress(requestID, message string) {
	h.mu.RLock()
	state, exists := h.progress[requestID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.Status = "completed"
	state.Message = message
	state.CurrentStep = state.TotalSteps
	state.LastUpdate = time.Now()

	update := &ProgressUpdate{
		RequestID:  requestID,
		Operation:  state.Operation,
		Current:    state.TotalSteps,
		Total:      state.TotalSteps,
		Message:    message,
		Status:     "completed",
		Percentage: 100,
	}

	// Notify subscribers
	for _, ch := range state.subscribers {
		select {
		case ch <- update:
		default:
		}
	}

	// Remove from progress map after a delay
	go func() {
		time.Sleep(5 * time.Minute)
		h.mu.Lock()
		delete(h.progress, requestID)
		h.mu.Unlock()
	}()
}

// FailProgress marks a progress operation as failed
func (h *Handler) FailProgress(requestID, message string, err error) {
	h.mu.RLock()
	state, exists := h.progress[requestID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.Status = "failed"
	state.Message = message
	state.Error = err
	state.LastUpdate = time.Now()

	update := &ProgressUpdate{
		RequestID: requestID,
		Operation: state.Operation,
		Message:   message,
		Status:    "failed",
		Error:     err,
	}

	// Notify subscribers
	for _, ch := range state.subscribers {
		select {
		case ch <- update:
		default:
		}
	}
}

// SubscribeToProgress subscribes to progress updates
func (h *Handler) SubscribeToProgress(requestID string) <-chan *ProgressUpdate {
	h.mu.RLock()
	state, exists := h.progress[requestID]
	h.mu.RUnlock()

	if !exists {
		return nil
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	ch := make(chan *ProgressUpdate, 10)
	state.subscribers = append(state.subscribers, ch)

	return ch
}

// UnsubscribeFromProgress unsubscribes from progress updates
func (h *Handler) UnsubscribeFromProgress(requestID string, ch <-chan *ProgressUpdate) {
	h.mu.RLock()
	state, exists := h.progress[requestID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for i, subscriber := range state.subscribers {
		if subscriber == ch {
			state.subscribers = append(state.subscribers[:i], state.subscribers[i+1:]...)
			close(subscriber)
			break
		}
	}
}

// cleanupLoop periodically cleans up old responses and progress states
func (h *Handler) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.cleanup()
		case <-h.ctx.Done():
			return
		}
	}
}

// cleanup removes old entries
func (h *Handler) cleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()

	// Clean up old responses
	for id, resp := range h.responses {
		if now.Sub(resp.Context.Timestamp) > 5*time.Minute {
			delete(h.responses, id)
		}
	}

	// Clean up old progress states
	for id, state := range h.progress {
		state.mu.RLock()
		age := now.Sub(state.LastUpdate)
		state.mu.RUnlock()

		if age > 30*time.Minute || state.Status == "completed" || state.Status == "failed" {
			delete(h.progress, id)
		}
	}
}

// Shutdown gracefully shuts down the handler
func (h *Handler) Shutdown() {
	h.cancel()

	h.mu.Lock()
	defer h.mu.Unlock()

	// Close all subscriber channels
	for _, state := range h.progress {
		state.mu.Lock()
		for _, ch := range state.subscribers {
			close(ch)
		}
		state.subscribers = nil
		state.mu.Unlock()
	}

	h.progress = make(map[string]*ProgressState)
	h.responses = make(map[string]*Response)
}

// GenerateRequestID generates a unique request ID
func GenerateRequestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// BuildSearchInProgress creates a search in progress response
func (h *Handler) BuildSearchInProgress(query string) *Response {
	data := TemplateData{
		MediaTitle: query,
	}
	return RenderTemplate(TemplateSearchInProgress, data)
}

// BuildSearchNoResults creates a no results response
func (h *Handler) BuildSearchNoResults(query string) *Response {
	data := TemplateData{
		MediaTitle: query,
	}
	return RenderTemplate(TemplateSearchNoResults, data)
}

// BuildSearchError creates a search error response
func (h *Handler) BuildSearchError(query string, err error) *Response {
	data := TemplateData{
		MediaTitle: query,
		Error:      err.Error(),
	}
	return RenderTemplate(TemplateSearchError, data)
}

// BuildRequestSuccess creates a request success response
func (h *Handler) BuildRequestSuccess(title, mediaType string, quotaUsed, quotaLimit, quotaRemaining int) *Response {
	quotaType := "电影"
	if mediaType == "tv" {
		quotaType = "剧集"
	}

	data := TemplateData{
		MediaTitle:     title,
		MediaType:      mediaType,
		QuotaUsed:      quotaUsed,
		QuotaLimit:     quotaLimit,
		QuotaRemaining: quotaRemaining,
		QuotaType:      quotaType,
	}
	return RenderTemplate(TemplateRequestSuccess, data)
}

// BuildQuotaExhausted creates a quota exhausted response
func (h *Handler) BuildQuotaExhausted(mediaType string, quotaUsed, quotaLimit int) *Response {
	quotaType := "电影"
	if mediaType == "tv" {
		quotaType = "剧集"
	}

	data := TemplateData{
		QuotaUsed:  quotaUsed,
		QuotaLimit: quotaLimit,
		QuotaType:  quotaType,
	}
	return RenderTemplate(TemplateRequestQuotaExhausted, data)
}

// BuildAccountNotLinked creates an account not linked response
func (h *Handler) BuildAccountNotLinked() *Response {
	return RenderTemplate(TemplateAccountNotLinked, TemplateData{})
}

// BuildRateLimited creates a rate limited response
func (h *Handler) BuildRateLimited(retryAfter time.Duration) *Response {
	data := TemplateData{
		RetryAfter: retryAfter,
	}
	return RenderTemplate(TemplateRateLimited, data)
}

// BuildNetworkError creates a network error response
func (h *Handler) BuildNetworkError(err error) *Response {
	data := TemplateData{
		Error: err.Error(),
	}
	return RenderTemplate(TemplateNetworkError, data)
}

// BuildOperationTimeout creates an operation timeout response
func (h *Handler) BuildOperationTimeout(operation string) *Response {
	return RenderTemplate(TemplateOperationTimeout, TemplateData{
		MediaTitle: operation,
	})
}

// BuildInvalidInput creates an invalid input response
func (h *Handler) BuildInvalidInput(message string) *Response {
	return RenderTemplate(TemplateInvalidInput, TemplateData{
		Error: message,
	})
}

// ProgressState methods

// Update updates the progress state
func (p *ProgressState) Update(message string, currentStep int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CurrentStep = currentStep
	p.Message = message
	p.LastUpdate = time.Now()
}

// Complete marks the progress as complete
func (p *ProgressState) Complete(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Status = "completed"
	p.Message = message
	p.CurrentStep = p.TotalSteps
	p.LastUpdate = time.Now()
}

// Fail marks the progress as failed
func (p *ProgressState) Fail(message string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Status = "failed"
	p.Message = message
	p.Error = err
	p.LastUpdate = time.Now()
}

// GetPercentage returns the completion percentage
func (p *ProgressState) GetPercentage() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.TotalSteps == 0 {
		return 0
	}
	return float64(p.CurrentStep) / float64(p.TotalSteps) * 100
}

// GetETA returns the estimated time to completion
func (p *ProgressState) GetETA() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.CurrentStep == 0 || p.Status != "in_progress" {
		return 0
	}

	elapsed := time.Since(p.StartedAt).Seconds()
	avgTime := elapsed / float64(p.CurrentStep)
	remaining := float64(p.TotalSteps-p.CurrentStep) * avgTime

	return time.Duration(remaining) * time.Second
}
