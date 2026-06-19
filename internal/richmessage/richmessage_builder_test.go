package richmessage

import (
	"strings"
	"testing"
)

func TestTableEscapesMarkdownPipes(t *testing.T) {
	msg := NewBuilder().Table([]string{"A|B"}, [][]string{{"x|y"}}).Build()
	if !strings.Contains(msg.Markdown, `A\|B`) || !strings.Contains(msg.Markdown, `x\|y`) {
		t.Fatalf("table cells were not escaped: %q", msg.Markdown)
	}
}

func TestParagraphEscapesMarkdownSyntax(t *testing.T) {
	msg := NewBuilder().Paragraph("hello *world* [link]").Build()
	if !strings.Contains(msg.Markdown, `\*world\*`) || !strings.Contains(msg.Markdown, `\[link\]`) {
		t.Fatalf("paragraph markdown syntax was not escaped: %q", msg.Markdown)
	}
}
