package circuit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the circuit breaker state
type State int32

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateHalfOpen:
		return "HALF_OPEN"
	case StateOpen:
		return "OPEN"
	default:
		return "UNKNOWN"
	}
}

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// CircuitBreaker is a circuit breaker implementation
type CircuitBreaker struct {
	name           string
	maxRequests    uint32
	interval       time.Duration
	timeout        time.Duration
	readyToTrip    func(counts Counts) bool
	onStateChange  func(name string, from State, to State)
	mu             sync.Mutex
	state          atomic.Value // State
	generation     atomic.Value // uint64
	counts         atomic.Value // *Counts
}

// Counts holds internal counters
type Counts struct {
	Requests             uint32
	TotalSuccesses       uint32
	TotalFailures        uint32
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

// Config holds circuit breaker configuration
type Config struct {
	Name          string
	MaxRequests   uint32
	Interval      time.Duration
	Timeout       time.Duration
	ReadyToTrip   func(Counts) bool
	OnStateChange func(name string, from State, to State)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config Config) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:        config.Name,
		maxRequests: config.MaxRequests,
		interval:    config.Interval,
		timeout:     config.Timeout,
		readyToTrip: config.ReadyToTrip,
		onStateChange: config.OnStateChange,
	}

	if cb.maxRequests == 0 {
		cb.maxRequests = 100
	}
	if cb.interval == 0 {
		cb.interval = 30 * time.Second
	}
	if cb.timeout == 0 {
		cb.timeout = 60 * time.Second
	}
	if cb.readyToTrip == nil {
		cb.readyToTrip = func(counts Counts) bool {
			return counts.Requests >= cb.maxRequests && counts.ConsecutiveFailures > 5
		}
	}

	cb.state.Store(StateClosed)
	cb.generation.Store(uint64(0))
	cb.counts.Store(&Counts{})

	return cb
}

// Execute runs the given function if the circuit breaker allows it
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if cb.state.Load().(State) == StateOpen {
		return ErrCircuitOpen
	}

	// Increment requests
	counts := cb.counts.Load().(*Counts)
	atomic.AddUint32(&counts.Requests, 1)

	err := fn()

	// Update counters based on result
	cb.updateCounters(err)

	// Check if we should trip the breaker
	if cb.shouldTrip(counts) {
		cb.trip()
		return ErrCircuitOpen
	}

	// Check if we should move to half-open
	if cb.state.Load().(State) == StateHalfOpen && err == nil {
		cb.reset()
	}

	return err
}

// ExecuteContext runs the given function with context support
func (cb *CircuitBreaker) ExecuteContext(ctx context.Context, fn func() error) error {
	generation := cb.currentGeneration()

	if cb.state.Load() == StateOpen {
		// Check if timeout has passed
		if time.Since(time.Unix(0, int64(generation))) > cb.timeout {
			cb.mu.Lock()
			if cb.state.Load() == StateOpen {
				cb.setState(StateHalfOpen)
			}
			cb.mu.Unlock()
		} else {
				return ErrCircuitOpen
			}
	}

	// In half-open state, limit concurrent requests
	if cb.state.Load() == StateHalfOpen && counts().Requests >= 1 {
		return ErrTooManyRequests
	}

	// Increment and execute
	counts := cb.counts.Load().(*Counts)
	atomic.AddUint32(&counts.Requests, 1)

	err := fn()

	// Update counters and state
	cb.updateCounters(err)

	if cb.shouldTrip(counts) {
		cb.trip()
		return ErrCircuitOpen
	}

	if err == nil && cb.state.Load() == StateHalfOpen {
		cb.reset()
	}

	return err
}

// State returns the current state
func (cb *CircuitBreaker) State() State {
	return cb.state.Load().(State)
}

// Counts returns the current counts (for testing)
func (cb *CircuitBreaker) Counts() Counts {
	return *cb.counts.Load().(*Counts)
}

func (cb *CircuitBreaker) currentGeneration() uint64 {
	return cb.generation.Load().(uint64)
}

func (cb *CircuitBreaker) setState(state State) {
	oldState := cb.state.Load().(State)
	if oldState == state {
		return
	}

	cb.state.Store(state)
	cb.generation.Store(uint64(time.Now().Unix()))

	// Reset counters when moving to closed or half-open
	if state == StateClosed || state == StateHalfOpen {
		cb.counts.Store(&Counts{})
	}

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, oldState, state)
	}
}

func (cb *CircuitBreaker) trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateOpen)
}

func (cb *CircuitBreaker) reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed)
}

func (cb *CircuitBreaker) shouldTrip(counts *Counts) bool {
	if counts == nil {
		return false
	}
	return cb.readyToTrip(*counts)
}

func (cb *CircuitBreaker) updateCounters(err error) {
	counts := cb.counts.Load().(*Counts)

	if err != nil {
		atomic.AddUint32(&counts.TotalFailures, 1)
		atomic.AddUint32(&counts.ConsecutiveFailures, 1)
		atomic.StoreUint32(&counts.ConsecutiveSuccesses, 0)
	} else {
		atomic.AddUint32(&counts.TotalSuccesses, 1)
		atomic.AddUint32(&counts.ConsecutiveSuccesses, 1)
		atomic.StoreUint32(&counts.ConsecutiveFailures, 0)
	}
}

func counts() Counts {
	// Helper for inline count access
	return Counts{}
}

// Manager manages multiple circuit breakers
type Manager struct {
	mu    sync.RWMutex
	breakers map[string]*CircuitBreaker
}

// NewManager creates a new circuit breaker manager
func NewManager() *Manager {
	return &Manager{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// Get gets or creates a circuit breaker
func (m *Manager) Get(name string, config Config) *CircuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if cb, exists := m.breakers[name]; exists {
		return cb
	}

	config.Name = name
	cb = NewCircuitBreaker(config)
	m.breakers[name] = cb
	return cb
}

// Execute executes a function through the named circuit breaker
func (m *Manager) Execute(name string, fn func() error) error {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if !exists {
		return fn()
	}

	return cb.Execute(fn)
}
