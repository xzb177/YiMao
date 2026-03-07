package ui

import (
	"fmt"
	"emby-telegram-bot/internal/callback"
)

// KeyboardBuilder 键盘构建器
type KeyboardBuilder struct {
	style UIStyle
}

// NewKeyboardBuilder 创建键盘构建器
func NewKeyboardBuilder(style UIStyle) *KeyboardBuilder {
	return &KeyboardBuilder{style: style}
}

// BuildMenuKeyboard 构建主菜单键盘
func (kb *KeyboardBuilder) BuildMenuKeyboard() *callback.Keyboard {
	switch kb.style {
	case StylePop:
		return kb.buildPopMenuKeyboard()
	case StyleNeon:
		return kb.buildNeonMenuKeyboard()
	case StyleFilm:
		return kb.buildFilmMenuKeyboard()
	default:
		return kb.buildPopMenuKeyboard()
	}
}

// BuildSearchKeyboard 构建搜索结果键盘
func (kb *KeyboardBuilder) BuildSearchKeyboard(results []interface{}, page, totalPages int) *callback.Keyboard {
	switch kb.style {
	case StyleNeon:
		return kb.buildNeonSearchKeyboard(results, page, totalPages)
	case StylePop:
		return kb.buildPopSearchKeyboard(results, page, totalPages)
	case StyleFilm:
		return kb.buildFilmSearchKeyboard(results, page, totalPages)
	default:
		return kb.buildNeonSearchKeyboard(results, page, totalPages)
	}
}

// BuildDetailKeyboard 构建详情页键盘
func (kb *KeyboardBuilder) BuildDetailKeyboard(hasRequests bool, canRequest bool) *callback.Keyboard {
	switch kb.style {
	case StyleNeon:
		return kb.buildNeonDetailKeyboard(hasRequests, canRequest)
	case StylePop:
		return kb.buildPopDetailKeyboard(hasRequests, canRequest)
	case StyleFilm:
		return kb.buildFilmDetailKeyboard(hasRequests, canRequest)
	default:
		return kb.buildNeonDetailKeyboard(hasRequests, canRequest)
	}
}

// BuildPaginationKeyboard 构建分页键盘
func (kb *KeyboardBuilder) BuildPaginationKeyboard(page, totalPages int) *callback.Keyboard {
	var rows [][]callback.Button

	// 上一页按钮
	if page > 1 {
		rows = append(rows, []callback.Button{
			{Text: "⬅️ 上一页", CallbackData: callback.BuildCallback("page", map[string]string{"page": fmt.Sprint(page - 1)})},
		})
	}

	// 页码指示器
	rows = append(rows, []callback.Button{
		{Text: fmt.Sprintf("%d/%d", page, totalPages), CallbackData: callback.BuildCallback("page", map[string]string{"page": fmt.Sprint(page)})},
	})

	// 下一页按钮
	if page < totalPages {
		rows = append(rows, []callback.Button{
			{Text: "下一页 ➡️", CallbackData: callback.BuildCallback("page", map[string]string{"page": fmt.Sprint(page + 1)})},
		})
	}

	return &callback.Keyboard{InlineKeyboard: rows}
}

// 波普艺术风格主菜单键盘
func (kb *KeyboardBuilder) buildPopMenuKeyboard() *callback.Keyboard {
	return &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{
				{Text: "🔥 搜索", CallbackData: "search"},
				{Text: "🎲 随机", CallbackData: "random"},
			},
			{
				{Text: "📊 排行", CallbackData: "trending"},
				{Text: "📋 请求", CallbackData: "myreqs"},
			},
			{
				{Text: "🧠 智能推荐", CallbackData: "ai"},
				{Text: "😺 不纠结", CallbackData: "nohesitate"},
			},
			{
				{Text: "⚙️ 设置", CallbackData: "settings"},
			},
		},
	}
}

// 暗黑霓虹风格主菜单键盘
func (kb *KeyboardBuilder) buildNeonMenuKeyboard() *callback.Keyboard {
	return &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{
				{Text: "🔍 搜索", CallbackData: "search"},
				{Text: "🎬 推荐", CallbackData: "recommend"},
			},
			{
				{Text: "📋 我的请求", CallbackData: "myreqs"},
				{Text: "⚙️ 设置", CallbackData: "settings"},
			},
		},
	}
}

// 文艺胶片风格主菜单键盘
func (kb *KeyboardBuilder) buildFilmMenuKeyboard() *callback.Keyboard {
	return &callback.Keyboard{
		InlineKeyboard: [][]callback.Button{
			{
				{Text: "🔍 搜索", CallbackData: "search"},
			},
			{
				{Text: "🎬 求片", CallbackData: "request"},
			},
			{
				{Text: "📋 我的请求", CallbackData: "myreqs"},
			},
		},
	}
}

// 暗黑霓虹风格搜索结果键盘
func (kb *KeyboardBuilder) buildNeonSearchKeyboard(results []interface{}, page, totalPages int) *callback.Keyboard {
	var rows [][]callback.Button

	// 数字按钮（每行4个）
	const buttonsPerRow = 4
	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         fmt.Sprint(i + 1),
			CallbackData: fmt.Sprintf("result:%d", i),
		})
	}

	// 分页行
	paginationRow := []callback.Button{}
	if page > 1 {
		paginationRow = append(paginationRow, callback.Button{
			Text:         "⬅️ 上一页",
			CallbackData: fmt.Sprintf("page:%d", page-1),
		})
	}
	paginationRow = append(paginationRow, callback.Button{
		Text:         fmt.Sprintf("%d/%d", page, totalPages),
		CallbackData: fmt.Sprintf("page:%d", page),
	})
	if page < totalPages {
		paginationRow = append(paginationRow, callback.Button{
			Text:         "下一页 ➡️",
			CallbackData: fmt.Sprintf("page:%d", page+1),
		})
	}
	rows = append(rows, paginationRow)

	// 返回按钮
	rows = append(rows, []callback.Button{
		{Text: "🔍 返回搜索", CallbackData: "search"},
		{Text: "⬅️ 主菜单", CallbackData: "start"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// 波普艺术风格搜索结果键盘
func (kb *KeyboardBuilder) buildPopSearchKeyboard(results []interface{}, page, totalPages int) *callback.Keyboard {
	var rows [][]callback.Button

	// 数字按钮（每行4个）
	const buttonsPerRow = 4
	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         fmt.Sprint(i + 1),
			CallbackData: fmt.Sprintf("result:%d", i),
		})
	}

	// 分页和返回
	paginationRow := []callback.Button{}
	if page > 1 {
		paginationRow = append(paginationRow, callback.Button{
			Text:         "⬅️",
			CallbackData: fmt.Sprintf("page:%d", page-1),
		})
	}
	paginationRow = append(paginationRow, callback.Button{
		Text:         fmt.Sprintf("%d/%d", page, totalPages),
		CallbackData: fmt.Sprintf("page:%d", page),
	})
	if page < totalPages {
		paginationRow = append(paginationRow, callback.Button{
			Text:         "➡️",
			CallbackData: fmt.Sprintf("page:%d", page+1),
		})
	}
	rows = append(rows, paginationRow)

	rows = append(rows, []callback.Button{
		{Text: "🔥 搜索", CallbackData: "search"},
		{Text: "🎲 随机", CallbackData: "random"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// 文艺胶片风格搜索结果键盘
func (kb *KeyboardBuilder) buildFilmSearchKeyboard(results []interface{}, page, totalPages int) *callback.Keyboard {
	var rows [][]callback.Button

	// 数字按钮（每行3个）
	const buttonsPerRow = 3
	displayCount := len(results)
	if displayCount > 6 {
		displayCount = 6
	}

	for i := 0; i < displayCount; i++ {
		if i%buttonsPerRow == 0 {
			rows = append(rows, []callback.Button{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
			Text:         fmt.Sprint(i + 1),
			CallbackData: fmt.Sprintf("result:%d", i),
		})
	}

	// 分页和返回
	paginationRow := []callback.Button{}
	if page > 1 {
		paginationRow = append(paginationRow, callback.Button{
			Text:         "◀",
			CallbackData: fmt.Sprintf("page:%d", page-1),
		})
	}
	paginationRow = append(paginationRow, callback.Button{
		Text:         fmt.Sprintf("%d/%d", page, totalPages),
		CallbackData: fmt.Sprintf("page:%d", page),
	})
	if page < totalPages {
		paginationRow = append(paginationRow, callback.Button{
			Text:         "▶",
			CallbackData: fmt.Sprintf("page:%d", page+1),
		})
	}
	rows = append(rows, paginationRow)

	rows = append(rows, []callback.Button{
		{Text: "🎬 求片", CallbackData: "request"},
		{Text: "⬅️ 返回", CallbackData: "start"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// 暗黑霓虹风格详情页键盘
func (kb *KeyboardBuilder) buildNeonDetailKeyboard(hasRequests bool, canRequest bool) *callback.Keyboard {
	var rows [][]callback.Button

	if canRequest {
		rows = append(rows, []callback.Button{
			{Text: "🔥 求片", CallbackData: "request"},
			{Text: "📺 添加订阅", CallbackData: "subscribe"},
		})
	}

	if hasRequests {
		rows = append(rows, []callback.Button{
			{Text: "📋 我的请求", CallbackData: "myreqs"},
		})
	}

	rows = append(rows, []callback.Button{
		{Text: "⬅️ 返回", CallbackData: "back"},
		{Text: "🏠 主菜单", CallbackData: "start"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// 波普艺术风格详情页键盘
func (kb *KeyboardBuilder) buildPopDetailKeyboard(hasRequests bool, canRequest bool) *callback.Keyboard {
	var rows [][]callback.Button

	if canRequest {
		rows = append(rows, []callback.Button{
			{Text: "💥 求片", CallbackData: "request"},
			{Text: "🎬 订阅", CallbackData: "subscribe"},
		})
	}

	rows = append(rows, []callback.Button{
		{Text: "⬅️ 返回", CallbackData: "back"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}

// 文艺胶片风格详情页键盘
func (kb *KeyboardBuilder) buildFilmDetailKeyboard(hasRequests bool, canRequest bool) *callback.Keyboard {
	var rows [][]callback.Button

	if canRequest {
		rows = append(rows, []callback.Button{
			{Text: "🎬 求片", CallbackData: "request"},
		})
	}

	if hasRequests {
		rows = append(rows, []callback.Button{
			{Text: "📋 我的请求", CallbackData: "myreqs"},
		})
	}

	rows = append(rows, []callback.Button{
		{Text: "◀ 返回", CallbackData: "back"},
	})

	return &callback.Keyboard{InlineKeyboard: rows}
}
