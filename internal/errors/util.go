package errors

import (
	"time"
)

func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ErrorCollector collects multiple errors
type ErrorCollector struct {
	errors []error
}

// NewErrorCollector creates a new error collector
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: make([]error, 0),
	}
}

// Add adds an error to the collector
func (ec *ErrorCollector) Add(err error) {
	if err != nil {
		ec.errors = append(ec.errors, err)
	}
}

// HasErrors returns true if there are any errors
func (ec *ErrorCollector) HasErrors() bool {
	return len(ec.errors) > 0
}

// ToError returns a combined error or nil
func (ec *ErrorCollector) ToError() error {
	if len(ec.errors) == 0 {
		return nil
	}
	if len(ec.errors) == 1 {
		return ec.errors[0]
	}
	return &MultiError{errors: ec.errors}
}

// MultiError represents multiple errors
type MultiError struct {
	errors []error
}

func (me *MultiError) Error() string {
	var msg string
	for _, err := range me.errors {
		if msg != "" {
			msg += "; "
		}
		msg += err.Error()
	}
	return msg
}

// Errors returns all underlying errors
func (me *MultiError) Errors() []error {
	return me.errors
}
