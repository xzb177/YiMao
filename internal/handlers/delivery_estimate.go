package handlers

import (
	"fmt"
	"os"
)

// 本文件实现「求片预产期 / 状态灯牌」功能（Batch B #1）。
//
// 设计目标：在用户**浏览候选资源列表**时，根据已扫到的候选资源（站点 + 做种数）
// 计算一个轻量「状态灯牌」，给用户一个心理预期——但**不承诺具体时间**（不伪装精确日期）。
//
// 数据来源：候选列表入口 ResourceHandler.handleShowList 已经并发搜各站点并把命中结果
// 转成 []CandidateResource（含 SiteName / Seeders 字段），O(n) 扫这份现成数据即可，
// 零额外网络开销。
//
// 灯牌规则（四档不合并，每档带「建议动作」）：
//
//	⚡ ≥ETA_THRESHOLD_HIGH 站点做种 -> 资源充足，很快到货 → 等着就好
//	🔄 1 ~ (阈值-1) 站点做种        -> 已有源，需要等做种 → 可去站点顶一下种
//	🐢 0 站点做种                   -> 暂无站点出源 → 可去求助群问问谁有
//	❓ 候选为空 / 数据缺失          -> 数据不足，系统还在找来源 → 稍等
//
// 这里「站点做种」指：该站点至少有一个候选资源的 seeder 数 > 0（即有人在做种），
// 按**去重后的站点数**统计（同一站点多条资源只算一个）。
//
// 纯函数 estimateDeliveryLamp 不读 env、不依赖 ResourceHandler，便于单测；
// 阈值由调用方传入（调用方用 etaThresholdHigh() 从 env 读取，遵循 config.getEnvInt 模式）。

// etaThresholdHighDefault 是 ETA_THRESHOLD_HIGH 的默认值（与 docs 附录一致）。
const etaThresholdHighDefault = 3

// etaThresholdHigh 从环境变量 ETA_THRESHOLD_HIGH 读取「资源充足」档位的站点做种数阈值。
// 遵循 internal/config/config.go 的 getEnvInt 模式（无效/缺失则回退默认值）。
// ResourceHandler 当前不持有 *config.Config，故在 handler 层就近读取，保持最小侵入。
func etaThresholdHigh() int {
	if value := os.Getenv("ETA_THRESHOLD_HIGH"); value != "" {
		var i int
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil && i > 0 {
			return i
		}
	}
	return etaThresholdHighDefault
}

// countSeedingSites 统计「有做种」的去重站点数：
// 即在候选资源里，至少有一条资源 Seeders > 0 的站点的数量（同站多条只计一次）。
// 返回的 seedingSites 用于灯牌分档。
func countSeedingSites(resources []CandidateResource) int {
	if len(resources) == 0 {
		return 0
	}
	seen := make(map[string]bool, len(resources))
	for _, r := range resources {
		if r.Seeders > 0 {
			seen[r.SiteName] = true
		}
	}
	return len(seen)
}

// estimateDeliveryLamp 根据「有做种的站点数」返回状态灯牌文案（纯函数，便于单测）。
//
// 参数：
//   - seedingSites:  有做种（Seeders>0）的去重站点数
//   - hasCandidate:  是否拿到了任何候选资源数据（区分「确实没源」与「数据缺失/还没搜」）
//   - thresholdHigh: 「资源充足」档位阈值（站点做种数 ≥ 此值 -> ⚡），来自 env ETA_THRESHOLD_HIGH
//
// 规则（四档，不承诺具体时间，每档带建议动作）：
//   - ❓ 候选为空 / 数据缺失（hasCandidate=false）
//   - ⚡ seedingSites >= thresholdHigh
//   - 🔄 1 <= seedingSites < thresholdHigh
//   - 🐢 seedingSites == 0
func estimateDeliveryLamp(seedingSites int, hasCandidate bool, thresholdHigh int) string {
	// 数据缺失 / 还没拿到任何候选 -> 最保守，不臆造。
	if !hasCandidate {
		return "❓ 数据不足，系统还在找来源 → 稍等"
	}

	// 防御：阈值非法时回退默认，避免分档退化。
	if thresholdHigh < 1 {
		thresholdHigh = etaThresholdHighDefault
	}

	switch {
	case seedingSites >= thresholdHigh:
		// ⚡ 充足：用户什么都不用做，等着就好。
		return "⚡ 资源充足，很快到货 → 等着就好"
	case seedingSites >= 1:
		// 🔄 有源但做种少：建议去站点顶一下种，加速做种。
		return "🔄 已有源，需要等做种 → 可去站点顶一下种"
	default:
		// 🐢 有候选数据但无站点出源：建议去求助群问问。
		return "🐢 暂无站点出源 → 可去求助群问问谁有"
	}
}

// deliveryLampForResources 是给候选列表入口用的便捷封装：
// 直接吃候选切片，内部完成「去重统计做种站点 -> 读阈值 -> 分档」。
// hasCandidate 由调用方传入（区分「搜过但 0 结果」与「还没搜/数据缺失」）。
func deliveryLampForResources(resources []CandidateResource, hasCandidate bool) string {
	seedingSites := countSeedingSites(resources)
	return estimateDeliveryLamp(seedingSites, hasCandidate, etaThresholdHigh())
}
