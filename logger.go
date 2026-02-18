package main

import (
	"fmt"
	"log"
	"os"
	"sync"
)

// LogLevel represents the severity level of a log message
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelNone
)

// Logger is a thread-safe logger with configurable levels
type Logger struct {
	mu       sync.RWMutex
	level    LogLevel
	file     *os.File
	logger   *log.Logger
	filePath string
}

var (
	globalLogger *Logger
	once         sync.Once
)

// InitLogger initializes the global logger
func InitLogger(logLevel string, logFile string) {
	once.Do(func() {
		globalLogger = NewLogger(logLevel, logFile)
		log.SetOutput(globalLogger)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	})
}

// NewLogger creates a new logger instance
func NewLogger(logLevelStr string, logFile string) *Logger {
	level := parseLogLevel(logLevelStr)

	logger := &Logger{
		level:    level,
		filePath: logFile,
	}

	// Open log file if specified
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logger.file = f
			logger.logger = log.New(f, "", log.LstdFlags)
		}
	}

	if logger.logger == nil {
		logger.logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	return logger
}

// parseLogLevel converts string to LogLevel
func parseLogLevel(level string) LogLevel {
	switch level {
	case "DEBUG", "debug":
		return LogLevelDebug
	case "INFO", "info":
		return LogLevelInfo
	case "WARN", "warn":
		return LogLevelWarn
	case "ERROR", "error":
		return LogLevelError
	case "NONE", "none":
		return LogLevelNone
	default:
		return LogLevelInfo
	}
}

// SetLevel changes the log level
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetLevelString changes the log level from string
func (l *Logger) SetLevelString(level string) {
	l.SetLevel(parseLogLevel(level))
}

// Debug logs a debug message
func (l *Logger) Debug(format string, v ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level <= LogLevelDebug {
		l.logger.Output(2, fmt.Sprintf("[DEBUG] "+format, v...))
	}
}

// Info logs an info message
func (l *Logger) Info(format string, v ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level <= LogLevelInfo {
		l.logger.Output(2, fmt.Sprintf("[INFO] "+format, v...))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(format string, v ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level <= LogLevelWarn {
		l.logger.Output(2, fmt.Sprintf("[WARN] "+format, v...))
	}
}

// Error logs an error message
func (l *Logger) Error(format string, v ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level <= LogLevelError {
		l.logger.Output(2, fmt.Sprintf("[ERROR] "+format, v...))
	}
}

// Close closes the log file
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Write implements io.Writer for compatibility
func (l *Logger) Write(p []byte) (n int, err error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.logger.Writer().Write(p)
}

// Global logger functions for convenience

// Debug logs a debug message globally
func DebugLog(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Debug(format, v...)
	}
}

// Info logs an info message globally
func InfoLog(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Info(format, v...)
	}
}

// Warn logs a warning message globally
func WarnLog(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Warn(format, v...)
	}
}

// ErrorLog logs an error message globally
func ErrorLog(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Error(format, v...)
	}
}

// GetLogLevel returns the current log level
func GetLogLevel() LogLevel {
	if globalLogger != nil {
		globalLogger.mu.RLock()
		defer globalLogger.mu.RUnlock()
		return globalLogger.level
	}
	return LogLevelInfo
}

// SetLogLevel sets the global log level
func SetLogLevel(level LogLevel) {
	if globalLogger != nil {
		globalLogger.SetLevel(level)
	}
}
