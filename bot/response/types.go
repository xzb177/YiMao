package response

import (
	"fmt"
	"strings"
	"time"
)

// ResponseType represents the type of response
type ResponseType int

const (
	ResponseTypeSuccess ResponseType = iota
	ResponseTypeError
	ResponseTypeInfo
	ResponseTypeWarning
	ResponseTypeLoading
	ResponseTypeProgress
)

// Severity represents the severity level of a message
type Severity int

const (
	SeverityLow Severity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// ResponseAction represents available actions for the user
type ResponseAction struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Icon        string `json:"icon,omitempty"`
	Style       string `json:"style,omitempty"` // primary, secondary, danger, success
	CallbackData string `json:"callback_data,omitempty"`
	URL         string `json:"url,omitempty"`
}

// ResponseContext provides context for the response
type ResponseContext struct {
	UserID      int64             `json:"user_id"`
	ChatID      int64             `json:"chat_id"`
	MessageID   int64             `json:"message_id,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	SessionData map[string]interface{} `json:"session_data,omitempty"`
}

// Response represents a structured response to the user
type Response struct {
	Type        ResponseType      `json:"type"`
	Severity    Severity          `json:"severity,omitempty"`
	Title       string            `json:"title,omitempty"`
	Message     string            `json:"message"`
	Details     string            `json:"details,omitempty"`
	ErrorCode   string            `json:"error_code,omitempty"`
	Actions     []ResponseAction  `json:"actions,omitempty"`
	Context     ResponseContext   `json:"context"`
	Suggestions []string         `json:"suggestions,omitempty"`
	ShowAlert   bool              `json:"show_alert,omitempty"`
	EditMode    bool              `json:"edit_mode,omitempty"`
	Dismissable bool              `json:"dismissable,omitempty"`
}

// Builder provides a fluent interface for building responses
type Builder struct {
	response *Response
}

// NewBuilder creates a new response builder
func NewBuilder() *Builder {
	return &Builder{
		response: &Response{
			Context: ResponseContext{
				Timestamp: time.Now(),
				Metadata:  make(map[string]string),
			},
			Dismissable: true,
		},
	}
}

// WithType sets the response type
func (b *Builder) WithType(t ResponseType) *Builder {
	b.response.Type = t
	return b
}

// WithSeverity sets the severity level
func (b *Builder) WithSeverity(s Severity) *Builder {
	b.response.Severity = s
	return b
}

// WithTitle sets the response title
func (b *Builder) WithTitle(title string) *Builder {
	b.response.Title = title
	return b
}

// WithMessage sets the main message
func (b *Builder) WithMessage(msg string) *Builder {
	b.response.Message = msg
	return b
}

// WithDetails sets additional details
func (b *Builder) WithDetails(details string) *Builder {
	b.response.Details = details
	return b
}

// WithError sets error information
func (b *Builder) WithError(code, message string) *Builder {
	b.response.ErrorCode = code
	b.response.Type = ResponseTypeError
	b.response.Message = message
	return b
}

// WithContext sets the response context
func (b *Builder) WithContext(ctx ResponseContext) *Builder {
	b.response.Context = ctx
	return b
}

// WithUserContext sets user information in context
func (b *Builder) WithUserContext(userID, chatID int64) *Builder {
	b.response.Context.UserID = userID
	b.response.Context.ChatID = chatID
	return b
}

// WithRequestID sets the request ID for tracing
func (b *Builder) WithRequestID(requestID string) *Builder {
	b.response.Context.RequestID = requestID
	return b
}

// WithMetadata adds metadata to the response
func (b *Builder) WithMetadata(key, value string) *Builder {
	if b.response.Context.Metadata == nil {
		b.response.Context.Metadata = make(map[string]string)
	}
	b.response.Context.Metadata[key] = value
	return b
}

// WithAction adds an action button
func (b *Builder) WithAction(id, label, icon string) *Builder {
	if b.response.Actions == nil {
		b.response.Actions = make([]ResponseAction, 0)
	}
	b.response.Actions = append(b.response.Actions, ResponseAction{
		ID:    id,
		Label: label,
		Icon:  icon,
	})
	return b
}

// WithActionFull adds a complete action with all options
func (b *Builder) WithActionFull(action ResponseAction) *Builder {
	if b.response.Actions == nil {
		b.response.Actions = make([]ResponseAction, 0)
	}
	b.response.Actions = append(b.response.Actions, action)
	return b
}

// WithSuggestions adds suggestion text
func (b *Builder) WithSuggestions(suggestions ...string) *Builder {
	b.response.Suggestions = suggestions
	return b
}

// WithAlert sets whether to show as alert popup
func (b *Builder) WithAlert(show bool) *Builder {
	b.response.ShowAlert = show
	return b
}

// WithEditMode sets edit mode
func (b *Builder) WithEditMode(edit bool) *Builder {
	b.response.EditMode = edit
	return b
}

// WithDismissable sets whether the response can be dismissed
func (b *Builder) WithDismissable(dismissable bool) *Builder {
	b.response.Dismissable = dismissable
	return b
}

// Build creates the final response
func (b *Builder) Build() *Response {
	return b.response
}

// Format formats the response for display
func (r *Response) Format() string {
	var sb strings.Builder

	// Add icon based on type
	sb.WriteString(r.getIcon())
	sb.WriteString(" ")

	// Add title if present
	if r.Title != "" {
		sb.WriteString(r.Title)
		sb.WriteString("\n\n")
	}

	// Add main message
	sb.WriteString(r.Message)

	// Add details if present
	if r.Details != "" {
		sb.WriteString("\n\n")
		sb.WriteString(r.Details)
	}

	// Add suggestions if present
	if len(r.Suggestions) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString("💡 ")
		sb.WriteString(strings.Join(r.Suggestions, "\n💡 "))
	}

	return sb.String()
}

// getIcon returns the appropriate icon for the response type
func (r *Response) getIcon() string {
	switch r.Type {
	case ResponseTypeSuccess:
		return "✅"
	case ResponseTypeError:
		switch r.Severity {
		case SeverityCritical:
			return "🚨"
		case SeverityHigh:
			return "❌"
		default:
			return "⚠️"
		}
	case ResponseTypeInfo:
		return "ℹ️"
	case ResponseTypeWarning:
		return "⚠️"
	case ResponseTypeLoading:
		return "⏳"
	case ResponseTypeProgress:
		return "📊"
	default:
		return "📌"
	}
}

// GetKeyboardActions returns actions formatted as keyboard buttons
func (r *Response) GetKeyboardActions() []ResponseAction {
	return r.Actions
}

// ToCallbackFormat converts response to callback format
func (r *Response) ToCallbackFormat() (string, [][]map[string]string) {
	text := r.Format()
	keyboard := r.buildKeyboard()
	return text, keyboard
}

// buildKeyboard builds the inline keyboard from actions
func (r *Response) buildKeyboard() [][]map[string]string {
	if len(r.Actions) == 0 {
		return nil
	}

	var keyboard [][]map[string]string
	var currentRow []map[string]string

	for _, action := range r.Actions {
		btn := map[string]string{
			"text": action.Label,
		}
		if action.CallbackData != "" {
			btn["callback_data"] = action.CallbackData
		}
		if action.URL != "" {
			btn["url"] = action.URL
		}

		currentRow = append(currentRow, btn)

		// Create rows of up to 2 buttons
		if len(currentRow) == 2 {
			keyboard = append(keyboard, currentRow)
			currentRow = []map[string]string{}
		}
	}

	if len(currentRow) > 0 {
		keyboard = append(keyboard, currentRow)
	}

	return keyboard
}

// String returns a string representation of the response
func (r *Response) String() string {
	return fmt.Sprintf("[%s] %s: %s", r.Type, r.Title, r.Message)
}

// String returns the string representation of ResponseType
func (t ResponseType) String() string {
	switch t {
	case ResponseTypeSuccess:
		return "SUCCESS"
	case ResponseTypeError:
		return "ERROR"
	case ResponseTypeInfo:
		return "INFO"
	case ResponseTypeWarning:
		return "WARNING"
	case ResponseTypeLoading:
		return "LOADING"
	case ResponseTypeProgress:
		return "PROGRESS"
	default:
		return "UNKNOWN"
	}
}

// String returns the string representation of Severity
func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
