package main

import (
	"fmt"

	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/ui"
)

func main() {
	fmt.Println("=== UI 优化测试 ===\n")

	// 1. 测试搜索结果构建器
	fmt.Println("【1. 搜索结果 - 暗黑霓虹风】")
	testSearchResultsBuilder()

	// 2. 测试媒体详情构建器
	fmt.Println("\n【2. 媒体详情 - 暗黑霓虹风】")
	testMediaDetailBuilder()

	// 3. 测试其他风格
	fmt.Println("\n【3. 其他风格对比】")
	testOtherStyles()

	fmt.Println("\n=== 测试完成 ===")
}

func testSearchResultsBuilder() {
	// 创建测试数据
	results := []services.SearchResult{
		{
			ID:             1,
			Title:          "复仇者联盟",
			OriginalTitle:  "The Avengers",
			Year:           2012,
			Rating:         8.4,
			Type:           "movie",
			Overview:       "超级英雄首次集结，共同对抗邪恶势力。当邪恶威胁降临，地球最强超级英雄首次联手！",
			ReleaseDate:    "2012-05-04",
			Runtime:        143,
			Genres:         []string{"动作", "科幻"},
			Popularity:     85.5,
		},
		{
			ID:             2,
			Title:          "复仇者联盟：终局之战",
			OriginalTitle:  "Avengers: Endgame",
			Year:           2019,
			Rating:         8.5,
			Type:           "movie",
			Overview:       "漫威十年布局的终极决战，复仇者联盟全员集结，与灭霸进行最终一战。",
			ReleaseDate:    "2019-04-26",
			Runtime:        181,
			Genres:         []string{"动作", "科幻", "冒险"},
			Popularity:     92.3,
		},
		{
			ID:             3,
			Title:          "复仇者联盟：无限战争",
			OriginalTitle:  "Avengers: Infinity War",
			Year:           2018,
			Rating:         8.4,
			Type:           "movie",
			Overview:       "宇宙最强反派灭霸登场，收集无限宝石，复仇者联盟遭遇前所未有的挑战。",
			ReleaseDate:    "2018-04-27",
			Runtime:        149,
			Genres:         []string{"动作", "科幻"},
			Popularity:     88.7,
		},
	}

	// 测试暗黑霓虹风格
	fmt.Println("--- 暗黑霓虹风 ---")
	neonBuilder := ui.NewSearchResultsBuilder(ui.StyleNeon)
	neonMessage := neonBuilder.BuildSearchResultsMessage("复仇者联盟", results, 1, 3)
	neonKeyboard := neonBuilder.BuildSearchKeyboard(results, 1, 1)

	fmt.Printf("消息长度: %d 字符\n", len(neonMessage))
	fmt.Printf("键盘行数: %d\n", len(neonKeyboard.Buttons))
	fmt.Println("前200字符:")
	fmt.Printf("%s\n", neonMessage[:min(200, len(neonMessage))])

	// 测试文艺胶片风格
	fmt.Println("\n--- 文艺胶片风 ---")
	filmBuilder := ui.NewSearchResultsBuilder(ui.StyleFilm)
	filmMessage := filmBuilder.BuildSearchResultsMessage("复仇者联盟", results, 1, 3)
	fmt.Printf("消息长度: %d 字符\n", len(filmMessage))
	fmt.Println("前200字符:")
	fmt.Printf("%s\n", filmMessage[:min(200, len(filmMessage))])

	// 测试波普艺术风格
	fmt.Println("\n--- 波普艺术风 ---")
	popBuilder := ui.NewSearchResultsBuilder(ui.StylePop)
	popMessage := popBuilder.BuildSearchResultsMessage("复仇者联盟", results, 1, 3)
	fmt.Printf("消息长度: %d 字符\n", len(popMessage))
	fmt.Println("前200字符:")
	fmt.Printf("%s\n", popMessage[:min(200, len(popMessage))])
}

func testMediaDetailBuilder() {
	// 创建测试数据
	info := &services.MediaInfo{
		ID:            2995,
		Title:         "复仇者联盟",
		OriginalTitle: "The Avengers",
		Year:          services.NewInt64(2012),
		Rating:        8.4,
		Type:          services.MediaTypeMovie,
		Overview:      "超级英雄首次集结，共同对抗邪恶势力。当邪恶威胁降临，地球最强超级英雄首次联手！钢铁侠、雷神、美国队长、绿巨人、黑寡妇、鹰眼六位英雄首次集结，组成复仇者联盟，共同对抗洛基及其外星军队的入侵。",
		ReleaseDate:   "2012-05-04",
		Runtime:       143,
		Genres:        []string{"动作", "科幻", "冒险"},
		Popularity:     85.5,
		Poster:        "/hHuJ7qoL4C9qfXNfbdgty8m.jpg",
		BackdropPath:  "/7RyHsO4yDXtBv1zZZ5stt5JH.jpg",
		VoteCount:     25000,
		VoteAverage:   8.4,
	}

	// 测试暗黑霓虹风格
	fmt.Println("--- 暗黑霓虹风 ---")
	neonBuilder := ui.NewMediaDetailBuilder(ui.StyleNeon)
	neonMessage := neonBuilder.BuildMediaDetailMessage(info)
	neonKeyboard := neonBuilder.BuildMediaDetailKeyboard(info, false, true)

	fmt.Printf("消息长度: %d 字符\n", len(neonMessage))
	fmt.Printf("键盘行数: %d\n", len(neonKeyboard.Buttons))
	fmt.Println("前300字符:")
	fmt.Printf("%s\n", neonMessage[:min(300, len(neonMessage))])

	// 测试文艺胶片风格
	fmt.Println("\n--- 文艺胶片风 ---")
	filmBuilder := ui.NewMediaDetailBuilder(ui.StyleFilm)
	filmMessage := filmBuilder.BuildMediaDetailMessage(info)
	fmt.Printf("消息长度: %d 字符\n", len(filmMessage))
	fmt.Println("前300字符:")
	fmt.Printf("%s\n", filmMessage[:min(300, len(filmMessage))])

	// 测试波普艺术风格
	fmt.Println("\n--- 波普艺术风 ---")
	popBuilder := ui.NewMediaDetailBuilder(ui.StylePop)
	popMessage := popBuilder.BuildMediaDetailMessage(info)
	fmt.Printf("消息长度: %d 字符\n", len(popMessage))
	fmt.Println("前300字符:")
	fmt.Printf("%s\n", popMessage[:min(300, len(popMessage))])
}

func testOtherStyles() {
	// 创建简单数据
	results := []services.SearchResult{
		{
			ID:        1,
			Title:     "测试影片",
			Year:      2024,
			Rating:    7.5,
			Type:      "movie",
			Overview:  "这是一个测试影片的简介。",
			Popularity: 50.0,
		},
	}

	// 测试所有风格
	styles := []ui.UIStyle{
		ui.StyleNeon,
		ui.StyleFilm,
		ui.StylePop,
	}

	styleNames := map[ui.UIStyle]string{
		ui.StyleNeon: "暗黑霓虹风",
		ui.StyleFilm: "文艺胶片风",
		ui.StylePop:  "波普艺术风",
	}

	for _, style := range styles {
		fmt.Printf("--- %s ---\n", styleNames[style])
		builder := ui.NewSearchResultsBuilder(style)
		message := builder.BuildSearchResultsMessage("测试", results, 1, 1)
		fmt.Printf("消息长度: %d 字符\n", len(message))
		fmt.Println("前150字符:")
		fmt.Printf("%s\n\n", message[:min(150, len(message))])
	}

	// 测试媒体详情的所有风格
	info := &services.MediaInfo{
		ID:       1,
		Title:    "测试影片",
		Rating:   7.5,
		Type:     services.MediaTypeMovie,
		Overview: "测试影片的简介",
		Genres:   []string{"测试"},
	}

	for _, style := range styles {
		fmt.Printf("--- %s ---\n", styleNames[style])
		builder := ui.NewMediaDetailBuilder(style)
		message := builder.BuildMediaDetailMessage(info)
		fmt.Printf("消息长度: %d 字符\n", len(message))
		fmt.Println("前150字符:")
		fmt.Printf("%s\n\n", message[:min(150, len(message))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
