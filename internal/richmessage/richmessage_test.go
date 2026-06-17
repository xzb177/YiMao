package richmessage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuilder(t *testing.T) {
	builder := NewBuilder()
	
	// Test heading
	builder.Heading("测试标题", 2)
	
	// Test paragraph
	builder.Paragraph("这是一段普通文本")
	
	// Test bold paragraph
	builder.BoldParagraph("这是加粗文本")
	
	// Test table
	headers := []string{"列1", "列2", "列3"}
	rows := [][]string{
		{"A1", "B1", "C1"},
		{"A2", "B2", "C2"},
	}
	builder.Table(headers, rows)
	
	// Test details (closed by default)
	builder.Details("点击展开", "这是折叠内容", false)
	
	// Test details (open by default)
	builder.Details("默认展开", "这是展开的内容", true)
	
	// Test divider
	builder.Divider()
	
	// Build
	msg := builder.Build()
	
	// Verify markdown is not empty
	if msg.Markdown == "" {
		t.Error("Markdown should not be empty")
	}
	
	// Verify it contains details syntax
	if !strings.Contains(msg.Markdown, "<details><summary>") {
		t.Error("Should contain closed details syntax")
	}
	
	if !strings.Contains(msg.Markdown, "<details open><summary>") {
		t.Error("Should contain open details syntax")
	}
	
	// Test JSON serialization
	jsonData, err := builder.ToJSON()
	if err != nil {
		t.Errorf("JSON serialization error: %v", err)
	}
	
	// Verify JSON is valid
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Errorf("Invalid JSON: %v", err)
	}
	
	fmt.Println("✅ All tests passed!")
}

func TestBuilderReset(t *testing.T) {
	builder := NewBuilder()
	builder.Heading("标题1", 2)
	builder.Reset()
	builder.Heading("标题2", 2)
	
	msg := builder.Build()
	if !strings.Contains(msg.Markdown, "标题2") {
		t.Error("Should contain title2 after reset")
	}
	if strings.Contains(msg.Markdown, "标题1") {
		t.Error("Should not contain title1 after reset")
	}
	
	fmt.Println("✅ Reset test passed!")
}

func TestHeadingLevelValidation(t *testing.T) {
	builder := NewBuilder()
	
	// Test level 0 (should be clamped to 1)
	builder.Heading("标题", 0)
	msg := builder.Build()
	if !strings.Contains(msg.Markdown, "# 标题") {
		t.Error("Level 0 should be clamped to 1")
	}
	
	// Test level 7 (should be clamped to 6)
	builder.Reset()
	builder.Heading("标题", 7)
	msg = builder.Build()
	if !strings.Contains(msg.Markdown, "###### 标题") {
		t.Error("Level 7 should be clamped to 6")
	}
	
	fmt.Println("✅ Heading level validation test passed!")
}

func TestEmptyTableHeaders(t *testing.T) {
	builder := NewBuilder()
	builder.Table([]string{}, [][]string{{"A", "B"}})
	msg := builder.Build()
	
	// Should not panic and should be empty
	if msg.Markdown != "" {
		t.Error("Empty headers should produce empty table")
	}
	
	fmt.Println("✅ Empty table headers test passed!")
}

func TestMediaInfoCard(t *testing.T) {
	info := MediaInfo{
		Title:     "流浪地球3",
		Year:      2026,
		Rating:    8.5,
		Genres:    []string{"科幻", "冒险"},
		Overview:  "太阳即将毁灭，人类启动流浪地球计划...",
		TMDBID:    12345,
		MediaType: "movie",
	}
	
	msg := BuildMediaInfoCard(info)
	
	// Verify markdown is not empty
	if msg.Markdown == "" {
		t.Error("Markdown should not be empty")
	}
	
	// Verify it contains expected content
	if !strings.Contains(msg.Markdown, "流浪地球3") {
		t.Error("Should contain title")
	}
	
	if !strings.Contains(msg.Markdown, "8.5") {
		t.Error("Should contain rating")
	}
	
	if !strings.Contains(msg.Markdown, "2026") {
		t.Error("Should contain year")
	}
	
	// Verify it contains details syntax
	if !strings.Contains(msg.Markdown, "<details><summary>") {
		t.Error("Should contain details syntax")
	}
	
	fmt.Println("✅ Media info card test passed!")
}

func TestMediaInfoCardEmptyTitle(t *testing.T) {
	info := MediaInfo{
		Title:  "",
		Year:   2026,
		Rating: 8.5,
	}
	
	msg := BuildMediaInfoCard(info)
	if !strings.Contains(msg.Markdown, "未知影视") {
		t.Error("Empty title should be replaced with '未知影视'")
	}
	
	fmt.Println("✅ Media info card empty title test passed!")
}

func TestSubscriptionDashboard(t *testing.T) {
	subs := []SubscriptionStatus{
		{Name: "流浪地球3", Status: "⬇️ 下载中", Progress: 70},
		{Name: "三体 S2", Status: "✅ 已入库", Progress: 100},
		{Name: "庆余年3", Status: "🔍 搜索中", Progress: 0},
	}
	
	msg := BuildSubscriptionDashboard(subs, 2, 5)
	
	// Verify markdown is not empty
	if msg.Markdown == "" {
		t.Error("Markdown should not be empty")
	}
	
	// Verify it contains expected content
	if !strings.Contains(msg.Markdown, "我的订阅状态") {
		t.Error("Should contain heading")
	}
	
	if !strings.Contains(msg.Markdown, "流浪地球3") {
		t.Error("Should contain subscription name")
	}
	
	if !strings.Contains(msg.Markdown, "70%") {
		t.Error("Should contain progress")
	}
	
	fmt.Println("✅ Subscription dashboard test passed!")
}

func TestSubscriptionDashboardEmpty(t *testing.T) {
	subs := []SubscriptionStatus{}
	
	msg := BuildSubscriptionDashboard(subs, 0, 0)
	if !strings.Contains(msg.Markdown, "暂无订阅") {
		t.Error("Empty subs should show '暂无订阅'")
	}
	
	fmt.Println("✅ Subscription dashboard empty test passed!")
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		progress int
		expected string
	}{
		{0, "░░░░░░░░░░ 0%"},
		{50, "█████░░░░░ 50%"},
		{100, "██████████ 100%"},
		{150, "██████████ 100%"},
		{-10, "░░░░░░░░░░ 0%"},
	}
	
	for _, test := range tests {
		result := buildProgressBar(test.progress)
		if result != test.expected {
			t.Errorf("Progress %d: expected %q, got %q", test.progress, test.expected, result)
		}
	}
	
	fmt.Println("✅ Progress bar test passed!")
}

func TestSendRichMessageValidation(t *testing.T) {
	// Test empty bot token
	err := SendRichMessage("", 12345, RichMessage{Markdown: "test"})
	if err == nil {
		t.Error("Should return error for empty bot token")
	}
	
	// Test zero chat ID
	err = SendRichMessage("token", 0, RichMessage{Markdown: "test"})
	if err == nil {
		t.Error("Should return error for zero chat ID")
	}
	
	// Test empty markdown
	err = SendRichMessage("token", 12345, RichMessage{Markdown: ""})
	if err == nil {
		t.Error("Should return error for empty markdown")
	}
	
	fmt.Println("✅ Validation test passed!")
}
