package retry

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Policy defines the retry policy
type Policy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Retriable    func(error) bool
	OnRetry      func(attempt int, err error)
}

// DefaultPolicy returns a default retry policy
func DefaultPolicy() *Policy {
	return &Policy{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
		Retriable:    DefaultRetriable,
	}
}

// HTTPPolicy returns a policy optimized for HTTP requests
func HTTPPolicy() *Policy {
	return &Policy{
		MaxAttempts:  3,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		Retriable:    IsRetriableHTTP,
	}
}

// DefaultRetriable is the default function to determine if an error is retriable
func DefaultRetriable(err error) bool {
	return err != nil
}

// IsRetriableHTTP checks if an HTTP error is retriable
func IsRetriableHTTP(err error) bool {
	if err == nil {
		return false
	}
	// Check if the error should be retried
	// This is a simple implementation - you might want to check for specific error types
	return true
}

// Do executes the given function with retry logic
func (p *Policy) Do(fn func() error) error {
	var lastErr error
	delay := p.InitialDelay

	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		if attempt > 0 {
			if p.OnRetry != nil {
				p.OnRetry(attempt, lastErr)
			}
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * p.Multiplier)
			if delay > p.MaxDelay {
				delay = p.MaxDelay
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		if !p.Retriable(err) {
			return fmt.Errorf("non-retriable error on attempt %d: %w", attempt+1, err)
		}
	}

	return fmt.Errorf("failed after %d attempts, last error: %w", p.MaxAttempts, lastErr)
}

// DoContext executes the given function with retry logic and context support
func (p *Policy) DoContext(ctx context.Context, fn func() error) error {
	delay := p.InitialDelay

	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				if p.OnRetry != nil {
					p.OnRetry(attempt, fmt.Errorf("retrying after error"))
				}
			}
			delay = time.Duration(math.Min(float64(delay)*p.Multiplier, float64(p.MaxDelay)))
		}

		err := fn()
		if err == nil {
			return nil
		}

		if !p.Retriable(err) {
			return fmt.Errorf("non-retriable error on attempt %d: %w", attempt+1, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return fmt.Errorf("failed after %d attempts with context", p.MaxAttempts)
}

// DoWithResult executes a function that returns a result with retry logic
func DoWithResult[T any](p *Policy, fn func() (T, error)) (T, error) {
	var lastErr error
	var zero T
	delay := p.InitialDelay

	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		if attempt > 0 {
			if p.OnRetry != nil {
				p.OnRetry(attempt, lastErr)
			}
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * p.Multiplier)
			if delay > p.MaxDelay {
				delay = p.MaxDelay
			}
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !p.Retriable(err) {
			return zero, fmt.Errorf("non-retriable error on attempt %d: %w", attempt+1, err)
		}
	}

	return zero, fmt.Errorf("failed after %d attempts, last error: %w", p.MaxAttempts, lastErr)
}

// Backoff calculates the delay for a given attempt using exponential backoff with jitter
func Backoff(attempt int, base, max time.Duration) time.Duration {
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	delay := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	if delay > max {
		delay = max
	}
	// Add jitter
	delay = time.Duration(float64(delay) * (0.8 + 0.4*float64(time.Now().UnixNano()%1000)/1000))
	return delay
}
