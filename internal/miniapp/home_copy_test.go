package miniapp

import (
	"os"
	"strings"
	"testing"
)

func miniAppSource(t *testing.T) string {
	t.Helper()
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(page)
}

func requireSource(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少源码契约 %q", want)
		}
	}
}

func rejectSource(t *testing.T, html string, stale ...string) {
	t.Helper()
	for _, value := range stale {
		if strings.Contains(html, value) {
			t.Fatalf("Mini App 仍包含废弃契约 %q", value)
		}
	}
}

func TestMiniAppUsesCinemaStudioAcrossRootPages(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`class="home-studio"`,
		`class="studio-console"`,
		`class="prompt-composer glass-surface"`,
		`class="screening-flow"`,
		`class="screening-scene"`,
		`class="film-thread"`,
		`class="search-workspace"`,
		`class="search-console"`,
		`class="search-results" aria-live="polite"`,
		`class="account-workspace"`,
		`class="account-summary"`,
		`class="account-content"`,
		`class="tool-workspace"`,
		`class="tool-header"`,
		`class="tool-body"`,
		"YiMao 放映室",
		"今晚想看点什么？",
		"电影大冒险",
		"许愿池",
		"问题反馈",
	)
	rejectSource(t, html,
		`class="featured-home"`,
		`class="featured-lead"`,
		`class="featured-rail"`,
		`class="feed-list"`,
		`class="wide-rail"`,
		"function feedCard(",
		"function featuredLeadArtwork(",
		`class="bottom glass-surface"`,
		"AI 精选",
	)
}

func TestHomeTimelineUsesSingleMobileTrackAndDesktopGrid(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"margin: 26px 0 0 21px",
		".thread-turn {",
		"margin: 0 0 24px",
		"width: 100%",
		"aspect-ratio: 4 / 5",
		".thread-turn.is-wide .thread-art {",
		"aspect-ratio: 16 / 7",
		"@media (min-width: 900px)",
		"grid-template-columns: repeat(2, minmax(0, 1fr))",
		"aspect-ratio: 2 / 3",
	)
	rejectSource(t, html,
		"padding-right: 16%",
		"padding-left: 18%",
		"width: calc(100% + 21px)",
		"margin-left: -21px",
	)
}

func TestHomeArtworkUsesOneIdentityAndIndependentFallbacks(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"function mediaIdentity(x)",
		"function screeningMedia(item,variant='lead')",
		"function screeningImageError(image,role='subject')",
		"data-artwork",
		`class="studio-ambient"`,
		`class="studio-subject"`,
		"media.dataset[role+'Failed']='1'",
		"if(subjectFailed&&ambientFailed)media.classList.add('is-error')",
		"function card(x){const mid=Number(id(x));if(!Number.isFinite(mid)||mid<=0)return ''",
		"for(const item of [...featured,...movies,...shows",
		"mediaIdentity(x)",
	)
}

func TestHomeComposerUsesAssistantWithSafeFallbacks(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`onsubmit="submitAssistant(event)"`,
		`/api/miniapp/v1/assistant`,
		"assistantHistory.slice(-6)",
		"assistantController?.abort()",
		"seq!==S.assistantSeq",
		"function safeAssistantItem(x)",
		"Number.isFinite(mid)&&mid>0",
		"['movie','tv','all'].includes",
		"esc(S.assistantReply)",
		"fallback_query||message",
	)
}

func TestShellUsesContextAppBarAndEntityDock(t *testing.T) {
	html := miniAppSource(t)
	start := strings.Index(html, "function chrome(")
	end := strings.Index(html, "function render(){")
	if start < 0 || end <= start {
		t.Fatal("未找到 chrome 渲染边界")
	}
	chrome := html[start:end]
	requireSource(t, chrome,
		"isHome=isRoot&&S.view==='home'",
		"const appBar=isRoot&&!isHome&&meta?",
		`<header class="app-bar">`,
		`<div class="bottom app-dock">`,
		`aria-label="主导航"`,
		`aria-current="page"`,
		"放映室",
		"找片",
		"我的",
	)
	rejectSource(t, chrome, `class="brand"`, `class="bottom glass-surface"`)
}

func TestGlobalVisualSystemIsMobileFirstAndDesktopReflows(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"--bg: #090a0c",
		"--accent: #f05a4f",
		"overflow-x: clip",
		"min-width: 0",
		"min-height: 44px",
		"@media (min-width: 900px)",
		".home-studio {",
		".search-workspace {",
		".tool-workspace {",
		".account-workspace {",
		"grid-template-columns: minmax(280px, .72fr) minmax(0, 1.28fr)",
		"@media (max-width: 350px)",
		"@media (prefers-reduced-motion: reduce)",
		"animation: none !important",
		"transition: none !important",
	)
	rejectSource(t, html, "#e9ff5b", "--bg:#11100f", "IntersectionObserver")
}

func TestGlassIsLimitedAndInputDriven(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`class="prompt-composer glass-surface"`,
		`class="app-bar"`,
		"function initGlassDynamics()",
		"document.querySelectorAll('.glass-surface').forEach(initGlassSurface)",
		"surface.addEventListener('pointermove'",
		"surface.addEventListener('pointerleave'",
		"if(glassMotionQuery?.matches){clearGlassDynamics();return}",
	)
	rejectSource(t, html,
		"document.querySelectorAll('.featured-rail').forEach(initGlassRail)",
		"function initGlassRail(",
		"window.addEventListener('scroll'",
		"requestAnimationFrame(loop)",
	)
}

func TestRootMotionIsOneShotAndReducedMotionSafe(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"rootMotion:''",
		"function queueRootMotion(next)",
		"from===next){S.rootMotion=''",
		"ROOT_VIEWS.indexOf(next)>ROOT_VIEWS.indexOf(from)?'forward':'backward'",
		"const motion=S.rootMotion;S.rootMotion=''",
		"root-enter-forward",
		"root-enter-backward",
		".tab-transition .nav.active i",
	)
	rejectSource(t, html, "animation: shine", "IntersectionObserver")
}

func TestSearchRacePaginationAndPersistentErrors(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"let searchController=null",
		"searchController?.abort()",
		"searchController=controller",
		"{query:S.query,type:S.searchType,page:targetPage}",
		"{signal:controller.signal}",
		"e?.name==='AbortError'",
		"S.query!==current.query||S.searchType!==current.type",
		"function loadMore(){if(!S.loading&&S.hasMore){const request={query:S.query,type:S.searchType,page:S.nextPage}",
		"S.page=Number(d.page)||current.page",
		"S.nextPage=Number(d.next_page)||S.page+1",
		"S.meError=me.reason?.message",
		"S.watchError=watch.reason?.message",
		"function homeDiscoverError()",
		"S.discoverAuthError=e.status===401||e.status===403",
	)
	rejectSource(t, html, "S.page++;doSearch(true)", "await r.text()")
}

func TestToolPagesIgnoreStaleResponses(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"toolSeq:0",
		"const seq=++S.toolSeq",
		"if(seq!==S.toolSeq||S.view!==view)return",
		"if(seq===S.toolSeq&&S.view===view){S.toolLoading=false;render()}",
		"view=S.view",
		"if(S.view===view)await goIssues()",
	)
}

func TestUntrustedMediaIDsAreNumericAndInvalidEntriesAreNotClickable(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"const mid=Number(w.tmdb_id),mt=w.media_type==='tv'||w.media_type==='电视剧'?'tv':'movie'",
		"const mid=Number(r.tmdb_id),mt=r.media_type==='tv'||r.media_type==='电视剧'?'tv':'movie'",
		"if(!Number.isFinite(mid)||mid<=0)return ''",
	)
	rejectSource(t, html,
		"onclick=\"openDetail(${w.tmdb_id}",
		"onclick=\"openDetail(${r.tmdb_id}",
	)
}

func TestDialogTelegramAndSafeAreaContractsRemain(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`role="dialog" aria-modal="true" aria-labelledby="dialog-title"`,
		"D.controller=new AbortController()",
		"if(!isCurrentDialog(ctx.seq))return",
		"if(event.key==='Escape')",
		"if(event.key!=='Tab')return",
		"restore?.isConnected",
		"setDialogBusy(ctx.seq,true)",
		"function backDetail(){S.detailSeq++",
		"tg?.BackButton?.show()",
		"tg?.HapticFeedback",
		"--tg-safe-area-inset-top",
		"--tg-content-safe-area-inset-top",
		"env(safe-area-inset-top, 0px)",
		"env(safe-area-inset-bottom, 0px)",
	)
}
