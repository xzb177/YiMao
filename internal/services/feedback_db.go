package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// FeedbackDB 数据库存储服务
type FeedbackDB struct {
	db       *sql.DB
	filePath string
	mu       sync.RWMutex
}

// NewFeedbackDB 创建反馈数据库服务
func NewFeedbackDB(dataDir string) (*FeedbackDB, error) {
	dbPath := fmt.Sprintf("%s/feedback.db", dataDir)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	fb := &FeedbackDB{
		db:       db,
		filePath: dbPath,
	}

	// 初始化表结构
	if err := fb.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	// 启动清理协程
	go fb.cleanupRoutine()

	return fb, nil
}

// initTables 初始化表结构
func (fb *FeedbackDB) initTables() error {
	schemas := []string{
		// 反馈表
		`CREATE TABLE IF NOT EXISTS feedback (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			issue_type TEXT NOT NULL,
			priority TEXT NOT NULL DEFAULT 'medium',
			status TEXT NOT NULL DEFAULT 'open',
			media_type TEXT,
			media_id TEXT,
			media_title TEXT,
			tmdb_id INTEGER,
			tags TEXT,
			images TEXT,
			template_used TEXT,
			similar_feedback_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// 回复表
		`CREATE TABLE IF NOT EXISTS feedback_replies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			feedback_id INTEGER NOT NULL,
			author_id INTEGER NOT NULL,
			author_name TEXT NOT NULL,
			content TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'custom',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (feedback_id) REFERENCES feedback(id) ON DELETE CASCADE
		)`,

		// 标签表
		`CREATE TABLE IF NOT EXISTS feedback_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			category TEXT NOT NULL,
			usage_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// 反馈-标签关联表
		`CREATE TABLE IF NOT EXISTS feedback_tag_relations (
			feedback_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (feedback_id, tag_id),
			FOREIGN KEY (feedback_id) REFERENCES feedback(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES feedback_tags(id) ON DELETE CASCADE
		)`,

		// 索引
		`CREATE INDEX IF NOT EXISTS idx_feedback_user ON feedback(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_status ON feedback(status)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_type ON feedback(issue_type)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_created ON feedback(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_replies_feedback ON feedback_replies(feedback_id)`,
	}

	for _, schema := range schemas {
		if _, err := fb.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// 插入默认标签
	if err := fb.initDefaultTags(); err != nil {
		log.Printf("[FeedbackDB] Failed to init default tags: %v", err)
	}

	return nil
}

// initDefaultTags 初始化默认标签
func (fb *FeedbackDB) initDefaultTags() error {
	defaultTags := []struct {
		name     string
		category string
	}{
		// 严重程度
		{"轻微", "severity"},
		{"一般", "severity"},
		{"严重", "severity"},
		{"紧急", "severity"},

		// 设备类型
		{"Web", "device"},
		{"iOS", "device"},
		{"Android", "device"},
		{"TV", "device"},

		// 网络类型
		{"WiFi", "network"},
		{"4G", "network"},
		{"5G", "network"},
		{"其他", "network"},

		// 问题分类
		{"画质", "category"},
		{"音频", "category"},
		{"字幕", "category"},
		{"搜索", "category"},
		{"播放", "category"},
		{"其他", "category"},
	}

	for _, tag := range defaultTags {
		_, err := fb.db.Exec(
			`INSERT OR IGNORE INTO feedback_tags (name, category) VALUES (?, ?)`,
			tag.name, tag.category,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// Feedback 反馈记录
type Feedback struct {
	ID                int64         `json:"id"`
	UserID            int64         `json:"user_id"`
	UserName          string        `json:"user_name"`
	Title             string        `json:"title"`
	Description       string        `json:"description"`
	IssueType         string        `json:"issue_type"`
	Priority          string        `json:"priority"`
	Status            string        `json:"status"`
	MediaType         string        `json:"media_type"`
	MediaID           string        `json:"media_id"`
	MediaTitle        string        `json:"media_title"`
	TmdbID            int           `json:"tmdb_id"`
	Tags              []string      `json:"tags"`
	Images            []string      `json:"images"`
	TemplateUsed      string        `json:"template_used"`
	SimilarFeedbackID int64         `json:"similar_feedback_id,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	Replies           []FeedbackReply `json:"replies,omitempty"`
}

// FeedbackReply 反馈回复
type FeedbackReply struct {
	ID         int64     `json:"id"`
	FeedbackID int64     `json:"feedback_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	Type       string    `json:"type"` // "template", "custom"
	CreatedAt  time.Time `json:"created_at"`
}

// FeedbackTag 标签
type FeedbackTag struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	UsageCount int       `json:"usage_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// CreateFeedback 创建反馈
func (fb *FeedbackDB) CreateFeedback(feedback *Feedback) (int64, error) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	// 序列化数组
	tagsJSON, _ := json.Marshal(feedback.Tags)
	imagesJSON, _ := json.Marshal(feedback.Images)

	now := time.Now()

	result, err := fb.db.Exec(`
		INSERT INTO feedback (
			user_id, user_name, title, description, issue_type,
			priority, status, media_type, media_id, media_title,
			tmdb_id, tags, images, template_used, similar_feedback_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		feedback.UserID, feedback.UserName, feedback.Title, feedback.Description,
		feedback.IssueType, feedback.Priority, feedback.Status, feedback.MediaType,
		feedback.MediaID, feedback.MediaTitle, feedback.TmdbID, string(tagsJSON),
		string(imagesJSON), feedback.TemplateUsed, feedback.SimilarFeedbackID,
		now, now,
	)

	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// 关联标签
	if len(feedback.Tags) > 0 {
		fb.associateTags(id, feedback.Tags)
	}

	log.Printf("[FeedbackDB] Created feedback #%d by user %d", id, feedback.UserID)
	return id, nil
}

// associateTags 关联标签
func (fb *FeedbackDB) associateTags(feedbackID int64, tags []string) error {
	for _, tagName := range tags {
		// 获取或创建标签
		var tagID int64
		err := fb.db.QueryRow("SELECT id FROM feedback_tags WHERE name = ?", tagName).Scan(&tagID)
		if err == sql.ErrNoRows {
			// 创建新标签
			result, err := fb.db.Exec(
				"INSERT INTO feedback_tags (name, category, usage_count) VALUES (?, 'custom', 1)",
				tagName,
			)
			if err != nil {
				return err
			}
			tagID, _ = result.LastInsertId()
		} else if err != nil {
			return err
		} else {
			// 更新使用计数
			fb.db.Exec("UPDATE feedback_tags SET usage_count = usage_count + 1 WHERE id = ?", tagID)
		}

		// 创建关联
		_, err = fb.db.Exec(
			"INSERT OR IGNORE INTO feedback_tag_relations (feedback_id, tag_id) VALUES (?, ?)",
			feedbackID, tagID,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetFeedback 获取反馈详情
func (fb *FeedbackDB) GetFeedback(feedbackID int64) (*Feedback, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	var feedback Feedback
	var tagsJSON, imagesJSON sql.NullString

	err := fb.db.QueryRow(`
		SELECT id, user_id, user_name, title, description, issue_type,
			   priority, status, media_type, media_id, media_title,
			   tmdb_id, tags, images, template_used, similar_feedback_id,
			   created_at, updated_at
		FROM feedback
		WHERE id = ?
	`, feedbackID).Scan(
		&feedback.ID, &feedback.UserID, &feedback.UserName, &feedback.Title,
		&feedback.Description, &feedback.IssueType, &feedback.Priority,
		&feedback.Status, &feedback.MediaType, &feedback.MediaID,
		&feedback.MediaTitle, &feedback.TmdbID, &tagsJSON, &imagesJSON,
		&feedback.TemplateUsed, &feedback.SimilarFeedbackID,
		&feedback.CreatedAt, &feedback.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// 反序列化数组
	if tagsJSON.Valid {
		json.Unmarshal([]byte(tagsJSON.String), &feedback.Tags)
	}
	if imagesJSON.Valid {
		json.Unmarshal([]byte(imagesJSON.String), &feedback.Images)
	}

	// 加载回复
	fb.loadFeedbackReplies(&feedback)

	return &feedback, nil
}

// loadFeedbackReplies 加载回复
func (fb *FeedbackDB) loadFeedbackReplies(feedback *Feedback) error {
	rows, err := fb.db.Query(`
		SELECT id, feedback_id, author_id, author_name, content, type, created_at
		FROM feedback_replies
		WHERE feedback_id = ?
		ORDER BY created_at ASC
	`, feedback.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var reply FeedbackReply
		if err := rows.Scan(
			&reply.ID, &reply.FeedbackID, &reply.AuthorID, &reply.AuthorName,
			&reply.Content, &reply.Type, &reply.CreatedAt,
		); err != nil {
			return err
		}
		feedback.Replies = append(feedback.Replies, reply)
	}

	return rows.Err()
}

// GetUserFeedbacks 获取用户反馈列表
func (fb *FeedbackDB) GetUserFeedbacks(userID int64, limit int) ([]*Feedback, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	query := `
		SELECT id, user_id, user_name, title, description, issue_type,
			   priority, status, media_type, media_id, media_title,
			   tmdb_id, tags, images, template_used, similar_feedback_id,
			   created_at, updated_at
		FROM feedback
		WHERE user_id = ?
		ORDER BY created_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := fb.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feedbacks []*Feedback
	for rows.Next() {
		var feedback Feedback
		var tagsJSON, imagesJSON sql.NullString

		if err := rows.Scan(
			&feedback.ID, &feedback.UserID, &feedback.UserName, &feedback.Title,
			&feedback.Description, &feedback.IssueType, &feedback.Priority,
			&feedback.Status, &feedback.MediaType, &feedback.MediaID,
			&feedback.MediaTitle, &feedback.TmdbID, &tagsJSON, &imagesJSON,
			&feedback.TemplateUsed, &feedback.SimilarFeedbackID,
			&feedback.CreatedAt, &feedback.UpdatedAt,
		); err != nil {
			return nil, err
		}

		// 反序列化数组
		if tagsJSON.Valid {
			json.Unmarshal([]byte(tagsJSON.String), &feedback.Tags)
		}
		if imagesJSON.Valid {
			json.Unmarshal([]byte(imagesJSON.String), &feedback.Images)
		}

		feedbacks = append(feedbacks, &feedback)
	}

	return feedbacks, nil
}

// GetOpenFeedbacks 获取所有待处理的反馈
func (fb *FeedbackDB) GetOpenFeedbacks() ([]*Feedback, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	rows, err := fb.db.Query(`
		SELECT id, user_id, user_name, title, description, issue_type,
			   priority, status, media_type, media_id, media_title,
			   tmdb_id, tags, images, template_used, similar_feedback_id,
			   created_at, updated_at
		FROM feedback
		WHERE status IN ('open', 'reply')
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feedbacks []*Feedback
	for rows.Next() {
		var feedback Feedback
		var tagsJSON, imagesJSON sql.NullString

		if err := rows.Scan(
			&feedback.ID, &feedback.UserID, &feedback.UserName, &feedback.Title,
			&feedback.Description, &feedback.IssueType, &feedback.Priority,
			&feedback.Status, &feedback.MediaType, &feedback.MediaID,
			&feedback.MediaTitle, &feedback.TmdbID, &tagsJSON, &imagesJSON,
			&feedback.TemplateUsed, &feedback.SimilarFeedbackID,
			&feedback.CreatedAt, &feedback.UpdatedAt,
		); err != nil {
			return nil, err
		}

		// 反序列化数组
		if tagsJSON.Valid {
			json.Unmarshal([]byte(tagsJSON.String), &feedback.Tags)
		}
		if imagesJSON.Valid {
			json.Unmarshal([]byte(imagesJSON.String), &feedback.Images)
		}

		feedbacks = append(feedbacks, &feedback)
	}

	return feedbacks, nil
}

// UpdateFeedbackStatus 更新反馈状态
func (fb *FeedbackDB) UpdateFeedbackStatus(feedbackID int64, status string) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	_, err := fb.db.Exec(
		"UPDATE feedback SET status = ?, updated_at = ? WHERE id = ?",
		status, time.Now(), feedbackID,
	)

	return err
}

// UpdateFeedbackPriority 更新反馈优先级
func (fb *FeedbackDB) UpdateFeedbackPriority(feedbackID int64, priority string) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	_, err := fb.db.Exec(
		"UPDATE feedback SET priority = ?, updated_at = ? WHERE id = ?",
		priority, time.Now(), feedbackID,
	)

	return err
}

// AddFeedbackReply 添加回复
func (fb *FeedbackDB) AddFeedbackReply(feedbackID int64, reply *FeedbackReply) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	_, err := fb.db.Exec(`
		INSERT INTO feedback_replies (feedback_id, author_id, author_name, content, type, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		feedbackID, reply.AuthorID, reply.AuthorName, reply.Content, reply.Type, time.Now(),
	)

	if err != nil {
		return err
	}

	// 如果不是模板回复，更新状态
	if reply.Type != "template" {
		_, err = fb.db.Exec(
			"UPDATE feedback SET status = 'reply', updated_at = ? WHERE id = ?",
			time.Now(), feedbackID,
		)
	}

	return err
}

// FindSimilarFeedbacks 查找相似反馈
func (fb *FeedbackDB) FindSimilarFeedbacks(feedbackID int64, issueType string, userID int64) ([]*Feedback, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	rows, err := fb.db.Query(`
		SELECT id, user_id, user_name, title, description, issue_type,
			   priority, status, media_type, media_id, media_title,
			   tmdb_id, tags, images, template_used, similar_feedback_id,
			   created_at, updated_at
		FROM feedback
		WHERE issue_type = ? AND user_id != ? AND id != ? AND status IN ('open', 'reply')
		ORDER BY created_at DESC
		LIMIT 10
	`, issueType, userID, feedbackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feedbacks []*Feedback
	for rows.Next() {
		var feedback Feedback
		var tagsJSON, imagesJSON sql.NullString

		if err := rows.Scan(
			&feedback.ID, &feedback.UserID, &feedback.UserName, &feedback.Title,
			&feedback.Description, &feedback.IssueType, &feedback.Priority,
			&feedback.Status, &feedback.MediaType, &feedback.MediaID,
			&feedback.MediaTitle, &feedback.TmdbID, &tagsJSON, &imagesJSON,
			&feedback.TemplateUsed, &feedback.SimilarFeedbackID,
			&feedback.CreatedAt, &feedback.UpdatedAt,
		); err != nil {
			return nil, err
		}

		// 反序列化数组
		if tagsJSON.Valid {
			json.Unmarshal([]byte(tagsJSON.String), &feedback.Tags)
		}
		if imagesJSON.Valid {
			json.Unmarshal([]byte(imagesJSON.String), &feedback.Images)
		}

		feedbacks = append(feedbacks, &feedback)
	}

	return feedbacks, nil
}

// GetFeedbackStats 获取反馈统计
type FeedbackStats struct {
	Total           int            `json:"total"`
	ByStatus        map[string]int `json:"by_status"`
	ByType          map[string]int `json:"by_type"`
	ByPriority      map[string]int `json:"by_priority"`
	AvgResolveTime  float64        `json:"avg_resolve_time"`
	ByDevice        map[string]int `json:"by_device"`
	ByNetwork       map[string]int `json:"by_network"`
	BySeverity      map[string]int `json:"by_severity"`
}

// GetFeedbackStats 获取反馈统计
func (fb *FeedbackDB) GetFeedbackStats() (*FeedbackStats, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	stats := &FeedbackStats{
		ByStatus:   make(map[string]int),
		ByType:     make(map[string]int),
		ByPriority: make(map[string]int),
		ByDevice:   make(map[string]int),
		ByNetwork:  make(map[string]int),
		BySeverity: make(map[string]int),
	}

	// 总数
	fb.db.QueryRow("SELECT COUNT(*) FROM feedback").Scan(&stats.Total)

	// 按状态统计
	rows, _ := fb.db.Query("SELECT status, COUNT(*) FROM feedback GROUP BY status")
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		stats.ByStatus[status] = count
	}
	rows.Close()

	// 按类型统计
	rows, _ = fb.db.Query("SELECT issue_type, COUNT(*) FROM feedback GROUP BY issue_type")
	for rows.Next() {
		var issueType string
		var count int
		rows.Scan(&issueType, &count)
		stats.ByType[issueType] = count
	}
	rows.Close()

	// 按优先级统计
	rows, _ = fb.db.Query("SELECT priority, COUNT(*) FROM feedback GROUP BY priority")
	for rows.Next() {
		var priority string
		var count int
		rows.Scan(&priority, &count)
		stats.ByPriority[priority] = count
	}
	rows.Close()

	// 按标签统计（设备类型）
	rows, _ = fb.db.Query(`
		SELECT t.name, COUNT(*) 
		FROM feedback_tag_relations r
		JOIN feedback_tags t ON r.tag_id = t.id
		WHERE t.category = 'device'
		GROUP BY t.name
	`)
	for rows.Next() {
		var device string
		var count int
		rows.Scan(&device, &count)
		stats.ByDevice[device] = count
	}
	rows.Close()

	// 按标签统计（网络类型）
	rows, _ = fb.db.Query(`
		SELECT t.name, COUNT(*) 
		FROM feedback_tag_relations r
		JOIN feedback_tags t ON r.tag_id = t.id
		WHERE t.category = 'network'
		GROUP BY t.name
	`)
	for rows.Next() {
		var network string
		var count int
		rows.Scan(&network, &count)
		stats.ByNetwork[network] = count
	}
	rows.Close()

	// 按标签统计（严重程度）
	rows, _ = fb.db.Query(`
		SELECT t.name, COUNT(*) 
		FROM feedback_tag_relations r
		JOIN feedback_tags t ON r.tag_id = t.id
		WHERE t.category = 'severity'
		GROUP BY t.name
	`)
	for rows.Next() {
		var severity string
		var count int
		rows.Scan(&severity, &count)
		stats.BySeverity[severity] = count
	}
	rows.Close()

	// 计算平均解决时间（已解决的问题）
	var avgSeconds sql.NullFloat64
	fb.db.QueryRow(`
		SELECT AVG(julianday(updated_at) - julianday(created_at)) * 86400
		FROM feedback
		WHERE status IN ('fixed', 'closed')
	`).Scan(&avgSeconds)
	if avgSeconds.Valid {
		stats.AvgResolveTime = avgSeconds.Float64 / 3600 // 转换为小时
	}

	return stats, nil
}

// GetAllTags 获取所有标签
func (fb *FeedbackDB) GetAllTags() ([]*FeedbackTag, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	rows, err := fb.db.Query("SELECT id, name, category, usage_count, created_at FROM feedback_tags ORDER BY usage_count DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*FeedbackTag
	for rows.Next() {
		var tag FeedbackTag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Category, &tag.UsageCount, &tag.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, &tag)
	}

	return tags, nil
}

// cleanupRoutine 定期清理旧反馈
func (fb *FeedbackDB) cleanupRoutine() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		fb.cleanupOldFeedbacks()
	}
}

// cleanupOldFeedbacks 清理 90 天前的已关闭反馈
func (fb *FeedbackDB) cleanupOldFeedbacks() int {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -90)

	result, err := fb.db.Exec(`
		DELETE FROM feedback
		WHERE status IN ('fixed', 'closed') AND updated_at < ?
	`, cutoff)

	if err != nil {
		log.Printf("[FeedbackDB] Cleanup error: %v", err)
		return 0
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("[FeedbackDB] Cleaned up %d old feedbacks", rowsAffected)
	}

	return int(rowsAffected)
}

// Close 关闭数据库连接
func (fb *FeedbackDB) Close() error {
	return fb.db.Close()
}
