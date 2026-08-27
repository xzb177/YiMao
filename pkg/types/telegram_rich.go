package types

// TelegramInputRichMessage is the typed Bot API 10.2/10.3 InputRichMessage
// transport. Exactly one of Markdown, HTML or Blocks must be set.
type TelegramInputRichMessage struct {
	Markdown string                          `json:"markdown,omitempty"`
	HTML     string                          `json:"html,omitempty"`
	Blocks   []TelegramInputRichBlock        `json:"blocks,omitempty"`
	Media    []TelegramInputRichMessageMedia `json:"media,omitempty"`
}

// TelegramInputRichBlock is a discriminated InputRichBlock.
type TelegramInputRichBlock struct {
	Type string `json:"type"`

	Blocks []TelegramInputRichBlock `json:"blocks,omitempty"`

	Photo   *TelegramRichPhoto `json:"photo,omitempty"`
	Caption *TelegramRichText  `json:"caption,omitempty"`

	// paragraph, heading, expandable_blockquote: RichText as a JSON string.
	Text   interface{} `json:"text,omitempty"`
	Size   int         `json:"size,omitempty"`
	Credit interface{} `json:"credit,omitempty"`

	IsCompact  bool                      `json:"is_compact,omitempty"`
	IsBordered bool                      `json:"is_bordered,omitempty"`
	IsStriped  bool                      `json:"is_striped,omitempty"`
	Cells      [][]TelegramRichTableCell `json:"cells,omitempty"`

	Align   string                      `json:"align,omitempty"`
	Buttons []TelegramRichMessageButton `json:"buttons,omitempty"`

	Document *TelegramInputMediaDocument `json:"document,omitempty"`
}

type TelegramRichPhoto struct {
	Type  string `json:"type"`
	Media string `json:"media"`
}

type TelegramRichText struct {
	Text string `json:"text"`
}

type TelegramRichTableCell struct {
	Text     interface{} `json:"text"`
	IsHeader bool        `json:"is_header,omitempty"`
}

// TelegramRichMessageButton is Bot API 10.3 RichMessageButton.
type TelegramRichMessageButton struct {
	Text         interface{}             `json:"text"`
	Style        string                  `json:"style,omitempty"`
	CallbackData string                  `json:"callback_data,omitempty"`
	URL          string                  `json:"url,omitempty"`
	WebApp       *TelegramWebAppInfo     `json:"web_app,omitempty"`
	Disabled     *TelegramDisabledButton `json:"disabled,omitempty"`
}

type TelegramInputMediaDocument struct {
	Type  string `json:"type"`
	Media string `json:"media"`
}

type TelegramInputRichMessageMedia struct {
	ID       string            `json:"id"`
	Media    TelegramRichPhoto `json:"media"`
	Upload   []byte            `json:"-"`
	Filename string            `json:"-"`
}
