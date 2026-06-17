package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/xzb177/yimao/internal/richmessage"
)

func main() {
	// Get bot token from environment
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		fmt.Println("❌ TELEGRAM_BOT_TOKEN not set")
		return
	}
	
	// Get chat ID from environment
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if chatIDStr == "" {
		fmt.Println("❌ TELEGRAM_CHAT_ID not set")
		return
	}
	
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		fmt.Printf("❌ Invalid TELEGRAM_CHAT_ID: %v\n", err)
		return
	}
	
	// Create sender
	sender := richmessage.NewRichMessageSender(botToken, chatID)
	
	// Example 1: Media Info Card
	fmt.Println("🎴 Sending Media Info Card...")
	mediaInfo := richmessage.MediaInfo{
		Title:     "流浪地球3",
		Year:      2026,
		Rating:    8.5,
		Genres:    []string{"科幻", "冒险"},
		Overview:  "太阳即将毁灭，人类启动流浪地球计划，试图带着地球逃离太阳系，寻找新的家园。",
		TMDBID:    12345,
		MediaType: "movie",
	}
	
	if err := sender.SendMediaInfoCard(mediaInfo); err != nil {
		fmt.Printf("❌ Failed to send media info card: %v\n", err)
	} else {
		fmt.Println("✅ Media info card sent successfully!")
	}
	
	// Example 2: Subscription Dashboard
	fmt.Println("\n📊 Sending Subscription Dashboard...")
	subs := []richmessage.SubscriptionStatus{
		{Name: "流浪地球3", Status: "⬇️ 下载中", Progress: 70},
		{Name: "三体 S2", Status: "✅ 已入库", Progress: 100},
		{Name: "庆余年3", Status: "🔍 搜索中", Progress: 0},
	}
	
	if err := sender.SendSubscriptionDashboard(subs, 2, 5); err != nil {
		fmt.Printf("❌ Failed to send subscription dashboard: %v\n", err)
	} else {
		fmt.Println("✅ Subscription dashboard sent successfully!")
	}
	
	// Example 3: Custom Rich Message
	fmt.Println("\n🔧 Sending Custom Rich Message...")
	builder := richmessage.NewBuilder()
	builder.Heading("自定义消息", 2)
	builder.Paragraph("这是一个自定义的 Rich Message 示例")
	builder.Table(
		[]string{"功能", "状态"},
		[][]string{
			{"影视信息卡片", "✅ 已实现"},
			{"订阅状态仪表盘", "✅ 已实现"},
			{"自定义消息", "✅ 已实现"},
		},
	)
	builder.Details("点击查看详情", "这是折叠的内容，可以包含任意文本。", false)
	
	msg := builder.Build()
	if err := sender.SendCustomRichMessage(msg); err != nil {
		fmt.Printf("❌ Failed to send custom rich message: %v\n", err)
	} else {
		fmt.Println("✅ Custom rich message sent successfully!")
	}
	
	fmt.Println("\n🎉 All examples completed!")
}
