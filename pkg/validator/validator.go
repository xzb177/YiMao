package validator

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Validator provides validation functions
type Validator struct {
	errors map[string]string
}

// New creates a new validator
func New() *Validator {
	return &Validator{
		errors: make(map[string]string),
	}
}

// Required checks if a string field is not empty
func (v *Validator) Required(field, value string) *Validator {
	if strings.TrimSpace(value) == "" {
		v.errors[field] = fmt.Sprintf("%s is required", field)
	}
	return v
}

// MinLength checks minimum length
func (v *Validator) MinLength(field, value string, min int) *Validator {
	if len(value) < min {
		v.errors[field] = fmt.Sprintf("%s must be at least %d characters", field, min)
	}
	return v
}

// MaxLength checks maximum length
func (v *Validator) MaxLength(field, value string, max int) *Validator {
	if len(value) > max {
		v.errors[field] = fmt.Sprintf("%s must be at most %d characters", field, max)
	}
	return v
}

// IsInt checks if value is a valid integer
func (v *Validator) IsInt(field, value string) *Validator {
	if _, err := strconv.Atoi(value); err != nil {
		v.errors[field] = fmt.Sprintf("%s must be a valid integer", field)
	}
	return v
}

// IsFloat checks if value is a valid float
func (v *Validator) IsFloat(field, value string) *Validator {
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		v.errors[field] = fmt.Sprintf("%s must be a valid number", field)
	}
	return v
}

// IsInRange checks if an integer is in range [min, max]
func (v *Validator) IsInRange(field, value string, min, max int) *Validator {
	if num, err := strconv.Atoi(value); err != nil {
		v.errors[field] = fmt.Sprintf("%s must be a valid integer", field)
	} else if num < min || num > max {
		v.errors[field] = fmt.Sprintf("%s must be between %d and %d", field, min, max)
	}
	return v
}

// IsURL checks if value is a valid URL
func (v *Validator) IsURL(field, value string) *Validator {
	if value == "" {
		return v
	}
	if _, err := url.ParseRequestURI(value); err != nil {
		v.errors[field] = fmt.Sprintf("%s must be a valid URL", field)
	}
	return v
}

// MatchesRegex checks if value matches regex pattern
func (v *Validator) MatchesRegex(field, value, pattern string) *Validator {
	if value == "" {
		return v
	}
	matched, err := regexp.MatchString(pattern, value)
	if err != nil {
		return v
	}
	if !matched {
		v.errors[field] = fmt.Sprintf("%s format is invalid", field)
	}
	return v
}

// IsOneOf checks if value is one of the allowed values
func (v *Validator) IsOneOf(field, value string, allowed []string) *Validator {
	if value == "" {
		return v
	}
	for _, a := range allowed {
		if value == a {
			return v
		}
	}
	v.errors[field] = fmt.Sprintf("%s must be one of: %s", field, strings.Join(allowed, ", "))
	return v
}

// Email validates email format
func (v *Validator) Email(field, value string) *Validator {
	if value == "" {
		return v
	}
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(value) {
		v.errors[field] = fmt.Sprintf("%s must be a valid email address", field)
	}
	return v
}

// HasError returns true if there are validation errors
func (v *Validator) HasError() bool {
	return len(v.errors) > 0
}

// Error returns all validation errors
func (v *Validator) Error() string {
	if len(v.errors) == 0 {
		return ""
	}

	var msgs []string
	for field, msg := range v.errors {
		msgs = append(msgs, fmt.Sprintf("%s: %s", field, msg))
	}
	return strings.Join(msgs, "; ")
}

// Errors returns the error map
func (v *Validator) Errors() map[string]string {
	return v.errors
}

// Add adds a custom error
func (v *Validator) Add(field, message string) {
	v.errors[field] = message
}

// Clear clears all errors
func (v *Validator) Clear() {
	v.errors = make(map[string]string)
}

// Common validation patterns
const (
	UsernameRegex = `^[a-zA-Z0-9_]{3,32}$`
	TmdbIDRegex   = `^\d+$`
)

// RequestInput represents common request input
type RequestInput struct {
	SearchQuery string
	MediaType   string
	TmdbID      string
	Rating      string
	Page        string
}

// ValidateRequestInput validates common request inputs
func ValidateRequestInput(input *RequestInput) *Validator {
	v := New()

	if input.SearchQuery != "" {
		v.Required("search_query", input.SearchQuery).
			MinLength("search_query", input.SearchQuery, 1).
			MaxLength("search_query", input.SearchQuery, 100)
	}

	if input.MediaType != "" {
		v.IsOneOf("media_type", input.MediaType, []string{"movie", "tv", ""})
	}

	if input.TmdbID != "" {
		v.Required("tmdb_id", input.TmdbID).
			MatchesRegex("tmdb_id", input.TmdbID, TmdbIDRegex)
	}

	if input.Rating != "" {
		v.Required("rating", input.Rating).
			IsInRange("rating", input.Rating, 1, 10)
	}

	if input.Page != "" {
		v.IsInt("page", input.Page)
	}

	return v
}

// ValidateCallbackData validates callback data
func ValidateCallbackData(data map[string]string) *Validator {
	v := New()

	// Validate action
	if action, ok := data["action"]; ok {
		v.Required("action", action)
	}

	// Validate media ID
	if tmdbID, ok := data["id"]; ok {
		v.Required("id", tmdbID).
			MatchesRegex("id", tmdbID, TmdbIDRegex)
	}

	// Validate rating
	if rating, ok := data["rating"]; ok {
		v.Required("rating", rating).
			IsInRange("rating", rating, 1, 10)
	}

	// Validate media type
	if mediaType, ok := data["type"]; ok {
		v.IsOneOf("type", mediaType, []string{"movie", "tv"})
	}

	return v
}
