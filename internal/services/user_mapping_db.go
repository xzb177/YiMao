package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
	_ "modernc.org/sqlite"
)

// NotifyPrefs stores user notification preferences
type NotifyPrefs struct {
	OnSubscribe bool `json:"on_subscribe"` // 订阅状态变更通知
	OnDownload  bool `json:"on_download"`  // 下载完成通知
	OnTransfer  bool `json:"on_transfer"`  // 入库完成通知
	OnReview    bool `json:"on_review"`    // 审核结果通知
}

// UserMapping represents a Telegram ↔ MoviePilot user mapping
type UserMapping struct {
	TelegramID       int64        `json:"telegram_id"`
	MPUserID         int64        `json:"mp_user_id"`
	MPUsername       string       `json:"mp_username"`
	TelegramUsername string       `json:"telegram_username,omitempty"`
	NotifyPrefs      *NotifyPrefs `json:"notify_prefs,omitempty"`
	BoundAt          time.Time    `json:"bound_at"`
	LastActive       time.Time    `json:"last_active"`
}

// UserMappingDB handles user mappings between Telegram and MoviePilot using SQLite
type UserMappingDB struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewUserMappingDB creates a new SQLite-backed user mapping service
func NewUserMappingDB(dataDir string) (*UserMappingDB, error) {
	dbPath := fmt.Sprintf("%s/user_mappings.db", dataDir)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 WAL 模式失败: %w", err)
	}

	// Create table and enforce one Telegram user per MoviePilot account.
	if err := createTable(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建表失败: %w", err)
	}

	svc := &UserMappingDB{
		db:   db,
		path: dbPath,
	}

	// Migrate from JSON if exists
	if err := svc.migrateFromJSON(dataDir); err != nil {
		logger.Info("[UserMappingDB] JSON 迁移警告: %v", err)
	}

	return svc, nil
}

// createTable creates the user_mappings table
func createTable(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS user_mappings (
		telegram_id       INTEGER PRIMARY KEY,
		mp_user_id        INTEGER NOT NULL,
		mp_username       TEXT NOT NULL,
		telegram_username TEXT DEFAULT '',
		notify_subscribe  INTEGER DEFAULT 1,
		notify_download   INTEGER DEFAULT 1,
		notify_transfer   INTEGER DEFAULT 1,
		notify_review     INTEGER DEFAULT 1,
		bound_at          TEXT NOT NULL,
		last_active       TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_mp_user_id ON user_mappings(mp_user_id);
	CREATE INDEX IF NOT EXISTS idx_mp_username ON user_mappings(mp_username);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	if err := deduplicateUserMappings(db); err != nil {
		return err
	}
	uniqueSchema := `
	CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_mp_user_id ON user_mappings(mp_user_id);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_mp_username ON user_mappings(mp_username);
	`
	_, err := db.Exec(uniqueSchema)
	return err
}

func deduplicateUserMappings(db *sql.DB) error {
	queries := []string{
		`DELETE FROM user_mappings WHERE rowid IN (
			SELECT rowid FROM (
				SELECT rowid, ROW_NUMBER() OVER (PARTITION BY mp_user_id ORDER BY last_active DESC, bound_at DESC, rowid DESC) AS rn
				FROM user_mappings
			) WHERE rn > 1
		);`,
		`DELETE FROM user_mappings WHERE rowid IN (
			SELECT rowid FROM (
				SELECT rowid, ROW_NUMBER() OVER (PARTITION BY mp_username ORDER BY last_active DESC, bound_at DESC, rowid DESC) AS rn
				FROM user_mappings
			) WHERE rn > 1
		);`,
	}
	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// migrateFromJSON migrates data from legacy JSON file
func (s *UserMappingDB) migrateFromJSON(dataDir string) error {
	jsonPath := fmt.Sprintf("%s/user_mappings.json", dataDir)

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Parse JSON
	var fileData struct {
		UserMappings    map[string]int64  `json:"user_mappings"`
		Usernames       map[string]string `json:"usernames"`
		ReverseMappings map[int64]string  `json:"reverse_mappings"`
	}
	if err := json.Unmarshal(data, &fileData); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}

	if len(fileData.UserMappings) == 0 {
		return nil
	}

	// Check if SQLite already has data
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM user_mappings").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		logger.Info("[UserMappingDB] SQLite 已有 %d 条记录，跳过 JSON 迁移", count)
		return nil
	}

	// Insert records
	now := time.Now().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO user_mappings
		(telegram_id, mp_user_id, mp_username, telegram_username, bound_at, last_active)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	migrated := 0
	for tgIDStr, mpID := range fileData.UserMappings {
		var tgID int64
		if _, err := fmt.Sscanf(tgIDStr, "%d", &tgID); err != nil || tgID <= 0 {
			logger.Info("[UserMappingDB] 跳过非法 Telegram ID: %s", tgIDStr)
			continue
		}
		mpUsername := fileData.Usernames[tgIDStr]
		if mpUsername == "" {
			mpUsername = fmt.Sprintf("user_%d", mpID)
		}

		if _, err := stmt.Exec(tgID, mpID, mpUsername, "", now, now); err != nil {
			logger.Info("[UserMappingDB] 迁移记录失败 tg=%d: %v", tgID, err)
			continue
		}
		migrated++
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	logger.Info("[UserMappingDB] 从 JSON 迁移了 %d 条记录", migrated)

	// Backup and remove JSON file
	backupPath := jsonPath + ".bak"
	os.Rename(jsonPath, backupPath)

	return nil
}

// GetMoviePilotUserID gets MoviePilot user ID for a Telegram user
func (s *UserMappingDB) GetMoviePilotUserID(telegramID int64) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var mpUserID int64
	err := s.db.QueryRow(
		"SELECT mp_user_id FROM user_mappings WHERE telegram_id = ?",
		telegramID,
	).Scan(&mpUserID)

	if err != nil {
		return 0, false
	}
	return mpUserID, true
}

// GetMoviePilotUsername gets the MoviePilot username for a Telegram user
func (s *UserMappingDB) GetMoviePilotUsername(telegramID int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var username string
	err := s.db.QueryRow(
		"SELECT mp_username FROM user_mappings WHERE telegram_id = ?",
		telegramID,
	).Scan(&username)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("用户未绑定 MoviePilot 账号")
		}
		return "", err
	}
	return username, nil
}

// GetTelegramUsername gets Telegram username for a user
func (s *UserMappingDB) GetTelegramUsername(telegramID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var username string
	err := s.db.QueryRow(
		"SELECT telegram_username FROM user_mappings WHERE telegram_id = ?",
		telegramID,
	).Scan(&username)

	if err != nil {
		return ""
	}
	return username
}

// SetTelegramUsername sets the Telegram username for a user
func (s *UserMappingDB) SetTelegramUsername(telegramID int64, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"UPDATE user_mappings SET telegram_username = ? WHERE telegram_id = ?",
		username, telegramID,
	)
	if err != nil {
		logger.Info("[UserMappingDB] 设置 Telegram 用户名失败: %v", err)
	}

	// Update last active
	s.updateLastActiveLocked(telegramID)
}

// AddMapping adds a user mapping
func (s *UserMappingDB) AddMapping(telegramID int64, mpUserID int64, mpUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if owner, exists := s.getTelegramIDByMoviePilotIDLocked(mpUserID); exists && owner != telegramID {
		return fmt.Errorf("MoviePilot 用户 ID %d 已绑定其他 Telegram 用户", mpUserID)
	}
	if owner, exists := s.getTelegramIDByMoviePilotUsernameLocked(mpUsername); exists && owner != telegramID {
		return fmt.Errorf("MoviePilot 用户名 %s 已绑定其他 Telegram 用户", mpUsername)
	}

	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO user_mappings
		(telegram_id, mp_user_id, mp_username, bound_at, last_active)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(telegram_id) DO UPDATE SET
			mp_user_id = excluded.mp_user_id,
			mp_username = excluded.mp_username,
			last_active = excluded.last_active
	`, telegramID, mpUserID, mpUsername, now, now)

	if err != nil {
		return fmt.Errorf("添加映射失败: %w", err)
	}

	logger.Info("[UserMappingDB] 添加映射: tg=%d → mp=%s(%d)", telegramID, mpUsername, mpUserID)
	return nil
}

// RemoveMapping removes a user mapping
func (s *UserMappingDB) RemoveMapping(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"DELETE FROM user_mappings WHERE telegram_id = ?",
		telegramID,
	)
	if err != nil {
		return fmt.Errorf("删除映射失败: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("未找到映射: telegram_id=%d", telegramID)
	}

	logger.Info("[UserMappingDB] 删除映射: tg=%d", telegramID)
	return nil
}

// GetTelegramIDByMoviePilotUsername gets Telegram ID by MoviePilot username
func (s *UserMappingDB) GetTelegramIDByMoviePilotUsername(username string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getTelegramIDByMoviePilotUsernameLocked(username)
}

func (s *UserMappingDB) getTelegramIDByMoviePilotUsernameLocked(username string) (int64, bool) {
	var telegramID int64
	err := s.db.QueryRow(
		"SELECT telegram_id FROM user_mappings WHERE mp_username = ?",
		username,
	).Scan(&telegramID)

	if err != nil {
		return 0, false
	}
	return telegramID, true
}

// GetNotifyPrefs gets notification preferences for a Telegram user
func (s *UserMappingDB) GetNotifyPrefs(telegramID int64) *NotifyPrefs {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var prefs NotifyPrefs
	err := s.db.QueryRow(`
		SELECT notify_subscribe, notify_download, notify_transfer, notify_review
		FROM user_mappings WHERE telegram_id = ?
	`, telegramID).Scan(&prefs.OnSubscribe, &prefs.OnDownload, &prefs.OnTransfer, &prefs.OnReview)

	if err != nil {
		// Return defaults (all enabled)
		return &NotifyPrefs{OnSubscribe: true, OnDownload: true, OnTransfer: true, OnReview: true}
	}
	return &prefs
}

// SetNotifyPrefs sets notification preferences for a Telegram user
func (s *UserMappingDB) SetNotifyPrefs(telegramID int64, prefs *NotifyPrefs) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE user_mappings
		SET notify_subscribe = ?, notify_download = ?, notify_transfer = ?, notify_review = ?
		WHERE telegram_id = ?
	`, prefs.OnSubscribe, prefs.OnDownload, prefs.OnTransfer, prefs.OnReview, telegramID)

	return err
}

// TouchLastActive updates the last active time for a user
func (s *UserMappingDB) TouchLastActive(telegramID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateLastActiveLocked(telegramID)
}

func (s *UserMappingDB) updateLastActiveLocked(telegramID int64) {
	now := time.Now().Format(time.RFC3339)
	s.db.Exec(
		"UPDATE user_mappings SET last_active = ? WHERE telegram_id = ?",
		now, telegramID,
	)
}

// GetAllMappings returns all user mappings
func (s *UserMappingDB) GetAllMappings() []UserMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT telegram_id, mp_user_id, mp_username, telegram_username,
		       notify_subscribe, notify_download, notify_transfer, notify_review,
		       bound_at, last_active
		FROM user_mappings
	`)
	if err != nil {
		logger.Info("[UserMappingDB] 查询所有映射失败: %v", err)
		return nil
	}
	defer rows.Close()

	var mappings []UserMapping
	for rows.Next() {
		var m UserMapping
		var prefs NotifyPrefs
		var boundAt, lastActive string

		if err := rows.Scan(
			&m.TelegramID, &m.MPUserID, &m.MPUsername, &m.TelegramUsername,
			&prefs.OnSubscribe, &prefs.OnDownload, &prefs.OnTransfer, &prefs.OnReview,
			&boundAt, &lastActive,
		); err != nil {
			continue
		}

		m.NotifyPrefs = &prefs
		m.BoundAt, _ = time.Parse(time.RFC3339, boundAt)
		m.LastActive, _ = time.Parse(time.RFC3339, lastActive)
		mappings = append(mappings, m)
	}

	return mappings
}

// GetMappingCount returns the total number of mappings
func (s *UserMappingDB) GetMappingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM user_mappings").Scan(&count)
	return count
}

// GetTelegramIDByJellyseerrID is a legacy alias for GetMoviePilotUserID
func (s *UserMappingDB) GetTelegramIDByJellyseerrID(jellyseerrID int64) (int64, bool) {
	// In the new schema, mp_user_id = jellyseerr ID (same MoviePilot user)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getTelegramIDByMoviePilotIDLocked(jellyseerrID)
}

func (s *UserMappingDB) getTelegramIDByMoviePilotIDLocked(mpUserID int64) (int64, bool) {
	var telegramID int64
	err := s.db.QueryRow(
		"SELECT telegram_id FROM user_mappings WHERE mp_user_id = ?",
		mpUserID,
	).Scan(&telegramID)

	if err != nil {
		return 0, false
	}
	return telegramID, true
}

// GetAllTelegramUsers returns all Telegram user IDs that have mappings
func (s *UserMappingDB) GetAllTelegramUsers() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT telegram_id FROM user_mappings")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var users []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			users = append(users, id)
		}
	}
	return users
}

// ForceSave is a no-op for SQLite (auto-persisted)
func (s *UserMappingDB) ForceSave() error {
	return nil
}

// Close closes the database connection
func (s *UserMappingDB) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
