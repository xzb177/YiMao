package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emby-telegram-bot/internal/circuit"
	apperrors "emby-telegram-bot/internal/errors"
	"emby-telegram-bot/internal/metrics"
	"emby-telegram-bot/pkg/retry"
)

// Client is an enhanced HTTP client with retry, circuit breaker, and metrics
type Client struct {
	baseClient    *http.Client
	circuitBreaker *circuit.CircuitBreaker
	retryPolicy    *retry.Policy
	baseURL        string
	defaultHeaders map[string]string
	name           string
}

// Config holds client configuration
type Config struct {
	Name            string
	BaseURL         string
	Timeout         time.Duration
	MaxRetries      int
	RetryDelay      time.Duration
	CircuitBreaker  bool
	EnableMetrics   bool
	DefaultHeaders  map[string]string
}

// NewClient creates a new HTTP client
func NewClient(config Config) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	client := &Client{
		baseClient: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		baseURL:        strings.TrimSuffix(config.BaseURL, "/"),
		defaultHeaders: config.DefaultHeaders,
		name:           config.Name,
	}

	// Setup retry policy
	if config.MaxRetries > 0 {
		client.retryPolicy = &retry.Policy{
			MaxAttempts:  config.MaxRetries + 1,
			InitialDelay: config.RetryDelay,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
			Retriable:    retry.IsRetriableHTTP,
			OnRetry: func(attempt int, err error) {
				fmt.Printf("[Client] %s: retry attempt %d after error: %v\n", config.Name, attempt, err)
			},
		}
	}

	// Setup circuit breaker
	if config.CircuitBreaker {
		client.circuitBreaker = circuit.NewCircuitBreaker(circuit.Config{
			Name:        config.Name,
			MaxRequests: 10,
			Interval:    30 * time.Second,
			Timeout:     60 * time.Second,
			OnStateChange: func(name string, from, to circuit.State) {
				fmt.Printf("[Client] Circuit breaker %s: %s -> %s\n", name, from, to)
			},
		})
	}

	return client
}

// Do executes an HTTP request with retry and circuit breaker
func (c *Client) Do(ctx context.Context, method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	start := time.Now()

	// Build request
	req, err := c.buildRequest(ctx, method, path, body, headers)
	if err != nil {
		return nil, apperrors.Wrap(err, "ERR_BUILD_REQUEST", "failed to build request")
	}

	// Execute with retry and circuit breaker
	var resp *http.Response
	var lastErr error

	exec := func() error {
		metrics.JellyseerrRequestTotal.Inc()

		resp, lastErr = c.baseClient.Do(req)
		if lastErr != nil {
			metrics.JellyseerrRequestError.Inc()
			return lastErr
		}

		// Check for non-2xx status codes
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
		}

		metrics.RequestSuccess.Inc()
		return nil
	}

	// Execute with retry
	if c.retryPolicy != nil {
		if err := c.retryPolicy.DoContext(ctx, exec); err != nil {
			return nil, apperrors.ExternalServiceFailed(c.name, err)
		}
	} else {
		if err := exec(); err != nil {
			return nil, apperrors.ExternalServiceFailed(c.name, err)
		}
	}

	// Record metrics
	if c.circuitBreaker != nil {
		duration := time.Since(start)
		metrics.JellyseerrRequestDuration.Observe(float64(duration.Milliseconds()))
	}

	return resp, nil
}

// Get executes a GET request
func (c *Client) Get(ctx context.Context, path string, headers map[string]string) (*http.Response, error) {
	return c.Do(ctx, "GET", path, nil, headers)
}

// Post executes a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	return c.Do(ctx, "POST", path, body, headers)
}

// Put executes a PUT request
func (c *Client) Put(ctx context.Context, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	return c.Do(ctx, "PUT", path, body, headers)
}

// Delete executes a DELETE request
func (c *Client) Delete(ctx context.Context, path string, headers map[string]string) (*http.Response, error) {
	return c.Do(ctx, "DELETE", path, nil, headers)
}

// GetJSON executes a GET request and parses JSON response
func (c *Client) GetJSON(ctx context.Context, path string, result interface{}, headers map[string]string) error {
	resp, err := c.Get(ctx, path, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(result)
}

// PostJSON executes a POST request with JSON body and parses JSON response
func (c *Client) PostJSON(ctx context.Context, path string, body, result interface{}, headers map[string]string) error {
	if headers == nil {
		headers = make(map[string]string)
	}
	headers["Content-Type"] = "application/json"

	resp, err := c.Post(ctx, path, body, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// buildRequest builds an HTTP request
func (c *Client) buildRequest(ctx context.Context, method, path string, body interface{}, headers map[string]string) (*http.Request, error) {
	// Build URL
	fullURL := c.baseURL + "/" + strings.TrimPrefix(path, "/")

	// Build body
	var bodyReader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			bodyReader = strings.NewReader(v)
		case []byte:
			bodyReader = bytes.NewReader(v)
		default:
			jsonData, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			bodyReader = bytes.NewReader(jsonData)
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}

	// Add default headers
	for k, v := range c.defaultHeaders {
		req.Header.Set(k, v)
	}

	// Add custom headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

// BuildURL builds a URL with query parameters
func (c *Client) BuildURL(base string, params map[string]string) string {
	u, _ := url.Parse(base)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Close closes the client and releases resources
func (c *Client) Close() error {
	c.baseClient.CloseIdleConnections()
	return nil
}
