package response_test

import (
	"testing"

	"emby-telegram-bot/bot/response"
)

// TestResponseBuilder tests the response builder functionality
func TestResponseBuilder(t *testing.T) {
	// Test basic success response
	resp := response.NewBuilder().
		WithType(response.ResponseTypeSuccess).
		WithTitle("操作成功").
		WithMessage("您的请求已成功处理").
		WithDetails("可以在「我的请求」中查看进度").
		Build()

	if resp.Type != response.ResponseTypeSuccess {
		t.Errorf("Expected type SUCCESS, got %v", resp.Type)
	}

	if resp.Title != "操作成功" {
		t.Errorf("Expected title '操作成功', got %v", resp.Title)
	}

	formatted := resp.Format()
	if formatted == "" {
		t.Error("Formatted response should not be empty")
	}

	t.Logf("Formatted response:\n%s", formatted)
}

// TestErrorResponse tests error response creation
func TestErrorResponse(t *testing.T) {
	resp := response.NewBuilder().
		WithType(response.ResponseTypeError).
		WithSeverity(response.SeverityHigh).
		WithTitle("请求失败").
		WithMessage("无法连接到服务器").
		WithDetails("请检查网络连接或稍后再试").
		WithSuggestions("检查网络设置", "联系管理员").
		WithAlert(true).
		Build()

	if resp.Severity != response.SeverityHigh {
		t.Errorf("Expected severity HIGH, got %v", resp.Severity)
	}

	if !resp.ShowAlert {
		t.Error("Expected ShowAlert to be true")
	}

	t.Logf("Error response:\n%s", resp.Format())
}

// TestTemplateRendering tests template-based responses
func TestTemplateRendering(t *testing.T) {
	// Test search in progress
	data := response.TemplateData{
		MediaTitle: "复仇者联盟",
	}
	resp := response.RenderTemplate(response.TemplateSearchInProgress, data)

	if resp.Type != response.ResponseTypeLoading {
		t.Errorf("Expected type LOADING, got %v", resp.Type)
	}

	t.Logf("Search in progress:\n%s", resp.Format())

	// Test quota exhausted
	quotaData := response.TemplateData{
		QuotaType:  "电影",
		QuotaUsed:  2,
		QuotaLimit: 2,
	}
	resp2 := response.RenderTemplate(response.TemplateRequestQuotaExhausted, quotaData)

	if resp2.Type != response.ResponseTypeError {
		t.Errorf("Expected type ERROR, got %v", resp2.Type)
	}

	t.Logf("Quota exhausted:\n%s", resp2.Format())
}

// TestHandlerOperations tests the response handler
func TestHandlerOperations(t *testing.T) {
	handler := response.NewHandler()
	defer handler.Shutdown()

	// Test progress tracking
	state := handler.StartProgress("req-123", "search", 3)

	if state.TotalSteps != 3 {
		t.Errorf("Expected 3 total steps, got %d", state.TotalSteps)
	}

	// Update progress
	handler.UpdateProgress("req-123", "正在搜索...", 1)
	handler.UpdateProgress("req-123", "处理结果...", 2)
	handler.CompleteProgress("req-123", "搜索完成")

	// Get final state
	finalState, exists := handler.GetProgress("req-123")
	if !exists {
		t.Error("Progress state should exist")
	}

	percentage := finalState.GetPercentage()
	if percentage != 100 {
		t.Errorf("Expected 100%% completion, got %.0f%%", percentage)
	}

	t.Logf("Progress test completed. Final percentage: %.0f%%", percentage)
}

// TestTracker tests the operation tracker
func TestTracker(t *testing.T) {
	tracker := response.NewTracker()
	defer tracker.Shutdown()

	// Create operation
	op := tracker.Create("test_operation", 12345, 67890)

	if op.Type != "test_operation" {
		t.Errorf("Expected type 'test_operation', got %v", op.Type)
	}

	// Create context
	ctx := response.NewContext(tracker, op)

	// Update progress
	err := ctx.Update(response.StatusRunning, 25, "第一步完成")
	if err != nil {
		t.Errorf("Update failed: %v", err)
	}

	err = ctx.Update(response.StatusRunning, 50, "第二步完成")
	if err != nil {
		t.Errorf("Update failed: %v", err)
	}

	// Complete operation
	err = ctx.Complete("操作完成")
	if err != nil {
		t.Errorf("Complete failed: %v", err)
	}

	// Convert to response
	resp := ctx.ToResponse()
	if resp.Type != response.ResponseTypeSuccess {
		t.Errorf("Expected success response, got %v", resp.Type)
	}

	t.Logf("Tracker test completed.\nFinal response:\n%s", resp.Format())
}

// TestResponseIntegration tests integration with bot types
func TestResponseIntegration(t *testing.T) {
	integration := response.NewIntegration()

	// Test various response types
	searchResp := integration.SearchInProgress("测试查询")
	if searchResp.Text == "" {
		t.Error("SearchInProgress response should not be empty")
	}

	successResp := integration.RequestSuccess("测试电影", "movie", 1, 2, 1, false)
	if successResp.Text == "" {
		t.Error("RequestSuccess response should not be empty")
	}

	quotaResp := integration.QuotaExhausted("movie", 2, 2)
	if quotaResp.ShowAlert != true {
		t.Error("QuotaExhausted response should show alert")
	}

	accountResp := integration.AccountNotLinked()
	if accountResp.Text == "" {
		t.Error("AccountNotLinked response should not be empty")
	}

	t.Logf("Integration test completed")
}

// TestConcurrentOperations tests concurrent access to handler and tracker
func TestConcurrentOperations(t *testing.T) {
	handler := response.NewHandler()
	tracker := response.NewTracker()
	defer handler.Shutdown()
	defer tracker.Shutdown()

	// Run concurrent operations
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(n int) {
			// Create operation
			op := tracker.Create("concurrent_op", int64(n), 67890)

			// Create progress
			state := handler.StartProgress(
				response.GenerateRequestID(),
				"operation",
				5,
			)

			// Simulate progress updates
			for j := 1; j <= 5; j++ {
				handler.UpdateProgress(state.RequestID, "Step", j)
			}
			handler.CompleteProgress(state.RequestID, "Done")

			// Complete operation
			tracker.Update(op.ID, response.StatusCompleted, 100, "Completed")

			done <- true
		}(i)
	}

	// Wait for all operations to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	t.Logf("Concurrent operations test completed successfully")
}

// TestActionButtons tests action button creation
func TestActionButtons(t *testing.T) {
	resp := response.NewBuilder().
		WithType(response.ResponseTypeInfo).
		WithTitle("选择操作").
		WithMessage("请选择一个操作").
		WithAction("action_1", "🔍 搜索", "search").
		WithAction("action_2", "📋 我的请求", "list").
		WithAction("action_3", "⚙️ 设置", "settings").
		Build()

	if len(resp.Actions) != 3 {
		t.Errorf("Expected 3 actions, got %d", len(resp.Actions))
	}

	// Use the exported GetKeyboardActions method instead
	actions := resp.GetKeyboardActions()
	if len(actions) != 3 {
		t.Errorf("Expected 3 keyboard actions, got %d", len(actions))
	}

	t.Logf("Actions: %d, Keyboard actions: %d", len(resp.Actions), len(actions))
}

// BenchmarkTemplateRendering benchmarks template rendering
func BenchmarkTemplateRendering(b *testing.B) {
	data := response.TemplateData{
		MediaTitle:     "复仇者联盟",
		MediaType:      "movie",
		QuotaUsed:      1,
		QuotaLimit:     2,
		QuotaRemaining: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = response.RenderTemplate(response.TemplateRequestSuccess, data)
	}
}

// BenchmarkResponseBuilder benchmarks response builder
func BenchmarkResponseBuilder(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = response.NewBuilder().
			WithType(response.ResponseTypeSuccess).
			WithTitle("操作成功").
			WithMessage("您的请求已成功处理").
			Build()
	}
}
