package handlers

import "testing"

// TestEstimateDelivery 覆盖三档分支 + 数据缺失兜底。
func TestEstimateDelivery(t *testing.T) {
	cases := []struct {
		name         string
		siteCount    int
		seederTotal  int
		hasCandidate bool
		want         string
	}{
		{
			name:         "无候选数据 -> 保守冷门",
			siteCount:    0,
			seederTotal:  0,
			hasCandidate: false,
			want:         "📦 到货预估：这片较冷门，可能要等几天 🐢",
		},
		{
			name:         "多站点 -> 充足",
			siteCount:    3,
			seederTotal:  0, // 站点数达标即可
			hasCandidate: true,
			want:         "📦 到货预估：资源充足，今晚就能看 🚀",
		},
		{
			name:         "做种很多 -> 充足",
			siteCount:    1,
			seederTotal:  50,
			hasCandidate: true,
			want:         "📦 到货预估：资源充足，今晚就能看 🚀",
		},
		{
			name:         "有候选少量做种 -> 一般",
			siteCount:    1,
			seederTotal:  5,
			hasCandidate: true,
			want:         "📦 到货预估：正常排队中，预计 1-2 天 ⏳",
		},
		{
			name:         "有候选但做种为0 -> 冷门",
			siteCount:    1,
			seederTotal:  0,
			hasCandidate: true,
			want:         "📦 到货预估：这片较冷门，可能要等几天 🐢",
		},
		{
			name:         "边界：恰好达到一般下限",
			siteCount:    1,
			seederTotal:  deliveryNormalSeederTotal,
			hasCandidate: true,
			want:         "📦 到货预估：正常排队中，预计 1-2 天 ⏳",
		},
		{
			name:         "边界：恰好达到充足做种阈值",
			siteCount:    1,
			seederTotal:  deliveryRichSeederTotal,
			hasCandidate: true,
			want:         "📦 到货预估：资源充足，今晚就能看 🚀",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := estimateDelivery(c.siteCount, c.seederTotal, c.hasCandidate)
			if got != c.want {
				t.Errorf("estimateDelivery(%d, %d, %v) = %q, want %q",
					c.siteCount, c.seederTotal, c.hasCandidate, got, c.want)
			}
		})
	}
}
