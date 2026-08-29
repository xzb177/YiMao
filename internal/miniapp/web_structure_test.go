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
	gridBtnRe     = regexp.MustCompile(`\{text:"([^"]+)",icon:"[a-z]+"(?:,tone:"([a-z]+)")?`)
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

func TestMiniAppCanonicalFunctionsAndRichDetailContract(t *testing.T) {
	html := miniAppSource(t)
	for _, name := range []string{"render", "homePage", "searchPage", "tasksPage", "chrome", "detailPage"} {
		if got := strings.Count(html, "function "+name+"("); got != 1 {
			t.Fatalf("canonical function %s count=%d", name, got)
		}
	}
	if strings.Contains(html, "detail-actions") || strings.Contains(html, "function detailActionButton(") {
		t.Fatal("legacy detail action path remains")
	}
	if !strings.Contains(html, "yh-detail") || !strings.Contains(html, "yhGrid(") {
		t.Fatal("premium detail grid contract missing")
	}
	for _, label := range []string{"搜索求片", "查看进度", "返回首页"} {
		if !strings.Contains(html, label) {
			t.Fatalf("search control %q missing", label)
		}
	}
	if strings.Contains(html, "normalizeControls();render") || strings.Contains(html, "render();normalizeControls") {
		t.Fatal("canonical render depends on normalizeControls")
	}
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
		for _, need := range []string{"function render()", "function startRoute()", "function homePage()", "function tasksPage()", "function yhGrid(rows)"} {
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
	// Inspect emitted inline attributes; implementation-time escapes are safe only when output handlers are valid.
	if strings.Contains(html, `onclick=\"navigate(\\\"`) || strings.Contains(html, `onclick=\"startSearchMode(\\\"`) {
		t.Fatal("emitted onclick contains an escaped quote")
	}
	// Media ids are passed through data-* attributes and validated in JS rather
	// than interpolated into the handler string, so no quoting is needed at all.
	if !strings.Contains(html, `onclick="openMediaFromElement(this)"`) {
		t.Fatal("media cards must dispatch through the validated data-attribute handler")
	}
	if strings.Contains(html, `openDetail(${id}`) || strings.Contains(html, `openDetail(${x.`) {
		t.Fatal("media ids must not be interpolated directly into an inline handler")
	}
}

var fourCharLexicon = map[string]bool{
	"搜索求片": true, "查看进度": true, "帮助说明": true, "更多功能": true,
	"返回首页": true, "刷新状态": true, "申请洗版": true, "进入许愿": true,
	"系统设置": true, "问题反馈": true, "游戏中心": true,
}

func TestMiniAppButtonLabelsAreFourCJKCharacters(t *testing.T) {
	html := miniAppSource(t)
	matches := gridBtnRe.FindAllStringSubmatch(html, -1)
	if len(matches) < 15 {
		t.Fatalf("expected the full three-column button set, found %d", len(matches))
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
			t.Errorf("only 搜索求片 keeps the success tone, got %q", label)
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
	// Every grid row must be a multiple of three buttons.
	for _, fn := range []string{"function yhHomeActions()", "function yhTaskActions()"} {
		at := strings.Index(html, fn)
		if at < 0 {
			t.Fatalf("missing %s", fn)
		}
		end := strings.Index(html[at:], "\n")
		row := html[at : at+end]
		if count := strings.Count(row, `{text:"`); count%3 != 0 || count == 0 {
			t.Errorf("%s emits %d buttons, want a multiple of 3", fn, count)
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
		`auxStartRoute.view==="detail"`,
		`openDetail(auxStartRoute.id,auxStartRoute.type,auxStartRoute.season)`,
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
