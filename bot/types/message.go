package types

// MessageResponse represents a response to be sent
type MessageResponse struct {
	Text     string
	Keyboard [][]map[string]string
	EditMode bool
}

// CallbackResponse represents a callback query response
type CallbackResponse struct {
	Text     string
	Keyboard [][]map[string]string
	ShowAlert bool
	EditMode  bool
}
