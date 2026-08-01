package miniapp

import (
	"os"
	"strings"
	"testing"
)

// TestHomeHeroCopyKeepsTheFirstScreenDirect 验证首页首屏保持简洁、可执行的产品表达。
func TestHomeHeroCopyKeepsTheFirstScreenDirect(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}

	html := string(page)
	for _, want := range []string{"今晚看什么？", "先逛逛热门，也可以直接搜片名。"} {
		if !strings.Contains(html, want) {
			t.Fatalf("首页缺少首屏文案 %q", want)
		}
	}
	for _, stale := range []string{"想看的，<br>交给云海。", "找到今晚真正想看的故事"} {
		if strings.Contains(html, stale) {
			t.Fatalf("首页仍包含旧口号 %q", stale)
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
