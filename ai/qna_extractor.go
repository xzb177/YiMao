package ai

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// Answer window - how long to wait for an answer after a question
const AnswerWindow = 3 * time.Minute

// QNAExtractor handles extraction of Q&A pairs from chat messages
type QNAExtractor struct {
	detector     *QNADetector
	store        *Store
	pending      map[int64][]*PendingQuestion // chat_id -> pending questions
	mu           sync.RWMutex
	adminIDs     map[int64]bool

	// Configuration
	adminAnswerBonus  float64 // Bonus for admin answers
	minAnswerQuality  float64 // Minimum quality score for an answer
	enabled           bool
}

// QACandidate represents a potential Q&A pair
type QACandidate struct {
	Question      string
	QuestionMsgID int64
	QuestionUser  int64
	QuestionTime  time.Time

	Answer     string
	AnswerMsgID int64
	AnswerUser int64
	AnswerTime time.Time

	IsAdminAnswer bool
	ChatID        int64
	Confidence    float64
}

// NewQNAExtractor creates a new Q&A extractor
func NewQNAExtractor(store *Store, adminIDs []int64) *QNAExtractor {
	adminMap := make(map[int64]bool)
	for _, id := range adminIDs {
		adminMap[id] = true
	}

	return &QNAExtractor{
		detector:        NewQNADetector(adminIDs),
		store:           store,
		pending:         make(map[int64][]*PendingQuestion),
		adminIDs:        adminMap,
		adminAnswerBonus: 0.3,
		minAnswerQuality:  0.4,
		enabled:          true,
	}
}

// SetEnabled enables or disables the extractor
func (e *QNAExtractor) SetEnabled(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = enabled
}

// SetAdmins updates the admin IDs
func (e *QNAExtractor) SetAdmins(adminIDs []int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.adminIDs = make(map[int64]bool)
	for _, id := range adminIDs {
		e.adminIDs[id] = true
	}
	e.detector.SetAdmins(adminIDs)
}

// ProcessMessage processes a message and potentially extracts a Q&A pair
func (e *QNAExtractor) ProcessMessage(ctx context.Context, msg *QNAMessage) (*QACandidate, error) {
	if !e.enabled || e.store == nil {
		return nil, nil
	}

	// First check if this is a question
	isQuestion, _ := e.detector.IsEmbyQuestion(msg.Content)

	if isQuestion {
		// Store as pending question
		pending := &PendingQuestion{
			ChatID:             msg.ChatID,
			MessageID:          msg.MessageID,
			UserID:             msg.UserID,
			Question:           msg.Content,
			QuestionNormalized: e.detector.NormalizeQuestion(msg.Content),
			AskedAt:            time.Now().Unix(),
			ExpiresAt:          time.Now().Add(AnswerWindow).Unix(),
			Status:             "pending",
		}

		if err := e.store.AddPendingQuestion(pending); err != nil {
			log.Printf("[QNA] Failed to store pending question: %v", err)
		}

		e.trackPendingQuestion(pending)
		return nil, nil
	}

	// Check if this could be an answer to a pending question
	if e.detector.IsPotentialAnswer(msg.Content) {
		return e.matchAnswerToQuestion(ctx, msg)
	}

	return nil, nil
}

// matchAnswerToQuestion attempts to match an answer to a pending question
func (e *QNAExtractor) matchAnswerToQuestion(ctx context.Context, msg *QNAMessage) (*QACandidate, error) {
	// Get pending questions for this chat
	pending, err := e.store.GetPendingQuestions(msg.ChatID, "pending")
	if err != nil || len(pending) == 0 {
		return nil, err
	}

	// Find the most recent unanswered question
	var bestMatch *PendingQuestion
	for _, q := range pending {
		if q.ExpiresAt < time.Now().Unix() {
			continue // Expired
		}
		if bestMatch == nil || q.AskedAt > bestMatch.AskedAt {
			bestMatch = q
		}
	}

	if bestMatch == nil {
		return nil, nil
	}

	// Check if this is from a different user (self-answers are less valuable)
	// Actually, self-answers can be valuable too - they might be corrections
	// Let's accept self-answers but with lower confidence

	// Check if answer is from admin
	isAdminAnswer := e.adminIDs[msg.UserID]

	// Calculate confidence
	baseConfidence := 0.5
	if isAdminAnswer {
		baseConfidence += e.adminAnswerBonus
	}

	// Check answer quality using heuristics
	answerQuality := e.evaluateAnswerQuality(bestMatch.Question, msg.Content)
	if answerQuality < e.minAnswerQuality {
		log.Printf("[QNA] Answer quality too low: %.2f", answerQuality)
		return nil, nil
	}
	baseConfidence += answerQuality * 0.2

	// Create Q&A candidate
	candidate := &QACandidate{
		Question:      bestMatch.Question,
		QuestionMsgID: bestMatch.MessageID,
		QuestionUser:  bestMatch.UserID,
		QuestionTime:  time.Unix(bestMatch.AskedAt, 0),

		Answer:     msg.Content,
		AnswerMsgID: msg.MessageID,
		AnswerUser: msg.UserID,
		AnswerTime: time.Now(),

		IsAdminAnswer: isAdminAnswer,
		ChatID:       msg.ChatID,
		Confidence:   baseConfidence,
	}

	// Mark question as answered
	if err := e.store.UpdatePendingQuestionStatus(bestMatch.ID, "answered"); err != nil {
		log.Printf("[QNA] Failed to update pending question: %v", err)
	}

	return candidate, nil
}

// evaluateAnswerQuality evaluates if an answer is good using heuristics
func (e *QNAExtractor) evaluateAnswerQuality(question, answer string) float64 {
	qualityScore := 0.0
	answerLower := strings.ToLower(answer)

	// Length check (good answers are usually substantial)
	if len(answer) >= 20 {
		qualityScore += 0.2
	}
	if len(answer) >= 50 {
		qualityScore += 0.1
	}

	// Contains actionable information
	actionable := []string{"点击", "选择", "输入", "设置", "://", "步骤", "方法", "操作", "可以", "需要"}
	for _, word := range actionable {
		if strings.Contains(answerLower, word) {
			qualityScore += 0.15
			break
		}
	}

	// Contains structured info (numbered list)
	if strings.Contains(answer, "1.") || strings.Contains(answer, "1、") {
		qualityScore += 0.2
	}

	// Contains URL
	if strings.Contains(answer, "http") {
		qualityScore += 0.15
	}

	// Contains Emby-related terms
	embyTerms := []string{"emby", "播放", "设置", "账号", "绑定", "转码", "字幕"}
	for _, term := range embyTerms {
		if strings.Contains(answerLower, term) {
			qualityScore += 0.1
			break
		}
	}

	// Not too short (less than 10 chars is suspicious)
	if len(answer) < 10 {
		qualityScore -= 0.3
	}

	// Cap at 1.0
	if qualityScore > 1.0 {
		qualityScore = 1.0
	}

	return qualityScore
}

// trackPendingQuestion tracks a pending question in memory
func (e *QNAExtractor) trackPendingQuestion(pending *PendingQuestion) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pending[pending.ChatID] = append(e.pending[pending.ChatID], pending)

	// Clean up old questions
	go func() {
		time.Sleep(AnswerWindow + 10*time.Second)
		e.removeExpiredQuestions(pending.ChatID)
	}()
}

// removeExpiredQuestions removes expired pending questions from memory
func (e *QNAExtractor) removeExpiredQuestions(chatID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().Unix()
	questions := e.pending[chatID]
	remaining := make([]*PendingQuestion, 0, len(questions))

	for _, q := range questions {
		if q.ExpiresAt > now && q.Status == "pending" {
			remaining = append(remaining, q)
		}
	}

	e.pending[chatID] = remaining
}

// SaveQAPair saves a Q&A candidate to the store
func (e *QNAExtractor) SaveQAPair(candidate *QACandidate) (*QAPair, error) {
	if e.store == nil {
		return nil, nil
	}

	qaPair := &QAPair{
		Question:           candidate.Question,
		Answer:             candidate.Answer,
		QuestionNormalized: e.detector.NormalizeQuestion(candidate.Question),
		SourceChatID:       candidate.ChatID,
		SourceUserID:       candidate.QuestionUser,
		AnswerUserID:       candidate.AnswerUser,
		IsAdminAnswer:      candidate.IsAdminAnswer,
		Confidence:         candidate.Confidence,
		CreatedAt:          time.Now().Unix(),
	}

	if err := e.store.AddQAPair(qaPair); err != nil {
		return nil, err
	}

	log.Printf("[QNA] Learned new Q&A pair (ID: %d, admin: %v, confidence: %.2f)",
		qaPair.ID, qaPair.IsAdminAnswer, qaPair.Confidence)

	return qaPair, nil
}

// CleanupExpiredPending removes expired pending questions from database
func (e *QNAExtractor) CleanupExpiredPending() (int64, error) {
	if e.store == nil {
		return 0, nil
	}

	return e.store.CleanupExpiredQuestions()
}

// GetStats returns statistics about the Q&A learning
func (e *QNAExtractor) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := map[string]interface{}{
		"enabled":        e.enabled,
		"pending_chats":  len(e.pending),
		"admin_ids":      len(e.adminIDs),
	}

	totalPending := 0
	for _, questions := range e.pending {
		totalPending += len(questions)
	}
	stats["pending_questions"] = totalPending

	return stats
}
