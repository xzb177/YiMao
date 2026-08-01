package miniapp

import (
	"os"
	"strings"
	"testing"
)

// TestHomeStartsWithArtworkAndFeaturedTitle 验证首页首屏由真实内容主导，而不是说明性开场。
func TestHomeStartsWithArtworkAndFeaturedTitle(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}

	html := string(page)
	for _, want := range []string{
		"lead.backdrop_path||lead.poster_path",
		`<section class="home-feature">`,
		"本周热看",
		"查看详情",
		`class="search home-search"`,
		"搜电影、剧集或演员",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("首页缺少内容主导的首屏契约 %q", want)
		}
	}
	for _, stale := range []string{
		"今晚看什么？",
		"先逛逛热门，也可以直接搜片名。",
		"想看的，<br>交给云海。",
		"找到今晚真正想看的故事",
	} {
		if strings.Contains(html, stale) {
			t.Fatalf("首页仍包含旧口号 %q", stale)
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
	if artwork, search := strings.Index(home, "home-feature"), strings.Index(home, "home-search"); artwork < 0 || search < 0 || artwork > search {
		t.Fatal("首页没有把主打作品放在搜索框之前")
	}
}

func TestMiniAppUsesSingleCinemaPaletteAndMotionFallback(t *testing.T) {
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
		"function loadMore(){if(!S.loading&&S.hasMore)doSearch(true,S.nextPage)",
		"S.page=Number(d.page)||targetPage",
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
		"env(safe-area-inset-top,0px)",
		"env(safe-area-inset-bottom,0px)",
		"--safe:var(--safe-bottom)",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Mini App 缺少安全区变量 %q", want)
		}
	}
}
