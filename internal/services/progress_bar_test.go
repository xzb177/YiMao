package services

import (
	"strings"
	"testing"
)

// TestRenderProgressBar 覆盖 total=0 / current>total / current<=0 等边界。
func TestRenderProgressBar(t *testing.T) {
	t.Run("total=0 不渲染进度条只显示已更到", func(t *testing.T) {
		got := renderProgressBar(7, 0)
		want := "📈 已更到 E07"
		if got != want {
			t.Errorf("renderProgressBar(7,0) = %q, want %q", got, want)
		}
	})

	t.Run("total为负 同样按未知处理", func(t *testing.T) {
		got := renderProgressBar(7, -5)
		want := "📈 已更到 E07"
		if got != want {
			t.Errorf("renderProgressBar(7,-5) = %q, want %q", got, want)
		}
	})

	t.Run("current<=0 返回空串", func(t *testing.T) {
		if got := renderProgressBar(0, 16); got != "" {
			t.Errorf("renderProgressBar(0,16) = %q, want empty", got)
		}
		if got := renderProgressBar(-1, 16); got != "" {
			t.Errorf("renderProgressBar(-1,16) = %q, want empty", got)
		}
	})

	t.Run("正常进度 含距完结集数", func(t *testing.T) {
		got := renderProgressBar(7, 16)
		// 7/16 = 43.75% -> 整数百分比 43；距完结 9 集
		if !strings.Contains(got, "已更 E07/E16") {
			t.Errorf("缺少集数信息: %q", got)
		}
		if !strings.Contains(got, "43%") {
			t.Errorf("百分比错误: %q", got)
		}
		if !strings.Contains(got, "距完结还差 9 集") {
			t.Errorf("缺少距完结提示: %q", got)
		}
	})

	t.Run("current>total 钳制为100%已完结", func(t *testing.T) {
		got := renderProgressBar(20, 16)
		if !strings.Contains(got, "已更 E16/E16") {
			t.Errorf("未钳制集数: %q", got)
		}
		if !strings.Contains(got, "100%") {
			t.Errorf("百分比未达100%%: %q", got)
		}
		if !strings.Contains(got, "已完结") {
			t.Errorf("缺少已完结提示: %q", got)
		}
		// 满格不应包含空格字符
		if strings.Contains(got, "░") {
			t.Errorf("100%% 不应有空格块: %q", got)
		}
	})

	t.Run("current=total 恰好完结", func(t *testing.T) {
		got := renderProgressBar(16, 16)
		if !strings.Contains(got, "100%") || !strings.Contains(got, "已完结") {
			t.Errorf("current=total 应为100%%已完结: %q", got)
		}
	})

	t.Run("进度条宽度恒定", func(t *testing.T) {
		got := renderProgressBar(1, 100)
		filled := strings.Count(got, "▓")
		empty := strings.Count(got, "░")
		if filled+empty != progressBarWidth {
			t.Errorf("进度条总宽度 = %d, want %d (%q)", filled+empty, progressBarWidth, got)
		}
		// 极小进度也至少填充 1 格
		if filled < 1 {
			t.Errorf("应至少填充1格: %q", got)
		}
	})
}
