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
		`class="search home-search glass-surface"`,
		`<section class="featured-home"`,
		`class="featured-lead"`,
		`class="featured-rail"`,
		`class="featured-poster-card"`,
		`class="featured-lead-media '+mode+'"`,
		"object-fit: contain",
		`class="feed-list"`,
		`class="wide-rail"`,
		"function feedCard(x,index)",
		"function mediaIdentity(x)",
		"const featuredUsed=new Set()",
		"featuredItems=(d.featured||[]).filter(x=>x&&Number(id(x))>0)",
		"...movies,...tv",
		"if(featuredRailItems.length===3)break",
		"function card(x){const mid=Number(id(x));if(mid<=0)return ''",
		"const popularSeen=new Set(featuredUsed),popularItems=[]",
		"Number(id(item))>0&&!popularSeen.has(identity)",
		"scheduleChromeDynamics()",
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
		`class="featured-mosaic"`,
		`class="featured-primary"`,
		`class="featured-stack"`,
		`class="featured-compact"`,
		"grid-template-rows: minmax(0, 1fr)",
		"const sidePicks=",
		"movies.slice(0,6).map(feedCard)",
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
		t.Fatal("首页没有按顶部搜索、电影精选、纵向热门列表的顺序渲染")
	}
	feedStart := strings.Index(html, "function feedCard(x,index)")
	if feedStart < 0 {
		t.Fatal("未找到热门 feed 渲染函数边界")
	}
	feedEnd := strings.Index(html[feedStart:], "function chrome(")
	if feedEnd <= 0 {
		t.Fatal("未找到热门 feed 渲染函数边界")
	}
	feed := html[feedStart : feedStart+feedEnd]
	if strings.Contains(feed, "<small>详情</small>") {
		t.Fatal("热门 feed 仍使用含糊的 Details 标签")
	}
	for _, want := range []string{"class=\"feed-row\"", "feed-ambient", "feed-subject", "暂无简介"} {
		if !strings.Contains(feed, want) {
			t.Fatalf("热门 feed 缺少完整行卡片契约 %q", want)
		}
	}
}

func TestHomePosterSurfacesPreserveFullArtwork(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, selector := range []string{
		".featured-lead-media.has-poster .featured-lead-subject",
		".featured-poster-media .featured-poster-subject",
		".feed-poster .feed-subject",
	} {
		start := strings.Index(html, selector+" {")
		if start < 0 {
			t.Fatalf("首页缺少海报完整展示选择器 %q", selector)
		}
		end := strings.Index(html[start:], "}\n")
		if end < 0 || !strings.Contains(html[start:start+end], "object-fit: contain") {
			t.Fatalf("首页海报仍可能被裁切 %q", selector)
		}
	}
	for _, want := range []string{
		`class="featured-lead-ambient"`,
		`class="featured-poster-ambient"`,
		`class="feed-ambient"`,
		"filter: blur(",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("首页海报缺少环境背景契约 %q", want)
		}
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
	for _, stale := range []string{"#e9ff5b", "--bg:#11100f"} {
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
		"--glass: rgba(18, 19, 23, .68)",
		"--glass-border: rgba(255, 255, 255, .2)",
		"--glass-highlight: rgba(255, 255, 255, .18)",
		"radial-gradient(circle at var(--glass-x) var(--glass-y)",
		"background-position: calc(var(--glass-x) + var(--glass-shift)) var(--glass-y)",
		"inset 0 1px 0 var(--glass-highlight)",
		"@supports ((-webkit-backdrop-filter: blur(1px)) or (backdrop-filter: blur(1px)))",
		"@supports not ((-webkit-backdrop-filter: blur(1px)) or (backdrop-filter: blur(1px)))",
		"-webkit-backdrop-filter: blur(18px) saturate(135%) contrast(105%)",
		"backdrop-filter: blur(18px) saturate(135%) contrast(105%)",
		"background-color: rgba(24, 25, 30, .96)",
		`class="app-bar glass-surface"`,
		`class="bottom glass-surface"`,
		`class="search home-search glass-surface"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少克制的 Liquid Glass 契约 %q", want)
		}
	}
	for _, contentSurface := range []string{".avatar,\n  .detail-actions", ".type-badge,\n  .confirm"} {
		if strings.Contains(html, contentSurface) {
			t.Fatalf("Glass 不应进入内容表面 %q", contentSurface)
		}
	}
}

func TestLiquidGlassDynamicsAreInputDrivenAndReducedMotionSafe(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"--glass-x: 50%",
		"--glass-y: 12%",
		"--glass-shift: 0px",
		"function initGlassDynamics()",
		"document.querySelectorAll('.glass-surface').forEach(initGlassSurface)",
		"document.querySelectorAll('.featured-rail').forEach(initGlassRail)",
		"surface.addEventListener('pointermove'",
		"surface.addEventListener('pointerdown'",
		"surface.addEventListener('pointerleave'",
		"surface.addEventListener('pointercancel'",
		"surface.addEventListener('pointerup'",
		"surface.style.setProperty('--glass-x'",
		"surface.style.setProperty('--glass-y'",
		"surface.style.setProperty('--glass-shift'",
		"state.frame=requestAnimationFrame(flush)",
		"state.frame=requestAnimationFrame(update)",
		"rail.addEventListener('scroll',state.scroll,{passive:true})",
		"if(glassMotionQuery?.matches){clearGlassDynamics();return}",
		"clearGlassDynamics()",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Liquid Glass 缺少输入驱动或减弱动态契约 %q", want)
		}
	}
	for _, stale := range []string{"window.addEventListener('scroll'", "gsap", "requestAnimationFrame(loop)"} {
		if strings.Contains(html, stale) {
			t.Fatalf("Liquid Glass 使用了非局部或连续动画机制 %q", stale)
		}
	}
}

func TestFeaturedRailFocusAndMissingIDIdentityAreCompatible(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"function railCards(rail){return Array.from(rail.children).filter(card=>card.classList.contains('media'))}",
		"const cards=railCards(rail)",
		"railCards(rail).forEach(card=>",
		".focus-rail .media.is-rail-active .featured-poster-frame",
		".featured-rail.focus-rail .featured-poster-card",
		"transform: none",
		"mid=Number(id(x));if(mid>0)return mt+':id:'+mid",
		"return mt+':fallback:'+title(x).trim().toLowerCase()+':'+year(x)+':'+artwork",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("精选横滑栏缺少焦点兼容或无 ID 身份契约 %q", want)
		}
	}
	if strings.Contains(html, "querySelectorAll(':scope > .media')") {
		t.Fatal("横滑栏焦点仍依赖旧 WebView 兼容性较差的 :scope 查询")
	}
}

func TestChromeUsesContextualRootAppBarWithoutWebsiteBranding(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	chromeStart := strings.Index(html, "function chrome(")
	chromeEnd := strings.Index(html, "function render(){")
	if chromeStart < 0 || chromeEnd <= chromeStart {
		t.Fatal("未找到 chrome 渲染边界")
	}
	chrome := html[chromeStart:chromeEnd]

	for _, want := range []string{
		"isRoot=!S.detailVisible&&ROOT_VIEWS.includes(S.view)",
		"home:['发现','今晚看什么']",
		"search:['搜索','片库就绪']",
		"me:['我的','你好，'+n]",
		`<header class="app-bar glass-surface">`,
		`<div class="app-context">`,
		`class="avatar" aria-label="打开我的"`,
		"const appBar=isRoot?",
	} {
		if !strings.Contains(chrome, want) {
			t.Fatalf("根页面缺少上下文 App Bar 契约 %q", want)
		}
	}

	for _, stale := range []string{
		`class="brand"`,
		`class="mark"`,
		"compact-top-bar",
		"云海影视",
	} {
		if strings.Contains(chrome, stale) {
			t.Fatalf("全局头部仍包含网站式品牌元素 %q", stale)
		}
	}
	for _, stale := range []string{".brand {", ".mark {", "icons={mark:"} {
		if strings.Contains(html, stale) {
			t.Fatalf("页面仍保留已废弃的头部品牌契约 %q", stale)
		}
	}
}

func TestRootMotionIsOneShotNavigationOnlyAndReducedMotionSafe(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		"rootMotion:''",
		"function queueRootMotion(next)",
		"from===next){S.rootMotion=''",
		"ROOT_VIEWS.indexOf(next)>ROOT_VIEWS.indexOf(from)?'forward':'backward'",
		"const motion=S.rootMotion;S.rootMotion=''",
		"root-enter-forward",
		"root-enter-backward",
		"@keyframes row-in",
		"@keyframes detail-in",
		`class="detail detail-enter"`,
		":nth-child(-n + 6)",
		".search:focus-within .search-icon",
		"transform: scale(.98)",
		".tab-transition .nav.active i",
		"animation: none !important",
		"transition: none !important",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少一次性原生动效契约 %q", want)
		}
	}
	for _, stale := range []string{
		"animation: surface-in",
		"animation: shine",
		"IntersectionObserver",
	} {
		if strings.Contains(html, stale) {
			t.Fatalf("Mini App 仍包含异步重放或滚动触发动效 %q", stale)
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
