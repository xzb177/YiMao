package types

// MessageResponse represents a response to be sent
type MessageResponse struct {
	Text     string
	Keyboard [][]map[string]string
	EditMode bool
}

// CallbackOverlay represents overlay info for editing the original message
type CallbackOverlay struct {
	Title  string
	Status string
}

// CallbackResponse represents a callback query response
type CallbackResponse struct {
	Text        string
	Keyboard    [][]map[string]string
	ShowAlert   bool
	EditMode    bool
	TextOverlay *CallbackOverlay // Optional overlay for editing the original message
}
