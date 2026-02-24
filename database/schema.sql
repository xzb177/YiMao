-- SQLite schema for AI Chat Feature Refactoring
-- This schema supports conversation persistence, message history, and user memory

-- Conversations table: stores metadata for each conversation
CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    chat_type TEXT NOT NULL,  -- 'private' or 'group' or 'supergroup'
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Messages table: stores individual messages within conversations
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    role TEXT NOT NULL,  -- 'user', 'assistant', 'system'
    content TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    tokens INTEGER DEFAULT 0,  -- Estimated token count for compaction
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_conv_user ON conversations(user_id);
CREATE INDEX IF NOT EXISTS idx_conv_chat_id ON conversations(chat_id);
CREATE INDEX IF NOT EXISTS idx_conv_updated ON conversations(updated_at);

-- User memory table: stores persistent user preferences and notes
CREATE TABLE IF NOT EXISTS user_memory (
    user_id INTEGER PRIMARY KEY,
    preferences TEXT,  -- JSON string for flexible key-value storage
    interaction_count INTEGER DEFAULT 0,
    last_interaction INTEGER,
    notes TEXT  -- JSON string for custom notes about the user
);

-- Session summary table: stores compacted summaries for long conversations
CREATE TABLE IF NOT EXISTS conversation_summaries (
    conversation_id TEXT PRIMARY KEY,
    summary TEXT NOT NULL,
    message_count INTEGER NOT NULL,  -- Number of messages summarized
    created_at INTEGER NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);
