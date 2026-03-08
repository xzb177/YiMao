// Package ui provides keyboard building utilities for the bot.
package ui

import (
	"fmt"

	"emby-telegram-bot/pkg/types"
)

// KeyboardBuilder helps build inline keyboards for various bot interactions.
type KeyboardBuilder struct{}

// NewKeyboardBuilder creates a new keyboard builder.
func NewKeyboardBuilder() *KeyboardBuilder {
	return &KeyboardBuilder{}
}

// BuildSearchKeyboard builds a keyboard for search results.
func (b *KeyboardBuilder) BuildSearchKeyboard(itemCount int, query string) *types.TelegramInlineKeyboard {
	var rows [][]types.TelegramInlineKeyboardButton

	// Result buttons
	row := []types.TelegramInlineKeyboardButton{}
	for i := 1; i <= itemCount && i <= 8; i++ {
		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i),
			CallbackData: fmt.Sprintf("search:id:%d", i),
		})

		if len(row) == 4 || i == itemCount {
			rows = append(rows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	// Navigation row
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回主菜单", CallbackData: "start"},
	}
	if itemCount >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}
	rows = append(rows, navRow)

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}

// BuildRecommendationKeyboard builds a keyboard for recommendation results.
func (b *KeyboardBuilder) BuildRecommendationKeyboard(itemCount int, recType string) *types.TelegramInlineKeyboard {
	var rows [][]types.TelegramInlineKeyboardButton

	if itemCount == 0 {
		// Empty state keyboard - simplified
		rows = append(rows, []types.TelegramInlineKeyboardButton{
			{Text: "🔥 热门", CallbackData: "search:type:trending"},
			{Text: "⭐ 高分", CallbackData: "search:type:toprated"},
		})
		rows = append(rows, []types.TelegramInlineKeyboardButton{
			{Text: "🆕 新片", CallbackData: "search:type:new"},
			{Text: "🎲 随机", CallbackData: "search:type:random"},
		})
		rows = append(rows, []types.TelegramInlineKeyboardButton{
			{Text: "⬅️ 返回主菜单", CallbackData: "start"},
		})
		return &types.TelegramInlineKeyboard{
			InlineKeyboard: rows,
		}
	}

	// Result buttons
	row := []types.TelegramInlineKeyboardButton{}
	for i := 1; i <= itemCount && i <= 8; i++ {
		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i),
			CallbackData: fmt.Sprintf("detail:id:%d:type:movie", i), // Simplified
		})

		if len(row) == 4 || i == itemCount {
			rows = append(rows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	// Navigation row
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "🔄 换一批", CallbackData: fmt.Sprintf("search:type:%s", recType)},
		{Text: "⬅️ 返回", CallbackData: "start_ai"},
	}
	rows = append(rows, navRow)

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}

// BuildMoodKeyboard builds a keyboard for mood-based recommendations.
func (b *KeyboardBuilder) BuildMoodKeyboard() *types.TelegramInlineKeyboard {
	rows := [][]types.TelegramInlineKeyboardButton{
		{
			{Text: "😌 解压轻松", CallbackData: "search:type:hot:mood:relax"},
			{Text: "🤯 烧脑刺激", CallbackData: "search:type:toprated:mood:mindblow"},
		},
		{
			{Text: "😭 情绪共鸣", CallbackData: "search:type:trending:mood:emotional"},
			{Text: "🧘 治愈慢节奏", CallbackData: "search:type:new:mood:healing"},
		},
		{
			{Text: "⬅️ 返回主菜单", CallbackData: "start"},
		},
	}

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}

// BuildEmptyMoodKeyboard builds a keyboard for when mood recommendations return no results.
func (b *KeyboardBuilder) BuildEmptyMoodKeyboard() *types.TelegramInlineKeyboard {
	rows := [][]types.TelegramInlineKeyboardButton{
		{
			{Text: "😌 解压轻松", CallbackData: "search:type:hot:mood:relax"},
			{Text: "🤯 烧脑刺激", CallbackData: "search:type:toprated:mood:mindblow"},
		},
		{
			{Text: "😭 情绪共鸣", CallbackData: "search:type:trending:mood:emotional"},
			{Text: "🧘 治愈慢节奏", CallbackData: "search:type:new:mood:healing"},
		},
		{
			{Text: "⬅️ 返回主菜单", CallbackData: "start"},
		},
	}

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}

// BuildSearchHistoryKeyboard builds a keyboard for search history.
func (b *KeyboardBuilder) BuildSearchHistoryKeyboard(hasHistory bool) *types.TelegramInlineKeyboard {
	rows := [][]types.TelegramInlineKeyboardButton{
		{
			{Text: "⬅️ 返回主菜单", CallbackData: "start"},
		},
	}

	if hasHistory {
		rows = append(rows, []types.TelegramInlineKeyboardButton{
			{Text: "🗑️ 清空历史", CallbackData: "search:clear_history:1"},
		})
	}

	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}

// BuildNoResultsKeyboard builds a keyboard for no search results.
func (b *KeyboardBuilder) BuildNoResultsKeyboard() *types.TelegramInlineKeyboard {
	return &types.TelegramInlineKeyboard{
		InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
			{
				{Text: "🔥 热门搜索", CallbackData: "start_ai"},
			},
			{
				{Text: "⬅️ 返回主菜单", CallbackData: "start"},
			},
		},
	}
}
