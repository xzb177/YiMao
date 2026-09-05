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
	if strings.Contains(html, "class=\"detail-actions\"") || strings.Contains(html, "function detailActionButton(") {
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
	"返回首页": true, "返回结果": true, "刷新状态": true, "申请洗版": true, "进入许愿": true,
	"求片提交": true, "提交求片": true, "继续搜索": true, "已经许愿": true, "状态确认": true, "选择季度": true,
	"系统设置": true, "问题反馈": true, "不可洗版": true, "加载更多": true,
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
		if style == "success" && label != "搜索求片" && label != "求片提交" && label != "提交求片" {
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

func TestMiniAppDetailStatusConfirmationRefreshesSelectedSeason(t *testing.T) {
	s := miniAppSource(t)
	for _, required := range []string{"refreshDetailStatus()", "状态已刷新", "状态确认失败", "type:" + "mediaType(x)", "season:season"} {
		if !strings.Contains(s, required) {
			t.Fatalf("missing detail refresh contract %q", required)
		}
	}
}

func TestDetailPageHasCompletePremiumVisualContract(t *testing.T) {
	html := miniAppSource(t)
	for _, selector := range []string{".yh-detail{", ".yh-detail-art img", ".yh-season-list", ".yh-season{", ".yh-detail-actions", ".yh-synopsis", ".yh-synopsis-toggle", "aria-pressed", "data-season"} {
		if !strings.Contains(html, selector) {
			t.Fatalf("missing detail visual contract %q", selector)
		}
	}
	if strings.Contains(html, `status==="in_library"){label="申请洗版"`) && strings.Contains(html, `text:"进入许愿"`) {
		t.Fatal("library detail must not use wish action")
	}
	if !strings.Contains(html, `href="data:,"`) {
		t.Fatal("Mini App favicon must not generate an app-owned 404")
	}
	if !strings.Contains(html, "refreshDetailStatus()") {
		t.Fatal("detail refresh action missing")
	}
}

func TestMiniAppSearchModeChangesCopyActionsAndPersists(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`function searchModeCopy()`,
		`S.mode==="wash"`,
		`title:"搜索洗版"`,
		`button:"搜索洗版"`,
		`已有内容换个更好的版本。只可选择已在库内容，新版本可用前会保留旧版。`,
		`function searchResultAction(x)`,
		`return status==="in_library"?"申请洗版":status==="unknown"?"状态确认":"不可洗版"`,
		`function requestSearch(){startSearchMode("request")}`,
		`function washSearch(){startSearchMode("wash")}`,
		`if(S.mode==="wash")`,
		`function loadMoreSearch(){if(!S.loading&&S.hasMore)searchNow(true,S.nextPage)}`,
		`function backDetail(){closeDialog();S.detailSeq++;S.detailVisible=false;render()}`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing mode contract %q", required)
		}
	}
	requestCopy := `title:"搜索求片",lede:"发中文或英文片名。点结果看详情，确认后再提交。",button:"搜索求片"`
	if !strings.Contains(html, requestCopy) {
		t.Fatalf("ordinary request copy must stay wash-free: missing %q", requestCopy)
	}
	if strings.Contains(requestCopy, "洗版") {
		t.Fatal("ordinary request copy leaks wash wording")
	}
}

func TestMiniAppUsesCanonicalFourStatusLabels(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`const SEARCH_STATUS_TEXT={in_library:"已在库",available:"可求片",downloading:"下载中",unknown:"状态暂未确认"}`,
		`function searchStatusText(x)`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing shared status contract %q", required)
		}
	}
	for _, legacy := range []string{"库中可看", "可以求片", "媒体库状态暂时无法确认", "选择季后确认状态"} {
		if strings.Contains(html, legacy) {
			t.Errorf("legacy Mini App search status remains: %q", legacy)
		}
	}
}

func TestMiniAppDetailAndSeasonStatusesUseCanonicalDisplayCopy(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`function mediaStatusText(x)`,
		`<span>"+esc(mediaStatusText(status))+"</span>`,
		`<b>"+esc(mediaStatusText(season.status))+"</b>`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing canonical detail display contract %q", required)
		}
	}
}

func TestOrdinaryRequestLibraryResultDoesNotOfferWash(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`return status==="in_library"?"查看详情"`,
		`}else if(status==="in_library"){primary={text:"返回结果",icon:"back",tone:"primary",action:"backDetail()"};secondary={text:"继续搜索",icon:"search",action:"continueFinding()"}}`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing ordinary request no-wash contract %q", required)
		}
	}
}

func TestWashModeDoesNotLeakAfterLeavingSearchFlow(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`if(view==="home"){S.mode="request";S.view="home";render();return}`,
		`function goTasks(){closeDialog();S.mode="request";S.detailVisible=false;S.view='tasks';S.tasks=null;render()}`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing wash scope reset %q", required)
		}
	}
}

func TestMiniAppSearchDockLabelFollowsMode(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`const searchDockLabel=S.mode==='wash'?'搜索洗版':'搜索求片'`,
		`const searchDockAction=S.mode==='wash'?'washSearch()':'requestSearch()'`,
		`tab("search",searchDockLabel,"search",searchDockAction)`,
		`function continueFinding(){`,
		`S.detailVisible=false;S.view='search';render();document.getElementById('q')?.focus()`,
		`function loadMoreSearch(){if(!S.loading&&S.hasMore)searchNow(true,S.nextPage)}`,
		`grid-template-columns:repeat(3,minmax(0,1fr))`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing dynamic search dock contract %q", required)
		}
	}
}

func TestMiniAppOrdinarySearchActionsUseFourCharacterBusinessCopy(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`return status==="in_library"?"查看详情"`,
		`return status==="in_library"?"查看详情":status==="downloading"?"查看进度":status==="unknown"?"状态确认":mediaType(x)==="tv"?"选择季度":"提交求片"`,
		`primary={text:"提交求片",icon:"search",tone:"success",action:"openRequestConfirm()"}`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing ordinary action contract %q", required)
		}
	}
}

func TestMiniAppWashResultActionsAreSeparatedAndUnavailableDisabled(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`.yh-pill{`,
		`.result-action{`,
		`min-height:44px`,
		`background:var(--yh-glass-2)`,
		`function resultActionClass(x)`,
		`class="result-action ${resultActionClass(x)}"`,
		`aria-disabled="true"`,
		`不可洗版`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing wash action visual contract %q", required)
		}
	}
	if strings.Contains(html, `.result-action{display:inline-flex;flex:none;align-items:center;justify-content:center;min-height:36px`) {
		t.Fatal("result action remains below the 44px touch target")
	}
}

func TestMiniAppWashUnavailableResultsSayNotInLibraryAndRemainReadOnly(t *testing.T) {
	html := miniAppSource(t)
	for _, required := range []string{
		`function washSearchStatusText(x)`,
		`return statusCode(x)==="in_library"?"已在库":"尚未入库"`,
		`.yh-pill{`,
		`pointer-events:none`,
		`result-action.is-disabled`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing wash result presentation contract %q", required)
		}
	}
	// Test the mode-dependent expression in the actual search renderer, not
	// an incidental choice of JavaScript quote style or a legacy helper.
	start := strings.Index(html, "function searchPage()")
	end := strings.Index(html, "function yhStateTone(item)")
	if start < 0 || end <= start {
		t.Fatal("search renderer boundaries missing")
	}
	modeStatus := regexp.MustCompile(`S\.mode\s*===\s*["']wash["']\s*\?\s*washSearchStatusText\(x\)\s*:\s*searchStatusText\(x\)`)
	if !modeStatus.MatchString(html[start:end]) {
		t.Fatal("search renderer must use wash status copy only in wash mode")
	}
}
