package handlers

// 本文件实现「求片预产期」功能（Batch A #1）。
//
// 设计目标：在求片成功后追加一条「到货预估」文案，给用户一个轻量的心理预期。
//
// 数据来源与限制说明：
//   - 理想情况下应根据候选资源的「有种站点数 / 做种数 seeder」来分档。
//   - 但当前求片流程（RequestHandler.Handle）走的是「提交审核」逻辑，
//     在提交点并不会主动去各站点搜索候选资源（那是 ResourceHandler 的职责），
//     因此这里**不会**为了拿 seeder 数据而额外发起网络请求（避免打乱/拖慢求片流程、引入不确定行为）。
//   - 折中方案：把分档逻辑抽成一个纯函数 estimateDelivery，调用方根据已掌握的
//     保守信息（站点数 / 做种数 / 是否有候选）传参；当完全没有候选信息时，
//     传入 hasCandidate=false，函数会返回最保守的「冷门」档位文案。
//
// 这样既保证了易测试性，又能在未来求片流程能拿到候选数据时直接复用，无需改动调用方以外的代码。

// 预产期分档阈值（经验值，可按需调整）
const (
	// 充足：多站点有种 或 单站点做种数很高
	deliveryRichSiteCount   = 3
	deliveryRichSeederTotal = 30
	// 一般：至少有候选且做种数不为 0
	deliveryNormalSeederTotal = 1
)

// estimateDelivery 根据候选资源情况返回「到货预估」文案（纯函数，便于单测）。
//
// 参数：
//   - siteCount:    有该资源的站点数量（命中候选的站点数）
//   - seederTotal:  所有候选资源的做种数(seeder)之和
//   - hasCandidate: 是否拿到了任何候选资源数据（用于区分「确实冷门」与「数据缺失」）
//
// 规则（分三档）：
//   - 资源充足（多站点 / 做种多）        -> 「资源充足，今晚就能看 🚀」
//   - 一般（有候选且有一定做种）          -> 「正常排队中，预计 1-2 天 ⏳」
//   - 冷门 / 无候选 / 数据缺失（保守兜底）-> 「这片较冷门，可能要等几天 🐢」
func estimateDelivery(siteCount, seederTotal int, hasCandidate bool) string {
	// 没有任何候选数据时，保守给出「冷门」档位（不臆造乐观结果）。
	if !hasCandidate {
		return "📦 到货预估：这片较冷门，可能要等几天 🐢"
	}

	// 充足：站点数多 或 做种总数高，任一满足即视为充足。
	if siteCount >= deliveryRichSiteCount || seederTotal >= deliveryRichSeederTotal {
		return "📦 到货预估：资源充足，今晚就能看 🚀"
	}

	// 一般：有候选且做种数达到下限。
	if seederTotal >= deliveryNormalSeederTotal {
		return "📦 到货预估：正常排队中，预计 1-2 天 ⏳"
	}

	// 其余（有候选但做种为 0 等）按冷门处理。
	return "📦 到货预估：这片较冷门，可能要等几天 🐢"
}
