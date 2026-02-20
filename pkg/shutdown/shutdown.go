package shutdown

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Hook is a function that performs cleanup
type Hook struct {
	Name    string
	Timeout time.Duration
	Func    func(context.Context) error
}

// Manager manages graceful shutdown
type Manager struct {
	hooks     []Hook
	timeout   time.Duration
	mu        sync.Mutex
	signals   []os.Signal
	shutdownCh chan struct{}
	done      chan struct{}
}

// NewManager creates a new shutdown manager
func NewManager(timeout time.Duration) *Manager {
	return &Manager{
		timeout:    timeout,
		shutdownCh: make(chan struct{}),
		done:       make(chan struct{}),
		signals:   []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}
}

// Add adds a shutdown hook
func (m *Manager) Add(name string, timeout time.Duration, fn func(context.Context) error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hooks = append(m.hooks, Hook{
		Name:    name,
		Timeout: timeout,
		Func:    fn,
	})
}

// AddDefault adds a hook with default timeout
func (m *Manager) AddDefault(name string, fn func(context.Context) error) {
	m.Add(name, 30*time.Second, fn)
}

// Start begins listening for shutdown signals
func (m *Manager) Start() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, m.signals...)

	go func() {
		select {
		case sig := <-sigChan:
			log.Printf("[Shutdown] Received signal: %v", sig)
			m.Trigger()
		case <-m.shutdownCh:
			// Manual shutdown
		}
	}()
}

// Trigger triggers graceful shutdown
func (m *Manager) Trigger() {
	select {
	case <-m.shutdownCh:
		return // Already shutting down
	default:
		close(m.shutdownCh)
	}

	m.runHooks()
	close(m.done)
}

// Wait waits for shutdown to complete
func (m *Manager) Wait() {
	<-m.done
}

// Done returns a channel that closes when shutdown is complete
func (m *Manager) Done() <-chan struct{} {
	return m.done
}

// IsShuttingDown returns true if shutdown is in progress
func (m *Manager) IsShuttingDown() bool {
	select {
	case <-m.shutdownCh:
		return true
	default:
		return false
	}
}

// runHooks runs all shutdown hooks
func (m *Manager) runHooks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("[Shutdown] Starting graceful shutdown (%d hooks)...", len(m.hooks))

	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	// Run hooks concurrently with a semaphore to limit concurrency
	const maxConcurrency = 5
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, hook := range m.hooks {
		wg.Add(1)
		go func(h Hook) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			log.Printf("[Shutdown] Running hook: %s", h.Name)

			hookCtx, hookCancel := context.WithTimeout(ctx, h.Timeout)
			defer hookCancel()

			if err := h.Func(hookCtx); err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					log.Printf("[Shutdown] Hook %s error: %v", h.Name, err)
				}
			}

			log.Printf("[Shutdown] Completed hook: %s", h.Name)
		}(hook)
	}

	wg.Wait()

	log.Printf("[Shutdown] All hooks completed")
}

// Global shutdown manager instance
var std = NewManager(30 * time.Second)

// Add adds a hook to the global manager
func Add(name string, timeout time.Duration, fn func(context.Context) error) {
	std.Add(name, timeout, fn)
}

// AddDefault adds a hook with default timeout
func AddDefault(name string, fn func(context.Context) error) {
	std.AddDefault(name, fn)
}

// Start starts the global shutdown manager
func Start() {
	std.Start()
}

// Trigger triggers shutdown
func Trigger() {
	std.Trigger()
}

// Wait waits for shutdown to complete
func Wait() {
	std.Wait()
}

// Done returns the done channel
func Done() <-chan struct{} {
	return std.Done()
}

// IsShuttingDown checks if shutdown is in progress
func IsShuttingDown() bool {
	return std.IsShuttingDown()
}

// RegisterSignalHandler registers signal handlers for common termination signals
func RegisterSignalHandler() {
	Start()
}
