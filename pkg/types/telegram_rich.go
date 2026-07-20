package types

// TelegramInputRichMessage is the typed Bot API 10.2 InputRichMessage
// transport. Exactly one of Markdown, HTML or Blocks must be set. A slideshow
// is one of the structured values accepted by Blocks.
type TelegramInputRichMessage struct {
	Markdown string                          `json:"markdown,omitempty"`
	HTML     string                          `json:"html,omitempty"`
	Blocks   []TelegramInputRichBlock        `json:"blocks,omitempty"`
	Media    []TelegramInputRichMessageMedia `json:"media,omitempty"`
}

type TelegramInputRichBlock struct {
	Type    string                   `json:"type"`
	Blocks  []TelegramInputRichBlock `json:"blocks,omitempty"`
	Photo   *TelegramRichPhoto       `json:"photo,omitempty"`
	Caption *TelegramRichText        `json:"caption,omitempty"`
}

type TelegramRichPhoto struct {
	Type  string `json:"type"`
	Media string `json:"media"`
}

type TelegramRichText struct {
	Text string `json:"text"`
}

type TelegramInputRichMessageMedia struct {
	ID    string            `json:"id"`
	Media TelegramRichPhoto `json:"media"`
	// Upload is sent as a multipart attachment and is never serialized into the
	// rich_message JSON. Media.Media must reference it as attach://<ID>.
	Upload   []byte `json:"-"`
	Filename string `json:"-"`
}
