package ai

import "time"

// QAPair represents a learned Q&A pair from group chat
type QAPair struct {
	ID                int64
	Question          string
	Answer            string
	QuestionNormalized string
	SourceChatID      int64
	SourceUserID      int64
	AnswerUserID      int64
	IsAdminAnswer     bool
	Confidence        float64
	CreatedAt         int64
	LastUsedAt        int64
	UsageCount        int
	SuccessCount      int
	FailCount         int
}

// PendingQuestion represents a question waiting for an answer
type PendingQuestion struct {
	ID                int64
	ChatID            int64
	MessageID         int64
	UserID            int64
	Question          string
	QuestionNormalized string
	AskedAt           int64
	ExpiresAt         int64
	Status            string // pending, answered, expired
}

// QAMatchResult represents a matched Q&A pair with similarity score
type QAMatchResult struct {
	QAPair     *QAPair
	Similarity float64
	MatchType  string // exact, keyword, fuzzy
}

// QNAMessage represents a chat message for Q&A processing (renamed to avoid conflict with ai/chat.go)
type QNAMessage struct {
	UserID    int64
	UserName  string
	Content   string
	ChatID    int64
	MessageID int64
	Timestamp time.Time
	IsReply   bool
	IsMention bool
}
