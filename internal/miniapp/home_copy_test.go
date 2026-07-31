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
