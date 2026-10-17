package services

import (
	"strings"
	"testing"
)

// 审计 A3：外部标题含特殊字符时，HTML 模式下不能原样发送（Telegram 400）。

func TestSanitizeTelegramHTMLTextEscapesTitle(t *testing.T) {
	in := "《Fast & Furious》 <速度与激情> R&B"
	out := sanitizeTelegramHTMLText(in)
	if strings.Contains(out, " & ") || strings.Contains(out, "<速度") {
		t.Fatalf("特殊字符未被转义: %q", out)
	}
	if !strings.Contains(out, "&amp;") || !strings.Contains(out, "&lt;") {
		t.Fatalf("转义结果不符合预期: %q", out)
	}
}

func TestSanitizeTelegramHTMLTextKeepsAllowedTags(t *testing.T) {
	in := `<b>《Fast &amp; Furious》</b> <code>123</code> <a href="https://x">link</a>`
	out := sanitizeTelegramHTMLText(in)
	if out != in {
		t.Fatalf("合法标签/实体被破坏: in=%q out=%q", in, out)
	}
}

func TestSanitizeTelegramHTMLTextStripsDisallowedTag(t *testing.T) {
	in := `<script>alert(1)</script> ok`
	out := sanitizeTelegramHTMLText(in)
	if strings.Contains(out, "<script>") {
		t.Fatalf("非法标签未被转义: %q", out)
	}
}

func TestNormalizePayloadEscapesHTMLText(t *testing.T) {
	payload := map[string]interface{}{
		"text":       "《Fast & Furious》",
		"parse_mode": "HTML",
	}
	normalizeTelegramPayload(payload)
	text := payload["text"].(string)
	if strings.Contains(text, " & ") {
		t.Fatalf("normalize 后 HTML 文本仍含裸 &: %q", text)
	}
	// 非 HTML 模式不动
	payload2 := map[string]interface{}{
		"text":       "《Fast & Furious》",
		"parse_mode": "Markdown",
	}
	normalizeTelegramPayload(payload2)
	if payload2["text"].(string) != "《Fast & Furious》" {
		t.Fatalf("非 HTML 模式不应转义: %q", payload2["text"])
	}
}

func TestNormalizePayloadKeepsIntentionalMarkup(t *testing.T) {
	payload := map[string]interface{}{
		"text":       `<b>《Fast &amp; Furious》</b>`,
		"parse_mode": "HTML",
	}
	normalizeTelegramPayload(payload)
	if got := payload["text"].(string); got != `<b>《Fast &amp; Furious》</b>` {
		t.Fatalf("已有转义+合法标签被破坏: %q", got)
	}
}
