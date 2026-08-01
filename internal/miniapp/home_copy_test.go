package miniapp

import (
	"os"
	"strings"
	"testing"
)

// TestHomeUsesAppFirstDiscoveryLayout 验证首页采用紧凑、可扫描的 App 布局，而不是网页 Hero 模板。
func TestHomeUsesAppFirstDiscoveryLayout(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}

	html := string(page)
	for _, want := range []string{
		"lead.backdrop_path||lead.poster_path",
		`class="top compact-top-bar glass-surface"`,
		`class="search home-search"`,
		`<section class="featured-mosaic"`,
		`class="featured-primary"`,
		`class="featured-stack"`,
		"grid-template-rows: minmax(0, 1fr)",
		`class="feed-list"`,
		`class="wide-rail"`,
		"function feedCard(x,index)",
		"今日精选",
		"搜片名、剧集或演员",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("首页缺少 App-first 发现页契约 %q", want)
		}
	}
	for _, stale := range []string{
		`class="home-feature"`,
		`class="feature"`,
		"rail('热门电影'",
		"本周热看",
		"大家最近在看",
		"今晚看什么？",
		"先逛逛热门，也可以直接搜片名。",
		"想看的，<br>交给云海。",
		"找到今晚真正想看的故事",
	} {
		if strings.Contains(html, stale) {
			t.Fatalf("首页仍包含旧 Hero 模板或旧口号 %q", stale)
		}
	}

	homeStart := strings.Index(html, "function home(){")
	if homeStart < 0 {
		t.Fatal("未找到首页渲染函数")
	}
	homeEnd := strings.Index(html[homeStart:], "let homeController")
	if homeEnd < 0 {
		t.Fatal("未找到首页渲染函数边界")
	}
	home := html[homeStart : homeStart+homeEnd]
	if !strings.Contains(home, "return search+homeDiscoverError()+featured+popular+shows") {
		t.Fatal("首页没有按顶部搜索、非对称精选、纵向热门列表的顺序渲染")
	}
}

func TestMiniAppUsesNeutralCinemaPaletteAndMotionFallback(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"--bg: #090a0c",
		"--accent: #f05a4f",
		"color: var(--accent-ink)",
		"data-type=\"'+esc(v)+'\" onclick=\"setSearchType(this.dataset.type)\"",
		"overflow-x: clip",
		"@media (prefers-reduced-motion: reduce)",
		`min-width: 320px`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少视觉系统契约 %q", want)
		}
	}
	for _, stale := range []string{"#e9ff5b", "radial-gradient", "--bg:#11100f"} {
		if strings.Contains(html, stale) {
			t.Fatalf("Mini App 仍包含旧视觉主题 %q", stale)
		}
	}
}

func TestMiniAppUsesReadableLiquidGlassWithFallback(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"--glass: #1a1b20",
		"--glass-border: rgba(255, 255, 255, .14)",
		"--glass-highlight: rgba(255, 255, 255, .1)",
		"box-shadow: inset 0 1px 0 var(--glass-highlight)",
		"@supports ((-webkit-backdrop-filter: blur(1px)) or (backdrop-filter: blur(1px)))",
		"@supports not ((-webkit-backdrop-filter: blur(1px)) or (backdrop-filter: blur(1px)))",
		"-webkit-backdrop-filter: blur(16px) saturate(120%)",
		"backdrop-filter: blur(16px) saturate(120%)",
		`class="top compact-top-bar glass-surface"`,
		`class="bottom glass-surface"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少克制的 Liquid Glass 契约 %q", want)
		}
	}
	for _, contentSurface := range []string{`class="search home-search glass-surface"`, ".avatar,\n  .detail-actions", ".type-badge,\n  .confirm"} {
		if strings.Contains(html, contentSurface) {
			t.Fatalf("Glass 不应进入内容表面 %q", contentSurface)
		}
	}
}

func TestAppShellReservesTabBarSpaceAndAccessibleTargets(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"--tab-height: 50px",
		"--tab-clearance: calc(var(--tab-height) + var(--safe-bottom) + var(--tab-gap) + 18px)",
		"padding: var(--safe-top) 16px var(--tab-clearance)",
		"bottom: calc(var(--safe-bottom) + var(--tab-gap))",
		`class="bottom glass-surface"`,
		`class="avatar" aria-label="打开我的"`,
		`class="search-submit" aria-label="搜索"`,
		`aria-current="page"`,
		`role="alert"`,
		`aria-busy="true"`,
		".search input {\n  min-width: 0;\n  min-height: 44px",
		"min-height: 44px",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("App shell 缺少底栏留白或无障碍契约 %q", want)
		}
	}
}

// TestMePagePersistsSectionErrors 验证“我的”页面失败后展示分区内联错误，而不是只弹短暂提示。
func TestMePagePersistsSectionErrors(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{"S.meError=me.reason?.message", "S.watchError=watch.reason?.message"} {
		if !strings.Contains(html, want) {
			t.Fatalf("我的页面缺少错误状态赋值 %q", want)
		}
	}
}

func TestSearchLoadMoreCommitsPageOnlyAfterSuccess(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"function loadMore(){if(!S.loading&&S.hasMore){const request={query:S.query,type:S.searchType,page:S.nextPage}",
		"S.page=Number(d.page)||current.page",
		"S.nextPage=Number(d.next_page)||S.page+1",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("搜索分页缺少成功后提交行为 %q", want)
		}
	}
	if strings.Contains(html, "S.page++;doSearch(true)") {
		t.Fatal("加载更多仍会在请求成功前推进页码")
	}
}

func TestSearchAbortsAndScopesEveryRequest(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"let searchController=null",
		"searchController?.abort()",
		"searchController=controller",
		"{query:S.query,type:S.searchType,page:targetPage}",
		"{signal:controller.signal}",
		"e?.name==='AbortError'",
		"S.query!==current.query||S.searchType!==current.type",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("搜索请求缺少取消或请求快照契约 %q", want)
		}
	}
}

func TestHomeLoadingHasPersistentSafeFailureStates(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"function homeDiscoverError()",
		"S.discoverLoading=false",
		"S.discoverAuthError=e.status===401||e.status===403",
		"Mini App 会话已过期，请从 Telegram 重新打开",
		"onclick=\"loadHome()\">重新加载",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("发现页缺少失败状态行为 %q", want)
		}
	}
	if strings.Contains(html, "await r.text()") {
		t.Fatal("前端仍会把原始 API 响应文本暴露给用户")
	}
}

func TestDialogsHaveCancellationAndAccessibleFocusLifecycle(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		`role="dialog" aria-modal="true" aria-labelledby="dialog-title"`,
		"D.controller=new AbortController()",
		"if(!isCurrentDialog(ctx.seq))return",
		"if(event.key==='Escape')",
		"if(event.key!=='Tab')return",
		"restore?.isConnected",
		"setDialogBusy(ctx.seq,true)",
		"function backDetail(){S.detailSeq++",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("对话框生命周期缺少行为 %q", want)
		}
	}
}

func TestMiniAppUsesTelegramAndBrowserSafeAreas(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"--tg-safe-area-inset-top",
		"--tg-content-safe-area-inset-top",
		"env(safe-area-inset-top, 0px)",
		"env(safe-area-inset-bottom, 0px)",
		"--safe:var(--safe-bottom)",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少安全区变量 %q", want)
		}
	}
}
