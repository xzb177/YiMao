package ai

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store handles persistence for conversations, messages, and user memory
type Store struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewStore creates a new SQLite store with the given database path
func NewStore(dbPath string) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	// Note: modernc.org/sqlite uses driver name "sqlite" not "sqlite3"
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set pragmas for performance
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	store := &Store{
		db:   db,
		path: dbPath,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates the necessary tables if they don't exist
func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		chat_id INTEGER NOT NULL,
		chat_type TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		tokens INTEGER DEFAULT 0,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
	CREATE INDEX IF NOT EXISTS idx_conv_user ON conversations(user_id);
	CREATE INDEX IF NOT EXISTS idx_conv_chat_id ON conversations(chat_id);
	CREATE INDEX IF NOT EXISTS idx_conv_updated ON conversations(updated_at);

	CREATE TABLE IF NOT EXISTS user_memory (
		user_id INTEGER PRIMARY KEY,
		preferences TEXT,
		interaction_count INTEGER DEFAULT 0,
		last_interaction INTEGER,
		notes TEXT
	);

	CREATE TABLE IF NOT EXISTS conversation_summaries (
		conversation_id TEXT PRIMARY KEY,
		summary TEXT NOT NULL,
		message_count INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);

	-- Q&A Learning tables
	CREATE TABLE IF NOT EXISTS qa_pairs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		question TEXT NOT NULL,
		answer TEXT NOT NULL,
		question_normalized TEXT NOT NULL,
		source_chat_id INTEGER NOT NULL,
		source_user_id INTEGER NOT NULL,
		answer_user_id INTEGER NOT NULL,
		is_admin_answer INTEGER DEFAULT 0,
		confidence REAL DEFAULT 0.5,
		created_at INTEGER NOT NULL,
		last_used_at INTEGER,
		usage_count INTEGER DEFAULT 0,
		success_count INTEGER DEFAULT 0,
		fail_count INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_qa_question_normalized ON qa_pairs(question_normalized);
	CREATE INDEX IF NOT EXISTS idx_qa_source_chat ON qa_pairs(source_chat_id);
	CREATE INDEX IF NOT EXISTS idx_qa_created_at ON qa_pairs(created_at);
	CREATE INDEX IF NOT EXISTS idx_qa_usage_count ON qa_pairs(usage_count DESC);

	CREATE TABLE IF NOT EXISTS qa_keywords (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		qa_id INTEGER NOT NULL,
		keyword TEXT NOT NULL,
		FOREIGN KEY (qa_id) REFERENCES qa_pairs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_qa_keywords_keyword ON qa_keywords(keyword);
	CREATE INDEX IF NOT EXISTS idx_qa_keywords_qa_id ON qa_keywords(qa_id);

	CREATE TABLE IF NOT EXISTS pending_questions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		message_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		question TEXT NOT NULL,
		question_normalized TEXT NOT NULL,
		asked_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		status TEXT DEFAULT 'pending'
	);

	CREATE INDEX IF NOT EXISTS idx_pending_qa_chat ON pending_questions(chat_id, asked_at);
	CREATE INDEX IF NOT EXISTS idx_pending_qa_status ON pending_questions(status);
	CREATE INDEX IF NOT EXISTS idx_pending_qa_expires ON pending_questions(expires_at);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Run migrations for new tables
	return s.runMigrations()
}

// Close closes the database connection
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// Conversation operations

// CreateConversation creates a new conversation in the store
func (s *Store) CreateConversation(convID string, userID, chatID int64, chatType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO conversations (id, user_id, chat_id, chat_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, convID, userID, chatID, chatType, now, now)

	return err
}

// GetConversation retrieves a conversation by ID
func (s *Store) GetConversation(convID string) (*ConversationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rec ConversationRecord
	err := s.db.QueryRow(`
		SELECT id, user_id, chat_id, chat_type, created_at, updated_at
		FROM conversations WHERE id = ?
	`, convID).Scan(&rec.ID, &rec.UserID, &rec.ChatID, &rec.ChatType, &rec.CreatedAt, &rec.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &rec, err
}

// GetConversationByChatID retrieves the most recent conversation for a user in a chat
func (s *Store) GetConversationByChatID(userID, chatID int64) (*ConversationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rec ConversationRecord
	err := s.db.QueryRow(`
		SELECT id, user_id, chat_id, chat_type, created_at, updated_at
		FROM conversations
		WHERE user_id = ? AND chat_id = ?
		ORDER BY updated_at DESC
		LIMIT 1
	`, userID, chatID).Scan(&rec.ID, &rec.UserID, &rec.ChatID, &rec.ChatType, &rec.CreatedAt, &rec.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &rec, err
}

// UpdateConversationTimestamp updates the updated_at timestamp
func (s *Store) UpdateConversationTimestamp(convID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.Exec(`
		UPDATE conversations SET updated_at = ? WHERE id = ?
	`, now, convID)

	return err
}

// DeleteConversation deletes a conversation and all its messages
func (s *Store) DeleteConversation(convID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM conversations WHERE id = ?`, convID)
	return err
}

// Message operations

// AddMessage adds a message to a conversation
func (s *Store) AddMessage(convID, role, content string, tokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO messages (conversation_id, role, content, timestamp, tokens)
		VALUES (?, ?, ?, ?, ?)
	`, convID, role, content, now, tokens)

	if err == nil {
		// Update conversation timestamp
		_, _ = s.db.Exec(`UPDATE conversations SET updated_at = ? WHERE id = ?`, now, convID)
	}

	return err
}

// GetMessages retrieves messages for a conversation, optionally limited
func (s *Store) GetMessages(convID string, limit int) ([]MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, conversation_id, role, content, timestamp, tokens
		FROM messages
		WHERE conversation_id = ?
		ORDER BY timestamp ASC
	`
	if limit > 0 {
		query += ` LIMIT ?`
	}

	rows, err := s.db.Query(query, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageRecord
	for rows.Next() {
		var msg MessageRecord
		err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &msg.Timestamp, &msg.Tokens)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// GetMessageCount returns the number of messages in a conversation
func (s *Store) GetMessageCount(convID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM messages WHERE conversation_id = ?
	`, convID).Scan(&count)

	return count, err
}

// GetTotalTokens returns the total token count for messages in a conversation
func (s *Store) GetTotalTokens(convID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total int
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(tokens), 0) FROM messages WHERE conversation_id = ?
	`, convID).Scan(&total)

	return total, err
}

// DeleteMessagesBefore deletes messages older than the given timestamp
func (s *Store) DeleteMessagesBefore(convID string, before int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		DELETE FROM messages WHERE conversation_id = ? AND timestamp < ?
	`, convID, before)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// DeleteOldMessages deletes all but the most recent N messages in a conversation
func (s *Store) DeleteOldMessages(convID string, keepLatest int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		DELETE FROM messages WHERE id IN (
			SELECT id FROM messages
			WHERE conversation_id = ?
			ORDER BY timestamp DESC
			LIMIT -1 OFFSET ?
		)
	`, convID, keepLatest)

	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// User Memory operations

// GetUserMemory retrieves user memory data
func (s *Store) GetUserMemory(userID int64) (*UserMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var mem UserMemory
	var preferences, notes sql.NullString

	err := s.db.QueryRow(`
		SELECT user_id, preferences, interaction_count, last_interaction, notes
		FROM user_memory WHERE user_id = ?
	`, userID).Scan(&mem.UserID, &preferences, &mem.InteractionCount, &mem.LastInteraction, &notes)

	if err == sql.ErrNoRows {
		// Return empty memory for new users
		return &UserMemory{UserID: userID}, nil
	}
	if err != nil {
		return nil, err
	}

	mem.UserID = userID
	if preferences.Valid {
		if err := json.Unmarshal([]byte(preferences.String), &mem.Preferences); err != nil {
			mem.Preferences = make(map[string]any)
		}
	} else {
		mem.Preferences = make(map[string]any)
	}

	if notes.Valid {
		mem.Notes = notes.String
	}

	return &mem, nil
}

// SaveUserMemory saves user memory data
func (s *Store) SaveUserMemory(mem *UserMemory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	preferencesJSON, err := json.Marshal(mem.Preferences)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO user_memory (user_id, preferences, interaction_count, last_interaction, notes)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			preferences = excluded.preferences,
			interaction_count = excluded.interaction_count,
			last_interaction = excluded.last_interaction,
			notes = excluded.notes
	`, mem.UserID, string(preferencesJSON), mem.InteractionCount, mem.LastInteraction, mem.Notes)

	return err
}

// IncrementInteractionCount increments the user's interaction count
func (s *Store) IncrementInteractionCount(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO user_memory (user_id, interaction_count, last_interaction)
		VALUES (?, 1, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			interaction_count = interaction_count + 1,
			last_interaction = ?
	`, userID, now, now)

	return err
}

// Summary operations

// SaveSummary saves a summary for a conversation
func (s *Store) SaveSummary(convID, summary string, messageCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO conversation_summaries (conversation_id, summary, message_count, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			summary = excluded.summary,
			message_count = excluded.message_count,
			created_at = excluded.created_at
	`, convID, summary, messageCount, now)

	return err
}

// GetSummary retrieves the summary for a conversation
func (s *Store) GetSummary(convID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var summary string
	err := s.db.QueryRow(`
		SELECT summary FROM conversation_summaries WHERE conversation_id = ?
	`, convID).Scan(&summary)

	if err == sql.ErrNoRows {
		return "", nil
	}
	return summary, err
}

// Cleanup operations

// DeleteOldConversations deletes conversations older than the given duration
func (s *Store) DeleteOldConversations(olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan).Unix()
	result, err := s.db.Exec(`DELETE FROM conversations WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Vacuum runs VACUUM to reclaim disk space
func (s *Store) Vacuum() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`VACUUM`)
	return err
}

// Stats returns statistics about the store
func (s *Store) Stats() (*StoreStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats StoreStats

	// Count conversations
	err := s.db.QueryRow(`SELECT COUNT(*) FROM conversations`).Scan(&stats.ConversationCount)
	if err != nil {
		return nil, err
	}

	// Count messages
	err = s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&stats.MessageCount)
	if err != nil {
		return nil, err
	}

	// Count users
	err = s.db.QueryRow(`SELECT COUNT(*) FROM user_memory`).Scan(&stats.UserCount)
	if err != nil {
		return nil, err
	}

	// Get database size
	var pageSize, pageCount int
	err = s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount)
	if err != nil {
		return nil, err
	}
	stats.SizeBytes = int64(pageSize) * int64(pageCount)

	return &stats, nil
}

// Record types

type ConversationRecord struct {
	ID        string
	UserID    int64
	ChatID    int64
	ChatType  string
	CreatedAt int64
	UpdatedAt int64
}

type MessageRecord struct {
	ID             int
	ConversationID string
	Role           string
	Content        string
	Timestamp      int64
	Tokens         int
}

type UserMemory struct {
	UserID            int64
	Preferences       map[string]any
	InteractionCount  int
	LastInteraction   int64
	Notes             string
}

type StoreStats struct {
	ConversationCount int
	MessageCount      int
	UserCount         int
	SizeBytes         int64
	QAPairCount       int
	PendingQuestionCount int
}

// ============================================================================
// Q&A Pair Operations
// ============================================================================

// AddQAPair adds a new Q&A pair to the store
func (s *Store) AddQAPair(pair *QAPair) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	result, err := s.db.Exec(`
		INSERT INTO qa_pairs (question, answer, question_normalized,
			source_chat_id, source_user_id, answer_user_id, is_admin_answer,
			confidence, created_at, usage_count, success_count, fail_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0)
	`, pair.Question, pair.Answer, pair.QuestionNormalized,
		pair.SourceChatID, pair.SourceUserID, pair.AnswerUserID,
		boolToInt(pair.IsAdminAnswer), pair.Confidence, now)

	if err != nil {
		return err
	}

	// Get the ID and add keywords
	id, _ := result.LastInsertId()
	pair.ID = id

	// Extract and add keywords using simple extraction
	keywords := s.extractKeywords(pair.Question)
	for _, kw := range keywords {
		if _, err := s.db.Exec(`INSERT INTO qa_keywords (qa_id, keyword) VALUES (?, ?)`, id, kw); err != nil {
			// Log but continue on keyword error
			continue
		}
	}

	return nil
}

// FindQAByNormalized finds a Q&A pair by normalized question
func (s *Store) FindQAByNormalized(normalized string) (*QAPair, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pair QAPair
	err := s.db.QueryRow(`
		SELECT id, question, answer, question_normalized, source_chat_id,
			source_user_id, answer_user_id, is_admin_answer, confidence,
			created_at, last_used_at, usage_count, success_count, fail_count
		FROM qa_pairs WHERE question_normalized = ?
		LIMIT 1
	`, normalized).Scan(
		&pair.ID, &pair.Question, &pair.Answer, &pair.QuestionNormalized,
		&pair.SourceChatID, &pair.SourceUserID, &pair.AnswerUserID,
		&pair.IsAdminAnswer, &pair.Confidence, &pair.CreatedAt,
		&pair.LastUsedAt, &pair.UsageCount, &pair.SuccessCount,
		&pair.FailCount,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &pair, err
}

// FindQAByKeywords finds Q&A pairs matching keywords
func (s *Store) FindQAByKeywords(keywords []string) ([]*QAPair, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(keywords) == 0 {
		return nil, nil
	}

	// Build query with placeholders
	query := `
		SELECT DISTINCT q.id, q.question, q.answer, q.question_normalized,
			q.source_chat_id, q.source_user_id, q.answer_user_id,
			q.is_admin_answer, q.confidence, q.created_at, q.last_used_at,
			q.usage_count, q.success_count, q.fail_count
		FROM qa_pairs q
		INNER JOIN qa_keywords k ON q.id = k.qa_id
		WHERE k.keyword IN (`

	args := make([]interface{}, len(keywords))
	for i, kw := range keywords {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args[i] = kw
	}
	query += ") ORDER BY q.usage_count DESC, q.created_at DESC LIMIT 20"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []*QAPair
	for rows.Next() {
		var pair QAPair
		err := rows.Scan(
			&pair.ID, &pair.Question, &pair.Answer, &pair.QuestionNormalized,
			&pair.SourceChatID, &pair.SourceUserID, &pair.AnswerUserID,
			&pair.IsAdminAnswer, &pair.Confidence, &pair.CreatedAt,
			&pair.LastUsedAt, &pair.UsageCount, &pair.SuccessCount,
			&pair.FailCount,
		)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, &pair)
	}

	return pairs, nil
}

// GetQAKeywords gets keywords for a Q&A pair
func (s *Store) GetQAKeywords(qaID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT keyword FROM qa_keywords WHERE qa_id = ?`, qaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keywords []string
	for rows.Next() {
		var kw string
		if err := rows.Scan(&kw); err != nil {
			return nil, err
		}
		keywords = append(keywords, kw)
	}

	return keywords, nil
}

// GetRecentQAPairs gets recent/popular Q&A pairs
func (s *Store) GetRecentQAPairs(limit int) ([]*QAPair, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, question, answer, question_normalized, source_chat_id,
			source_user_id, answer_user_id, is_admin_answer, confidence,
			created_at, last_used_at, usage_count, success_count, fail_count
		FROM qa_pairs ORDER BY usage_count DESC, created_at DESC LIMIT ?
	`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []*QAPair
	for rows.Next() {
		var pair QAPair
		err := rows.Scan(
			&pair.ID, &pair.Question, &pair.Answer, &pair.QuestionNormalized,
			&pair.SourceChatID, &pair.SourceUserID, &pair.AnswerUserID,
			&pair.IsAdminAnswer, &pair.Confidence, &pair.CreatedAt,
			&pair.LastUsedAt, &pair.UsageCount, &pair.SuccessCount,
			&pair.FailCount,
		)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, &pair)
	}

	return pairs, nil
}

// UpdateQAUsage updates usage statistics for a Q&A pair
func (s *Store) UpdateQAUsage(qaID int64, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()

	if success {
		_, err := s.db.Exec(`
			UPDATE qa_pairs
			SET usage_count = usage_count + 1,
				success_count = success_count + 1,
				last_used_at = ?
			WHERE id = ?
		`, now, qaID)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE qa_pairs
		SET usage_count = usage_count + 1,
			fail_count = fail_count + 1,
			last_used_at = ?
		WHERE id = ?
	`, now, qaID)
	return err
}

// GetQAPairByID gets a Q&A pair by ID
func (s *Store) GetQAPairByID(id int64) (*QAPair, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pair QAPair
	err := s.db.QueryRow(`
		SELECT id, question, answer, question_normalized, source_chat_id,
			source_user_id, answer_user_id, is_admin_answer, confidence,
			created_at, last_used_at, usage_count, success_count, fail_count
		FROM qa_pairs WHERE id = ?
	`, id).Scan(
		&pair.ID, &pair.Question, &pair.Answer, &pair.QuestionNormalized,
		&pair.SourceChatID, &pair.SourceUserID, &pair.AnswerUserID,
		&pair.IsAdminAnswer, &pair.Confidence, &pair.CreatedAt,
		&pair.LastUsedAt, &pair.UsageCount, &pair.SuccessCount,
		&pair.FailCount,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &pair, err
}

// DeleteQAPair deletes a Q&A pair
func (s *Store) DeleteQAPair(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM qa_pairs WHERE id = ?`, id)
	return err
}

// GetAllQAPairs returns all Q&A pairs with pagination
func (s *Store) GetAllQAPairs(offset, limit int) ([]*QAPair, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, question, answer, question_normalized, source_chat_id,
			source_user_id, answer_user_id, is_admin_answer, confidence,
			created_at, last_used_at, usage_count, success_count, fail_count
		FROM qa_pairs ORDER BY created_at DESC LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []*QAPair
	for rows.Next() {
		var pair QAPair
		err := rows.Scan(
			&pair.ID, &pair.Question, &pair.Answer, &pair.QuestionNormalized,
			&pair.SourceChatID, &pair.SourceUserID, &pair.AnswerUserID,
			&pair.IsAdminAnswer, &pair.Confidence, &pair.CreatedAt,
			&pair.LastUsedAt, &pair.UsageCount, &pair.SuccessCount,
			&pair.FailCount,
		)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, &pair)
	}

	return pairs, nil
}

// ============================================================================
// Pending Question Operations
// ============================================================================

// AddPendingQuestion adds a pending question
func (s *Store) AddPendingQuestion(q *PendingQuestion) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO pending_questions
			(chat_id, message_id, user_id, question, question_normalized,
			 asked_at, expires_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, q.ChatID, q.MessageID, q.UserID, q.Question,
		q.QuestionNormalized, q.AskedAt, q.ExpiresAt, q.Status)

	return err
}

// GetPendingQuestions gets pending questions for a chat
func (s *Store) GetPendingQuestions(chatID int64, status string) ([]*PendingQuestion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, chat_id, message_id, user_id, question, question_normalized,
			asked_at, expires_at, status
		FROM pending_questions
		WHERE chat_id = ? AND status = ? AND expires_at > ?
		ORDER BY asked_at DESC
	`, chatID, status, time.Now().Unix())

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var questions []*PendingQuestion
	for rows.Next() {
		var q PendingQuestion
		err := rows.Scan(
			&q.ID, &q.ChatID, &q.MessageID, &q.UserID,
			&q.Question, &q.QuestionNormalized, &q.AskedAt,
			&q.ExpiresAt, &q.Status,
		)
		if err != nil {
			return nil, err
		}
		questions = append(questions, &q)
	}

	return questions, nil
}

// UpdatePendingQuestionStatus updates the status of a pending question
func (s *Store) UpdatePendingQuestionStatus(id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE pending_questions SET status = ? WHERE id = ?
	`, status, id)

	return err
}

// CleanupExpiredQuestions removes expired pending questions
func (s *Store) CleanupExpiredQuestions() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	result, err := s.db.Exec(`
		DELETE FROM pending_questions WHERE expires_at < ?
	`, now)

	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// GetPendingQuestionCount returns count of pending questions by status
func (s *Store) GetPendingQuestionCount(status string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM pending_questions WHERE status = ? AND expires_at > ?
	`, status, time.Now().Unix()).Scan(&count)

	return count, err
}

// ============================================================================
// Helper Functions
// ============================================================================

// boolToInt converts a boolean to integer (0 or 1)
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// intToBool converts an integer to boolean
func intToBool(i int) bool {
	return i != 0
}

// extractKeywords extracts keywords from text
func (s *Store) extractKeywords(text string) []string {
	// Simple extraction: split by whitespace and filter short words
	words := strings.Fields(text)
	keywords := make(map[string]bool)

	// Stop words (common words to ignore)
	stopWords := map[string]bool{
		"的": true, "了": true, "在": true, "是": true,
		"我": true, "有": true, "和": true, "就": true,
		"不": true, "人": true, "都": true, "一": true,
		"一个": true, "上": true, "也": true, "很": true,
		"到": true, "说": true, "要": true, "去": true,
		"你": true, "会": true, "着": true, "没有": true,
		"吗": true, "呢": true, "吧": true,
	}

	for _, word := range words {
		word = strings.ToLower(strings.Trim(word, "。，、；：？！\"'（）【】"))
		if len(word) >= 2 && !stopWords[word] {
			keywords[word] = true
		}
	}

	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}
	return result
}

// runMigrations runs any necessary database migrations
func (s *Store) runMigrations() error {
	// Check if qa_pairs table exists, if not create it
	var tableExists int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='qa_pairs'").Scan(&tableExists)
	if err != nil {
		return err
	}

	if tableExists == 0 {
		log.Println("[Store] Running Q&A migration...")
		qaSchema := `
		CREATE TABLE IF NOT EXISTS qa_pairs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			question_normalized TEXT NOT NULL,
			source_chat_id INTEGER NOT NULL,
			source_user_id INTEGER NOT NULL,
			answer_user_id INTEGER NOT NULL,
			is_admin_answer INTEGER DEFAULT 0,
			confidence REAL DEFAULT 0.5,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER,
			usage_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			fail_count INTEGER DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_qa_question_normalized ON qa_pairs(question_normalized);
		CREATE INDEX IF NOT EXISTS idx_qa_source_chat ON qa_pairs(source_chat_id);
		CREATE INDEX IF NOT EXISTS idx_qa_created_at ON qa_pairs(created_at);
		CREATE INDEX IF NOT EXISTS idx_qa_usage_count ON qa_pairs(usage_count DESC);

		CREATE TABLE IF NOT EXISTS qa_keywords (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			qa_id INTEGER NOT NULL,
			keyword TEXT NOT NULL,
			FOREIGN KEY (qa_id) REFERENCES qa_pairs(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_qa_keywords_keyword ON qa_keywords(keyword);
		CREATE INDEX IF NOT EXISTS idx_qa_keywords_qa_id ON qa_keywords(qa_id);

		CREATE TABLE IF NOT EXISTS pending_questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			message_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			question TEXT NOT NULL,
			question_normalized TEXT NOT NULL,
			asked_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			status TEXT DEFAULT 'pending'
		);

		CREATE INDEX IF NOT EXISTS idx_pending_qa_chat ON pending_questions(chat_id, asked_at);
		CREATE INDEX IF NOT EXISTS idx_pending_qa_status ON pending_questions(status);
		CREATE INDEX IF NOT EXISTS idx_pending_qa_expires ON pending_questions(expires_at);
		`
		_, err = s.db.Exec(qaSchema)
		if err != nil {
			return fmt.Errorf("failed to create Q&A tables: %w", err)
		}
		log.Println("[Store] Q&A tables created successfully")
	}

	return nil
}
