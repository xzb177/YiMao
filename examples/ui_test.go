package main

import (
	"fmt"

	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/ui"
)

func main() {
	fmt.Println("=== YiMao UI 模块测试示例 ===\n")

	// 1. 测试主菜单（波普艺术风格）
	fmt.Println("【1. 主菜单 - 波普艺术风格】")
	fmt.Println(ui.BuildMenu("YiMao · 你的私人选片师", "今天想看什么？"))
	fmt.Println()

	// 2. 测试搜索结果（暗黑霓虹风格）
	fmt.Println("【2. 搜索结果 - 暗黑霓虹风格】")
	searchResults := []services.SearchResult{
		{
			ID:             1,
			Title:          "复仇者联盟",
			OriginalTitle:  "The Avengers",
			Year:           2012,
			Rating:         8.4,
			Type:           "movie",
			Overview:       "当邪恶威胁降临，地球最强超级英雄首次集结！",
			ReleaseDate:    "2012-05-04",
			Runtime:        143,
			Genres:         []string{"动作", "科幻"},
			Popularity:     85.5,
		},
		{
			ID:             2,
			Title:          "盗梦空间",
			OriginalTitle:  "Inception",
			Year:           2010,
			Rating:         9.3,
			Type:           "movie",
			Overview:       "造梦师柯布接受了一项艰巨的任务：在潜意识中植入一个想法。",
			ReleaseDate:    "2010-07-16",
			Runtime:        148,
			Genres:         []string{"科幻", "惊悚"},
			Popularity:     78.2,
		},
	}
	fmt.Println(ui.BuildSearchResults("科幻", searchResults, 1, 2))
	fmt.Println()

	// 3. 测试推荐内容（文艺胶片风格）
	fmt.Println("【3. 推荐内容 - 文艺胶片风格】")
	fmt.Println(ui.BuildRecommendation("今日推荐 · 治愈系电影", searchResults, "happy"))
	fmt.Println()

	// 4. 测试媒体详情（暗黑霓虹风格）
	fmt.Println("【4. 媒体详情 - 暗黑霓虹风格】")
	fmt.Println(ui.BuildMediaDetail(&searchResults[0]))
	fmt.Println()

	// 5. 测试请求列表（极简卡片风格）
	fmt.Println("【5. 请求列表 - 极简卡片风格】")
	requests := []services.SubscribeItem{
		{
			ID:           1,
			Name:         "复仇者联盟",
			Year:         "2012",
			Type:         "movie",
			State:        "downloading",
			Season:       0,
			TotalEpisode: 0,
			Date:         "2026-03-08 14:30:00",
		},
		{
			ID:           2,
			Name:         "绝命毒师",
			Year:         "2008",
			Type:         "tv",
			State:        "completed",
			Season:       1,
			TotalEpisode: 7,
			Date:         "2026-03-07 10:20:00",
		},
		{
			ID:           3,
			Name:         "沙丘",
			Year:         "2021",
			Type:         "movie",
			State:        "pending",
			Season:       0,
			TotalEpisode: 0,
			Date:         "2026-03-08 15:45:00",
		},
	}
	fmt.Println(ui.BuildRequestList(requests, 1, 1, 3))
	fmt.Println()

	// 6. 测试所有风格的菜单
	fmt.Println("【6. 各风格主菜单对比】")
	fmt.Println("--- 暗黑霓虹风 ---")
	fmt.Println(ui.NewBuilder(ui.StyleNeon).BuildMenu("测试", "副标题"))
	fmt.Println()
	fmt.Println("--- 文艺胶片风 ---")
	fmt.Println(ui.NewBuilder(ui.StyleFilm).BuildMenu("测试", "副标题"))
	fmt.Println()
	fmt.Println("--- 波普艺术风 ---")
	fmt.Println(ui.NewBuilder(ui.StylePop).BuildMenu("测试", "副标题"))
	fmt.Println()
	fmt.Println("--- 极简卡片风 ---")
	fmt.Println(ui.NewBuilder(ui.StyleCard).BuildMenu("测试", "副标题"))
	fmt.Println()
	fmt.Println("--- 沉浸电影风 ---")
	fmt.Println(ui.NewBuilder(ui.StyleCinema).BuildMenu("测试", "副标题"))
	fmt.Println()

	// 7. 测试键盘构建
	fmt.Println("【7. 键盘构建测试】")
	kb := ui.NewKeyboardBuilder(ui.StylePop).BuildMenuKeyboard()
	fmt.Printf("波普艺术菜单键盘: %+v\n", kb)

	kb = ui.NewKeyboardBuilder(ui.StyleNeon).BuildDetailKeyboard(true, true)
	fmt.Printf("暗黑霓虹详情键盘: %+v\n", kb)

	kb = ui.NewKeyboardBuilder(ui.StyleCard).BuildPaginationKeyboard(1, 3)
	fmt.Printf("极简卡片分页键盘: %+v\n", kb)

	fmt.Println("\n=== 测试完成 ===")
}
