package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 审计第 7 条：观影画像按用户名模糊匹配 Emby 用户 —— 注册用户名 "zhang"
// 就能看到 "zhangsan" 的观看记录。本测试要求只做精确匹配（大小写不敏感）。
func TestFindEmbyUserByNameExactMatchOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"Id":"u-zhangsan","Name":"zhangsan"},{"Id":"u-li","Name":"li"}]`)
	}))
	defer server.Close()

	svc := NewPortraitService(server.URL, "test-key")

	// "zhang" 只是 "zhangsan" 的前缀：必须找不到，绝不能返回 zhangsan 的 ID。
	if id, err := svc.FindEmbyUserByName("zhang"); err == nil {
		t.Fatalf("expected no match for %q, but fuzzy match returned id=%q", "zhang", id)
	}

	// 反向包含也不能命中：Emby 用户名是 "li" 的子串场景。
	if id, err := svc.FindEmbyUserByName("zhangsanli"); err == nil {
		t.Fatalf("expected no match for %q, but fuzzy match returned id=%q", "zhangsanli", id)
	}

	// 精确匹配（大小写不敏感）仍然有效。
	id, err := svc.FindEmbyUserByName("zhangsan")
	if err != nil || id != "u-zhangsan" {
		t.Fatalf("exact match failed: id=%q err=%v", id, err)
	}
	id, err = svc.FindEmbyUserByName("ZhangSan")
	if err != nil || id != "u-zhangsan" {
		t.Fatalf("case-insensitive exact match failed: id=%q err=%v", id, err)
	}
}
