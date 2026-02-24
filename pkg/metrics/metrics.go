package metrics

import (
	"sync"
	"time"
)

// MetricType defines the type of metric
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
)

// Metric represents a single metric
type Metric struct {
	Type      MetricType
	Name      string
	Value     float64
	Timestamp time.Time
	Labels    map[string]string
}

// MetricsCollector collects and tracks metrics
type MetricsCollector struct {
	mu      sync.RWMutex
	metrics map[string][]*Metric
}

var globalCollector = &MetricsCollector{
	metrics: make(map[string][]*Metric),
}

// GetCollector returns the global metrics collector
func GetCollector() *MetricsCollector {
	return globalCollector
}

// IncrementCounter increments a counter metric
func (m *MetricsCollector) IncrementCounter(name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric := &Metric{
		Type:      MetricTypeCounter,
		Name:      name,
		Value:     1,
		Timestamp: time.Now(),
		Labels:    labels,
	}

	m.metrics[name] = append(m.metrics[name], metric)
}

// SetGauge sets a gauge metric value
func (m *MetricsCollector) SetGauge(name string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric := &Metric{
		Type:      MetricTypeGauge,
		Name:      name,
		Value:     value,
		Timestamp: time.Now(),
		Labels:    labels,
	}

	m.metrics[name] = append(m.metrics[name], metric)
}

// RecordDuration records a duration metric (in milliseconds)
func (m *MetricsCollector) RecordDuration(name string, duration time.Duration, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric := &Metric{
		Type:      MetricTypeHistogram,
		Name:      name,
		Value:     float64(duration.Milliseconds()),
		Timestamp: time.Now(),
		Labels:    labels,
	}

	m.metrics[name] = append(m.metrics[name], metric)
}

// GetMetrics returns all metrics for a given name
func (m *MetricsCollector) GetMetrics(name string) []*Metric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if metrics, exists := m.metrics[name]; exists {
		return metrics
	}
	return nil
}

// GetAllMetrics returns all collected metrics
func (m *MetricsCollector) GetAllMetrics() map[string][]*Metric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string][]*Metric)
	for name, metrics := range m.metrics {
		result[name] = append([]*Metric{}, metrics...)
	}
	return result
}

// ClearMetrics clears all metrics (useful for testing or periodic cleanup)
func (m *MetricsCollector) ClearMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metrics = make(map[string][]*Metric)
}

// Metric names
const (
	// API call metrics
	MetricAPICall       = "api_call"
	MetricAPICallDuration = "api_call_duration_ms"
	MetricAPICallFailed  = "api_call_failed"

	// User interaction metrics
	MetricUserAction = "user_action"
	MetricCallback    = "callback_processed"

	// System metrics
	MetricActiveSessions   = "active_sessions"
	MetricCacheHit         = "cache_hit"
	MetricCacheMiss        = "cache_miss"
	MetricSessionCleanup   = "session_cleanup"
	MetricSaveOperation    = "save_operation"
)

// Helper functions for common metric operations

// IncrementAPICall increments the API call counter
func IncrementAPICall(service, method string) {
	labels := map[string]string{
		"service": service,
		"method":  method,
	}
	globalCollector.IncrementCounter(MetricAPICall, labels)
}

// RecordAPICallFailure records an API call failure
func RecordAPICallFailure(service, method string) {
	labels := map[string]string{
		"service": service,
		"method":  method,
	}
	globalCollector.IncrementCounter(MetricAPICallFailed, labels)
}

// RecordAPICallDuration records an API call duration
func RecordAPICallDuration(service, method string, duration time.Duration) {
	labels := map[string]string{
		"service": service,
		"method":  method,
	}
	globalCollector.RecordDuration(MetricAPICallDuration, duration, labels)
}

// RecordUserAction records a user action
func RecordUserAction(action, userID string) {
	labels := map[string]string{
		"action": action,
		"user":   userID,
	}
	globalCollector.IncrementCounter(MetricUserAction, labels)
}

// RecordCallback records a callback processing
func RecordCallback(callbackType, status string) {
	labels := map[string]string{
		"type":   callbackType,
		"status": status,
	}
	globalCollector.IncrementCounter(MetricCallback, labels)
}

// RecordCacheHit records a cache hit
func RecordCacheHit(cacheName string) {
	labels := map[string]string{
		"cache": cacheName,
	}
	globalCollector.IncrementCounter(MetricCacheHit, labels)
}

// RecordCacheMiss records a cache miss
func RecordCacheMiss(cacheName string) {
	labels := map[string]string{
		"cache": cacheName,
	}
	globalCollector.IncrementCounter(MetricCacheMiss, labels)
}

// UpdateActiveSessions updates the active sessions gauge
func UpdateActiveSessions(count int) {
	globalCollector.SetGauge(MetricActiveSessions, float64(count), nil)
}
