package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Level represents log level
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
	LevelNone
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
	LevelFatal: "FATAL",
	LevelNone:  "NONE",
}

func (l Level) String() string {
	return levelNames[l]
}

// ParseLevel parses a level from string
func ParseLevel(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "FATAL":
		return LevelFatal
	case "NONE":
		return LevelNone
	default:
		return LevelInfo
	}
}

// Field represents a log field
type Field struct {
	Key   string
	Value interface{}
}

// Logger is the structured logger interface
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
	WithRequestID(id string) Logger
	SetLevel(level Level)
	GetLevel() Level
	AddWriter(w io.Writer)
	Close() error
}

// F creates a new log field
func F(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// StdLogger is the standard logger implementation
type StdLogger struct {
	mu            sync.RWMutex
	level         atomic.Value // Level
	writers       []io.Writer
	requestID     atomic.Value // string
	fields        atomic.Value // []Field
	ctx           context.Context
	flushInterval time.Duration
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewLogger creates a new logger
func NewLogger(level Level) *StdLogger {
	l := &StdLogger{
		writers:       []io.Writer{os.Stdout},
		flushInterval: 30 * time.Second,
		done:          make(chan struct{}),
	}
	l.level.Store(level)
	l.fields.Store([]Field{})
	l.ctx = context.Background()

	// Start flush goroutine
	l.wg.Add(1)
	go l.flushLoop()

	return l
}

// SetLevel sets the log level
func (l *StdLogger) SetLevel(level Level) {
	l.level.Store(level)
}

// GetLevel returns the current log level
func (l *StdLogger) GetLevel() Level {
	return l.level.Load().(Level)
}

// AddWriter adds a writer to the logger
func (l *StdLogger) AddWriter(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writers = append(l.writers, w)
}

// With adds persistent fields to the logger
func (l *StdLogger) With(fields ...Field) Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newLogger := &StdLogger{
		writers:       make([]io.Writer, len(l.writers)),
		flushInterval: l.flushInterval,
		done:          l.done,
		ctx:           l.ctx,
	}
	newLogger.level.Store(l.level.Load())
	newLogger.requestID.Store(l.requestID.Load())
	newLogger.fields.Store(l.fields.Load())

	copy(newLogger.writers, l.writers)

	// Merge existing fields with new fields
	oldFields := l.fields.Load().([]Field)
	mergedFields := make([]Field, len(oldFields), len(oldFields)+len(fields))
	copy(mergedFields, oldFields)
	mergedFields = append(mergedFields, fields...)
	newLogger.fields.Store(mergedFields)

	return newLogger
}

// WithContext adds context to the logger
func (l *StdLogger) WithContext(ctx context.Context) Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newLogger := &StdLogger{
		writers:       make([]io.Writer, len(l.writers)),
		flushInterval: l.flushInterval,
		done:          l.done,
		ctx:           ctx,
	}
	newLogger.level.Store(l.level.Load())
	newLogger.requestID.Store(l.requestID.Load())
	newLogger.fields.Store(l.fields.Load())

	copy(newLogger.writers, l.writers)
	return newLogger
}

// WithRequestID adds request ID to the logger
func (l *StdLogger) WithRequestID(id string) Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newLogger := &StdLogger{
		writers:       make([]io.Writer, len(l.writers)),
		flushInterval: l.flushInterval,
		done:          l.done,
		ctx:           l.ctx,
	}
	newLogger.level.Store(l.level.Load())
	newLogger.fields.Store(l.fields.Load())
	newLogger.requestID.Store(id)

	copy(newLogger.writers, l.writers)
	return newLogger
}

// Debug logs a debug message
func (l *StdLogger) Debug(msg string, fields ...Field) {
	l.log(LevelDebug, msg, fields...)
}

// Info logs an info message
func (l *StdLogger) Info(msg string, fields ...Field) {
	l.log(LevelInfo, msg, fields...)
}

// Warn logs a warning message
func (l *StdLogger) Warn(msg string, fields ...Field) {
	l.log(LevelWarn, msg, fields...)
}

// Error logs an error message
func (l *StdLogger) Error(msg string, fields ...Field) {
	l.log(LevelError, msg, fields...)
}

// Fatal logs a fatal message and exits
func (l *StdLogger) Fatal(msg string, fields ...Field) {
	l.log(LevelFatal, msg, fields...)
	os.Exit(1)
}

// log is the internal logging method
func (l *StdLogger) log(level Level, msg string, fields ...Field) {
	if level < l.GetLevel() {
		return
	}

	// Get caller info
	_, file, line, _ := runtime.Caller(2)

	// Build log entry
	entry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"level":     level.String(),
		"message":   msg,
		"file":      filepath.Base(file),
		"line":      line,
	}

	// Add request ID if present
	if reqID := l.requestID.Load(); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			entry["request_id"] = reqIDStr
		}
	}

	// Add persistent fields
	for _, f := range l.fields.Load().([]Field) {
		entry[f.Key] = f.Value
	}

	// Add temporary fields
	for _, f := range fields {
		entry[f.Key] = f.Value
	}

	// Format and write
	formatted := l.format(entry)
	l.mu.Lock()
	for _, w := range l.writers {
		w.Write([]byte(formatted))
		w.Write([]byte("\n"))
	}
	l.mu.Unlock()

	// Flush on fatal or error
	if level >= LevelError {
		l.flush()
	}

	if level == LevelFatal {
		os.Exit(1)
	}
}

// format formats a log entry
func (l *StdLogger) format(entry map[string]interface{}) string {
	return fmt.Sprintf("[%s] %s %-5s %s",
		entry["timestamp"],
		getRequestID(entry),
		entry["level"],
		entry["message"],
	)
}

func getRequestID(entry map[string]interface{}) string {
	if v, ok := entry["request_id"]; ok && v != "" {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "-"
}

// flush flushes all writers
func (l *StdLogger) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, w := range l.writers {
		if f, ok := w.(interface{ Sync() error }); ok {
			f.Sync()
		}
	}
}

// flushLoop runs periodic flush
func (l *StdLogger) flushLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.flush()
		case <-l.done:
			return
		}
	}
}

// Close closes the logger
func (l *StdLogger) Close() error {
	close(l.done)
	l.wg.Wait()
	l.flush()
	return nil
}

// Global logger instance
var std = NewLogger(LevelInfo)

// Package-level convenience functions
func SetLevel(level Level) {
	std.SetLevel(level)
}

func SetOutput(w io.Writer) {
	std.AddWriter(w)
}

func Debug(msg string, fields ...Field) {
	std.Debug(msg, fields...)
}

func Info(msg string, fields ...Field) {
	std.Info(msg, fields...)
}

func Warn(msg string, fields ...Field) {
	std.Warn(msg, fields...)
}

func Error(msg string, fields ...Field) {
	std.Error(msg, fields...)
}

func Fatal(msg string, fields ...Field) {
	std.Fatal(msg, fields...)
}

func With(fields ...Field) Logger {
	return std.With(fields...)
}

func WithContext(ctx context.Context) Logger {
	return std.WithContext(ctx)
}

func WithRequestID(id string) Logger {
	return std.WithRequestID(id)
}

// AddLogFile adds a file writer to the logger
func AddLogFile(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Open file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	std.AddWriter(f)
	return nil
}
