package prometheus

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"emby-telegram-bot/internal/metrics"
)

var (
	registry = metrics.GetRegistry()
	mu        sync.RWMutex
)

// Handler returns the Prometheus metrics endpoint handler
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")

		// Gather all metrics
		metricsData := gatherMetrics()

		// Write in Prometheus text format
		for _, metric := range metricsData {
			w.WriteString(metric)
		}
	}
}

// Metric represents a single Prometheus metric
type Metric struct {
	Name   string
	Type   string   // counter, gauge, histogram, summary
	Help   string
	Values []MetricValue
}

// MetricValue represents a single metric value with labels
type MetricValue struct {
	Value   float64
	Labels  map[string]string
}

// gatherMetrics collects all metrics in Prometheus format
func gatherMetrics() []string {
	var lines []string

	lines = append(lines, "# HELP")
	lines = append(lines, "# Metrics endpoint for emby-telegram-bot")
	lines = append(lines, "")

	// Counters
	counters := map[string]*metrics.Counter{
		"requests_total":                    metrics.RequestTotal,
		"requests_success_total":               metrics.RequestSuccess,
		"requests_error_total":                 metrics.RequestError,
		"telegram_messages_sent_total":          metrics.TelegramMessageSent,
		"telegram_messages_error_total":         metrics.TelegramMessageError,
		"telegram_callbacks_total":              metrics.TelegramCallbackTotal,
		"jellyseerr_requests_total":            metrics.JellyseerrRequestTotal,
		"jellyseerr_requests_error_total":        metrics.JellyseerrRequestError,
	}

	for name, counter := range counters {
		lines = append(lines, formatCounter(name, counter))
	}

	// Gauges
	gauges := map[string]*metrics.Gauge{
		"active_users":      metrics.ActiveUsers,
		"total_requests":    metrics.TotalRequests,
		"goroutines":       metrics.Goroutines,
		"memory_alloc_bytes": metrics.MemoryAlloc,
	}

	for name, gauge := range gauges {
		lines = append(lines, formatGauge(name, gauge))
	}

	// Histograms
	histograms := map[string]*metrics.Histogram{
		"request_duration_ms":     metrics.RequestDuration,
		"jellyseerr_request_duration_ms": metrics.JellyseerrRequestDuration,
	}

	for name, hist := range histograms {
		lines = append(lines, formatHistogram(name, hist)...)
	}

	return lines
}

// formatCounter formats a counter metric
func formatCounter(name string, counter *metrics.Counter) string {
	help := "Total number of " + name
	line := fmt.Sprintf("# HELP %s\n", name)
	line += fmt.Sprintf("# TYPE %s\n", "counter")
	line += fmt.Sprintf("# %s\n\n", help)

	value := counter.Get()
	line += fmt.Sprintf("%s %d", name, value)

	return line
}

// formatGauge formats a gauge metric
func formatGauge(name string, gauge *metrics.Gauge) string {
	help := "Current value of " + name
	line := fmt.Sprintf("# HELP %s\n", name)
	line += fmt.Sprintf("# TYPE %s\n", "gauge")
	line += fmt.Sprintf("# %s\n\n", help)

	line += fmt.Sprintf("%s %d", name, gauge.Get())

	return line
}

// formatHistogram formats a histogram metric
func formatHistogram(name string, histogram *metrics.Histogram) string {
	help := "Distribution of " + name
	line := fmt.Sprintf("# HELP %s\n", name)
	line += fmt.Sprintf("# TYPE histogram\n")
	line += fmt.Sprintf("# %s\n\n", help)

	// P50, P90, P95, P99
	p50 := histogram.Percentile(0.50)
	p90 := histogram.Percentile(0.90)
	p95 := histogram.Percentile(0.95)
	p99 := histogram.Percentile(0.99)

	line += fmt.Sprintf("%s_quantile{p=\"0.5\"} %.2f\n", name, p50)
	line += fmt.Sprintf("%s_quantile{p=\"0.9\"} %.2f\n", name, p90)
	line += fmt.Sprintf("%s_quantile{p=\"0.95\"} %.2f\n", name, p95)
	line += fmt.Sprintf("%s_quantile{p=\"0.99\"} %.2f\n", name, p99)

	line += fmt.Sprintf("%s_sum %d\n", name, histogram.Count())

	return line
}

// MetricsHandler returns a JSON metrics endpoint for dashboard
func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	data := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"uptime":    getUptime(),
		"metrics":   getDetailedMetrics(),
		"system":    getSystemMetrics(),
	}

	json.NewEncoder(w).Encode(data)
}

// getUptime returns process uptime in seconds
func getUptime() int64 {
	return time.Since(startTime).Seconds()
}

var startTime = time.Now()

// getDetailedMetrics returns detailed metrics
func getDetailedMetrics() map[string]interface{} {
	return map[string]interface{}{
		"requests": map[string]interface{}{
			"total":    metrics.RequestTotal.Get(),
			"success": metrics.RequestSuccess.Get(),
			"error":   metrics.RequestError.Get(),
		},
		"telegram": map[string]interface{}{
			"messages_sent":   metrics.TelegramMessageSent.Get(),
			"messages_error":  metrics.TelegramMessageError.Get(),
			"callbacks_total": metrics.TelegramCallbackTotal.Get(),
		},
		"jellyseerr": map[string]interface{}{
			"requests_total":   metrics.JellyseerrRequestTotal.Get(),
			"requests_error":   metrics.JellyseerrRequestError.Get(),
			"p50_duration_ms": metrics.JellyseerrRequestDuration.Percentile(0.50),
			"p95_duration_ms": metrics.JellyseerrRequestDuration.Percentile(0.95),
			"p99_duration_ms": metrics.JellyseerrRequestDuration.Percentile(0.99),
		},
		"users": map[string]interface{}{
			"active": metrics.ActiveUsers.Get(),
		},
	}
}

// getSystemMetrics returns system metrics
func getSystemMetrics() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"goroutines":  runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc":       m.Alloc,
			"total_alloc": m.TotalAlloc,
			"sys":          m.Sys,
			"heap_alloc":  m.HeapAlloc,
			"heap_inuse":  m.HeapInuse,
			"heap_released": m.HeapReleased,
			"stack_inuse":  m.StackInuse,
			"stack_inuse_bytes":  m.StackInuse,
		},
		"gc": map[string]interface{}{
			"num_gc":          m.NumGC,
			"num_force_gc":     m.NumForcedGC,
			"gc_pause_fraction": m.GCPauseFraction,
		},
	}
}

// StartMetricsServer starts the metrics server
func StartMetricsServer(addr string) error {
	mu.Lock()
	defer mu.Unlock()

	// Check if already running
	if _, ok := registry.(*metrics.Registry); !ok {
		return fmt.Errorf("metrics registry not initialized")
	}

	log.Printf("[Prometheus] Starting metrics server on %s", addr)

	// Start metrics collection
	metrics.InitSystemMetrics()

	// Start HTTP server in background
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", Handler())
		mux.HandleFunc("/metrics/json", MetricsHandler)

		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[Prometheus] Metrics server error: %v", err)
		}
	}()

	return nil
}

// RecordRequest records a request metric
func RecordRequest(success bool, duration time.Duration) {
	metrics.RequestTotal.Inc()
	if success {
		metrics.RequestSuccess.Inc()
	} else {
		metrics.RequestError.Inc()
	}
	metrics.RequestDuration.obe(float64(duration.Milliseconds()))
}

// RecordTelegramMessage records a Telegram message
func RecordTelegramMessage(success bool) {
	metrics.TelegramMessageSent.Inc()
	if !success {
		metrics.TelegramMessageError.Inc()
	}
}

// RecordTelegramCallback records a Telegram callback
func RecordTelegramCallback() {
	metrics.TelegramCallbackTotal.Inc()
}

// RecordJellyseerrRequest records a Jellyseerr API call
func RecordJellyseerrRequest(success bool, duration time.Duration) {
	metrics.JellyseerrRequestTotal.Inc()
	if !success {
		metrics.JellyseerrRequestError.Inc()
	}
	metrics.JellyseerrRequestDuration.Observe(float64(duration.Milliseconds()))
}

// UpdateActiveUsers updates the active users gauge
func UpdateActiveUsers(count int) {
	metrics.ActiveUsers.Set(int64(count))
}
