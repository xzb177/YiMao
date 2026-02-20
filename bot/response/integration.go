package response

import (
	"fmt"

	"emby-telegram-bot/bot/types"
)

// Integration provides integration between the response system and bot handlers
type Integration struct {
	handler *Handler
	locale  *LocalizationProvider
}

// NewIntegration creates a new response integration
func NewIntegration() *Integration {
	return &Integration{
		handler: NewHandler(),
		locale:  NewLocalizationProvider(LocaleZH),
	}
}

// SetLocale sets the locale for responses
func (i *Integration) SetLocale(locale Locale) {
	i.locale.SetLocale(locale)
}

// GetHandler returns the underlying response handler
func (i *Integration) GetHandler() *Handler {
	return i.handler
}

// GetLocale returns the localization provider
func (i *Integration) GetLocale() *LocalizationProvider {
	return i.locale
}

// ConvertToMessageResponse converts a Response to types.MessageResponse
func (i *Integration) ConvertToMessageResponse(resp *Response) *types.MessageResponse {
	keyboard := i.buildKeyboard(resp)

	return &types.MessageResponse{
		Text:     resp.Format(),
		Keyboard: keyboard,
		EditMode: resp.EditMode,
	}
}

// ConvertToCallbackResponse converts a Response to types.CallbackResponse
func (i *Integration) ConvertToCallbackResponse(resp *Response) *types.CallbackResponse {
	keyboard := i.buildKeyboard(resp)

	return &types.CallbackResponse{
		Text:     resp.Format(),
		Keyboard: keyboard,
		ShowAlert: resp.ShowAlert,
		EditMode:  resp.EditMode,
	}
}

// buildKeyboard builds the keyboard from response actions
func (i *Integration) buildKeyboard(resp *Response) [][]map[string]string {
	if len(resp.Actions) == 0 {
		return nil
	}

	var keyboard [][]map[string]string
	var currentRow []map[string]string

	for _, action := range resp.Actions {
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

		// Create rows of up to 2 buttons for primary actions
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

// SearchInProgress creates a search in progress response
func (i *Integration) SearchInProgress(query string) *types.CallbackResponse {
	resp := i.handler.BuildSearchInProgress(query)
	return i.ConvertToCallbackResponse(resp)
}

// SearchNoResults creates a no results response
func (i *Integration) SearchNoResults(query string) *types.MessageResponse {
	resp := i.handler.BuildSearchNoResults(query)
	return i.ConvertToMessageResponse(resp)
}

// SearchError creates a search error response
func (i *Integration) SearchError(query string, err error) *types.MessageResponse {
	resp := i.handler.BuildSearchError(query, err)
	return i.ConvertToMessageResponse(resp)
}

// RequestSuccess creates a request success response
func (i *Integration) RequestSuccess(title, mediaType string, quotaUsed, quotaLimit, quotaRemaining int, isAdmin bool) *types.CallbackResponse {
	resp := i.handler.BuildRequestSuccess(title, mediaType, quotaUsed, quotaLimit, quotaRemaining)
	if isAdmin {
		resp.Title = "⭐ 管理员请求已提交"
		resp.Details = "管理员权限，无配额限制"
	}
	return i.ConvertToCallbackResponse(resp)
}

// QuotaExhausted creates a quota exhausted response
func (i *Integration) QuotaExhausted(mediaType string, quotaUsed, quotaLimit int) *types.CallbackResponse {
	resp := i.handler.BuildQuotaExhausted(mediaType, quotaUsed, quotaLimit)
	return i.ConvertToCallbackResponse(resp)
}

// AccountNotLinked creates an account not linked response
func (i *Integration) AccountNotLinked() *types.CallbackResponse {
	resp := i.handler.BuildAccountNotLinked()
	return i.ConvertToCallbackResponse(resp)
}

// RateLimited creates a rate limited response
func (i *Integration) RateLimited(retryAfter int) *types.CallbackResponse {
	resp := i.handler.BuildRateLimited(0) // Not used in template
	resp.ShowAlert = true
	return i.ConvertToCallbackResponse(resp)
}

// NetworkError creates a network error response
func (i *Integration) NetworkError(err error) *types.MessageResponse {
	resp := i.handler.BuildNetworkError(err)
	return i.ConvertToMessageResponse(resp)
}

// OperationTimeout creates an operation timeout response
func (i *Integration) OperationTimeout(operation string) *types.MessageResponse {
	resp := i.handler.BuildOperationTimeout(operation)
	return i.ConvertToMessageResponse(resp)
}

// InvalidInput creates an invalid input response
func (i *Integration) InvalidInput(message string) *types.MessageResponse {
	resp := i.handler.BuildInvalidInput(message)
	return i.ConvertToMessageResponse(resp)
}

// InvalidInputBuilder creates an invalid input response with builder
func (i *Integration) InvalidInputBuilder(message string) *MessageResponseBuilder {
	resp := i.handler.BuildInvalidInput(message)
	return &MessageResponseBuilder{resp: i.ConvertToMessageResponse(resp)}
}

// Success creates a simple success response
func (i *Integration) Success(message string) *types.MessageResponse {
	resp := Success(message)
	return i.ConvertToMessageResponse(resp)
}

// SuccessBuilder creates a success response with builder
func (i *Integration) SuccessBuilder(message string) *MessageResponseBuilder {
	resp := Success(message)
	return &MessageResponseBuilder{resp: i.ConvertToMessageResponse(resp)}
}

// Error creates a simple error response
func (i *Integration) Error(message string) *types.MessageResponse {
	resp := Error(message)
	return i.ConvertToMessageResponse(resp)
}

// ErrorBuilder creates an error response with builder
func (i *Integration) ErrorBuilder(message string) *MessageResponseBuilder {
	resp := Error(message)
	return &MessageResponseBuilder{resp: i.ConvertToMessageResponse(resp)}
}

// Info creates an info response
func (i *Integration) Info(message string) *types.MessageResponse {
	resp := Info(message)
	return i.ConvertToMessageResponse(resp)
}

// InfoBuilder creates an info response with builder
func (i *Integration) InfoBuilder(message string) *MessageResponseBuilder {
	resp := Info(message)
	return &MessageResponseBuilder{resp: i.ConvertToMessageResponse(resp)}
}

// Warning creates a warning response
func (i *Integration) Warning(message string) *types.MessageResponse {
	resp := Warning(message)
	return i.ConvertToMessageResponse(resp)
}

// WarningBuilder creates a warning response with builder
func (i *Integration) WarningBuilder(message string) *MessageResponseBuilder {
	resp := Warning(message)
	return &MessageResponseBuilder{resp: i.ConvertToMessageResponse(resp)}
}

// Loading creates a loading response
func (i *Integration) Loading(message string) *CallbackResponseBuilder {
	resp := Loading(message)
	return &CallbackResponseBuilder{resp: i.ConvertToCallbackResponse(resp)}
}

// Progress creates a progress response
func (i *Integration) Progress(current, total int, message string) *types.CallbackResponse {
	resp := Progress(current, total, message)
	return i.ConvertToCallbackResponse(resp)
}

// MediaAlreadyAvailable creates a response for when media is already available
func (i *Integration) MediaAlreadyAvailable(title string) *types.CallbackResponse {
	resp := NewBuilder().
		WithType(ResponseTypeSuccess).
		WithTitle("✨ 内容已存在").
		WithMessage(fmt.Sprintf("🎬 %s", title)).
		WithDetails("这部电影已经在库中了，可以直接观看").
		Build()
	return i.ConvertToCallbackResponse(resp)
}

// RequestFailed creates a failed request response with specific error
func (i *Integration) RequestFailed(title, reason string) *types.CallbackResponse {
	resp := NewBuilder().
		WithType(ResponseTypeError).
		WithSeverity(SeverityMedium).
		WithTitle("❌ 请求失败").
		WithMessage(fmt.Sprintf("🎬 %s", title)).
		WithDetails(reason).
		WithAlert(true).
		Build()
	return i.ConvertToCallbackResponse(resp)
}

// RatingSaved creates a response for saved rating
func (i *Integration) RatingSaved(title string, rating float64, avgRating float64, count int) *types.CallbackResponse {
	data := TemplateData{
		MediaTitle:       title,
		QuotaUsed:        int(rating),     // Reuse as rating
		QuotaLimit:       int(avgRating),  // Reuse as avg
		QuotaRemaining:   count,           // Reuse as count
	}
	resp := RenderTemplate(TemplateRatingSuccess, data)
	resp.ShowAlert = true
	return i.ConvertToCallbackResponse(resp)
}

// RatingUpdated creates a response for updated rating
func (i *Integration) RatingUpdated(title string, rating float64) *types.CallbackResponse {
	data := TemplateData{
		MediaTitle: title,
		QuotaUsed:  int(rating),
	}
	resp := RenderTemplate(TemplateRatingUpdated, data)
	resp.ShowAlert = true
	return i.ConvertToCallbackResponse(resp)
}

// ErrorWithDetails creates an error response with title, details, and suggestion
func (i *Integration) ErrorWithDetails(title, details, suggestion string) *types.MessageResponse {
	resp := NewBuilder().
		WithType(ResponseTypeError).
		WithSeverity(SeverityMedium).
		WithTitle(title).
		WithDetails(details).
		WithSuggestions(suggestion).
		Build()
	return i.ConvertToMessageResponse(resp)
}

// ErrorWithDetailsBuilder creates an error response with builder
func (i *Integration) ErrorWithDetailsBuilder(title, details, suggestion string) *MessageResponseBuilder {
	resp := NewBuilder().
		WithType(ResponseTypeError).
		WithSeverity(SeverityMedium).
		WithTitle(title).
		WithDetails(details).
		WithSuggestions(suggestion).
		Build()
	return &MessageResponseBuilder{resp: i.ConvertToMessageResponse(resp)}
}

// ErrorResponseBuilder is a builder for error responses
type ErrorResponseBuilder struct {
	resp *types.MessageResponse
}

// WithEditMode sets the edit mode
func (e *ErrorResponseBuilder) WithEditMode(edit bool) *ErrorResponseBuilder {
	e.resp.EditMode = edit
	return e
}

// ToCallbackResponse converts to callback response
func (e *ErrorResponseBuilder) ToCallbackResponse() *types.CallbackResponse {
	return &types.CallbackResponse{
		Text:      e.resp.Text,
		Keyboard:  e.resp.Keyboard,
		EditMode:  e.resp.EditMode,
		ShowAlert: false,
	}
}

// WithKeyboard sets the keyboard
func (e *ErrorResponseBuilder) WithKeyboard(keyboard [][]map[string]string) *ErrorResponseBuilder {
	e.resp.Keyboard = keyboard
	return e
}

// MessageResponseBuilder is a builder for message responses
type MessageResponseBuilder struct {
	resp *types.MessageResponse
}

// Get returns the underlying message response
func (m *MessageResponseBuilder) Get() *types.MessageResponse {
	return m.resp
}

// WithEditMode sets the edit mode for message response
func (m *MessageResponseBuilder) WithEditMode(edit bool) *MessageResponseBuilder {
	m.resp.EditMode = edit
	return m
}

// WithKeyboard sets the keyboard for message response
func (m *MessageResponseBuilder) WithKeyboard(keyboard [][]map[string]string) *MessageResponseBuilder {
	m.resp.Keyboard = keyboard
	return m
}

// ToCallbackResponse converts message response to callback response
func (m *MessageResponseBuilder) ToCallbackResponse() *types.CallbackResponse {
	return &types.CallbackResponse{
		Text:      m.resp.Text,
		Keyboard:  m.resp.Keyboard,
		EditMode:  m.resp.EditMode,
		ShowAlert: false,
	}
}

// CallbackResponseBuilder is a builder for callback responses
type CallbackResponseBuilder struct {
	resp *types.CallbackResponse
}

// ToCallbackResponse returns the callback response
func (c *CallbackResponseBuilder) ToCallbackResponse() *types.CallbackResponse {
	return c.resp
}
