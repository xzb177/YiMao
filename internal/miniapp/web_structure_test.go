package miniapp

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

const miniAppBootstrap = "render();initTelegramWebApp();syncBack();"

// backslash is built from an interpreted literal so the assertions below can
// look for escape sequences without the test source needing escaped escapes.
var backslash = "\\"

var (
	scriptTagRe   = regexp.MustCompile(`(?s)<script\b[^>]*>|</script>`)
	inlineBlockRe = regexp.MustCompile(`(?s)<script(?:\s+[a-zA-Z-]+(?:="[^"]*")?)*\s*>(.*?)</script>`)
	onclickRe     = regexp.MustCompile(`onclick="([^"]*)"`)
	onsubmitRe    = regexp.MustCompile(`onsubmit="([^"]*)"`)
	cinemaBtnRe   = regexp.MustCompile(`\{text:"([^"]+)"(?:,style:"([a-z]+)")?`)
)

// scriptSpans pairs every script open tag with its close, so a wrongly nested
// block is detected instead of merely counting equal numbers of tags.
func scriptSpans(t *testing.T, html string) (depth int, maxDepth int, spans [][2]int) {
	t.Helper()
	open := -1
	for _, m := range scriptTagRe.FindAllStringIndex(html, -1) {
		if strings.HasPrefix(html[m[0]:m[1]], "</") {
			depth--
			if depth < 0 {
				t.Fatalf("</script> at offset %d closes without a matching open tag", m[0])
			}
			if depth == 0 && open >= 0 {
				spans = append(spans, [2]int{open, m[1]})
				open = -1
			}
			continue
		}
		if depth == 0 {
			open = m[0]
		}
		depth++
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return depth, maxDepth, spans
}

// bodyOutsideScripts returns every byte of <body> a browser treats as markup or
// text rather than JavaScript.
func bodyOutsideScripts(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, "<body>")
	if start < 0 {
		t.Fatal("Mini App HTML has no <body>")
	}
	body := html[start:]
	_, _, spans := scriptSpans(t, body)
	var out strings.Builder
	cursor := 0
	for _, span := range spans {
		if span[0] < cursor {
			continue
		}
		out.WriteString(body[cursor:span[0]])
		cursor = span[1]
	}
	out.WriteString(body[cursor:])
	return out.String()
}

func TestShippedMiniAppScriptBlocksAreBalancedAndNotNested(t *testing.T) {
	html := miniAppSource(t)
	depth, maxDepth, spans := scriptSpans(t, html)
	if depth != 0 {
		t.Fatalf("unbalanced script tags: final depth=%d", depth)
	}
	if maxDepth != 1 {
		t.Fatalf("script blocks must not nest: max depth=%d", maxDepth)
	}
	if len(spans) != 2 {
		t.Fatalf("expected exactly the inline block plus the SDK tag, got %d blocks", len(spans))
	}
	if opens, closes := strings.Count(html, "<script"), strings.Count(html, "</script>"); opens != closes {
		t.Fatalf("script tag counts differ: open=%d close=%d", opens, closes)
	}
	if opens, closes := strings.Count(html, "<style>"), strings.Count(html, "</style>"); opens != closes {
		t.Fatalf("style tag counts differ: open=%d close=%d", opens, closes)
	}
}

func TestMiniAppBootstrapRunsInsideAScriptBlock(t *testing.T) {
	html := miniAppSource(t)
	if got := strings.Count(html, miniAppBootstrap); got != 1 {
		t.Fatalf("bootstrap call must appear exactly once, got %d", got)
	}
	if strings.Contains(bodyOutsideScripts(t, html), miniAppBootstrap) {
		t.Fatal("bootstrap call sits in the document body as text and can never execute")
	}
	found := false
	for _, block := range inlineBlockRe.FindAllStringSubmatch(html, -1) {
		if !strings.Contains(block[1], miniAppBootstrap) {
			continue
		}
		found = true
		for _, need := range []string{"function render()", "function startRoute()", "function homePage()", "function tasksPage()"} {
			if !strings.Contains(block[1], need) {
				t.Errorf("bootstrap block is missing %s, so the call would run before it is defined", need)
			}
		}
	}
	if !found {
		t.Fatal("bootstrap call is not inside any inline script block")
	}
	if !strings.Contains(html, `<script async src="/miniapp/telegram-web-app.js" onload="initTelegramWebApp()" onerror="resolveTelegramReady?.()"></script>`) {
		t.Fatal("deferred Telegram SDK tag must be preserved")
	}
	for _, contract := range []string{"const telegramReady=new Promise", "await telegramReady", "resolveTelegramReady?.();resolveTelegramReady=null"} {
		if !strings.Contains(html, contract) {
			t.Errorf("telegramReady contract broken: missing %q", contract)
		}
	}
}

func TestMiniAppInlineHandlersHaveNoBackslashEscapes(t *testing.T) {
	html := miniAppSource(t)
	for _, re := range []*regexp.Regexp{onclickRe, onsubmitRe} {
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if strings.Contains(m[1], backslash) {
				t.Errorf("inline handler contains a backslash escape and is invalid JS: %q", m[1])
			}
		}
	}
	// The cinema action strings are injected verbatim into onclick, so an
	// escaped quote anywhere in them produces a runtime syntax error.
	for _, m := range regexp.MustCompile(`action:"([^"]*)"`).FindAllStringSubmatch(html, -1) {
		if strings.Contains(m[1], backslash) {
			t.Errorf("cinema button action is over-escaped: %q", m[1])
		}
	}
	if !strings.Contains(html, `action:"navigate('search')"`) || !strings.Contains(html, `action:"navigate('tasks')"`) {
		t.Fatal("cinema buttons must emit single-quoted, valid handlers")
	}
	if !strings.Contains(html, `openDetail(${id},'${type}',0)`) {
		t.Fatal("search result handler must emit a single-quoted media type")
	}
}

var fourCharLexicon = map[string]bool{
	"搜索求片": true, "查看进度": true, "帮助说明": true, "更多功能": true,
	"返回首页": true, "刷新状态": true, "申请洗版": true, "进入许愿": true,
	"系统设置": true, "问题反馈": true,
}

func TestMiniAppButtonLabelsAreFourCJKCharacters(t *testing.T) {
	html := miniAppSource(t)
	matches := cinemaBtnRe.FindAllStringSubmatch(html, -1)
	if len(matches) < 12 {
		t.Fatalf("expected the full cinema/playbill button set, found %d", len(matches))
	}
	seen := map[string]bool{}
	for _, m := range matches {
		label, style := m[1], m[2]
		seen[label] = true
		runes := []rune(label)
		if len(runes) != 4 {
			t.Errorf("button label %q has %d characters, want exactly 4", label, len(runes))
		}
		for _, r := range runes {
			if !unicode.Is(unicode.Han, r) {
				t.Errorf("button label %q must be CJK only, found %q", label, r)
			}
		}
		if !fourCharLexicon[label] {
			t.Errorf("button label %q is outside the approved lexicon", label)
		}
		if style == "success" && label != "搜索求片" {
			t.Errorf("only 搜索求片 keeps the success style, got %q", label)
		}
	}
	for _, need := range []string{"搜索求片", "查看进度", "帮助说明", "更多功能", "返回首页", "刷新状态"} {
		if !seen[need] {
			t.Errorf("core label %q is missing from the shipped screens", need)
		}
	}
	for _, stale := range []string{`{text:"帮助"`, `{text:"更多"`, `{text:"返回"`, `{text:"刷新"`, `{text:"洗版"`, `{text:"许愿池"`, `{text:"设置"`, `{text:"主菜单"`, `{text:"求片进度"`} {
		if strings.Contains(html, stale) {
			t.Errorf("stale short label remains: %s", stale)
		}
	}
}

func TestMiniAppStartAppRoutingSurvivesLabelChange(t *testing.T) {
	requireSource(t, miniAppSource(t),
		`function startRoute()`,
		`q.get("tgWebAppStartParam")||q.get("start_param")`,
		`raw==="tasks"||raw==="progress"`,
		`raw==="search"`,
		`detail_`,
		`return{view:"home"}`,
		`approvedStartRoute.view==="detail"`,
		`openDetail(approvedStartRoute.id,approvedStartRoute.type,approvedStartRoute.season)`,
	)
}

func TestServedMiniAppHTMLMatchesTheAuditedSource(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/miniapp", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	served := response.Body.String()
	if served != miniAppSource(t) {
		t.Fatal("served Mini App HTML differs from the audited web/index.html")
	}
	depth, maxDepth, _ := scriptSpans(t, served)
	if depth != 0 || maxDepth != 1 {
		t.Fatalf("served HTML has broken script nesting: depth=%d max=%d", depth, maxDepth)
	}
	if strings.Contains(bodyOutsideScripts(t, served), miniAppBootstrap) {
		t.Fatal("served HTML leaves the bootstrap call outside a script block")
	}
}
