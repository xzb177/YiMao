package handlers

import "testing"

// TestEstimateDeliveryLamp 覆盖四档灯牌分支 + 阈值边界。
func TestEstimateDeliveryLamp(t *testing.T) {
	const th = etaThresholdHighDefault // 3

	cases := []struct {
		name          string
		seedingSites  int
		hasCandidate  bool
		thresholdHigh int
		want          string
	}{
		// ❓ 数据缺失 / 还没拿到候选
		{
			name:          "无候选数据 -> ❓ 还在找源中",
			seedingSites:  0,
			hasCandidate:  false,
			thresholdHigh: th,
			want:          "❓ 数据不足，系统还在找来源 → 稍等",
		},
		{
			name:          "数据缺失优先于 seedingSites（即便有数也忽略）",
			seedingSites:  10,
			hasCandidate:  false,
			thresholdHigh: th,
			want:          "❓ 数据不足，系统还在找来源 → 稍等",
		},
		// ⚡ 资源充足
		{
			name:          "做种站点数 = 阈值 -> ⚡ 充足（边界，含等号）",
			seedingSites:  th,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "⚡ 资源充足，很快到货 → 等着就好",
		},
		{
			name:          "做种站点数 > 阈值 -> ⚡ 充足",
			seedingSites:  th + 2,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "⚡ 资源充足，很快到货 → 等着就好",
		},
		// 🔄 已有源需要等种
		{
			name:          "做种站点数 = 阈值-1 -> 🔄 等种（上边界）",
			seedingSites:  th - 1,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "🔄 已有源，需要等做种 → 可去站点顶一下种",
		},
		{
			name:          "做种站点数 = 1 -> 🔄 等种（下边界）",
			seedingSites:  1,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "🔄 已有源，需要等做种 → 可去站点顶一下种",
		},
		// 🐢 暂无源
		{
			name:          "有候选但 0 站点做种 -> 🐢 待补档",
			seedingSites:  0,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "🐢 暂无站点出源 → 可去求助群问问谁有",
		},
		// 自定义阈值
		{
			name:          "自定义阈值=1：1 站做种即 ⚡",
			seedingSites:  1,
			hasCandidate:  true,
			thresholdHigh: 1,
			want:          "⚡ 资源充足，很快到货 → 等着就好",
		},
		{
			name:          "自定义阈值=5：4 站做种仍为 🔄",
			seedingSites:  4,
			hasCandidate:  true,
			thresholdHigh: 5,
			want:          "🔄 已有源，需要等做种 → 可去站点顶一下种",
		},
		// 防御：非法阈值回退默认值 3
		{
			name:          "非法阈值(0) 回退默认3：2 站 -> 🔄",
			seedingSites:  2,
			hasCandidate:  true,
			thresholdHigh: 0,
			want:          "🔄 已有源，需要等做种 → 可去站点顶一下种",
		},
		{
			name:          "非法阈值(0) 回退默认3：3 站 -> ⚡",
			seedingSites:  3,
			hasCandidate:  true,
			thresholdHigh: 0,
			want:          "⚡ 资源充足，很快到货 → 等着就好",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := estimateDeliveryLamp(c.seedingSites, c.hasCandidate, c.thresholdHigh)
			if got != c.want {
				t.Errorf("estimateDeliveryLamp(%d, %v, %d) = %q, want %q",
					c.seedingSites, c.hasCandidate, c.thresholdHigh, got, c.want)
			}
		})
	}
}

// TestDeliveryLampSuggestedActions 确保四档文案各自带上「建议动作」短语（需求 #3）。
// 极端数据：极大做种站点数、刚好 0 站、阈值边界，逐档校验建议动作不缺失。
func TestDeliveryLampSuggestedActions(t *testing.T) {
	const th = etaThresholdHighDefault
	cases := []struct {
		name         string
		seedingSites int
		hasCandidate bool
		mustContain  string
	}{
		{"⚡档建议:等着就好", th, true, "等着就好"},
		{"⚡档建议:超大做种数仍带建议", 9999, true, "等着就好"},
		{"🔄档建议:去站点顶种", 1, true, "可去站点顶一下种"},
		{"🐢档建议:去求助群", 0, true, "可去求助群问问谁有"},
		{"❓档建议:稍等", 0, false, "稍等"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := estimateDeliveryLamp(c.seedingSites, c.hasCandidate, th)
			if !containsSubstr(got, c.mustContain) {
				t.Errorf("estimateDeliveryLamp 文案 %q 缺少建议动作 %q", got, c.mustContain)
			}
		})
	}
}

// containsSubstr 是测试内联的子串判断，避免额外引入 strings 依赖到本测试组。
func containsSubstr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestCountSeedingSites 验证「有做种站点数」去重统计。
func TestCountSeedingSites(t *testing.T) {
	cases := []struct {
		name      string
		resources []CandidateResource
		want      int
	}{
		{
			name:      "空切片 -> 0",
			resources: nil,
			want:      0,
		},
		{
			name: "全部 0 做种 -> 0",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 0},
				{SiteName: "B", Seeders: 0},
			},
			want: 0,
		},
		{
			name: "同站点多条做种只计一次",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 5},
				{SiteName: "A", Seeders: 10},
				{SiteName: "B", Seeders: 3},
			},
			want: 2,
		},
		{
			name: "混合：部分站点 0 做种不计入",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 5},
				{SiteName: "B", Seeders: 0},
				{SiteName: "C", Seeders: 1},
			},
			want: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := countSeedingSites(c.resources)
			if got != c.want {
				t.Errorf("countSeedingSites() = %d, want %d", got, c.want)
			}
		})
	}
}

// TestDeliveryLampForResources 验证端到端封装（默认阈值 3）。
func TestDeliveryLampForResources(t *testing.T) {
	// 默认阈值为 3（未设置 ETA_THRESHOLD_HIGH env）。
	cases := []struct {
		name         string
		resources    []CandidateResource
		hasCandidate bool
		want         string
	}{
		{
			name:         "无候选 -> ❓",
			resources:    nil,
			hasCandidate: false,
			want:         "❓ 数据不足，系统还在找来源 → 稍等",
		},
		{
			name: "3 站做种 -> ⚡",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 1},
				{SiteName: "B", Seeders: 2},
				{SiteName: "C", Seeders: 3},
			},
			hasCandidate: true,
			want:         "⚡ 资源充足，很快到货 → 等着就好",
		},
		{
			name: "2 站做种 -> 🔄",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 1},
				{SiteName: "B", Seeders: 2},
			},
			hasCandidate: true,
			want:         "🔄 已有源，需要等做种 → 可去站点顶一下种",
		},
		{
			name: "有候选但全 0 做种 -> 🐢",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 0},
			},
			hasCandidate: true,
			want:         "🐢 暂无站点出源 → 可去求助群问问谁有",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deliveryLampForResources(c.resources, c.hasCandidate)
			if got != c.want {
				t.Errorf("deliveryLampForResources() = %q, want %q", got, c.want)
			}
		})
	}
}
