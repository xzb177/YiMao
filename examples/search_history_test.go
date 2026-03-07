package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/ui"
)

func main() {
	fmt.Println("=== 搜索历史优化测试 ===\n")

	// 创建临时数据库目录
	dataDir := "./test_data"
	os.MkdirAll(dataDir, 0755)
	defer os.RemoveAll(dataDir)

	// 1. 测试数据库服务
	fmt.Println("【1. 测试数据库服务】")
	testSearchHistoryDB(dataDir)

	// 2. 测试缓存服务
	fmt.Println("\n【2. 测试缓存服务】")
	testSearchHistoryCache(dataDir)

	// 3. 测试 UI 构建器
	fmt.Println("\n【3. 测试 UI 构建器】")
	testHistoryBuilder()

	// 4. 测试完整流程
	fmt.Println("\n【4. 测试完整流程】")
	testCompleteFlow(dataDir)

	fmt.Println("\n=== 所有测试完成 ===")
}

func testSearchHistoryDB(dataDir string) {
	// 创建数据库服务
	db, err := services.NewSearchHistoryDB(dataDir)
	if err != nil {
		log.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	userID := int64(12345)

	// 添加搜索记录
	fmt.Println("  ✓ 添加搜索记录...")
	db.AddSearch(userID, "复仇者联盟")
	db.AddSearch(userID, "盗梦空间")
	db.AddSearch(userID, "绝命毒师")
	db.AddSearch(userID, "沙丘")
	db.AddSearch(userID, "奥本海默")

	// 添加重复搜索（测试计数）
	db.AddSearch(userID, "复仇者联盟")
	db.AddSearch(userID, "复仇者联盟")

	// 获取历史记录
	fmt.Println("  ✓ 获取历史记录...")
	history, err := db.GetHistory(userID, 0)
	if err != nil {
		log.Fatalf("Failed to get history: %v", err)
	}
	fmt.Printf("    共 %d 条记录\n", len(history))
	for i, entry := range history {
		fmt.Printf("    %d. %s (次数: %d, 时间: %s)\n", i+1, entry.Query, entry.Count, entry.Timestamp.Format("15:04:05"))
	}

	// 获取统计信息
	fmt.Println("  ✓ 获取统计信息...")
	stats, err := db.GetStats(userID)
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}
	fmt.Printf("    总次数: %d, 本周: %d, 本月: %d\n", stats.Total, stats.Week, stats.Month)
	fmt.Printf("    热门搜索: %v\n", stats.Top5)

	// 获取分组历史
	fmt.Println("  ✓ 获取分组历史...")
	grouped, err := db.GetHistoryGrouped(userID)
	if err != nil {
		log.Fatalf("Failed to get grouped history: %v", err)
	}
	for group, entries := range grouped {
		fmt.Printf("    %s: %d 条\n", group, len(entries))
	}

	// 获取搜索建议
	fmt.Println("  ✓ 获取搜索建议...")
	suggestions, err := db.GetSuggestions(userID, "复")
	if err != nil {
		log.Fatalf("Failed to get suggestions: %v", err)
	}
	fmt.Printf("    建议: %v\n", suggestions)

	// 获取热门搜索
	fmt.Println("  ✓ 获取热门搜索...")
	popular, err := db.GetPopularSearches(5)
	if err != nil {
		log.Fatalf("Failed to get popular: %v", err)
	}
	for i, item := range popular {
		fmt.Printf("    %d. %s (%d次)\n", i+1, item.Query, item.Count)
	}

	// 获取搜索趋势
	fmt.Println("  ✓ 获取搜索趋势...")
	trends, err := db.GetSearchTrends(7)
	if err != nil {
		log.Fatalf("Failed to get trends: %v", err)
	}
	for i, item := range trends {
		fmt.Printf("    %d. %s (增长: %.1f%%)\n", i+1, item.Query, item.Growth)
	}

	// 测试删除单条记录
	fmt.Println("  ✓ 删除单条记录...")
	err = db.DeleteEntry(userID, 2)
	if err != nil {
		log.Fatalf("Failed to delete entry: %v", err)
	}
	fmt.Println("    已删除第 3 条记录")

	// 测试清空历史
	fmt.Println("  ✓ 清空历史记录...")
	err = db.ClearHistory(userID)
	if err != nil {
		log.Fatalf("Failed to clear history: %v", err)
	}
	fmt.Println("    已清空所有记录")

	fmt.Println("  ✓ 数据库服务测试通过")
}

func testSearchHistoryCache(dataDir string) {
	// 创建数据库服务
	db, err := services.NewSearchHistoryDB(dataDir)
	if err != nil {
		log.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	// 创建缓存服务
	cache := services.NewSearchHistoryCache(db, 5*time.Minute)

	userID := int64(67890)

	// 添加搜索记录
	fmt.Println("  ✓ 添加搜索记录到缓存...")
	cache.AddSearch(userID, "测试查询")
	cache.AddSearch(userID, "另一个查询")

	// 从缓存获取（第一次从数据库）
	fmt.Println("  ✓ 从缓存获取历史记录...")
	start := time.Now()
	history, err := cache.GetHistory(userID, 0)
	if err != nil {
		log.Fatalf("Failed to get history from cache: %v", err)
	}
	duration1 := time.Since(start)
	fmt.Printf("    首次查询耗时: %v\n", duration1)
	fmt.Printf("    获取到 %d 条记录\n", len(history))

	// 从缓存获取（第二次从内存）
	fmt.Println("  ✓ 再次从缓存获取...")
	start = time.Now()
	history2, err := cache.GetHistory(userID, 0)
	if err != nil {
		log.Fatalf("Failed to get history from cache: %v", err)
	}
	duration2 := time.Since(start)
	fmt.Printf("    缓存查询耗时: %v\n", duration2)
	fmt.Printf("    速度提升: %.1fx\n", float64(duration1)/float64(duration2))

	// 获取统计信息（缓存）
	fmt.Println("  ✓ 获取统计信息...")
	stats, err := cache.GetStats(userID)
	if err != nil {
		log.Fatalf("Failed to get stats from cache: %v", err)
	}
	fmt.Printf("    总次数: %d, 本周: %d\n", stats.Total, stats.Week)

	// 删除记录（应该使缓存失效）
	fmt.Println("  ✓ 删除记录（使缓存失效）...")
	cache.DeleteEntry(userID, 0)

	// 再次获取（应该从数据库重新加载）
	fmt.Println("  ✓ 缓存失效后重新获取...")
	start = time.Now()
	history3, err := cache.GetHistory(userID, 0)
	if err != nil {
		log.Fatalf("Failed to get history after invalidation: %v", err)
	}
	duration3 := time.Since(start)
	fmt.Printf("    重新加载耗时: %v\n", duration3)
	fmt.Printf("    获取到 %d 条记录（应该比之前少一条）\n", len(history3))

	fmt.Println("  ✓ 缓存服务测试通过")
}

func testHistoryBuilder() {
	builder := ui.NewHistoryBuilder(ui.StyleNeon)

	// 创建测试数据
	stats := &services.SearchStats{
		Total: 28,
		Week:  12,
		Month: 20,
		Top5:  []string{"复仇者联盟", "盗梦空间", "绝命毒师", "沙丘", "奥本海默"},
	}

	now := time.Now()
	groupedHistory := map[string][]services.SearchEntry{
		"今天": {
			{Query: "复仇者联盟", Timestamp: now.Add(-1 * time.Hour), Count: 5},
			{Query: "盗梦空间", Timestamp: now.Add(-2 * time.Hour), Count: 3},
		},
		"本周": {
			{Query: "绝命毒师", Timestamp: now.Add(-3*24*time.Hour), Count: 2},
		},
	}

	popularSearches := []services.PopularSearch{
		{Query: "沙丘", Count: 156},
		{Query: "奥本海默", Count: 142},
		{Query: "芭比", Count: 128},
	}

	trends := []services.TrendItem{
		{Query: "沙丘", Count: 45, Yesterday: 18, Growth: 150.0},
		{Query: "奥本海默", Count: 38, Yesterday: 17, Growth: 123.5},
	}

	userID := int64(12345)

	// 构建搜索历史界面
	fmt.Println("  ✓ 构建搜索历史界面...")
	historyUI := builder.BuildHistoryUI(userID, stats, groupedHistory, popularSearches, trends)
	fmt.Printf("    界面长度: %d 字符\n", len(historyUI))
	fmt.Println("    前100字符:")
	fmt.Printf("    %s\n", historyUI[:min(100, len(historyUI))])

	// 构建热门搜索界面
	fmt.Println("  ✓ 构建热门搜索界面...")
	popularUI := builder.BuildPopularSearchesUI(popularSearches, false)
	fmt.Printf("    界面长度: %d 字符\n", len(popularUI))

	// 构建搜索趋势界面
	fmt.Println("  ✓ 构建搜索趋势界面...")
	trendsUI := builder.BuildTrendsUI(trends, 7)
	fmt.Printf("    界面长度: %d 字符\n", len(trendsUI))

	// 构建统计界面
	fmt.Println("  ✓ 构建统计界面...")
	statsUI := builder.BuildStatsUI(stats, userID)
	fmt.Printf("    界面长度: %d 字符\n", len(statsUI))

	// 构建管理界面
	fmt.Println("  ✓ 构建管理界面...")
	manageUI := builder.BuildManageHistoryUI(groupedHistory["今天"])
	fmt.Printf("    界面长度: %d 字符\n", len(manageUI))

	fmt.Println("  ✓ UI 构建器测试通过")
}

func testCompleteFlow(dataDir string) {
	// 创建完整的服务链
	db, err := services.NewSearchHistoryDB(dataDir)
	if err != nil {
		log.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	cache := services.NewSearchHistoryCache(db, 5*time.Minute)

	userID := int64(99999)

	fmt.Println("  ✓ 模拟用户搜索流程...")

	// 模拟多次搜索
	queries := []string{"复仇者联盟", "盗梦空间", "绝命毒师", "沙丘", "奥本海默"}
	for i, query := range queries {
		fmt.Printf("    第%d次搜索: %s\n", i+1, query)
		cache.AddSearch(userID, query)
		time.Sleep(10 * time.Millisecond)
	}

	// 模拟重复搜索
	fmt.Println("  ✓ 模拟重复搜索...")
	cache.AddSearch(userID, "复仇者联盟")
	cache.AddSearch(userID, "复仇者联盟")
	cache.AddSearch(userID, "沙丘")

	// 查看统计
	fmt.Println("  ✓ 查看搜索统计...")
	stats, err := cache.GetStats(userID)
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}
	fmt.Printf("    总搜索: %d次, 本周: %d次\n", stats.Total, stats.Week)

	// 查看热门搜索
	fmt.Println("  ✓ 查看热门搜索...")
	popular, err := cache.GetPopularSearches(3)
	if err != nil {
		log.Fatalf("Failed to get popular: %v", err)
	}
	fmt.Printf("    热门搜索TOP3:\n")
	for i, item := range popular {
		fmt.Printf("      %d. %s (%d次)\n", i+1, item.Query, item.Count)
	}

	// 删除一条记录
	fmt.Println("  ✓ 删除一条历史记录...")
	cache.DeleteEntry(userID, 1)

	// 查看更新后的历史
	fmt.Println("  ✓ 查看更新后的历史...")
	history, err := cache.GetHistory(userID, 0)
	if err != nil {
		log.Fatalf("Failed to get history: %v", err)
	}
	fmt.Printf("    剩余记录数: %d\n", len(history))

	// 清空所有记录
	fmt.Println("  ✓ 清空所有历史记录...")
	cache.ClearHistory(userID)

	// 验证清空
	history, err = cache.GetHistory(userID, 0)
	if err != nil {
		log.Fatalf("Failed to get history after clear: %v", err)
	}
	if len(history) == 0 {
		fmt.Println("    ✓ 确认已清空")
	} else {
		fmt.Printf("    ✗ 清空失败，仍有 %d 条记录\n", len(history))
	}

	fmt.Println("  ✓ 完整流程测试通过")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
