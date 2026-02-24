package ai

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	`

	_, err := s.db.Exec(schema)
	return err
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
}
