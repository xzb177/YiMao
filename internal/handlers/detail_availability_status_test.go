package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

func TestBuildDetailFromSearchPreservesAuthoritativeAvailabilityStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	manager := session.NewManager(time.Hour, 10)
	handler := NewDetailHandler(manager, nil, services.NewMoviePilotClient(server.URL, "", ""), nil)

	for _, tc := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "confirmed available", status: "已在库", want: "已在库"},
		{name: "active subscription", status: "下载中", want: "下载中"},
		{name: "confirmed requestable", status: "可求片", want: "可求片"},
		{name: "lookup failure stays unknown", status: "状态暂未确认", want: "状态暂未确认"},
		{name: "empty stays unknown", status: "", want: "状态暂未确认"},
		{name: "untrusted status stays unknown", status: "<b>已在库</b>", want: "状态暂未确认"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := manager.GetOrCreate(1001)
			resp := handler.buildDetailFromSearch(session.SearchItem{
				ID:       "4048",
				Title:    "状态语义测试",
				Year:     2026,
				Type:     "movie",
				Overview: "测试详情状态不会在 MoviePilot 元数据查询失败后被改写。",
				Status:   tc.status,
			}, "movie", sess)
			if resp == nil {
				t.Fatal("detail response is nil")
			}
			if !strings.Contains(resp.RichMessage, tc.want) {
				t.Fatalf("detail lost status %q: %s", tc.status, resp.RichMessage)
			}
			if tc.status == "状态暂未确认" && strings.Contains(resp.RichMessage, "已在库") {
				t.Fatalf("unknown status was overstated as available: %s", resp.RichMessage)
			}
		})
	}
}

func TestEphemeralMediaCaptionEscapesStatus(t *testing.T) {
	caption := buildEphemeralMediaCaption(richmessage.MediaInfo{
		Title:     "测试",
		MediaType: "movie",
		Status:    "<b>伪状态</b>",
	})
	if strings.Contains(caption, "<b>伪状态</b>") || !strings.Contains(caption, "&lt;b&gt;伪状态&lt;/b&gt;") {
		t.Fatalf("status was not escaped: %s", caption)
	}
}
