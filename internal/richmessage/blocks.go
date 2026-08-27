package richmessage

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

// Status labels shown on user-facing cards. Keep these short and stable.
const (
	StatusSubmitted = "已提交"
	StatusPending   = "等待管理员审核"
	StatusApproved  = "已批准"
	StatusRejected  = "已拒绝"
	StatusInLibrary = "已在库"
	StatusDownload  = "下载中"
	StatusPlayable  = "可播放"
	StatusSearching = "正在寻找资源"
	StatusSyncing   = "正在同步"
)

// Card is a Bot API 10.3 structured rich message plus markdown fallback.
type Card struct {
	Markdown string
	Blocks   []types.TelegramInputRichBlock
	Media    []types.TelegramInputRichMessageMedia
}

func (c Card) Rich() RichMessage {
	return RichMessage{Markdown: c.Markdown, Blocks: c.Blocks, Media: c.Media}
}

func (c Card) Input() *types.TelegramInputRichMessage {
	if len(c.Blocks) > 0 {
		return &types.TelegramInputRichMessage{Blocks: c.Blocks, Media: c.Media}
	}
	if strings.TrimSpace(c.Markdown) != "" {
		return &types.TelegramInputRichMessage{Markdown: c.Markdown, Media: c.Media}
	}
	return nil
}

type blockBuilder struct {
	blocks []types.TelegramInputRichBlock
	md     strings.Builder
}

func newBlockBuilder() *blockBuilder {
	return &blockBuilder{}
}

func (b *blockBuilder) heading(text string, size int) *blockBuilder {
	if size < 1 {
		size = 1
	}
	if size > 6 {
		size = 6
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "heading", Text: text, Size: size})
	b.md.WriteString(strings.Repeat("#", size))
	b.md.WriteString(" ")
	b.md.WriteString(escapeMarkdownInline(text))
	b.md.WriteString("\n\n")
	return b
}

func (b *blockBuilder) paragraph(text string) *blockBuilder {
	if strings.TrimSpace(text) == "" {
		return b
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "paragraph", Text: text})
	b.md.WriteString(escapeMarkdownInline(text))
	b.md.WriteString("\n\n")
	return b
}

func (b *blockBuilder) bold(text string) *blockBuilder {
	if strings.TrimSpace(text) == "" {
		return b
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "paragraph", Text: []interface{}{map[string]interface{}{"type": "bold", "text": text}}})
	b.md.WriteString("**")
	b.md.WriteString(escapeMarkdownInline(text))
	b.md.WriteString("**\n\n")
	return b
}

func (b *blockBuilder) photo(url string) *blockBuilder {
	return b.photoCredit(url, "")
}

func (b *blockBuilder) photoCredit(url, credit string) *blockBuilder {
	if strings.TrimSpace(url) == "" {
		return b
	}
	block := types.TelegramInputRichBlock{
		Type:  "photo",
		Photo: &types.TelegramRichPhoto{Type: "photo", Media: url},
	}
	if c := strings.TrimSpace(credit); c != "" {
		block.Credit = c
	}
	b.blocks = append(b.blocks, block)
	return b
}

func (b *blockBuilder) kicker(text string) *blockBuilder {
	if strings.TrimSpace(text) == "" {
		return b
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "heading", Text: text, Size: 1})
	b.md.WriteString(text)
	b.md.WriteString("\n\n")
	return b
}

func (b *blockBuilder) compactTable(pairs [][]string) *blockBuilder {
	if len(pairs) == 0 {
		return b
	}
	cells := make([][]types.TelegramRichTableCell, 0, len(pairs))
	mdRows := make([][]string, 0, len(pairs))
	for _, pair := range pairs {
		if len(pair) < 2 || strings.TrimSpace(pair[1]) == "" {
			continue
		}
		cells = append(cells, []types.TelegramRichTableCell{
			{Text: pair[0], IsHeader: true},
			{Text: pair[1]},
		})
		mdRows = append(mdRows, []string{pair[0], pair[1]})
	}
	if len(cells) == 0 {
		return b
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "table", IsCompact: true, Cells: cells})
	b.md.WriteString("| 项目 | 详情 |\n|------|------|\n")
	for _, row := range mdRows {
		b.md.WriteString("| ")
		b.md.WriteString(escapeTableCell(row[0]))
		b.md.WriteString(" | ")
		b.md.WriteString(escapeTableCell(row[1]))
		b.md.WriteString(" |\n")
	}
	b.md.WriteString("\n")
	return b
}

func (b *blockBuilder) expandable(title, body string) *blockBuilder {
	body = strings.TrimSpace(body)
	if body == "" {
		return b
	}
	runes := []rune(body)
	if len(runes) > 800 {
		body = string(runes[:800]) + "..."
	}
	block := types.TelegramInputRichBlock{Type: "expandable_blockquote", Text: body}
	if strings.TrimSpace(title) != "" {
		block.Credit = title
	}
	b.blocks = append(b.blocks, block)
	b.md.WriteString(">")
	b.md.WriteString(escapeMarkdownInline(body))
	b.md.WriteString("\n\n")
	return b
}

func (b *blockBuilder) paragraphParts(parts ...interface{}) *blockBuilder {
	if len(parts) == 0 {
		return b
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "paragraph", Text: parts})
	for _, part := range parts {
		switch v := part.(type) {
		case string:
			b.md.WriteString(escapeMarkdownInline(v))
		case map[string]interface{}:
			if btn, ok := v["button"].(types.TelegramRichMessageButton); ok {
				b.md.WriteString(escapeMarkdownInline(fmt.Sprint(btn.Text)))
			}
		}
	}
	b.md.WriteString("\n\n")
	return b
}

func (b *blockBuilder) buttonRow(buttons ...types.TelegramRichMessageButton) *blockBuilder {
	clean := make([]types.TelegramRichMessageButton, 0, len(buttons))
	for _, btn := range buttons {
		if btn.Text == nil || fmt.Sprint(btn.Text) == "" {
			continue
		}
		clean = append(clean, btn)
	}
	if len(clean) == 0 {
		return b
	}
	align := "left"
	if len(clean) == 1 {
		align = "center"
	}
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "buttons", Align: align, Buttons: clean})
	for i, btn := range clean {
		if i > 0 {
			b.md.WriteString(" · ")
		}
		b.md.WriteString(escapeMarkdownInline(fmt.Sprint(btn.Text)))
	}
	b.md.WriteString("\n\n")
	return b
}

func (b *blockBuilder) divider() *blockBuilder {
	b.blocks = append(b.blocks, types.TelegramInputRichBlock{Type: "divider"})
	b.md.WriteString("---\n\n")
	return b
}

func (b *blockBuilder) card() Card {
	return Card{Markdown: b.md.String(), Blocks: b.blocks}
}

func richButton(text, callback, style string, disabled bool) types.TelegramRichMessageButton {
	if style == "" {
		style = types.ButtonStyleFor(text, callback)
	}
	btn := types.TelegramRichMessageButton{Text: text, Style: style}
	if disabled {
		btn.Disabled = types.DisabledButtonValue()
		btn.Style = style
	} else {
		btn.CallbackData = callback
	}
	return btn
}

func richTextButton(btn types.TelegramRichMessageButton) map[string]interface{} {
	return map[string]interface{}{"type": "button", "button": btn}
}

func richWebAppButton(text, url, style string) types.TelegramRichMessageButton {
	return types.TelegramRichMessageButton{Text: text, Style: style, WebApp: &types.TelegramWebAppInfo{URL: url}}
}

// AppendKeyboardAsButtons moves a leftover inline keyboard into InputRichBlockButtons.
func AppendKeyboardAsButtons(rich *types.TelegramInputRichMessage, kb *types.TelegramInlineKeyboard) {
	if rich == nil || kb == nil {
		return
	}
	for _, row := range kb.InlineKeyboard {
		btns := make([]types.TelegramRichMessageButton, 0, len(row))
		for _, src := range row {
			style := src.Style
			if style == "" {
				style = types.ButtonStyleFor(src.Text, src.CallbackData)
			}
			btn := types.TelegramRichMessageButton{Text: src.Text, Style: style, CallbackData: src.CallbackData, URL: src.URL, WebApp: src.WebApp, Disabled: src.Disabled}
			btns = append(btns, btn)
		}
		if len(btns) == 0 {
			continue
		}
		rich.Blocks = append(rich.Blocks, types.TelegramInputRichBlock{Type: "buttons", Align: "left", Buttons: btns})
	}
	if len(rich.Blocks) == 0 {
		return
	}
	if strings.TrimSpace(rich.Markdown) != "" {
		rich.Blocks = append([]types.TelegramInputRichBlock{{Type: "paragraph", Text: rich.Markdown}}, rich.Blocks...)
		rich.Markdown = ""
	}
	rich.HTML = ""
}

func WithoutHero(in *types.TelegramInputRichMessage) *types.TelegramInputRichMessage {
	if in == nil {
		return nil
	}
	out := *in
	out.Media = nil
	blocks := make([]types.TelegramInputRichBlock, 0, len(in.Blocks))
	for _, block := range in.Blocks {
		if block.Type == "photo" {
			continue
		}
		blocks = append(blocks, block)
	}
	out.Blocks = blocks
	return &out
}

func InlineKeyboardFromBlocks(rich *types.TelegramInputRichMessage) *types.TelegramInlineKeyboard {
	if rich == nil {
		return nil
	}
	rows := make([][]types.TelegramInlineKeyboardButton, 0)
	for _, block := range rich.Blocks {
		if block.Type != "buttons" {
			continue
		}
		row := make([]types.TelegramInlineKeyboardButton, 0, len(block.Buttons))
		for _, btn := range block.Buttons {
			label := fmt.Sprint(btn.Text)
			style := btn.Style
			if style == "" {
				style = types.ButtonStyleFor(label, btn.CallbackData)
			}
			converted := types.TelegramInlineKeyboardButton{Text: label, CallbackData: btn.CallbackData, URL: btn.URL, WebApp: btn.WebApp, Style: style, Disabled: btn.Disabled}
			row = append(row, converted)
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return &types.TelegramInlineKeyboard{InlineKeyboard: rows}
}
