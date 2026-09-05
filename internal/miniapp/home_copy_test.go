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
			t.Errorf("Mini App 缺少源码契约 %q", want)
		}
	}
}

func rejectSource(t *testing.T, html string, stale ...string) {
	t.Helper()
	for _, value := range stale {
		if strings.Contains(html, value) {
			t.Errorf("Mini App 仍包含废弃契约 %q", value)
		}
	}
}

func requireOrder(t *testing.T, html string, values ...string) {
	t.Helper()
	last := -1
	for _, value := range values {
		next := strings.Index(html, value)
		if next < 0 {
			t.Errorf("Mini App 缺少顺序契约 %q", value)
			return
		}
		if next <= last {
			t.Errorf("Mini App 顺序错误：%q 必须出现在前一项之后", value)
			return
		}
		last = next
	}
}

// TestMiniAppHeroIsFullBleedBackdropWithScrimAndStatus covers the premium hero.
func TestMiniAppHeroIsFullBleedBackdropWithScrimAndStatus(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`function yhHero()`,
		`class="yh-hero"`,
		`class="yh-hero-art"`,
		`class="yh-hero-fallback"`,
		`class="yh-hero-scrim"`,
		`class="yh-kicker">YUNHAI · CINEMA`,
		`class="yh-title">云海求片`,
		`class="yh-tagline">想看的，交给云海`,
		`直接发片名，或点搜索。提交后可在进度里查到。`,
		`class="yh-status"`,
		`class="yh-dot"`,
		`在线 · 可求片`,
		"https://image.tmdb.org/t/p/w1280",
		"letter-spacing:.24em",
		"linear-gradient(to top,var(--yh-base-0)",
	)
	requireOrder(t, html,
		`class="yh-hero-art"`,
		`class="yh-hero-scrim"`,
		`class="yh-kicker">YUNHAI · CINEMA`,
		`class="yh-title">云海求片`,
		`class="yh-tagline">想看的，交给云海`,
		`class="yh-status"`,
	)
}

// TestMiniAppUsesLayeredDarkGlassTokens pins the new visual system.
func TestMiniAppUsesLayeredDarkGlassTokens(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"--yh-base-0:#07080b",
		"--yh-base-2:#0d0f14",
		"--yh-glass:",
		"--yh-line:rgba(255,255,255,.09)",
		"backdrop-filter:blur(",
		"--yh-rhythm:20px",
		"env(safe-area-inset-top,0px)",
		"env(safe-area-inset-bottom,0px)",
		"min-height:100dvh",
		"--yh-dock-clear:calc(var(--yh-dock-h) + var(--yh-safe-b) + 26px)",
		"padding-bottom:var(--yh-dock-clear)",
		"@media (prefers-reduced-motion:reduce)",
		"animation:none !important",
	)
	// The rejected cinema skin must be gone entirely.
	rejectSource(t, html,
		"--cinema-success",
		"--cinema-primary",
		"cinema-card",
		"cinema-hero",
		"cinema-credit",
		"cinema-buttons",
		"cinema-facts",
		"cinema-result",
		"taskPlaybill",
	)
	// Secondary text must stay readable, not washed out (D9/D10).
	requireSource(t, html, "--yh-ink-2:#c3ccd8", "--yh-ink-3:#8e99a8")
	rejectSource(t, html, "opacity:.35", "opacity:.4;color:var(--yh-ink-3)")
}

// TestMiniAppRailKeepsTitleInsideTheCardElement is the D1 regression guard.
func TestMiniAppRailKeepsTitleInsideTheCardElement(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`function yhCard(item,caption)`,
		`function yhRail(title,note,items,caption)`,
		`class="yh-rail"`,
		`class="yh-art"`,
		`class="yh-badge"`,
	)
	start := strings.Index(html, "function yhCard(item,caption)")
	end := strings.Index(html, "function yhRail(")
	if start < 0 || end <= start {
		t.Fatal("未找到 rail card 渲染边界")
	}
	card := html[start:end]
	// Artwork, title and year must all be emitted inside the single button element.
	for _, need := range []string{
		`<button type="button" class="yh-card"`,
		`<span class="yh-art">`,
		`<strong>'+esc(title)+'</strong>`,
		`<small>'+esc(year||caption||"")+'</small>`,
		`</button>`,
	} {
		if !strings.Contains(card, need) {
			t.Errorf("rail card 缺少 %q，标题必须与海报同属一个卡片元素", need)
		}
	}
	if strings.Contains(card, "</button>") && strings.Index(card, "<strong>") > strings.Index(card, "</button>") {
		t.Error("rail card 标题被渲染到卡片元素之外，会与海报脱节滚动")
	}
	// Fixed column width, ellipsis truncation and a right-side gutter.
	requireSource(t, html,
		".yh-card{flex:0 0 122px;width:122px;min-width:122px;max-width:122px;",
		".yh-card strong{display:block;width:122px;max-width:122px;",
		"text-overflow:ellipsis",
		`.yh-rail::after{content:""`,
		"flex:0 0 var(--yh-gutter)",
	)
	// The badge is a small pill anchored to the artwork, never over the title.
	requireSource(t, html, ".yh-badge{position:absolute;left:7px;bottom:7px;")
}

// TestMiniAppActionGridIsThreeColumns mirrors the chat-side 方案1 grid.
func TestMiniAppActionGridIsThreeColumns(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`function yhGrid(rows)`,
		`function yhHomeActions()`,
		`function yhTaskActions()`,
		`class="yh-grid"`,
		"grid-template-columns:repeat(3,minmax(0,1fr))",
		"min-height:74px",
		`class="yh-act is-`,
		"stroke-width:1.7",
		"linear-gradient(165deg,var(--yh-green-1),var(--yh-green-2))",
	)
	start := strings.Index(html, "function yhHomeActions()")
	end := strings.Index(html, "function yhTaskActions()")
	if start < 0 || end <= start {
		t.Fatal("未找到首页动作栅格边界")
	}
	grid := html[start:end]
	requireOrder(t, grid, `"搜索求片"`, `"查看进度"`, `"申请洗版"`, `"进入许愿"`, `"帮助说明"`, `"更多功能"`)
	if strings.Count(grid, `tone:"success"`) != 1 {
		t.Errorf("首页栅格只能有一个 success 按钮，实际 %d", strings.Count(grid, `tone:"success"`))
	}
	if !strings.Contains(grid, `{text:"搜索求片",icon:"search",tone:"success"`) {
		t.Error("只有搜索求片可以使用绿色渐变")
	}
	// Dock must clear content and centre the selected pill.
	requireSource(t, html,
		`class="yh-dock"`,
		`aria-label="主导航"`,
		`aria-current="page"`,
		".yh-dock button{display:grid;justify-items:center;",
		`.yh-dock button[aria-current="page"]{background:rgba(76,141,255,.16)`,
	)
}

// TestMiniAppProgressViewIsATimeline covers the tasks redesign.
func TestMiniAppProgressViewIsATimeline(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`function yhSteps(item)`,
		`function yhTask(item)`,
		`function tasksPage()`,
		`class="yh-time"`,
		`class="yh-step"`,
		`class="yh-node`,
		"is-done",
		"is-now",
		`function groupedTasks(items)`,
		`function taskTimelineBlock(requestID)`,
		`function toggleTaskTimeline(element)`,
		`class="yh-task-state `,
		`class="timeline-toggle"`,
		`进度明细`,
		`/api/miniapp/v1/me`,
		`/api/miniapp/v1/progress?request_id=`,
	)
	// The redesign moved facts out of the old table. Keep the identity,
	// year/type/season, state, and expandable event controls tied to yhTask.
	start := strings.Index(html, "function yhTask(item)")
	end := strings.Index(html, "function mcTaskGroup(")
	if start < 0 || end <= start {
		t.Fatal("task renderer boundaries missing")
	}
	task := html[start:end]
	requireSource(t, task,
		`esc(mediaTitle(item))`,
		`class="task-meta"`,
		`esc(item?.media_year||item?.year||'年份待定')`,
		`mediaType(item)==='tv'?'剧集内容':'电影内容'`,
		`Number(item.season)`,
		`item?.business_type==='wash'?'洗版任务':'求片任务'`,
		`esc(status)`,
		`yhSteps(item)`,
		`data-request-id="`,
		`esc(id)`,
		`aria-expanded="`,
		`S.timelineOpen===id`,
		`toggleTaskTimeline(this)`,
		`taskTimelineBlock(id)`,
		`item.can_cancel&&id`,
	)
}

func TestMiniAppKeepsAuthenticatedBusinessAPIs(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`await telegramReady`,
		`tg=telegramWebApp()||tg`,
		`X-Telegram-Init-Data`,
		`/api/miniapp/v1/search`,
		`/api/miniapp/v1/detail`,
		`/api/miniapp/v1/request`,
		`/api/miniapp/v1/wash`,
		`/api/miniapp/v1/watchlist`,
		`/api/miniapp/v1/wishes`,
		`/api/miniapp/v1/issues`,
		`/api/miniapp/v1/dynamic`,
		`/api/miniapp/v1/discover`,
	)
	// initData must never be accepted from the query string, and identity must
	// never be read from the unsigned initDataUnsafe payload.
	rejectSource(t, html, "initDataUnsafe?.user?.id", "?initData=")
}

func TestMiniAppHeroReusesDiscoverPayloadWithoutPerRenderFetch(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`function miniBackdrop(item)`,
		"https://image.tmdb.org/t/p/w1280",
		`api("/api/miniapp/v1/discover")`,
		`(S.homeDynamic?.featured||[]).find(function(x){return miniBackdrop(x)})`,
		"Promise.allSettled",
	)
	// One batched load per home render, not a TMDB call per painted card.
	if strings.Count(html, `api("/api/miniapp/v1/discover")`) != 1 {
		t.Errorf("discover 应只在 loadHome 批量调用一次，实际 %d 次", strings.Count(html, `api("/api/miniapp/v1/discover")`))
	}
}
