package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"emby-telegram-bot/internal/services"
)

func main() {
	// 配置路径
	dataDir := "./data"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}

	jsonFile := filepath.Join(dataDir, "feedback.json")
	dbFile := filepath.Join(dataDir, "feedback.db")

	log.Println("========================================")
	log.Println("反馈数据迁移工具")
	log.Println("========================================")
	log.Printf("JSON 文件: %s", jsonFile)
	log.Printf("数据库文件: %s", dbFile)
	log.Println("")

	// 检查 JSON 文件是否存在
	if _, err := os.Stat(jsonFile); os.IsNotExist(err) {
		log.Println("⚠️  JSON 文件不存在，无需迁移")
		return
	}

	// 检查数据库文件是否已存在
	if _, err := os.Stat(dbFile); err == nil {
		log.Println("⚠️  数据库文件已存在")
		log.Print("⚠️  是否继续？这将覆盖现有数据 (y/n): ")

		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			log.Println("❌ 迁移已取消")
			return
		}

		// 删除旧数据库
		os.Remove(dbFile)
		log.Println("✓ 已删除旧数据库文件")
	}

	// 读取 JSON 文件
	log.Println("📖 读取 JSON 文件...")
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("❌ 无法读取 JSON 文件: %v", err)
	}

	// 解析 JSON
	log.Println("🔍 解析 JSON 数据...")
	type OldIssue struct {
		ID          int64                      `json:"id"`
		UserID      int64                      `json:"user_id"`
		UserName    string                     `json:"user_name"`
		Title       string                     `json:"title"`
		Description string                     `json:"description"`
		Status      string                     `json:"status"`
		Priority    string                     `json:"priority"`
		MediaType   string                     `json:"media_type"`
		MediaID     string                     `json:"media_id"`
		MediaTitle  string                     `json:"media_title"`
		TmdbID      int                        `json:"tmdb_id"`
		CreatedAt   time.Time                  `json:"created_at"`
		UpdatedAt   time.Time                  `json:"updated_at"`
		Replies     []services.IssueReply      `json:"replies"`
	}

	var oldIssues map[int64]*OldIssue
	if err := json.Unmarshal(data, &oldIssues); err != nil {
		log.Fatalf("❌ 解析 JSON 失败: %v", err)
	}

	log.Printf("✓ 找到 %d 个反馈", len(oldIssues))
	log.Println("")

	// 初始化数据库
	log.Println("🗄️  初始化 SQLite 数据库...")
	feedbackDB, err := services.NewFeedbackDB(dataDir)
	if err != nil {
		log.Fatalf("❌ 初始化数据库失败: %v", err)
	}
	log.Println("✓ 数据库初始化成功")
	log.Println("")

	// 开始迁移
	log.Println("📦 开始迁移数据...")
	log.Println("")

	migrated := 0
	failed := 0
	now := time.Now()

	for id, oldIssue := range oldIssues {
		// 检查是否已存在
		if _, exists := feedbackDB.GetFeedback(id); exists {
			log.Printf("⏭️  跳过 #%d（已存在）", id)
			continue
		}

		// 转换为新格式
		feedback := &services.Feedback{
			ID:          oldIssue.ID,
			UserID:      oldIssue.UserID,
			UserName:    oldIssue.UserName,
			Title:       oldIssue.Title,
			Description: oldIssue.Description,
			IssueType:   inferIssueType(oldIssue.Title),
			Priority:    oldIssue.Priority,
			Status:      oldIssue.Status,
			MediaType:   oldIssue.MediaType,
			MediaID:     oldIssue.MediaID,
			MediaTitle:  oldIssue.MediaTitle,
			TmdbID:      oldIssue.TmdbID,
			CreatedAt:   oldIssue.CreatedAt,
			UpdatedAt:   oldIssue.UpdatedAt,
			Tags:        []string{},
			Images:      []string{},
		}

		// 创建反馈
		newID, err := feedbackDB.CreateFeedback(feedback)
		if err != nil {
			log.Printf("❌ 创建反馈 #%d 失败: %v", id, err)
			failed++
			continue
		}

		// 迁移回复
		for _, oldReply := range oldIssue.Replies {
			_, err := feedbackDB.AddReply(
				id,
				oldReply.AuthorID,
				oldReply.AuthorName,
				oldReply.Content,
				oldReply.Type,
			)
			if err != nil {
				log.Printf("❌ 迁移回复失败 (issue #%d): %v", id, err)
			}
		}

		migrated++
		if migrated%10 == 0 {
			log.Printf("  已迁移 %d / %d", migrated, len(oldIssues))
		}
	}

	log.Println("")
	log.Println("========================================")
	log.Println("迁移完成")
	log.Println("========================================")
	log.Printf("✓ 成功: %d", migrated)
	log.Printf("✗ 失败: %d", failed)
	log.Printf("📊 总数: %d", len(oldIssues))
	log.Println("")

	// 统计新数据库
	totalCount, _ := feedbackDB.CountAll()
	openCount, _ := feedbackDB.CountByStatus("open")
	fixedCount, _ := feedbackDB.CountByStatus("fixed")

	log.Println("📈 新数据库统计：")
	log.Printf("• 总反馈数: %d", totalCount)
	log.Printf("• 待处理: %d", openCount)
	log.Printf("• 已解决: %d", fixedCount)
	log.Println("")

	// 备份 JSON 文件
	backupFile := jsonFile + ".backup." + time.Now().Format("20060102_150405")
	if err := os.Rename(jsonFile, backupFile); err != nil {
		log.Printf("⚠️  无法备份 JSON 文件: %v", err)
	} else {
		log.Printf("✓ JSON 文件已备份到: %s", backupFile)
	}

	log.Println("")
	log.Println("✅ 迁移成功完成！")
	log.Println("")
	log.Println("📝 下一步：")
	log.Println("1. 检查迁移结果")
	log.Println("2. 如果一切正常，可以删除备份文件")
	log.Println("3. 更新配置文件，启用新的反馈功能")
}

// inferIssueType 从标题推断问题类型
func inferIssueType(title string) string {
	// 简单的关键词匹配
	keywordMap := map[string][]string{
		"video_quality": {"画质", "模糊", "马赛克", "分辨率", "画面"},
		"audio_quality": {"音频", "音质", "音量", "音画不同步", "声音"},
		"subtitle":     {"字幕", "翻译", "字符"},
		"search":       {"搜索", "找不到", "检索"},
		"playback":     {"播放", "卡顿", "无法播放", "缓冲"},
	}

	for issueType, keywords := range keywordMap {
		for _, kw := range keywords {
			if len(title) >= len(kw) {
				for i := 0; i <= len(title)-len(kw); i++ {
					match := true
					for j := 0; j < len(kw); j++ {
						if title[i+j] != kw[j] {
							match = false
							break
						}
					}
					if match {
						return issueType
					}
				}
			}
		}
	}

	return "other"
}
