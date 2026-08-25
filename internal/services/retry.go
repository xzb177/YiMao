package services

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts int           // Maximum number of attempts (default 3)
	BaseDelay   time.Duration // Base delay for exponential backoff (default 100ms)
	MaxDelay    time.Duration // Maximum delay between retries (default 5s)
	Multiplier  float64       // Delay multiplier for exponential backoff (default 2)
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		Multiplier:  2.0,
	}
}

// RetryableFunc is a function that can be retried
type RetryableFunc func() error

// IsRetryable checks if an error is retryable
type IsRetryable func(error) bool

// DefaultIsRetryable returns true for network errors and 5xx status codes
func DefaultIsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for timeout errors
	if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
		return true
	}

	// Check for temporary errors
	if netErr, ok := err.(interface{ Temporary() bool }); ok && netErr.Temporary() {
		return true
	}

	// Check for specific HTTP errors
	if httpErr, ok := err.(*HTTPError); ok {
		// Retry on 5xx errors and 429 (Too Many Requests)
		return httpErr.StatusCode >= 500 || httpErr.StatusCode == 429
	}

	return true // Default to retrying on error
}

// RetryHTTP executes an HTTP request with retry logic.
//
// A request that carries a body may only be replayed when net/http captured a
// GetBody rewinder for it (every http.NewRequest with a *bytes.Buffer,
// *bytes.Reader or *strings.Reader body does). Without GetBody the second
// attempt would ship the original Content-Length with an already drained body,
// which upstreams reject with "ContentLength=N with Body length 0". Such
// requests are therefore executed exactly once and their error is returned
// unchanged instead of being retried into a misleading "max retries exceeded".
func RetryHTTP(ctx context.Context, client *http.Client, req *http.Request, cfg *RetryConfig) (*http.Response, error) {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}

	replayable := requestIsReplayable(req)

	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			// Calculate delay with exponential backoff and jitter
			delay := calculateBackoff(attempt, cfg)

			logger.Info("[Retry] Attempt %d/%d for %s, waiting %v",
				attempt+1, cfg.MaxAttempts, req.URL.String(), delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Clone the request for each attempt; the body is rebuilt from GetBody so
		// every attempt sends the exact same payload. A request whose body cannot
		// be rewound is sent once, as-is, and never replayed.
		reqClone := req
		if replayable {
			clone, cloneErr := cloneRequest(req)
			if cloneErr != nil {
				if lastErr != nil {
					return nil, fmt.Errorf("request body cannot be replayed, retry disabled: %w", lastErr)
				}
				return nil, fmt.Errorf("request body cannot be replayed: %w", cloneErr)
			}
			reqClone = clone
		}

		resp, err := client.Do(reqClone)
		if err != nil {
			lastErr = err
			if !DefaultIsRetryable(err) {
				return nil, fmt.Errorf("non-retryable error: %w", err)
			}
			if !replayable {
				return nil, fmt.Errorf("request body cannot be replayed, retry disabled: %w", err)
			}
			continue
		}

		// Check status code
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// Close body before retry
		resp.Body.Close()

		// Don't retry client errors (4xx) except 429
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return nil, &HTTPError{
				StatusCode: resp.StatusCode,
				Err:        fmt.Errorf("client error: %s", resp.Status),
			}
		}

		lastErr = &HTTPError{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status),
		}
		if !replayable {
			return nil, fmt.Errorf("request body cannot be replayed, retry disabled: %w", lastErr)
		}
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxAttempts, lastErr)
}

// Retry executes a function with retry logic
func Retry(fn RetryableFunc, cfg *RetryConfig) error {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}

	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, cfg)
			logger.Info("[Retry] Attempt %d/%d, waiting %v", attempt+1, cfg.MaxAttempts, delay)
			time.Sleep(delay)
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		if !DefaultIsRetryable(err) {
			return err
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxAttempts, lastErr)
}

// calculateBackoff calculates the delay for a given attempt with exponential backoff and jitter
func calculateBackoff(attempt int, cfg *RetryConfig) time.Duration {
	// Exponential backoff: baseDelay * multiplier^(attempt-1)
	delay := float64(cfg.BaseDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Add jitter (±25%)
	jitter := delay * 0.25 * (2.0*(float64(time.Now().UnixNano()%1000)/1000.0) - 1.0)
	delay += jitter

	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	return time.Duration(delay)
}

// requestIsReplayable reports whether the request can be sent more than once
// without truncating its body. Bodyless requests are always replayable.
func requestIsReplayable(req *http.Request) bool {
	if req == nil {
		return false
	}
	if req.Body == nil || req.Body == http.NoBody {
		return true
	}
	if req.ContentLength == 0 {
		return true
	}
	return req.GetBody != nil
}

// cloneRequest copies an HTTP request for a (re)try and rebuilds its body from
// GetBody so Content-Length and the actual payload always agree.
func cloneRequest(req *http.Request) (*http.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("cannot clone nil request")
	}

	r := req.Clone(req.Context())

	// Reset URL if needed (it may have been modified during redirects)
	r.URL = req.URL

	if req.Body != nil && req.Body != http.NoBody && req.ContentLength != 0 {
		if req.GetBody == nil {
			return nil, fmt.Errorf("request body is not rewindable (no GetBody)")
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("rebuild request body: %w", err)
		}
		r.Body = body
		r.ContentLength = req.ContentLength
	}

	return r, nil
}

// HTTPError represents an HTTP error with status code
type HTTPError struct {
	StatusCode int
	Err        error
}

func (e *HTTPError) Error() string {
	return e.Err.Error()
}

func (e *HTTPError) Timeout() bool {
	return false
}

func (e *HTTPError) Temporary() bool {
	// 5xx errors and 429 are considered temporary
	return e.StatusCode >= 500 || e.StatusCode == 429
}
