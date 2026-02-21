package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// UserDB manages user data in SQLite database
type UserDB struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// UserData represents comprehensive user information
type UserData struct {
	TelegramID      int64     `json:"telegram_id"`
	UserName        string    `json:"user_name"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	LanguageCode    string    `json:"language_code"`
	IsBot           bool      `json:"is_bot"`
	JellyseerrID    int       `json:"jellyseerr_id"`
	JellyseerrName  string    `json:"jellyseerr_name"`
	JellyseerrUsername string `json:"jellyseerr_username"`
	IsAdmin         bool      `json:"is_admin"`
	IsPremium       bool      `json:"is_premium"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastActiveAt    time.Time `json:"last_active_at"`

	// Quota data
	MovieQuotaLimit  int `json:"movie_quota_limit"`
	MovieQuotaUsed   int `json:"movie_quota_used"`
	TVQuotaLimit     int `json:"tv_quota_limit"`
	TVQuotaUsed      int `json:"tv_quota_used"`

	// Settings
	NotificationEnabled bool     `json:"notification_enabled"`
	NotificationTypes   []string `json:"notification_types"` // movie, tv, all
	PreferredLanguage  string   `json:"preferred_language"`
	PreferredQuality   string   `json:"preferred_quality"`  // 4k, 1080p, 720p

	// Stats
	TotalRequests      int `json:"total_requests"`
	ApprovedRequests   int `json:"approved_requests"`
	DeclinedRequests   int `json:"declined_requests"`
	CompletedRequests  int `json:"completed_requests"`
}

// UserRequest represents a media request made by a user
type UserRequest struct {
	ID            int64     `json:"id"`
	TelegramID    int64     `json:"telegram_id"`
	MediaID       int       `json:"media_id"`
	MediaTitle    string    `json:"media_title"`
	MediaType     string    `json:"media_type"`     // movie, tv
	TmdbID        int       `json:"tmdb_id"`
	PosterPath    string    `json:"poster_path"`
	Status        string    `json:"status"`        // pending, approved, declined, available
	JellyseerrRequestID int `json:"jellyseerr_request_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

// Database global instance
var globalUserDB *UserDB
var dbOnce sync.Once

// GetGlobalUserDB returns the global user database instance
func GetGlobalUserDB(dataDir string) (*UserDB, error) {
	var initErr error
	dbOnce.Do(func() {
		globalUserDB, initErr = NewUserDB(filepath.Join(dataDir, "users.db"))
		if initErr != nil {
			log.Printf("[UserDB] Failed to initialize: %v", initErr)
			return
		}
		log.Printf("[UserDB] Initialized at %s", globalUserDB.path)
	})
	return globalUserDB, initErr
}

// NewUserDB creates a new user database
func NewUserDB(dbPath string) (*UserDB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite doesn't support multiple writers well
	db.SetMaxIdleConns(1)

	userDB := &UserDB{
		db:   db,
		path: dbPath,
	}

	// Initialize schema
	if err := userDB.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return userDB, nil
}

// initSchema creates the database tables
func (udb *UserDB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		telegram_id INTEGER PRIMARY KEY,
		user_name TEXT,
		first_name TEXT,
		last_name TEXT,
		language_code TEXT,
		is_bot INTEGER DEFAULT 0,
		jellyseerr_id INTEGER,
		jellyseerr_name TEXT,
		jellyseerr_username TEXT,
		is_admin INTEGER DEFAULT 0,
		is_premium INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_active_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		-- Quota
		movie_quota_limit INTEGER DEFAULT 2,
		movie_quota_used INTEGER DEFAULT 0,
		tv_quota_limit INTEGER DEFAULT 2,
		tv_quota_used INTEGER DEFAULT 0,

		-- Settings
		notification_enabled INTEGER DEFAULT 1,
		notification_types TEXT DEFAULT '[]',
		preferred_language TEXT DEFAULT 'zh',
		preferred_quality TEXT DEFAULT '1080p',

		-- Stats
		total_requests INTEGER DEFAULT 0,
		approved_requests INTEGER DEFAULT 0,
		declined_requests INTEGER DEFAULT 0,
		completed_requests INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS user_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER NOT NULL,
		media_id INTEGER NOT NULL,
		media_title TEXT NOT NULL,
		media_type TEXT NOT NULL,
		tmdb_id INTEGER NOT NULL,
		poster_path TEXT,
		status TEXT DEFAULT 'pending',
		jellyseerr_request_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME,

		FOREIGN KEY (telegram_id) REFERENCES users(telegram_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS user_feedback (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER NOT NULL,
		media_id INTEGER,
		media_title TEXT,
		media_type TEXT,
		problem_type TEXT,
		message TEXT NOT NULL,
		jellyseerr_issue_id INTEGER,
		status TEXT DEFAULT 'open',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (telegram_id) REFERENCES users(telegram_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_user_requests_telegram_id ON user_requests(telegram_id);
	CREATE INDEX IF NOT EXISTS idx_user_requests_status ON user_requests(status);
	CREATE INDEX IF NOT EXISTS idx_user_feedback_telegram_id ON user_feedback(telegram_id);
	`

	_, err := udb.db.Exec(schema)
	return err
}

// Close closes the database connection
func (udb *UserDB) Close() error {
	udb.mu.Lock()
	defer udb.mu.Unlock()
	return udb.db.Close()
}

// UpsertUser creates or updates a user record
func (udb *UserDB) UpsertUser(user *UserData) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	query := `
	INSERT INTO users (
		telegram_id, user_name, first_name, last_name, language_code, is_bot,
		jellyseerr_id, jellyseerr_name, jellyseerr_username, is_admin, is_premium,
		movie_quota_limit, movie_quota_used, tv_quota_limit, tv_quota_used,
		notification_enabled, notification_types, preferred_language, preferred_quality,
		total_requests, approved_requests, declined_requests, completed_requests,
		last_active_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(telegram_id) DO UPDATE SET
		user_name = excluded.user_name,
		first_name = excluded.first_name,
		last_name = excluded.last_name,
		language_code = excluded.language_code,
		jellyseerr_id = excluded.jellyseerr_id,
		jellyseerr_name = excluded.jellyseerr_name,
		jellyseerr_username = excluded.jellyseerr_username,
		is_admin = excluded.is_admin,
		is_premium = excluded.is_premium,
		movie_quota_limit = excluded.movie_quota_limit,
		movie_quota_used = excluded.movie_quota_used,
		tv_quota_limit = excluded.tv_quota_limit,
		tv_quota_used = excluded.tv_quota_used,
		notification_enabled = excluded.notification_enabled,
		notification_types = excluded.notification_types,
		preferred_language = excluded.preferred_language,
		preferred_quality = excluded.preferred_quality,
		total_requests = excluded.total_requests,
		approved_requests = excluded.approved_requests,
		declined_requests = excluded.declined_requests,
		completed_requests = excluded.completed_requests,
		updated_at = CURRENT_TIMESTAMP,
		last_active_at = excluded.last_active_at
	`

	// Convert notification types to JSON
	notifTypesJSON, _ := json.Marshal(user.NotificationTypes)

	_, err := udb.db.Exec(query,
		user.TelegramID, user.UserName, user.FirstName, user.LastName, user.LanguageCode, boolToInt(user.IsBot),
		user.JellyseerrID, user.JellyseerrName, user.JellyseerrUsername, boolToInt(user.IsAdmin), boolToInt(user.IsPremium),
		user.MovieQuotaLimit, user.MovieQuotaUsed, user.TVQuotaLimit, user.TVQuotaUsed,
		boolToInt(user.NotificationEnabled), string(notifTypesJSON), user.PreferredLanguage, user.PreferredQuality,
		user.TotalRequests, user.ApprovedRequests, user.DeclinedRequests, user.CompletedRequests,
		user.LastActiveAt,
	)

	return err
}

// GetUser retrieves a user by Telegram ID
func (udb *UserDB) GetUser(telegramID int64) (*UserData, error) {
	udb.mu.RLock()
	defer udb.mu.RUnlock()

	query := `
	SELECT telegram_id, user_name, first_name, last_name, language_code, is_bot,
		jellyseerr_id, jellyseerr_name, jellyseerr_username, is_admin, is_premium,
		created_at, updated_at, last_active_at,
		movie_quota_limit, movie_quota_used, tv_quota_limit, tv_quota_used,
		notification_enabled, notification_types, preferred_language, preferred_quality,
		total_requests, approved_requests, declined_requests, completed_requests
	FROM users WHERE telegram_id = ?
	`

	user := &UserData{}
	var notifTypesJSON string

	err := udb.db.QueryRow(query, telegramID).Scan(
		&user.TelegramID, &user.UserName, &user.FirstName, &user.LastName, &user.LanguageCode,
		&user.IsBot, &user.JellyseerrID, &user.JellyseerrName, &user.JellyseerrUsername,
		&user.IsAdmin, &user.IsPremium, &user.CreatedAt, &user.UpdatedAt, &user.LastActiveAt,
		&user.MovieQuotaLimit, &user.MovieQuotaUsed, &user.TVQuotaLimit, &user.TVQuotaUsed,
		&user.NotificationEnabled, &notifTypesJSON, &user.PreferredLanguage, &user.PreferredQuality,
		&user.TotalRequests, &user.ApprovedRequests, &user.DeclinedRequests, &user.CompletedRequests,
	)

	if err != nil {
		return nil, err
	}

	// Parse notification types
	json.Unmarshal([]byte(notifTypesJSON), &user.NotificationTypes)

	return user, nil
}

// UpdateLastActive updates the user's last active timestamp
func (udb *UserDB) UpdateLastActive(telegramID int64) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	_, err := udb.db.Exec("UPDATE users SET last_active_at = CURRENT_TIMESTAMP WHERE telegram_id = ?", telegramID)
	return err
}

// LinkJellyseerr links a Telegram user to a Jellyseerr account
func (udb *UserDB) LinkJellyseerr(telegramID int64, jellyseerrID int, jellyseerrName, jellyseerrUsername string) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	query := `
	UPDATE users SET
		jellyseerr_id = ?,
		jellyseerr_name = ?,
		jellyseerr_username = ?,
		updated_at = CURRENT_TIMESTAMP
	WHERE telegram_id = ?
	`

	_, err := udb.db.Exec(query, jellyseerrID, jellyseerrName, jellyseerrUsername, telegramID)
	return err
}

// CreateRequest creates a new media request
func (udb *UserDB) CreateRequest(req *UserRequest) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	query := `
	INSERT INTO user_requests (telegram_id, media_id, media_title, media_type, tmdb_id, poster_path, status, jellyseerr_request_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := udb.db.Exec(query,
		req.TelegramID, req.MediaID, req.MediaTitle, req.MediaType, req.TmdbID, req.PosterPath, req.Status, req.JellyseerrRequestID,
	)
	if err != nil {
		return err
	}

	// Update user stats
	_, err = udb.db.Exec("UPDATE users SET total_requests = total_requests + 1 WHERE telegram_id = ?", req.TelegramID)

	id, _ := result.LastInsertId()
	req.ID = id
	return err
}

// GetRequests retrieves all requests for a user
func (udb *UserDB) GetRequests(telegramID int64) ([]UserRequest, error) {
	udb.mu.RLock()
	defer udb.mu.RUnlock()

	query := `
	SELECT id, telegram_id, media_id, media_title, media_type, tmdb_id, poster_path,
		status, jellyseerr_request_id, created_at, updated_at, completed_at
	FROM user_requests WHERE telegram_id = ?
	ORDER BY created_at DESC
	`

	rows, err := udb.db.Query(query, telegramID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []UserRequest
	for rows.Next() {
		var req UserRequest
		err := rows.Scan(
			&req.ID, &req.TelegramID, &req.MediaID, &req.MediaTitle, &req.MediaType,
			&req.TmdbID, &req.PosterPath, &req.Status, &req.JellyseerrRequestID,
			&req.CreatedAt, &req.UpdatedAt, &req.CompletedAt,
		)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	return requests, nil
}

// UpdateRequestStatus updates the status of a request
func (udb *UserDB) UpdateRequestStatus(jellyseerrRequestID int64, status string) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	tx, err := udb.db.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	// Update request status
	query := "UPDATE user_requests SET status = ?, updated_at = CURRENT_TIMESTAMP"
	if status == "available" {
		query += ", completed_at = CURRENT_TIMESTAMP"
	}
	query += " WHERE jellyseerr_request_id = ?"

	_, err = tx.Exec(query, status, jellyseerrRequestID)
	if err != nil {
		return err
	}

	// Update user stats
	var statusColumn string
	switch status {
	case "approved":
		statusColumn = "approved_requests"
	case "declined":
		statusColumn = "declined_requests"
	case "available":
		statusColumn = "completed_requests"
	default:
		statusColumn = ""
	}

	if statusColumn != "" {
		_, err = tx.Exec(fmt.Sprintf(`
			UPDATE users SET %s = %s + 1
			WHERE telegram_id IN (SELECT telegram_id FROM user_requests WHERE jellyseerr_request_id = ?)
		`, statusColumn, statusColumn), jellyseerrRequestID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CreateFeedback creates a new feedback entry
func (udb *UserDB) CreateFeedback(telegramID int64, mediaID int, mediaTitle, mediaType, problemType, message string, jellyseerrIssueID int) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	query := `
	INSERT INTO user_feedback (telegram_id, media_id, media_title, media_type, problem_type, message, jellyseerr_issue_id)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := udb.db.Exec(query, telegramID, mediaID, mediaTitle, mediaType, problemType, message, jellyseerrIssueID)
	return err
}

// GetFeedbacks retrieves all feedback for a user
func (udb *UserDB) GetFeedbacks(telegramID int64) ([]struct {
	ID            int
	MediaID       int
	MediaTitle    string
	MediaType     string
	ProblemType   string
	Message       string
	Status        string
	CreatedAt     time.Time
}, error) {
	udb.mu.RLock()
	defer udb.mu.RUnlock()

	query := `
	SELECT id, media_id, media_title, media_type, problem_type, message, status, created_at
	FROM user_feedback WHERE telegram_id = ?
	ORDER BY created_at DESC
	`

	rows, err := udb.db.Query(query, telegramID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feedbacks []struct {
		ID          int
		MediaID     int
		MediaTitle  string
		MediaType   string
		ProblemType string
		Message     string
		Status      string
		CreatedAt   time.Time
	}

	for rows.Next() {
		var f struct {
			ID          int
			MediaID     int
			MediaTitle  string
			MediaType   string
			ProblemType string
			Message     string
			Status      string
			CreatedAt   time.Time
		}
		err := rows.Scan(&f.ID, &f.MediaID, &f.MediaTitle, &f.MediaType, &f.ProblemType, &f.Message, &f.Status, &f.CreatedAt)
		if err != nil {
			return nil, err
		}
		feedbacks = append(feedbacks, f)
	}

	return feedbacks, nil
}

// GetAllUsers retrieves all users (for admin)
func (udb *UserDB) GetAllUsers() ([]UserData, error) {
	udb.mu.RLock()
	defer udb.mu.RUnlock()

	query := `
	SELECT telegram_id, user_name, first_name, last_name, language_code,
		jellyseerr_id, jellyseerr_name, is_admin, is_premium, created_at, last_active_at,
		total_requests, approved_requests, completed_requests
	FROM users ORDER BY created_at DESC
	`

	rows, err := udb.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserData
	for rows.Next() {
		var u UserData
		err := rows.Scan(
			&u.TelegramID, &u.UserName, &u.FirstName, &u.LastName, &u.LanguageCode,
			&u.JellyseerrID, &u.JellyseerrName, &u.IsAdmin, &u.IsPremium, &u.CreatedAt, &u.LastActiveAt,
			&u.TotalRequests, &u.ApprovedRequests, &u.CompletedRequests,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	return users, nil
}

// SetAdmin sets a user as admin
func (udb *UserDB) SetAdmin(telegramID int64, isAdmin bool) error {
	udb.mu.Lock()
	defer udb.mu.Unlock()

	_, err := udb.db.Exec("UPDATE users SET is_admin = ? WHERE telegram_id = ?", boolToInt(isAdmin), telegramID)
	return err
}

// GetUserByJellyseerrID retrieves a user by Jellyseerr ID
func (udb *UserDB) GetUserByJellyseerrID(jellyseerrID int) (*UserData, error) {
	udb.mu.RLock()
	defer udb.mu.RUnlock()

	query := `
	SELECT telegram_id, user_name, first_name, jellyseerr_id, jellyseerr_name, is_admin
	FROM users WHERE jellyseerr_id = ?
	`

	user := &UserData{}
	err := udb.db.QueryRow(query, jellyseerrID).Scan(
		&user.TelegramID, &user.UserName, &user.FirstName,
		&user.JellyseerrID, &user.JellyseerrName, &user.IsAdmin,
	)

	if err != nil {
		return nil, err
	}

	return user, nil
}

// Helper functions
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i != 0
}
