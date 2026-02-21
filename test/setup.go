package test

import (
	"os"
	"testing"
	"time"
)

// SetupTest initializes the test environment
func SetupTest(t *testing.T) func() {
	// Set test environment variables
	os.Setenv("TELEGRAM_BOT_TOKEN", "test_token")
	os.Setenv("TELEGRAM_CHAT_ID", "12345")
	os.Setenv("JELLYSEERR_URL", "http://localhost:8081")
	os.Setenv("JELLYSEERR_API_KEY", "test_key")
	os.Setenv("ADMINS", "123456:TestUser,789012:Admin")
	os.Setenv("DATA_DIR", "/tmp/test_data")
	os.Setenv("LOG_DIR", "/tmp/test_logs")

	// Create test directories
	os.MkdirAll("/tmp/test_data", 0755)
	os.MkdirAll("/tmp/test_logs", 0755)
}

// TeardownTest cleans up after tests
func TeardownTest(t *testing.T) func() {
	// Clean up test data
	os.RemoveAll("/tmp/test_data")
	os.RemoveAll("/tmp/test_logs")
}

// AssertNoError fails the test if err is not nil
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertEqual fails the test if got != want
func AssertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// AssertNotEqual fails the test if got == want
func AssertNotEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got == want {
		t.Errorf("got %v, want not %v", got, want)
	}
}

// AssertTrue fails the test if condition is false
func AssertTrue(t *testing.T, condition bool, msg string) {
	t.Helper()
	if !condition {
		t.Errorf("%s: expected true, got false", msg)
	}
}

// AssertFalse fails the test if condition is true
func AssertFalse(t *testing.T, condition bool, msg string) {
	t.Helper()
	if condition {
		t.Errorf("%s: expected false, got true", msg)
	}
}

// AssertContains fails the test if substr is not in s
func AssertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

// AssertNotContains fails the test if substr is in s
func AssertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if contains(s, substr) {
		t.Errorf("expected %q to not contain %q", s, substr)
	}
}

// AssertLen fails the test if collection doesn't have expected length
func AssertLen[T any](t *testing.T, collection T, expectedLen int) {
	t.Helper()
	length := 0
	switch v := any(collection).(type) {
	case []string:
		length = len(v)
	case []int:
		length = len(v)
	case []string:
		length = len(v)
	case []Field:
		length = len(v)
	case map[string]string:
		length = len(v)
	case interface{ Len() int }:
		length = v.Len()
	default:
		t.Fatalf("unsupported collection type: %T", collection)
	}
	if length != expectedLen {
		t.Errorf("expected length %d, got %d", expectedLen, length)
	}
}

// AssertPanics fails if the function panics
func AssertPanics(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected panic, got: %v", r)
		}
	}()
	f()
}

// AssertEventually retries the assertion until timeout
func AssertEventually(t *testing.T, condition func() bool, msg string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("%s: condition not met after %v", msg, timeout)
}

// AssertNil fails the test if value is not nil
func AssertNil(t *testing.T, value interface{}) {
	t.Helper()
	if value != nil {
		t.Errorf("expected nil, got %v", value)
	}
}

// AssertNotNil fails the test if value is nil
func AssertNotNil(t *testing.T, value interface{}) {
	t.Helper()
	if value == nil {
		t.Errorf("expected non-nil value")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Test helper types
type Field struct {
	Key   string
	Value interface{}
}
