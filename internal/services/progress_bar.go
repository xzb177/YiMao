package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现「群追剧·集数进度条」功能（Batch A #2 / Batch B B3 季相对口径修复）。
//
// 目标：在剧集入库聚合通知里追加一行进度，例如：
//   已更 S03E07/S03E16 ▓▓▓▓░░░░ 44%
// 能拿到本季总集数时额外显示「距完结还差 N 集」。
//
// 数据来源与边界处理（B3 修复后）：
//   - 分母用「当前季总集数」（TMDB tv/{id}/season/{n} 的 episodes 长度），
//     绝不用 number_of_episodes（全剧跨季总集数）——否则多季剧百分比错乱。
//   - 取不到本季集数（total<=0）时只显示「已更到 SxxExx」，不渲染进度条、不显示百分比，
//     绝不除零、绝不臆造总集数。
//   - current>total（例如 TMDB 数据滞后）时做钳制，避免出现 >100% 或越界。

const progressBarWidth = 8 // 进度条字符宽度

// progressFillAndPercent 用同一份计算同时得出「填充格数」与「百分比」（B5 口径统一）。
// clamped/total 已保证 total>0、1<=clamped<=total。两者都基于同一比例 ratio=clamped/total：
//   - filled  = round(width * ratio)，并钳到 [1,width]。
//   - percent = round(100 * ratio)，与 filled 同源同舍入，避免「条满了但百分比不到 100」之类的口径打架。
func progressFillAndPercent(clamped, total int) (filled, percent int) {
	ratio := float64(clamped) / float64(total)
	filled = int(float64(progressBarWidth)*ratio + 0.5)
	if filled < 1 {
		filled = 1
	}
	if filled > progressBarWidth {
		filled = progressBarWidth
	}
	percent = int(100*ratio + 0.5)
	if percent > 100 {
		percent = 100
	}
	return filled, percent
}

// buildBar 根据填充格数渲染进度条字符串。
func buildBar(filled int) string {
	bar := ""
	for i := 0; i < progressBarWidth; i++ {
		if i < filled {
			bar += "▓"
		} else {
			bar += "░"
		}
	}
	return bar
}

// renderProgressBar 渲染一行进度条文案（纯函数，便于单测；不带季号，单季/兜底场景使用）。
//
// 入参：
//   - current: 当前已更到的集数（用最大集号表示）
//   - total:   总集数（未知时传 0）
//
// 返回：
//   - total<=0:        "📈 已更到 E07"          （未知总数，不渲染进度条，不除零）
//   - current<=0:      ""                       （没有有效集数）
//   - 正常:            "📈 已更 E07/E16 ▓▓▓▓░░░░ 44%（距完结还差 9 集）"
//   - current>=total:  "📈 已更 E16/E16 ▓▓▓▓▓▓▓▓ 100%（已完结）"
func renderProgressBar(current, total int) string {
	if current <= 0 {
		return ""
	}

	// 未知总集数：只显示已更到第几集，不渲染进度条（避免除零/瞎编）。
	if total <= 0 {
		return fmt.Sprintf("📈 已更到 E%02d", current)
	}

	// 钳制 current 到 [1, total]，防止 TMDB 数据滞后导致越界/超 100%。
	clamped := current
	if clamped > total {
		clamped = total
	}

	filled, percent := progressFillAndPercent(clamped, total)
	bar := buildBar(filled)

	remaining := total - clamped
	suffix := "（已完结）"
	if remaining > 0 {
		suffix = fmt.Sprintf("（距完结还差 %d 集）", remaining)
	}

	return fmt.Sprintf("📈 已更 E%02d/E%02d %s %d%%%s", clamped, total, bar, percent, suffix)
}

// renderSeasonProgressBar 渲染「季相对」进度行（B3 多季剧口径，纯函数便于单测）。
//
// 入参：
//   - season:  季号（>0 时带 Sxx 前缀；<=0 退化为不带季号的 renderProgressBar）。
//   - current: 本季已更到的集号。
//   - seasonTotal: 本季总集数（来自 TMDB 季详情；未知传 0）。
//
// 返回：
//   - current<=0:        ""                          （无有效集数）
//   - seasonTotal<=0:    "📈 已更到 S03E07"          （拿不到本季集数：只报到第几集，无百分比、无距完结）
//   - 正常:              "📈 已更 S03E07/S03E16 ▓▓▓▓░░░░ 44%（距完结还差 9 集）"
//   - current>=total:    "📈 已更 S03E16/S03E16 ▓▓▓▓▓▓▓▓ 100%（已完结）"
func renderSeasonProgressBar(season, current, seasonTotal int) string {
	if current <= 0 {
		return ""
	}
	if season <= 0 {
		// 无季号信息：退回不带季号的渲染（仍遵守 total<=0 不显示百分比）。
		return renderProgressBar(current, seasonTotal)
	}

	// 拿不到本季集数：只显示「已更到 SxxExx」，不显示百分比/距完结（B3 硬要求）。
	if seasonTotal <= 0 {
		return fmt.Sprintf("📈 已更到 S%02dE%02d", season, current)
	}

	clamped := current
	if clamped > seasonTotal {
		clamped = seasonTotal
	}

	filled, percent := progressFillAndPercent(clamped, seasonTotal)
	bar := buildBar(filled)

	remaining := seasonTotal - clamped
	suffix := "（已完结）"
	if remaining > 0 {
		suffix = fmt.Sprintf("（距完结还差 %d 集）", remaining)
	}

	return fmt.Sprintf("📈 已更 S%02dE%02d/S%02dE%02d %s %d%%%s",
		season, clamped, season, seasonTotal, bar, percent, suffix)
}

// renderSeriesProgressBar 渲染「全剧」口径进度行（纯函数，便于单测）。
//
// 与 renderSeasonProgressBar 区别：全剧不带季号、用全剧累计已更集数 / 全剧总集数，前缀「全剧：」。
//
// 入参：
//   - current: 全剧累计已更集数（跨季累计；未知/<=0 返回空）。
//   - total:   全剧总集数（来自 TMDB number_of_episodes；未知传 0）。
//
// 返回：
//   - current<=0:     ""                              （无有效集数，不渲染）
//   - total<=0:       "全剧：已更到 E52"               （拿不到全剧总数：只报累计，无百分比）
//   - 正常:           "全剧：E52/E100 ▓▓▓▓▓░░░ 52%"    （不带「距完结」，避免与本季口径混淆）
//   - current>=total: "全剧：E100/E100 ▓▓▓▓▓▓▓▓ 100%"  （钳制，防越界）
func renderSeriesProgressBar(current, total int) string {
	if current <= 0 {
		return ""
	}
	if total <= 0 {
		// 拿不到全剧总集数：只报累计已更到第几集，不渲染进度条/百分比（不除零、不臆造）。
		return fmt.Sprintf("全剧：已更到 E%02d", current)
	}

	clamped := current
	if clamped > total {
		clamped = total
	}
	filled, percent := progressFillAndPercent(clamped, total)
	bar := buildBar(filled)
	return fmt.Sprintf("全剧：E%02d/E%02d %s %d%%", clamped, total, bar, percent)
}

// renderDualProgressBar 渲染「双口径」进度（需求 #4：本季 + 全剧两行）。
//
// 设计：本季口径仍复用 renderSeasonProgressBar（季号 + 本季集数/本季总集数 + 距完结），
// 全剧口径用 renderSeriesProgressBar（全剧累计/全剧总集数）。
//
// 退化规则（守住边界，绝不臆造、绝不除零）：
//   - 两个口径都拿得到 → 两行：
//     本季：📈 已更 S03E12/S03E16 ▓▓▓░░ 75%（距完结还差 4 集）
//     全剧：E52/E100 ▓▓▓▓▓░░ 52%
//   - 只有本季（拿不到全剧总数）→ 仅本季那一行（沿用 renderSeasonProgressBar 行为）。
//   - 只有全剧（拿不到本季集数但有全剧）→ 退化为本季「已更到 SxxExx」单行 + 全剧行。
//   - 两者都拿不到 → 单行「已更到 SxxExx」（沿用 renderSeasonProgressBar 兜底）。
//
// 入参：
//   - season:        季号（<=0 时本季行退化为不带季号渲染）。
//   - seasonCurrent: 本季已更到的集号。
//   - seasonTotal:   本季总集数（未知传 0）。
//   - seriesCurrent: 全剧累计已更集数（未知/无法计算传 0 → 不渲染全剧行）。
//   - seriesTotal:   全剧总集数（未知传 0 → 全剧行退化为「已更到 EXX」或整体不显示）。
//
// 返回：用 "\n" 连接的 1~2 行；seasonCurrent<=0 且 seriesCurrent<=0 时返回空串。
func renderDualProgressBar(season, seasonCurrent, seasonTotal, seriesCurrent, seriesTotal int) string {
	seasonLine := renderSeasonProgressBar(season, seasonCurrent, seasonTotal)
	seriesLine := renderSeriesProgressBar(seriesCurrent, seriesTotal)

	switch {
	case seasonLine != "" && seriesLine != "":
		// 双口径都有 → 两行。本季在上、全剧在下。
		return seasonLine + "\n" + seriesLine
	case seasonLine != "":
		// 只有本季口径。
		return seasonLine
	case seriesLine != "":
		// 本季集号无效但全剧有效（少见，保守只显全剧行）。
		return seriesLine
	default:
		return ""
	}
}

// getTMDBSeriesEpisodeStats 取「全剧总集数」与「截至指定季为止的累计集数基数」（需求 #4 双口径）。
//
// 返回：
//   - seriesTotal:  全剧总集数（number_of_episodes）。
//   - priorEpisodes: 在 currentSeason 之前的所有「正片季」（season_number>=1）的 episode_count 之和，
//     用作全剧累计基数：全剧已更累计 = priorEpisodes + 本季已更集号。
//
// 任一环节失败/数据缺失时返回 (0,0)，调用方据此退化为只渲染本季口径（绝不臆造、绝不除零）。
// 仅在聚合通知 flush 时调用一次，不阻塞主流程。
func (s *WebhookService) getTMDBSeriesEpisodeStats(tmdbID string, currentSeason int) (seriesTotal, priorEpisodes int) {
	if tmdbID == "" {
		return 0, 0
	}

	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		logger.Warn("[TMDB] TMDB_API_KEY 未配置，跳过剧集统计")
		return 0, 0
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s?api_key=%s&language=zh-CN", tmdbID, apiKey)

	resp, err := client.Get(url)
	if err != nil {
		logger.Info("[TMDB] 获取全剧集数失败 ID=%s: %v", tmdbID, err)
		return 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Info("[TMDB] 获取全剧集数返回状态 %d ID=%s", resp.StatusCode, tmdbID)
		return 0, 0
	}

	var result struct {
		NumberOfEpisodes int `json:"number_of_episodes"`
		Seasons          []struct {
			SeasonNumber int `json:"season_number"`
			EpisodeCount int `json:"episode_count"`
		} `json:"seasons"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Info("[TMDB] 解析全剧集数失败 ID=%s: %v", tmdbID, err)
		return 0, 0
	}

	seriesTotal = result.NumberOfEpisodes
	// 累计基数：currentSeason 之前的正片季（season_number>=1）集数之和。
	// 跳过 season 0（特别篇/花絮），避免污染正片累计口径。
	for _, se := range result.Seasons {
		if se.SeasonNumber >= 1 && se.SeasonNumber < currentSeason {
			priorEpisodes += se.EpisodeCount
		}
	}
	return seriesTotal, priorEpisodes
}

// getTMDBTotalEpisodes 从 TMDB 获取剧集的「全剧跨季」总集数（number_of_episodes）。
//
// Deprecated（B3 修复）：进度条已改为季相对口径（见 getTMDBSeasonEpisodes / renderSeasonProgressBar），
// 不再用全剧总集数当分母。此方法保留备用，当前进度条逻辑不再调用。
func (s *WebhookService) getTMDBTotalEpisodes(tmdbID string) int {
	if tmdbID == "" {
		return 0
	}

	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		logger.Warn("[TMDB] TMDB_API_KEY 未配置，跳过总集数查询")
		return 0
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s?api_key=%s&language=zh-CN", tmdbID, apiKey)

	resp, err := client.Get(url)
	if err != nil {
		logger.Info("[TMDB] 获取总集数失败 ID=%s: %v", tmdbID, err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Info("[TMDB] 获取总集数返回状态 %d ID=%s", resp.StatusCode, tmdbID)
		return 0
	}

	var result struct {
		NumberOfEpisodes int `json:"number_of_episodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Info("[TMDB] 解析总集数失败 ID=%s: %v", tmdbID, err)
		return 0
	}

	return result.NumberOfEpisodes
}

// getTMDBSeasonEpisodes 从 TMDB 获取「指定季」的总集数（season episode_count）。
// 用于多季剧的季相对进度（B3：current=本季集号，total 必须用本季集数，绝不用全剧总集数）。
// 取不到（季不存在 / 请求失败 / count<=0）时返回 0，调用方据此退化为「只显示已更到 SxEy」。
//
// 复用与 getTMDBTotalEpisodes 相同的 HTTP/apiKey 策略；仅在聚合通知 flush 时调用，不阻塞主流程。
func (s *WebhookService) getTMDBSeasonEpisodes(tmdbID string, season int) int {
	if tmdbID == "" || season <= 0 {
		return 0
	}

	apiKey := s.tmdbAPIKey
	if apiKey == "" {
		logger.Warn("[TMDB] TMDB_API_KEY 未配置，跳过季集数查询")
		return 0
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s/season/%d?api_key=%s&language=zh-CN", tmdbID, season, apiKey)

	resp, err := client.Get(url)
	if err != nil {
		logger.Info("[TMDB] 获取季集数失败 ID=%s S%d: %v", tmdbID, season, err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Info("[TMDB] 获取季集数返回状态 %d ID=%s S%d", resp.StatusCode, tmdbID, season)
		return 0
	}

	// season 详情接口返回 episodes 数组；用其长度作为本季集数（比上层 seasons[].episode_count 更实时）。
	var result struct {
		Episodes []struct {
			EpisodeNumber int `json:"episode_number"`
		} `json:"episodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Info("[TMDB] 解析季集数失败 ID=%s S%d: %v", tmdbID, season, err)
		return 0
	}

	return len(result.Episodes)
}

// buildEpisodeProgressLine 根据聚合信息构建进度行文案。
// current 取本次聚合中的最大集号（= 本季集号）。
//
// B3 多季剧口径修复（按 Codex 拆口径）：
//   - current 是「本季」集号（Emby 推送的 IndexNumber 是季内集号），
//     因此分母必须用「当前季总集数」，绝不能用 TMDB 全剧跨季总集数，否则百分比错乱。
//   - 能拿到当前季集数 → 显示季相对进度，如「已更 S3E12/S3E16 ▓… 75%」。
//   - 拿不到当前季集数 → 只显示「已更到 S3E12」，不显示百分比、不显示「距完结差N集」。
func (s *WebhookService) buildEpisodeProgressLine(agg *EpisodeAggregation) string {
	if agg == nil || len(agg.Episodes) == 0 {
		return ""
	}

	// 取最大集号作为「当前已更到」（agg.Episodes 在 flush 前已排序，这里再取 max 以防万一）。
	current := agg.Episodes[0]
	for _, ep := range agg.Episodes {
		if ep > current {
			current = ep
		}
	}

	// 尝试取「当前季」总集数（B3：本季分母必须是季集数，不是全剧总集数）。
	seasonTotal := 0
	if agg.Season > 0 && agg.EnhancedInfo != nil && agg.EnhancedInfo.TMDBID != "" {
		seasonTotal = s.getTMDBSeasonEpisodes(agg.EnhancedInfo.TMDBID, agg.Season)
	}

	// 需求 #4 双口径：再尝试取「全剧总集数」+「本季之前累计集数」，组合出全剧口径。
	//   - 全剧累计已更 = priorEpisodes + 本季已更集号（current）。
	//   - 任一数据拿不到（seriesTotal<=0 或无 TMDBID）→ renderSeriesProgressBar 自然退化/返回空，
	//     renderDualProgressBar 据此只渲染本季单行（绝不臆造）。
	seriesCurrent, seriesTotal := 0, 0
	if agg.Season > 0 && agg.EnhancedInfo != nil && agg.EnhancedInfo.TMDBID != "" {
		total, prior := s.getTMDBSeriesEpisodeStats(agg.EnhancedInfo.TMDBID, agg.Season)
		if total > 0 {
			seriesTotal = total
			seriesCurrent = prior + current
		}
	}

	return renderDualProgressBar(agg.Season, current, seasonTotal, seriesCurrent, seriesTotal)
}
