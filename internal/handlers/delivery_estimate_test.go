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
			want:          "❓ 还在找源中……",
		},
		{
			name:          "数据缺失优先于 seedingSites（即便有数也忽略）",
			seedingSites:  10,
			hasCandidate:  false,
			thresholdHigh: th,
			want:          "❓ 还在找源中……",
		},
		// ⚡ 资源充足
		{
			name:          "做种站点数 = 阈值 -> ⚡ 充足（边界，含等号）",
			seedingSites:  th,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "⚡ 资源充足，很快到货",
		},
		{
			name:          "做种站点数 > 阈值 -> ⚡ 充足",
			seedingSites:  th + 2,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "⚡ 资源充足，很快到货",
		},
		// 🔄 已有源需要等种
		{
			name:          "做种站点数 = 阈值-1 -> 🔄 等种（上边界）",
			seedingSites:  th - 1,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "🔄 已有源，需要等种",
		},
		{
			name:          "做种站点数 = 1 -> 🔄 等种（下边界）",
			seedingSites:  1,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "🔄 已有源，需要等种",
		},
		// 🐢 暂无源
		{
			name:          "有候选但 0 站点做种 -> 🐢 待补档",
			seedingSites:  0,
			hasCandidate:  true,
			thresholdHigh: th,
			want:          "🐢 暂无源，待补档",
		},
		// 自定义阈值
		{
			name:          "自定义阈值=1：1 站做种即 ⚡",
			seedingSites:  1,
			hasCandidate:  true,
			thresholdHigh: 1,
			want:          "⚡ 资源充足，很快到货",
		},
		{
			name:          "自定义阈值=5：4 站做种仍为 🔄",
			seedingSites:  4,
			hasCandidate:  true,
			thresholdHigh: 5,
			want:          "🔄 已有源，需要等种",
		},
		// 防御：非法阈值回退默认值 3
		{
			name:          "非法阈值(0) 回退默认3：2 站 -> 🔄",
			seedingSites:  2,
			hasCandidate:  true,
			thresholdHigh: 0,
			want:          "🔄 已有源，需要等种",
		},
		{
			name:          "非法阈值(0) 回退默认3：3 站 -> ⚡",
			seedingSites:  3,
			hasCandidate:  true,
			thresholdHigh: 0,
			want:          "⚡ 资源充足，很快到货",
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
			want:         "❓ 还在找源中……",
		},
		{
			name: "3 站做种 -> ⚡",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 1},
				{SiteName: "B", Seeders: 2},
				{SiteName: "C", Seeders: 3},
			},
			hasCandidate: true,
			want:         "⚡ 资源充足，很快到货",
		},
		{
			name: "2 站做种 -> 🔄",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 1},
				{SiteName: "B", Seeders: 2},
			},
			hasCandidate: true,
			want:         "🔄 已有源，需要等种",
		},
		{
			name: "有候选但全 0 做种 -> 🐢",
			resources: []CandidateResource{
				{SiteName: "A", Seeders: 0},
			},
			hasCandidate: true,
			want:         "🐢 暂无源，待补档",
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
