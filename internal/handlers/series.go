package handlers

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// ─────────────────────────────────────
// 系列补全：求电影时检测 TMDB collection，
// 发现库里/订阅里缺同系列其他部时，主动提议补齐。
//
// 回调格式：
//   series_view:cid:<collectionID>          查看系列缺片清单
//   （逐部求片直接复用现有 request:id:...:type:movie 流程）
// ─────────────────────────────────────

// SeriesHandler 系列补全处理器。
type SeriesHandler struct {
	tmdb       *services.TMDBClient
	moviepilot *services.MoviePilotClient
	webhook    *services.WebhookService
}

// NewSeriesHandler 创建系列补全处理器。
func NewSeriesHandler(tmdb *services.TMDBClient, mp *services.MoviePilotClient, webhook *services.WebhookService) *SeriesHandler {
	return &SeriesHandler{tmdb: tmdb, moviepilot: mp, webhook: webhook}
}

// seriesGap 一部缺失的系列影片。
type seriesGap struct {
	TmdbID int
	Title  string
	Year   int
}

func releasedPart(date string) bool {
	date = strings.TrimSpace(date)
	if date == "" {
		return false
	}
	parsed, err := time.Parse("2006-01-02", date)
	return err == nil && !parsed.After(time.Now())
}

// DetectGaps 检查一部电影所属系列的缺片情况。
// 返回 (系列名, 缺片列表)；无系列或全齐时列表为空。
// 只做轻量检查：订阅缓存 + Emby 精确匹配，超过 6 部的系列只看前 6 部（控制耗时）。
func (h *SeriesHandler) DetectGaps(tmdbID int) (string, []seriesGap) {
	if h.tmdb == nil || h.webhook == nil {
		return "", nil
	}
	movie, err := h.tmdb.GetMovieDetails(tmdbID)
	if err != nil || movie == nil || movie.BelongsToCollection == nil {
		return "", nil
	}
	collection, err := h.tmdb.GetCollection(movie.BelongsToCollection.ID)
	if err != nil || collection == nil || len(collection.Parts) < 2 {
		return "", nil
	}

	var subscribed map[int]struct{}
	if h.moviepilot != nil {
		subscribed, _ = h.moviepilot.CachedSubscriptionTMDBIDs()
	}

	// 先筛选最多 6 部候选，再并发查 Emby；串行 6×5 秒会拖过 Telegram 回调窗口。
	candidates := make([]struct {
		id    int
		title string
		year  int
	}, 0, 6)
	for _, part := range collection.Parts {
		if part.ID == tmdbID || part.ID <= 0 || strings.TrimSpace(part.ReleaseDate) == "" {
			continue
		}
		if _, ok := subscribed[part.ID]; ok {
			continue
		}
		if len(candidates) >= 6 {
			break
		}
		year := 0
		if len(part.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(part.ReleaseDate[:4])
		}
		candidates = append(candidates, struct {
			id    int
			title string
			year  int
		}{part.ID, part.Title, year})
	}

	gaps := make([]seriesGap, len(candidates))
	keep := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for i := range candidates {
		i := i
		if h.webhook == nil {
			keep[i] = true
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			existing, err := h.webhook.SearchEmbyMediaByTMDB(candidates[i].id, services.MediaTypeMovie)
			// Emby 查询失败时保守跳过，不把「未知」误报成缺片。
			if err == nil && existing == nil {
				keep[i] = true
			}
		}()
	}
	wg.Wait()
	var filtered []seriesGap
	for i, c := range candidates {
		if keep[i] {
			gaps[i] = seriesGap{TmdbID: c.id, Title: c.title, Year: c.year}
			filtered = append(filtered, gaps[i])
		}
	}
	return collection.Name, filtered
}

// BuildSuggestion 基于缺片结果构造回执附言与「查看系列」按钮；无缺片返回空。
func (h *SeriesHandler) BuildSuggestion(tmdbID int) (string, *types.TelegramInlineKeyboardButton) {
	name, gaps := h.DetectGaps(tmdbID)
	if len(gaps) == 0 {
		return "", nil
	}
	movie, err := h.tmdb.GetMovieDetails(tmdbID)
	if err != nil || movie == nil || movie.BelongsToCollection == nil {
		return "", nil
	}
	text := fmt.Sprintf("🎞 这部属于「%s」，库里还缺 %d 部", html.EscapeString(name), len(gaps))
	btn := &types.TelegramInlineKeyboardButton{
		Text:         fmt.Sprintf("🎞 补齐系列（缺 %d 部）", len(gaps)),
		CallbackData: fmt.Sprintf("series_view:cid:%d", movie.BelongsToCollection.ID),
	}
	return text, btn
}

// Handle 处理 series_view 回调：列出缺片清单，每部一个求片按钮。
func (h *SeriesHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	cidStr := ctx.Callback.Params["cid"]
	cid, err := strconv.Atoi(cidStr)
	if err != nil || cid <= 0 {
		return &callback.Response{CallbackMsg: "系列信息已失效", ShowAlert: true}, nil
	}
	collection, err := h.tmdb.GetCollection(cid)
	if err != nil || collection == nil {
		return &callback.Response{CallbackMsg: "系列信息拉取失败，稍后再试", ShowAlert: true}, nil
	}

	var subscribed map[int]struct{}
	if h.moviepilot != nil {
		subscribed, _ = h.moviepilot.CachedSubscriptionTMDBIDs()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎞 <b>%s</b>\n", html.EscapeString(collection.Name)))
	sb.WriteString("──────────────────\n")

	// 先构造已上映部分，Emby 精确查询并发执行，避免 N×5 秒串行超时。
	parts := make([]services.TMDBCollectionPart, 0, len(collection.Parts))
	for _, part := range collection.Parts {
		if part.ID > 0 && releasedPart(part.ReleaseDate) {
			parts = append(parts, part)
		}
	}
	// 候选列表中的 Emby 查询也必须 fail-closed：查询错误代表身份状态未知，
	// 不能把未知项目显示成可求片，否则会制造重复请求。
	libraryUnknown := make(map[int]bool, len(parts))
	library := make(map[int]bool, len(parts))
	var libraryMu sync.Mutex
	var wg sync.WaitGroup
	for _, part := range parts {
		part := part
		if _, ok := subscribed[part.ID]; ok || h.webhook == nil {
			library[part.ID] = true
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			existing, err := h.webhook.SearchEmbyMediaByTMDB(part.ID, services.MediaTypeMovie)
			libraryMu.Lock()
			defer libraryMu.Unlock()
			if err != nil {
				libraryUnknown[part.ID] = true
				return
			}
			if existing != nil {
				library[part.ID] = true
			}
		}()
	}
	wg.Wait()

	kb := services.NewKeyboardBuilder()
	missing := 0
	for _, part := range parts {
		year := ""
		if len(part.ReleaseDate) >= 4 {
			year = " · " + part.ReleaseDate[:4]
		}
		escapedTitle := html.EscapeString(part.Title)
		if library[part.ID] {
			sb.WriteString(fmt.Sprintf("✅ %s%s\n", escapedTitle, year))
			continue
		}
		if libraryUnknown[part.ID] {
			sb.WriteString(fmt.Sprintf("❔ %s%s · 状态暂无法确认\n", escapedTitle, year))
			continue
		}
		missing++
		sb.WriteString(fmt.Sprintf("⬜ %s%s\n", escapedTitle, year))
		if missing <= 6 {
			label := part.Title
			if len([]rune(label)) > 16 {
				label = string([]rune(label)[:16]) + "…"
			}
			kb.AddButton("➕ "+label, fmt.Sprintf("request:id:%d:type:movie", part.ID))
			kb.NewRow()
		}
	}

	if missing == 0 {
		sb.WriteString("\n这个系列已经齐了 🎉")
	} else {
		sb.WriteString(fmt.Sprintf("\n缺 %d 部，点下面按钮逐部求片（走正常审核流程）", missing))
	}
	kb.AddButton("🏠 主菜单", "start")

	logger.Info("[Series] 用户 %d 查看系列 %d（%s），缺 %d 部", ctx.UserID, cid, collection.Name, missing)
	return &callback.Response{
		Text:      sb.String(),
		ParseMode: "HTML",
		Edit:      false,
		Keyboard:  convertKeyboard(kb.Build()),
	}, nil
}
