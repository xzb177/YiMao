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

func TestMiniAppHomeMatchesCinemaA(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html, `class="cinema-card" id="welcome"`, `class="cinema-hero"`, `class="cinema-credit">YUNHAI · CINEMA`, `<h1>云海求片</h1>`, `class="cinema-tag">想看的，交给云海`, `直接发片名，或点搜索。提交后可在进度里查到。`, `class="cinema-status">在线 · 可求片`, `--cinema-success:#34c759`, `--cinema-primary:#1c3d73`, `{text:"搜索求片",style:"success"`, `{text:"查看进度"`, `{text:"帮助说明"`, `{text:"更多功能"`)
	rejectSource(t, html, "云海求片助手", "首次加载约需 2-3 分钟", "Today badge", "今天 badge")
}

func TestMiniAppProgressMatchesPlaybillB(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html, `class="cinema-kicker">NOW PLAYING`, `function taskPlaybill(item)`, `class="cinema-tag">${esc(status)}`, `Emby 确认可看后会再通知你。`, `function taskFacts(item)`, `<tr><td>年份</td>`, `<tr><td>类型</td>`, `<tr><td>下一步</td>`, `{text:"返回首页"`, `{text:"刷新状态"`, `/api/miniapp/v1/me`, `/api/miniapp/v1/progress?request_id=`)
}

func TestMiniAppStartAppRoutes(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html, `function startRoute()`, `q.get("tgWebAppStartParam")||q.get("start_param")`, `raw==="tasks"||raw==="progress"`, `raw==="search"`, `raw.match(/`, `approvedStartRoute.view==="detail"`, `openDetail(approvedStartRoute.id,approvedStartRoute.type,approvedStartRoute.season)`)
}

func TestMiniAppKeepsAuthenticatedBusinessAPIs(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html, `await telegramReady`, `tg=telegramWebApp()||tg`, `X-Telegram-Init-Data`, `/api/miniapp/v1/search`, `/api/miniapp/v1/detail`, `/api/miniapp/v1/request`, `/api/miniapp/v1/wash`, `/api/miniapp/v1/watchlist`, `/api/miniapp/v1/wishes`, `/api/miniapp/v1/issues`)
}

func TestMiniAppUsesTMDBLandscapeHeroWithoutPerRenderFetch(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html, `function miniBackdrop(item)`, `https://image.tmdb.org/t/p/w1280`, `api("/api/miniapp/v1/discover")`, `(S.homeDynamic?.featured||[]).find(x=>miniBackdrop(x))`)
}
