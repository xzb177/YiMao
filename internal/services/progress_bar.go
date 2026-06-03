package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现「群追剧·集数进度条」功能（Batch A #2）。
//
// 目标：在剧集入库聚合通知里追加一行进度，例如：
//   已更 E07/E16 ▓▓▓▓▓░░░ 44%
// 能拿到总集数时额外显示「距完结还差 N 集」。
//
// 数据来源与边界处理：
//   - 总集数从 TMDB 取（剧集 number_of_episodes，按整部剧的总集数）。
//   - 取不到总集数（total<=0）时只显示「已更到 EXX」，不渲染进度条，
//     绝不除零、绝不臆造总集数。
//   - current>total（例如 TMDB 数据滞后）时做钳制，避免出现 >100% 或越界。

const progressBarWidth = 8 // 进度条字符宽度

// renderProgressBar 渲染一行进度条文案（纯函数，便于单测）。
//
// 入参：
//   - current: 当前已更到的集数（用最大集号表示）
//   - total:   该剧总集数（来自 TMDB；未知时传 0）
//
// 返回：
//   - total<=0:        "已更到 E07"            （未知总数，不渲染进度条，不除零）
//   - current<=0:      ""                       （没有有效集数，调用方自行决定是否显示）
//   - 正常:            "已更 E07/E16 ▓▓▓▓▓░░░ 44%（距完结还差 9 集）"
//   - current>=total:  "已更 E16/E16 ▓▓▓▓▓▓▓▓ 100%（已完结）"
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

	// 计算填充格数（四舍五入到最接近的格），并保证至少 1 格、至多满格。
	filled := int(float64(progressBarWidth)*float64(clamped)/float64(total) + 0.5)
	if filled < 1 {
		filled = 1
	}
	if filled > progressBarWidth {
		filled = progressBarWidth
	}

	bar := ""
	for i := 0; i < progressBarWidth; i++ {
		if i < filled {
			bar += "▓"
		} else {
			bar += "░"
		}
	}

	percent := clamped * 100 / total

	remaining := total - clamped
	suffix := ""
	if remaining <= 0 {
		suffix = "（已完结）"
	} else {
		suffix = fmt.Sprintf("（距完结还差 %d 集）", remaining)
	}

	return fmt.Sprintf("📈 已更 E%02d/E%02d %s %d%%%s", clamped, total, bar, percent, suffix)
}

// getTMDBTotalEpisodes 从 TMDB 获取剧集的总集数（number_of_episodes）。
// 取不到时返回 0（调用方会据此跳过进度条渲染）。
//
// 复用 getTMDBBackdrop 相同的 HTTP/apiKey 取值策略，不引入新的客户端依赖。
// 该函数仅在剧集聚合通知 flush 时（已在独立协程/定时器中）调用，不阻塞 webhook 主流程。
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

// buildEpisodeProgressLine 根据聚合信息构建进度行文案。
// current 取本次聚合中的最大集号；total 从 TMDB 取整部剧总集数。
//
// 说明：TMDB number_of_episodes 是整部剧（跨季）的总集数，对单季剧最精确；
// 多季剧为近似展示。取不到 total 时退化为「已更到 EXX」（见 renderProgressBar）。
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

	// 尝试从 TMDB 拿总集数；拿不到则 total=0，renderProgressBar 会只显示「已更到 EXX」。
	total := 0
	if agg.EnhancedInfo != nil && agg.EnhancedInfo.TMDBID != "" {
		total = s.getTMDBTotalEpisodes(agg.EnhancedInfo.TMDBID)
	}

	return renderProgressBar(current, total)
}

