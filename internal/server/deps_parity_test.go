package server

import (
	"reflect"
	"testing"

	botHandlers "github.com/xzb177/yimao/internal/bot"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"time"
)

// 审计第 8 条：webhook 模式的依赖注入曾经只映射了 10 个字段，漏了
// AdminHandler、MyRequests、TMDB、SearchHistoryDB（以及 SearchHistory），
// 导致 webhook 模式下管理员输入流程失效、回复被当搜索词、/requests
// 一直回「服务未就绪」。toBotDeps 必须与 bot.Dependencies 的全部字段对齐。
func TestToBotDepsCoversAllFields(t *testing.T) {
	sessMgr := session.NewManager(time.Hour, 10)
	deps := &Dependencies{
		Telegram:        &services.TelegramClient{},
		MoviePilot:      &services.MoviePilotClient{},
		SessionMgr:      sessMgr,
		AdminService:    services.NewAdminService(t.TempDir()),
		BindingRequest:  &services.BindingRequestService{},
		AdminHandler:    &handlers.AdminHandler{},
		QuotaService:    &services.QuotaService{},
		FeedbackHandler: &handlers.FeedbackHandler{},
		WishHandler:     &handlers.WishHandler{},
		MyRequests:      &handlers.MyRequestsHandler{},
		TMDB:            &services.TMDBClient{},
		SearchHistory:   &services.SearchHistoryService{},
		SearchHistoryDB: &services.SearchHistoryDB{},
		IssueService:    &services.IssueService{},
	}

	got := toBotDeps(deps)

	// 每一个目标字段都必须被映射（非零值），且与源字段是同一个指针。
	v := reflect.ValueOf(got).Elem()
	typ := v.Type()
	want := map[string]reflect.Value{
		"Telegram":        reflect.ValueOf(deps.Telegram),
		"MoviePilot":      reflect.ValueOf(deps.MoviePilot),
		"SessionMgr":      reflect.ValueOf(deps.SessionMgr),
		"BindingRequest":  reflect.ValueOf(deps.BindingRequest),
		"AdminService":    reflect.ValueOf(deps.AdminService),
		"AdminHandler":    reflect.ValueOf(deps.AdminHandler),
		"QuotaService":    reflect.ValueOf(deps.QuotaService),
		"SearchHistory":   reflect.ValueOf(deps.SearchHistory),
		"SearchHistoryDB": reflect.ValueOf(deps.SearchHistoryDB),
		"TMDB":            reflect.ValueOf(deps.TMDB),
		"IssueService":    reflect.ValueOf(deps.IssueService),
		"FeedbackHandler": reflect.ValueOf(deps.FeedbackHandler),
		"WishHandler":     reflect.ValueOf(deps.WishHandler),
		"MyRequests":      reflect.ValueOf(deps.MyRequests),
	}
	_ = botHandlers.Dependencies{} // 编译期确认引用的结构体存在
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if name == "UserMapping" {
			// 接口类型：只断言非空（本测试未注入实现）。
			continue
		}
		if name == "FallbackService" {
			// 由 toBotDeps 按轮询模式的惯例自行构造（poll.go 同样做法）。
			if v.Field(i).IsNil() {
				t.Fatalf("FallbackService must be constructed by toBotDeps, got nil")
			}
			continue
		}
		src, ok := want[name]
		if !ok {
			t.Fatalf("bot.Dependencies has field %s not covered by toBotDeps test map; update toBotDeps and this test", name)
		}
		if v.Field(i).IsNil() {
			t.Fatalf("toBotDeps left %s nil", name)
		}
		if v.Field(i).Pointer() != src.Pointer() {
			t.Fatalf("toBotDeps mapped %s to the wrong instance", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Fatalf("test map contains %s but bot.Dependencies has no such field", name)
	}
}
