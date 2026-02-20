package metrics

import (
	"runtime"
	"sync"
	"time"
)

// Registry holds all metrics
type Registry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

var (
	registry = &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
)

// Counter is a monotonically increasing counter
type Counter struct {
	mu    sync.Mutex
	name  string
	value uint64
}

// NewCounter creates a new counter
func NewCounter(name string) *Counter {
	c := &Counter{name: name}
	registry.mu.Lock()
	registry.counters[name] = c
	registry.mu.Unlock()
	return c
}

// Inc increments the counter
func (c *Counter) Inc() {
	c.mu.Lock()
	c.value++
	c.mu.Unlock()
}

// Add adds a value to the counter
func (c *Counter) Add(delta uint64) {
	c.mu.Lock()
	c.value += delta
	c.mu.Unlock()
}

// Get returns the current value
func (c *Counter) Get() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Gauge is a metric that can go up or down
type Gauge struct {
	mu    sync.Mutex
	name  string
	value int64
}

// NewGauge creates a new gauge
func NewGauge(name string) *Gauge {
	g := &Gauge{name: name}
	registry.mu.Lock()
	registry.gauges[name] = g
	registry.mu.Unlock()
	return g
}

// Set sets the gauge value
func (g *Gauge) Set(value int64) {
	g.mu.Lock()
	g.value = value
	g.mu.Unlock()
}

// Inc increments the gauge
func (g *Gauge) Inc() {
	g.mu.Lock()
	g.value++
	g.mu.Unlock()
}

// Dec decrements the gauge
func (g *Gauge) Dec() {
	g.mu.Lock()
	g.value--
	g.mu.Unlock()
}

// Get returns the current value
func (g *Gauge) Get() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.value
}

// Histogram tracks distributions of values
type Histogram struct {
	mu      sync.Mutex
	name    string
	values  []float64
	maxSize int
}

// NewHistogram creates a new histogram
func NewHistogram(name string, maxSize int) *Histogram {
	h := &Histogram{
		name:    name,
		maxSize: maxSize,
		values:  make([]float64, 0, maxSize),
	}
	registry.mu.Lock()
	registry.histograms[name] = h
	registry.mu.Unlock()
	return h
}

// Observe records a value
func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.values = append(h.values, value)
	if len(h.values) > h.maxSize {
		// Remove oldest value
		h.values = h.values[1:]
	}
}

// Percentile returns the approximate percentile value
func (h *Histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}

	// Simple implementation - sort and return percentile
	values := make([]float64, len(h.values))
	copy(values, h.values)

	// Simple sort
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}

	index := int(float64(len(values)) * p / 100)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

// Count returns the number of observations
func (h *Histogram) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.values)
}

// GetCounter returns a counter by name
func GetCounter(name string) *Counter {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	if c, ok := registry.counters[name]; ok {
		return c
	}

	c := NewCounter(name)
	return c
}

// GetGauge returns a gauge by name
func GetGauge(name string) *Gauge {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	if g, ok := registry.gauges[name]; ok {
		return g
	}

	g := NewGauge(name)
	return g
}

// GetHistogram returns a histogram by name
func GetHistogram(name string) *Histogram {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	if h, ok := registry.histograms[name]; ok {
		return h
	}

	h := NewHistogram(name, 1000)
	return h
}

// System metrics
var (
	// Request metrics
	RequestTotal    = NewCounter("requests_total")
	RequestSuccess  = NewCounter("requests_success")
	RequestError    = NewCounter("requests_error")
	RequestDuration = NewHistogram("request_duration_ms", 1000)

	// Telegram metrics
	TelegramMessageSent    = NewCounter("telegram_messages_sent")
	TelegramMessageError   = NewCounter("telegram_messages_error")
	TelegramCallbackTotal  = NewCounter("telegram_callbacks_total")

	// Jellyseerr metrics
	JellyseerrRequestTotal  = NewCounter("jellyseerr_requests_total")
	JellyseerrRequestError  = NewCounter("jellyseerr_requests_error")
	JellyseerrRequestDuration = NewHistogram("jellyseerr_request_duration_ms", 500)

	// User metrics
	ActiveUsers   = NewGauge("active_users")
	TotalRequests = NewGauge("total_requests_today")

	// System metrics
	Goroutines = NewGauge("goroutines")
	MemoryAlloc = NewGauge("memory_alloc_bytes")
)

// InitSystemMetrics initializes system metric collection
func InitSystemMetrics() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			updateSystemMetrics()
		}
	}()
}

func updateSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	Goroutines.Set(int64(runtime.NumGoroutine()))
	MemoryAlloc.Set(int64(m.Alloc))
}
