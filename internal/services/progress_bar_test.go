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
		// 7/16 = 43.75% -> 四舍五入百分比 44（与 filled 同源舍入，口径统一）；距完结 9 集
		if !strings.Contains(got, "已更 E07/E16") {
			t.Errorf("缺少集数信息: %q", got)
		}
		if !strings.Contains(got, "44%") {
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

// TestRenderSeriesProgressBar 覆盖全剧口径单行渲染的边界。
func TestRenderSeriesProgressBar(t *testing.T) {
	t.Run("正常 52/100=52% 不带距完结", func(t *testing.T) {
		got := renderSeriesProgressBar(52, 100)
		if !strings.Contains(got, "全剧：E52/E100") {
			t.Errorf("全剧集数信息错误: %q", got)
		}
		if !strings.Contains(got, "52%") {
			t.Errorf("应为 52%%: %q", got)
		}
		if strings.Contains(got, "距完结") {
			t.Errorf("全剧口径不应带距完结: %q", got)
		}
	})
	t.Run("total<=0 只显示累计", func(t *testing.T) {
		got := renderSeriesProgressBar(52, 0)
		if got != "全剧：已更到 E52" {
			t.Errorf("无全剧总数应只报累计: %q", got)
		}
		if strings.Contains(got, "%") {
			t.Errorf("无总数不应有百分比: %q", got)
		}
	})
	t.Run("current<=0 返回空", func(t *testing.T) {
		if got := renderSeriesProgressBar(0, 100); got != "" {
			t.Errorf("current=0 应空: %q", got)
		}
		if got := renderSeriesProgressBar(-5, 100); got != "" {
			t.Errorf("current<0 应空: %q", got)
		}
	})
	t.Run("current>total 钳制100%", func(t *testing.T) {
		got := renderSeriesProgressBar(120, 100)
		if !strings.Contains(got, "全剧：E100/E100") || !strings.Contains(got, "100%") {
			t.Errorf("应钳制到 100%%: %q", got)
		}
		if strings.Contains(got, "░") {
			t.Errorf("100%% 不应有空格块: %q", got)
		}
	})
}

// TestRenderDualProgressBar 覆盖需求 #4 双口径的四种组合边界：双有/只本季/只全剧/都无。
func TestRenderDualProgressBar(t *testing.T) {
	t.Run("双口径都有 渲染两行", func(t *testing.T) {
		got := renderDualProgressBar(3, 12, 16, 52, 100)
		lines := strings.Split(got, "\n")
		if len(lines) != 2 {
			t.Fatalf("应为两行, got %d 行: %q", len(lines), got)
		}
		if !strings.Contains(lines[0], "S03E12/S03E16") || !strings.Contains(lines[0], "75%") {
			t.Errorf("第一行应为本季 75%%: %q", lines[0])
		}
		if !strings.Contains(lines[1], "全剧：E52/E100") || !strings.Contains(lines[1], "52%") {
			t.Errorf("第二行应为全剧 52%%: %q", lines[1])
		}
	})

	t.Run("只有本季总数 拿不到全剧 退化为单行本季", func(t *testing.T) {
		got := renderDualProgressBar(3, 12, 16, 0, 0)
		if strings.Contains(got, "\n") {
			t.Errorf("应为单行: %q", got)
		}
		if !strings.Contains(got, "S03E12/S03E16") || !strings.Contains(got, "75%") {
			t.Errorf("应仅本季口径: %q", got)
		}
		if strings.Contains(got, "全剧") {
			t.Errorf("不应出现全剧行: %q", got)
		}
	})

	t.Run("拿不到本季集数但有全剧 退化为本季已更到+全剧两行", func(t *testing.T) {
		// 本季 total=0 → 本季行为「已更到 S03E12」（无百分比）；全剧仍可渲染。
		got := renderDualProgressBar(3, 12, 0, 52, 100)
		lines := strings.Split(got, "\n")
		if len(lines) != 2 {
			t.Fatalf("应为两行(本季已更到 + 全剧), got %q", got)
		}
		if lines[0] != "📈 已更到 S03E12" {
			t.Errorf("本季行应为已更到无百分比: %q", lines[0])
		}
		if !strings.Contains(lines[1], "全剧：E52/E100") {
			t.Errorf("全剧行缺失: %q", lines[1])
		}
	})

	t.Run("两个口径都拿不到 单行已更到", func(t *testing.T) {
		got := renderDualProgressBar(3, 12, 0, 0, 0)
		if got != "📈 已更到 S03E12" {
			t.Errorf("都无应单行已更到: %q", got)
		}
	})

	t.Run("本季集号无效 但全剧有效 只显全剧行", func(t *testing.T) {
		got := renderDualProgressBar(3, 0, 16, 52, 100)
		if strings.Contains(got, "\n") {
			t.Errorf("本季 current<=0 本季行应为空, 只剩全剧单行: %q", got)
		}
		if !strings.Contains(got, "全剧：E52/E100") {
			t.Errorf("应只显全剧行: %q", got)
		}
	})

	t.Run("全部无效 返回空串", func(t *testing.T) {
		if got := renderDualProgressBar(3, 0, 16, 0, 100); got != "" {
			t.Errorf("本季 current<=0 且全剧 current<=0 应空: %q", got)
		}
	})

	t.Run("season<=0 本季行退化为不带季号 全剧行正常", func(t *testing.T) {
		got := renderDualProgressBar(0, 7, 16, 52, 100)
		lines := strings.Split(got, "\n")
		if len(lines) != 2 {
			t.Fatalf("应两行: %q", got)
		}
		if !strings.Contains(lines[0], "已更 E07/E16") {
			t.Errorf("本季行应不带季号: %q", lines[0])
		}
	})
}

// 必须用「本季集数」当分母，绝不用全剧总集数。
func TestRenderSeasonProgressBar(t *testing.T) {
	t.Run("多季剧 用本季集数当分母 S3E12/S3E16=75%", func(t *testing.T) {
		// current=12（本季集号），季总集数=16 → 75%；
		// 全剧总集数 100 不应参与计算（绝不用 12/100=12%）。
		got := renderSeasonProgressBar(3, 12, 16)
		if !strings.Contains(got, "S03E12/S03E16") {
			t.Errorf("季相对集数信息错误: %q", got)
		}
		if !strings.Contains(got, "75%") {
			t.Errorf("应为本季 75%%（12/16），got %q", got)
		}
		if strings.Contains(got, "12%") {
			t.Errorf("绝不能用全剧总集数算出 12%%: %q", got)
		}
		if !strings.Contains(got, "距完结还差 4 集") {
			t.Errorf("本季距完结应差 4 集: %q", got)
		}
	})

	t.Run("拿不到本季集数 只显示已更到 不显示百分比", func(t *testing.T) {
		got := renderSeasonProgressBar(3, 12, 0)
		want := "📈 已更到 S03E12"
		if got != want {
			t.Errorf("renderSeasonProgressBar(3,12,0) = %q, want %q", got, want)
		}
		if strings.Contains(got, "%") {
			t.Errorf("无本季集数时绝不显示百分比: %q", got)
		}
		if strings.Contains(got, "距完结") {
			t.Errorf("无本季集数时绝不显示距完结: %q", got)
		}
	})

	t.Run("本季集数为负 同样按未知处理", func(t *testing.T) {
		got := renderSeasonProgressBar(2, 5, -3)
		want := "📈 已更到 S02E05"
		if got != want {
			t.Errorf("负 total 应按未知: got %q want %q", got, want)
		}
	})

	t.Run("current<=0 返回空", func(t *testing.T) {
		if got := renderSeasonProgressBar(1, 0, 16); got != "" {
			t.Errorf("current=0 应空: %q", got)
		}
		if got := renderSeasonProgressBar(1, -2, 16); got != "" {
			t.Errorf("current<0 应空: %q", got)
		}
	})

	t.Run("current>total 钳制为本季100%已完结", func(t *testing.T) {
		got := renderSeasonProgressBar(1, 20, 16)
		if !strings.Contains(got, "S01E16/S01E16") {
			t.Errorf("应钳制到本季末集: %q", got)
		}
		if !strings.Contains(got, "100%") || !strings.Contains(got, "已完结") {
			t.Errorf("应为本季 100%% 已完结: %q", got)
		}
		if strings.Contains(got, "░") {
			t.Errorf("100%% 不应有空格块: %q", got)
		}
	})

	t.Run("season<=0 退化为不带季号渲染", func(t *testing.T) {
		got := renderSeasonProgressBar(0, 7, 16)
		if !strings.Contains(got, "已更 E07/E16") {
			t.Errorf("season=0 应退化为不带季号: %q", got)
		}
	})

	t.Run("filled与percent口径同源 不打架", func(t *testing.T) {
		// 16/16 必须 100% 且满格；1/16 必须 <100% 且非满格。
		full := renderSeasonProgressBar(2, 16, 16)
		if !strings.Contains(full, "100%") || strings.Contains(full, "░") {
			t.Errorf("满进度口径不一致: %q", full)
		}
		low := renderSeasonProgressBar(2, 1, 16)
		if strings.Contains(low, "100%") {
			t.Errorf("低进度不应 100%%: %q", low)
		}
	})
}
