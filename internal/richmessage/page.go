package richmessage

import (
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

// Page is the one user-visible card template: heading, tagline, 1-2 sentences,
// optional compact facts, then a 2-column 10.3 button grid.
type Page struct {
	Kicker  string
	Heading string
	Tagline string
	Body    string
	Status  string
	Facts   [][]string
	Buttons [][]types.TelegramRichMessageButton
}

func applyPage(b *blockBuilder, p Page) {
	if k := strings.TrimSpace(p.Kicker); k != "" {
		b.kicker(k)
	}
	if h := strings.TrimSpace(p.Heading); h != "" {
		b.heading(types.CleanButtonText(h), 3)
	}
	if t := strings.TrimSpace(p.Tagline); t != "" {
		b.bold(t)
	}
	if body := strings.TrimSpace(p.Body); body != "" {
		b.paragraph(body)
	}
	if s := strings.TrimSpace(p.Status); s != "" {
		b.paragraph(s)
	}
	if len(p.Facts) > 0 {
		b.compactTable(p.Facts)
	}
	for _, row := range p.Buttons {
		if len(row) > 0 {
			b.buttonRow(row...)
		}
	}
}

func BuildPage(p Page) Card {
	b := newBlockBuilder()
	applyPage(b, p)
	return b.card()
}

func pageBtn(text, callback, style string) types.TelegramRichMessageButton {
	return richButton(text, callback, style, false)
}

func pair(a, acb, as, b, bcb, bs string) []types.TelegramRichMessageButton {
	return []types.TelegramRichMessageButton{pageBtn(a, acb, as), pageBtn(b, bcb, bs)}
}

func full(text, callback, style string) []types.TelegramRichMessageButton {
	return []types.TelegramRichMessageButton{pageBtn(text, callback, style)}
}

// BuildPageFromPlainText turns leftover HTML/plain menus into the same template.
func BuildPageFromPlainText(text string) Card {
	raw := strings.ReplaceAll(text, "\r\n", "\n")
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, types.CleanButtonText(line))
	}
	if len(lines) == 0 {
		return BuildPage(Page{})
	}
	heading := lines[0]
	if len([]rune(heading)) > 16 {
		return BuildPage(Page{Body: strings.Join(lines, "\n")})
	}
	p := Page{Heading: heading}
	if len(lines) == 2 {
		p.Body = lines[1]
	} else if len(lines) > 2 {
		p.Tagline = lines[1]
		p.Body = strings.Join(lines[2:], "\n")
	}
	return BuildPage(p)
}
