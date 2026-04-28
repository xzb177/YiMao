// Package presenters provides message building services for search and recommendations.
package presenters

import (
	"fmt"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/types"
)

// SearchPresenter builds messages for search results.
type SearchPresenter struct{}

// NewSearchPresenter creates a new search presenter.
func NewSearchPresenter() *SearchPresenter {
	return &SearchPresenter{}
}

// SearchResults builds a response for search results.
func (p *SearchPresenter) SearchResults(results []services.SearchResult, query string) *callback.Response {
	text := p.buildSearchResultsText(results, query)
	keyboard := p.buildSearchKeyboard(results, query)

	return &callback.Response{
		Text:     text,
		Edit:     false,
		Keyboard: keyboard,
	}
}

// buildSearchResultsText builds the text message for search results.
func (p *SearchPresenter) buildSearchResultsText(results []services.SearchResult, query string) string {
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	for i, item := range results.Results {
		if i >= 8 {
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬 电影"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)
	}

	return text
}

// buildSearchKeyboard builds the inline keyboard for search results.
func (p *SearchPresenter) buildSearchKeyboard(results []services.SearchResult, query string) *types.TelegramInlineKeyboard {
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 {
			break
		}

		// Convert Chinese type to English for callback data
		mediaTypeForCallback := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaTypeForCallback = "tv"
		}

		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, mediaTypeForCallback),
		})

		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	// Add navigation row
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	}
	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}
	keyboardRows = append(keyboardRows, navRow)

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}
}

// RecommendationPresenter builds messages for recommendations.
type RecommendationPresenter struct{}

// NewRecommendationPresenter creates a new recommendation presenter.
func NewRecommendationPresenter() *RecommendationPresenter {
	return &RecommendationPresenter{}
}

// RecommendationResults builds a response for recommendation results.
func (p *RecommendationPresenter) RecommendationResults(results []services.SearchResult, recType string) *callback.Response {
	msg := p.buildRecommendationText(results, recType)
	keyboard := p.buildRecommendationKeyboard(results, recType)

	return &callback.Response{
		Text:     msg,
		Edit:     true,
		Keyboard: keyboard,
	}
}

// ErrorRecommendation builds an error response for recommendations.
func (p *RecommendationPresenter) ErrorRecommendation(err error) *callback.Response {
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()
	msg.Text("😓 推荐服务暂时不可用").Newline()
	msg.Newline()
	msg.Italic("💡 稍后再试或使用搜索功能")

	kb := services.NewKeyboardBuilder()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboardBuilder(kb),
	}
}

// buildRecommendationText builds the text message for recommendations.
func (p *RecommendationPresenter) buildRecommendationText(results []services.SearchResult, recType string) string {
	msg := services.NewMessageBuilder()
	msg.Bold("🎬 精选推荐").Newline()
	msg.Newline()

	title, subtitle := p.getRecommendationTitle(recType)
	msg.Italic(title).Newline()
	msg.Text(subtitle).Newline()
	msg.Newline()

	if len(results) == 0 {
		msg.Italic("💫 暂时没有找到相关内容").Newline()
		msg.Newline()
		msg.Text("试试其他分类，或许有惊喜哦")
		return msg.Build()
	}

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i, item := range results[:displayCount] {
		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf(" (%d)", item.Year)
		}

		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		mediaType := "🎬"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺"
		}

		msg.Textf("%d. %s%s%s%s", i+1, item.Title, year, mediaType, rating).Newline()
	}

	return msg.Build()
}

// getRecommendationTitle returns the title and subtitle for a recommendation type.
func (p *RecommendationPresenter) getRecommendationTitle(recType string) (title, subtitle string) {
	switch recType {
	case "trending":
		title = "🔥 本周热门"
		subtitle = "大家都在看的好片"
	case "hot":
		title = "📺 热门剧集"
		subtitle = "追剧必看热门番"
	case "toprated":
		title = "⭐ 必看神作"
		subtitle = "高分经典，不容错过"
	case "new":
		title = "🆕 最新上映"
		subtitle = "刚上线的新鲜内容"
	case "random":
		title = "🎲 随机探索"
		subtitle = "发现未知的精彩"
	default:
		title = "🎬 精选推荐"
		subtitle = "为您推荐优质内容"
	}
	return
}

// buildRecommendationKeyboard builds the inline keyboard for recommendations.
func (p *RecommendationPresenter) buildRecommendationKeyboard(results []services.SearchResult, recType string) *types.TelegramInlineKeyboard {
	kb := services.NewKeyboardBuilder()

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	if len(results) == 0 {
		kb.AddButton("🔥 热门电影", "search:type:trending")
		kb.AddButton("📺 热播剧集", "search:type:hot")
		kb.NewRow()
		kb.AddButton("⭐ 高分佳作", "search:type:toprated")
		kb.AddButton("🆕 最新上线", "search:type:new")
		kb.NewRow()
		kb.AddButton("🎲 随机发现", "search:type:random")
		kb.NewRow()
		kb.AddButton("⬅️ 返回主菜单", "start")
		return convertKeyboardBuilder(kb)
	}

	for i, item := range results[:displayCount] {
		mediaTypeForCallback := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaTypeForCallback = "tv"
		}
		kb.AddButton(fmt.Sprintf("%d", i+1), fmt.Sprintf("detail:id:%d:type:%s", item.ID, mediaTypeForCallback))

		if (i+1)%4 == 0 || i == displayCount-1 {
			kb.NewRow()
		}
	}

	kb.AddButton("🔄 换一批", fmt.Sprintf("search:type:%s", recType))
	kb.AddButton("⬅️ 返回主菜单", "start")
	kb.NewRow()
	kb.AddButton("🤖 其他推荐", "ai")

	return convertKeyboardBuilder(kb)
}

// SessionSearchResults saves search results to session and returns keyboard.
func (p *SearchPresenter) SessionSearchResults(userID int64, sessMgr *session.Manager, results []services.SearchResult, source string) *types.TelegramInlineKeyboard {
	sess := sessMgr.GetOrCreate(userID)
	searchItems := make([]session.SearchItem, 0, len(results.Results))
	if len(results.Results) > 8 {
		searchItems = make([]session.SearchItem, 8)
	}

	for i, item := range results.Results {
		if i >= 8 {
			break
		}
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems[i] = session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Rating:   item.Rating,
			Poster:   item.Poster,
			Overview: item.Overview,
		}
	}
	sess.SetSearchResults(searchItems, 1, source)

	return p.buildSearchKeyboard(results, source)
}

// convertKeyboardBuilder converts services.KeyboardBuilder to types.TelegramInlineKeyboard.
func convertKeyboardBuilder(kb *services.KeyboardBuilder) *types.TelegramInlineKeyboard {
	return &types.TelegramInlineKeyboard{
		InlineKeyboard: convertKeyboard(kb.Build()),
	}
}

// convertKeyboard converts callback.Keyboard to [][]types.TelegramInlineKeyboardButton.
func convertKeyboard(kb *callback.Keyboard) [][]types.TelegramInlineKeyboardButton {
	if kb == nil {
		return nil
	}

	rows := make([][]types.TelegramInlineKeyboardButton, len(kb.InlineKeyboard))
	for i, row := range kb.InlineKeyboard {
		buttons := make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			buttons[j] = types.TelegramInlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
			}
		}
		rows[i] = buttons
	}
	return rows
}
