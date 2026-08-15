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
			t.Errorf("Mini App 信息顺序错误：%q 必须出现在前一项之后", value)
			return
		}
		last = next
	}
}

func TestV11HomeFollowsTheWatchToPlaybackPath(t *testing.T) {
	html := miniAppSource(t)
	if count := strings.Count(html, "私人影视任务中心"); count != 2 {
		t.Errorf("文档标题与首页副标题应各保留一处，实际为 %d 处", count)
	}
	requireSource(t, html,
		"function homeWorkbench()",
		"function homeTonightSection()",
		"function homeBlockerSection()",
		"function homeActiveSection()",
		"今晚要看",
		"卡住的事",
		"正在替你办",
		"快捷任务",
		`class="home-section home-shortcuts"`,
		`/api/miniapp/v1/dynamic`,
		"recently_added",
	)
	requireOrder(t, html,
		`${homeTonightSection()}`,
		`${homeBlockerSection()}`,
		`${homeActiveSection()}`,
		`class="home-section home-shortcuts"`,
	)
}

func TestHomeIsACinematicActionDeskWithRealTaskState(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`class="action-home"`,
		`class="home-context"`,
		`class="context-line"`,
		`class="profile-trigger"`,
		"今晚看点什么",
		"把想看的，<br>一次搞定。",
		"你的私人影视任务中心",
		`class="home-search"`,
		"搜片名、演员或关键词",
		`class="home-workbench"`,
		`class="home-section home-shortcuts"`,
		`class="task-entry-grid"`,
		`data-action="request"`,
		`data-action="wash"`,
		"求片",
		"洗版",
		`class="status-beacon `,
		"home-active",
		"正在替你办",
	)
	requireOrder(t, html,
		`class="home-context"`,
		`class="home-search"`,
		`class="home-workbench"`,
		`class="task-entry-grid"`,
	)
	rejectSource(t, html,
		"YiMao 放映室",
		"home-studio",
		"studio-console",
		"screening-flow",
		"screening-scene",
		"film-thread",
		"search-workspace",
		"archive-workspace",
		"pointermove",
		"CINEMA / HUB",
		"product-kicker",
	)
}

func TestCinematicSkinIsRestrainedMotionSafeAndTaskFirst(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"--glass:",
		"--glass-strong:",
		"--soft-shadow:",
		"backdrop-filter:blur(",
		"animation:statusPulse",
		"@keyframes statusPulse",
		"transform:translateY(1px)",
		"function taskAccessibleLabel(item)",
		`aria-label="${esc(taskAccessibleLabel(item))}"`,
		"查看全部",
	)
	rejectSource(t, html,
		"CINEMA / HUB",
		"product-kicker",
		`aria-label="打开任务"`,
	)
}

func TestHomeDistinguishesLoadFailuresFromEmptyState(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"function homeLoadError(message)",
		"媒体库暂时没加载成功",
		"任务暂时没加载成功",
		`onclick="loadHome()"`,
		">重试</button>",
		"S.homeDynamic?.error",
		"S.tasks?.error",
		"if(S.tasks?.error)S.tasks={...S.tasks,error:''}",
		"if(S.homeDynamic?.error)S.homeDynamic={...S.homeDynamic,error:''}",
	)
}

func TestRootNavigationHasExactlyThreeProductPaths(t *testing.T) {
	html := miniAppSource(t)
	start := strings.Index(html, "function chrome(")
	end := strings.Index(html, "function render(")
	if start < 0 || end <= start {
		t.Fatal("未找到主框架渲染边界")
	}
	chrome := html[start:end]
	requireSource(t, chrome,
		`class="app-dock glass-dock"`,
		`aria-label="主导航"`,
		`aria-current="page"`,
		"找片",
		"任务",
		"监控",
		`data-view="monitor"`,
	)
	rejectSource(t, chrome, "放映室", "我的", "闯关", `class="brand"`)
}

func TestSearchIsGroupedAndStatusDriven(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"function groupedSearchResults(items)",
		"function searchResultSection(kind,items)",
		`class="result-lead"`,
		`class="result-row"`,
		"电影",
		"剧集",
		"original_title",
		"genres",
		"media_status",
		"x?.status?.code||'unknown'",
		"function detailPrimaryAction(x)",
		"求这部",
		"选择季",
		"洗版",
		"查看进度",
		"已在库中",
		`/api/miniapp/v1/detail?id=`,
	)
	rejectSource(t, html, "预计完成时间", "码率", "4K 优先")
}

func TestWashIsAFirstClassSearchMode(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"mode:'request'",
		"function startSearchMode(mode)",
		"['request','wash'].includes(mode)",
		"已有内容换个更好的版本",
		"新版本确认可用前，旧版会保留",
		"const season=x.type==='tv'?S.season:0",
		"if(x.type==='tv'&&season<=0)",
		`/api/miniapp/v1/wash`,
		"body:JSON.stringify(payload)",
	)
}

func TestMonitorUsesSafeNativeOverviewPage(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`/api/miniapp/v1/monitor`,
		"function monitorPage()",
		"async function loadMonitor()",
		"总体状态",
		"剩余空间",
		"下载队列",
		"24h 入库活动",
		"下载",
		"整理",
		"媒体库",
		"刷新",
		"monitorStateLabel",
		"monitorError",
		"textContent",
	)
	rejectSource(t, html, "quest-panel", "quest-choice")
}

func TestTasksAggregateRequestAndWashByNextAction(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"function taskGroup(item)",
		"function groupedTasks(items)",
		"需要处理",
		"进行中",
		"已完成",
		"没有找到",
		"business_type",
		"request",
		"wash",
		"function taskNext(item)",
		"updated_at||item.created_at",
		`/api/miniapp/v1/me`,
		`/api/miniapp/v1/progress?request_id=`,
	)
}

func TestAuxiliaryFeaturesRemainAvailableButSecondary(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`/api/miniapp/v1/dynamic`,
		`/api/miniapp/v1/discover`,
		`/api/miniapp/v1/assistant`,
		`/api/miniapp/v1/watchlist`,
		`/api/miniapp/v1/wishes`,
		`/api/miniapp/v1/issues`,
		`class="secondary-tools"`,
	)
}

func TestDynamicMediaIsEscapedValidatedAndNotPutInJSContexts(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"function esc(value)",
		"const mid=Number(",
		"Number.isFinite(mid)",
		"mid<=0",
		"['movie','tv'].includes(mt)",
		"data-mid=",
		"data-type=",
		"openMediaFromElement(this)",
	)
	rejectSource(t, html,
		"onclick=\"openDetail(${item.",
		"onclick=\"openDetail(${x.",
		"onclick=\"openDetail(${r.",
		"onclick=\"openDetail(${w.",
	)
}

func TestTelegramSDKCannotBlockFirstVisiblePaint(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`<main id="app" class="shell"><section class="boot-fallback"`,
		`<script async src="/miniapp/telegram-web-app.js" onload="initTelegramWebApp()" onerror="resolveTelegramReady?.()"></script>`,
		"function telegramWebApp()",
	)
	rejectSource(t, html,
		`<main id="app" class="shell"></main>`,
		`<script src="/miniapp/telegram-web-app.js"></script>`,
		`https://telegram.org/js/telegram-web-app.js`,
	)
}

func TestNetworkRacesErrorsAndTelegramContractsRemain(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"tg?.initData",
		"'X-Telegram-Init-Data':tg?.initData||''",
		"let searchController=null",
		"searchController?.abort()",
		"const seq=++S.searchSeq",
		"seq!==S.searchSeq",
		"S.query!==current.query||S.searchType!==current.type",
		"S.searchError=e.message",
		`class="persistent-error"`,
		"const seq=++S.detailSeq",
		"if(seq!==S.detailSeq||!S.detailVisible)return",
		"const seq=++S.meSeq",
		"if(seq!==S.meSeq||S.view!=='tasks')return",
		"tg?.BackButton?.show()",
		"tg?.HapticFeedback",
		"--tg-safe-area-inset-top",
		"--tg-content-safe-area-inset-top",
		"env(safe-area-inset-top, 0px)",
		"env(safe-area-inset-bottom, 0px)",
	)
}

func TestV11SubmissionUsesPersistentResultInsteadOfToastOnly(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"function showSubmissionResult(result,businessType)",
		"submissionResultTitle(status,businessType)",
		"任务已提交",
		"当前状态",
		"查看任务",
		"继续找片",
		"showSubmissionResult(result,'request')",
		"showSubmissionResult(result,'wash')",
		"result.request_id",
	)
}

func TestRequestSubmissionKeepsAPIErrorsPublic(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"constructor(message,status=0,body=null)",
		"this.body=body",
		"throw new APIError(message,response.status,body)",
		"typeof body?.message==='string'?body.message",
		"status==='created'?'success'",
		"status.startsWith('duplicate_')?'warning':'error'",
	)
}

func TestRequestSubmissionFailureAlwaysHasPersistentRecoverableResult(t *testing.T) {
	html := miniAppSource(t)
	requestStart := strings.Index(html, "async function submitRequest(button)")
	washStart := strings.Index(html, "async function submitWash(button)")
	resultStart := strings.Index(html, "function showSubmissionResult(result,businessType)")
	resultEnd := strings.Index(html, "function viewSubmittedTasks()")
	if requestStart < 0 || washStart <= requestStart || resultStart < 0 || resultEnd <= resultStart {
		t.Fatal("未找到求片提交或结果对话框边界")
	}

	request := html[requestStart:washStart]
	requireSource(t, request,
		"if(error?.name==='AbortError'||!isCurrentDialog(ctx.seq))return",
		"const failure=requestFailureResult(error)",
		"showSubmissionResult(failure,'request')",
	)
	rejectSource(t, request,
		"toast(",
		"showSubmissionResult(error.body,'request')",
	)

	requireSource(t, html,
		"function requestFailureResult(error)",
		"['not_bound','quota_exceeded','in_library','upcoming','unknown','failed'].includes(bodyStatus)",
		"status:Number(error?.status)===401?'failed':knownStatus",
		"Number(error?.status)===401?'身份验证已失效，请重新打开 Mini App'",
		"retryable:Number(error?.status)!==401",
	)

	result := html[resultStart:resultEnd]
	requireSource(t, result,
		"const message=typeof result.message==='string'?result.message:''",
		"wash?String(result.request_id??''):typeof result.request_id==='string'?result.request_id:''",
		"['created','duplicate_own'].includes(status)",
		"/^[A-Za-z0-9_-]{1,128}$/.test(rawRequestID)",
		`class="submission-identity${status==='failed'?' submission-failed':''}"`,
		"${message?`<p class=\"submission-message\">${esc(message)}</p>`:''}",
		"!wash&&status==='failed'&&result.retryable!==false?'<button class=\"primary\" onclick=\"openRequestConfirm()\">重试</button>':''",
		"<button class=\"secondary\" onclick=\"continueFinding()\">继续找片</button>",
	)
	rejectSource(t, result,
		"const requestID=String(result.request_id??'')",
	)

	requireSource(t, html,
		"if(status==='failed')return label+'提交失败'",
		".submission-identity.submission-failed{border-color:var(--bad);background:rgba(255,129,117,.08)}",
		"async function submitWash(button)",
		"toast(error.message||'洗版失败')",
	)
}

func TestSearchCanContinueWhenFilteredPageIsEmpty(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`${S.hasMore?'<button class="secondary" onclick="loadMoreSearch()">加载更多</button>':''}`,
	)
	if strings.Contains(html, `S.results.length&&S.hasMore`) {
		t.Fatal("empty filtered page hides the load-more action")
	}
}

func TestDialogTimelineAndMutationContractsRemain(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		`role="dialog" aria-modal="true" aria-labelledby="dialog-title"`,
		"D.controller=new AbortController()",
		"if(!isCurrentDialog(ctx.seq))return",
		"if(event.key==='Escape')",
		"if(event.key!=='Tab')return",
		"restore?.isConnected",
		"setDialogBusy(ctx.seq,true)",
		"Array.isArray(d.events)?d.events:[]",
		`class="timeline-node timeline-${nodeTone}`,
		`aria-label="进度节点"`,
		`/api/miniapp/v1/request`,
		`/api/miniapp/v1/request/cancel`,
	)
}

func TestDemoIsReadOnlyAndCoversAllPrimaryPaths(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"const DEMO=new URLSearchParams(location.search).get('demo')==='1'",
		"function demoResponse(path,options={})",
		"if(DEMO&&method!=='GET')",
		"request_id:'demo-request'",
		"request_id:'demo-wash'",
		"business_type:'request'",
		"business_type:'wash'",
		"media_status",
		"monitorDemo",
	)
}

func TestLayoutIsMobileFirstDenseAndMotionSafe(t *testing.T) {
	html := miniAppSource(t)
	requireSource(t, html,
		"overflow-x: clip",
		"min-width: 0",
		"min-height: 44px",
		"--dock-clearance:",
		"padding-bottom: var(--dock-clearance)",
		"@media (max-width: 350px)",
		"@media (min-width: 900px)",
		"@media (prefers-reduced-motion: reduce)",
		"animation: none !important",
		"transition: none !important",
	)
}
