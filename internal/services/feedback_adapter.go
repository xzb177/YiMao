package services

import (
	"fmt"
	"log"
)

// FeedbackAdapter 反馈适配器 - 连接旧功能和新功能
type FeedbackAdapter struct {
	issueService  *IssueService   // 旧的 JSON 存储
	feedbackDB    *FeedbackDB     // 新的 SQLite 存储
	useEnhanced   bool            // 是否使用增强功能
	templateService *FeedbackTemplateService
	similarityChecker *SimilarityChecker
}

// NewFeedbackAdapter 创建反馈适配器
func NewFeedbackAdapter(
	issueService *IssueService,
	feedbackDB *FeedbackDB,
	useEnhanced bool,
) *FeedbackAdapter {
	adapter := &FeedbackAdapter{
		issueService:   issueService,
		feedbackDB:     feedbackDB,
		useEnhanced:    useEnhanced,
		templateService: NewFeedbackTemplateService(),
		similarityChecker: NewSimilarityChecker(0.5),
	}
	return adapter
}

// SetUseEnhanced 设置是否使用增强功能
func (a *FeedbackAdapter) SetUseEnhanced(enabled bool) {
	a.useEnhanced = enabled
	log.Printf("[FeedbackAdapter] Enhanced mode: %v", enabled)
}

// CreateFeedback 创建反馈（兼容旧接口）
func (a *FeedbackAdapter) CreateFeedback(
	userID int64,
	userName, title, description string,
	mediaType, mediaID, mediaTitle string,
) (interface{}, error) {
	if a.useEnhanced && a.feedbackDB != nil {
		// 使用新功能（SQLite）
		feedback := &Feedback{
			UserID:       userID,
			UserName:     userName,
			Title:        title,
			Description:  description,
			IssueType:    a.inferIssueType(title),
			Priority:     "medium",
			Status:       "open",
			MediaType:    mediaType,
			MediaID:      mediaID,
			MediaTitle:   mediaTitle,
		}
		
		id, err := a.feedbackDB.CreateFeedback(feedback)
		if err != nil {
			return nil, err
		}
		feedback.ID = id
		return feedback, nil
	} else {
		// 使用旧功能（JSON）
		issue, err := a.issueService.CreateIssue(
			userID,
			userName,
			title,
			description,
			mediaType,
			mediaID,
			mediaTitle,
		)
		if err != nil {
			return nil, err
		}
		return issue, nil
	}
}

// GetFeedback 获取反馈（兼容旧接口）
func (a *FeedbackAdapter) GetFeedback(id int64) (interface{}, bool) {
	if a.useEnhanced && a.feedbackDB != nil {
		// 使用新功能
		return a.feedbackDB.GetFeedback(id)
	} else {
		// 使用旧功能
		return a.issueService.GetIssue(id)
	}
}

// GetUserFeedbacks 获取用户反馈列表（兼容旧接口）
func (a *FeedbackAdapter) GetUserFeedbacks(userID int64) []interface{} {
	if a.useEnhanced && a.feedbackDB != nil {
		// 使用新功能
		feedbacks, _ := a.feedbackDB.GetUserFeedbacks(userID)
		result := make([]interface{}, len(feedbacks))
		for i, fb := range feedbacks {
			result[i] = fb
		}
		return result
	} else {
		// 使用旧功能
		issues := a.issueService.GetUserIssues(userID)
		result := make([]interface{}, len(issues))
		for i, issue := range issues {
			result[i] = issue
		}
		return result
	}
}

// GetOpenFeedbacks 获取待处理反馈（兼容旧接口）
func (a *FeedbackAdapter) GetOpenFeedbacks() []interface{} {
	if a.useEnhanced && a.feedbackDB != nil {
		// 使用新功能
		feedbacks, _ := a.feedbackDB.GetFeedbacksByStatus("open")
		result := make([]interface{}, len(feedbacks))
		for i, fb := range feedbacks {
			result[i] = fb
		}
		return result
	} else {
		// 使用旧功能
		issues := a.issueService.GetOpenIssues()
		result := make([]interface{}, len(issues))
		for i, issue := range issues {
			result[i] = issue
		}
		return result
	}
}

// UpdateStatus 更新状态（兼容旧接口）
func (a *FeedbackAdapter) UpdateStatus(id int64, status string) error {
	if a.useEnhanced && a.feedbackDB != nil {
		// 使用新功能
		return a.feedbackDB.UpdateFeedbackStatus(id, status)
	} else {
		// 使用旧功能
		return a.issueService.UpdateStatus(id, IssueStatus(status))
	}
}

// AddReply 添加回复（兼容旧接口）
func (a *FeedbackAdapter) AddReply(
	feedbackID int64,
	authorID int64,
	authorName, content string,
) error {
	if a.useEnhanced && a.feedbackDB != nil {
		// 使用新功能
		_, err := a.feedbackDB.AddReply(feedbackID, authorID, authorName, content, "custom")
		return err
	} else {
		// 使用旧功能
		_, err := a.issueService.AddReply(feedbackID, authorID, authorName, content, "custom")
		return err
	}
}

// GetTemplates 获取模板（仅新功能）
func (a *FeedbackAdapter) GetTemplates(issueType string) []FeedbackTemplate {
	return a.templateService.GetTemplatesByType(issueType)
}

// GetTemplate 获取单个模板（仅新功能）
func (a *FeedbackAdapter) GetTemplate(templateID string) (*FeedbackTemplate, bool) {
	return a.templateService.GetTemplate(templateID)
}

// FindSimilarFeedbacks 查找相似反馈（仅新功能）
func (a *FeedbackAdapter) FindSimilarFeedbacks(
	userID int64,
	issueType string,
	ignoreID int64,
) ([]*Feedback, error) {
	if !a.useEnhanced || a.feedbackDB == nil {
		return nil, fmt.Errorf("enhanced mode not enabled")
	}
	
	return a.feedbackDB.FindSimilarFeedbacks(ignoreID, issueType, userID)
}

// AddImages 添加图片（仅新功能）
func (a *FeedbackAdapter) AddImages(feedbackID int64, imageUrls []string) error {
	if !a.useEnhanced || a.feedbackDB == nil {
		return fmt.Errorf("enhanced mode not enabled")
	}
	
	return a.feedbackDB.AddFeedbackImages(feedbackID, imageUrls)
}

// GetStatistics 获取统计信息（仅新功能）
func (a *FeedbackAdapter) GetStatistics() (*FeedbackStatistics, error) {
	if !a.useEnhanced || a.feedbackDB == nil {
		return nil, fmt.Errorf("enhanced mode not enabled")
	}
	
	return a.feedbackDB.GetFeedbackStatistics()
}

// ExportToCSV 导出为 CSV（仅新功能）
func (a *FeedbackAdapter) ExportToCSV() ([]byte, error) {
	if !a.useEnhanced || a.feedbackDB == nil {
		return nil, fmt.Errorf("enhanced mode not enabled")
	}
	
	feedbacks, err := a.feedbackDB.GetAllFeedbacks()
	if err != nil {
		return nil, err
	}
	
	return a.feedbackDB.ExportToCSV(feedbacks)
}

// ExportToExcel 导出为 Excel（仅新功能）
func (a *FeedbackAdapter) ExportToExcel() ([]byte, error) {
	if !a.useEnhanced || a.feedbackDB == nil {
		return nil, fmt.Errorf("enhanced mode not enabled")
	}
	
	feedbacks, err := a.feedbackDB.GetAllFeedbacks()
	if err != nil {
		return nil, err
	}
	
	return a.feedbackDB.ExportToExcel(feedbacks)
}

// MigrateOldDataFromJSON 从旧 JSON 迁移数据到 SQLite
func (a *FeedbackAdapter) MigrateOldDataFromJSON() error {
	if a.feedbackDB == nil || a.issueService == nil {
		return fmt.Errorf("services not initialized")
	}
	
	log.Printf("[FeedbackAdapter] Starting data migration...")
	
	// 获取所有旧数据
	allIssues := make([]*Issue, 0)
	// 需要在 IssueService 中添加 GetAllIssues() 方法
	// 这里假设我们可以通过某种方式获取所有 issues
	
	// 迁移每个 issue 到新数据库
	migrated := 0
	for _, issue := range allIssues {
		// 将 Issue 转换为 Feedback
		feedback := &Feedback{
			ID:          issue.ID,
			UserID:      issue.UserID,
			UserName:    issue.UserName,
			Title:       issue.Title,
			Description: issue.Description,
			IssueType:   a.inferIssueType(issue.Title),
			Priority:    string(issue.Priority),
			Status:      string(issue.Status),
			MediaType:   issue.MediaType,
			MediaID:     issue.MediaID,
			MediaTitle:  issue.MediaTitle,
			TmdbID:      issue.TmdbID,
			CreatedAt:   issue.CreatedAt,
			UpdatedAt:   issue.UpdatedAt,
		}
		
		// 保存到 SQLite
		_, err := a.feedbackDB.CreateFeedback(feedback)
		if err != nil {
			log.Printf("[FeedbackAdapter] Failed to migrate issue #%d: %v", issue.ID, err)
			continue
		}
		
		// 迁移回复
		for _, reply := range issue.Replies {
			_, err := a.feedbackDB.AddReply(
				issue.ID,
				reply.AuthorID,
				reply.AuthorName,
				reply.Content,
				reply.Type,
			)
			if err != nil {
				log.Printf("[FeedbackAdapter] Failed to migrate reply for issue #%d: %v", issue.ID, err)
			}
		}
		
		migrated++
	}
	
	log.Printf("[FeedbackAdapter] Migration completed: %d/%d issues migrated", migrated, len(allIssues))
	return nil
}

// inferIssueType 从标题推断问题类型
func (a *FeedbackAdapter) inferIssueType(title string) string {
	// 根据标题关键词推断类型
	if containsKeyword(title, "画质", "模糊", "马赛克", "分辨率") {
		return "video_quality"
	}
	if containsKeyword(title, "音频", "音质", "音量", "音画不同步") {
		return "audio_quality"
	}
	if containsKeyword(title, "字幕", "翻译") {
		return "subtitle"
	}
	if containsKeyword(title, "搜索", "找不到") {
		return "search"
	}
	if containsKeyword(title, "播放", "卡顿", "无法播放") {
		return "playback"
	}
	
	return "other"
}

// containsKeyword 检查是否包含关键词
func containsKeyword(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if len(text) >= len(kw) {
			for i := 0; i <= len(text)-len(kw); i++ {
				match := true
				for j := 0; j < len(kw); j++ {
					if text[i+j] != kw[j] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}

// GetIssueService 获取旧的 IssueService（用于兼容）
func (a *FeedbackAdapter) GetIssueService() *IssueService {
	return a.issueService
}

// GetFeedbackDB 获取新的 FeedbackDB
func (a *FeedbackAdapter) GetFeedbackDB() *FeedbackDB {
	return a.feedbackDB
}

// IsEnhancedMode 是否使用增强模式
func (a *FeedbackAdapter) IsEnhancedMode() bool {
	return a.useEnhanced
}
