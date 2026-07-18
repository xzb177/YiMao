package handlers

import (
	"os"
	"strings"
	"testing"
)

func TestBaseUserCopyRegression(t *testing.T) {
	tests := []struct {
		file   string
		want   []string
		forbid []string
	}{
		{
			file:   "request.go",
			want:   []string{"《%s》", "/link 用户名 密码", "📊 求片进度", "今日求片次数已用完"},
			forbid: []string{"《%s}", "服务配置错误", "无效的 TMDB ID", "配额操作失败"},
		},
		{
			file:   "link.go",
			want:   []string{"/link 用户名 密码", "密码重置失败，请稍后再试或联系管理员"},
			forbid: []string{"/link 用户名\n", `Text: "❌ 密码重置失败：" + err.Error()`},
		},
		{
			file:   "callback.go",
			want:   []string{"📺 求整季", "求第%d季", "🏠 主菜单", "/link 用户名 密码"},
			forbid: []string{"✅ 订阅全季", "⬅️ 返回主菜单", "🆔 TMDB ID:", `Text:        "❌ 画像生成失败：" + err.Error()`},
		},
		{
			file:   "search.go",
			want:   []string{"也可以点「许愿」：片源以后出现时，会继续帮你留意。", "✨ 许愿", "🏠 主菜单"},
			forbid: []string{"该条目缺少 TMDB ID", "⬅️ 返回主菜单"},
		},
		{
			file:   "resource.go",
			want:   []string{"暂时没找到可用片源", "🏠 主菜单"},
			forbid: []string{"text.WriteString(fmt.Sprintf(\"🔑 搜索词", "text.WriteString(fmt.Sprintf(\"🌐 已搜索", "站点 RSS 需要认证", "subscribeResult := fmt.Sprintf(\"ℹ️ 手动选源已下线"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			b, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			for _, want := range tt.want {
				if !strings.Contains(s, want) {
					t.Errorf("missing required copy %q", want)
				}
			}
			for _, forbidden := range tt.forbid {
				if strings.Contains(s, forbidden) {
					t.Errorf("contains forbidden user copy %q", forbidden)
				}
			}
		})
	}
}
