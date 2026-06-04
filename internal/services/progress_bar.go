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
		apiKey = "a62307d3a16cd0a605de3857d9ed614e" // fallback default key（与 getTMDBBackdrop 一致）
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
		apiKey = "a62307d3a16cd0a605de3857d9ed614e" // fallback default key（与 getTMDBBackdrop 一致）
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

	// 尝试取「当前季」总集数（B3：分母必须是季集数，不是全剧总集数）。
	seasonTotal := 0
	if agg.Season > 0 && agg.EnhancedInfo != nil && agg.EnhancedInfo.TMDBID != "" {
		seasonTotal = s.getTMDBSeasonEpisodes(agg.EnhancedInfo.TMDBID, agg.Season)
	}

	return renderSeasonProgressBar(agg.Season, current, seasonTotal)
}

