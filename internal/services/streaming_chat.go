package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"emby-telegram-bot/ai"
)

// StreamingChatHandler handles streaming AI chat for group chats
type StreamingChatHandler struct {
	agent         *ai.Agent
	convMgr       *ai.ConversationManager
	tgClient      *TelegramClient
	sessions      map[int64]*StreamingSession
	mu            sync.RWMutex
	qaMatcher     *ai.QNAMatcher
	qaExtractor   *ai.QNAExtractor
	store         *ai.Store
	enableQAMatch bool
	enableQALearn bool
	adminIDs      map[int64]bool
}

// StreamingSession represents an active streaming session
type StreamingSession struct {
	ConversationID string
	MessageID      int64
	StartedAt      time.Time
	cancel         context.CancelFunc
}

// NewStreamingChatHandler creates a new streaming chat handler
func NewStreamingChatHandler(agent *ai.Agent, convMgr *ai.ConversationManager, tgClient *TelegramClient, store *ai.Store) *StreamingChatHandler {
	var qaMatcher *ai.QNAMatcher
	var qaExtractor *ai.QNAExtractor
	if store != nil {
		qaMatcher = ai.NewQNAMatcher(store)
		qaExtractor = ai.NewQNAExtractor(store, nil)
	}

	return &StreamingChatHandler{
		agent:         agent,
		convMgr:       convMgr,
		tgClient:      tgClient,
		sessions:      make(map[int64]*StreamingSession),
		qaMatcher:     qaMatcher,
		qaExtractor:   qaExtractor,
		store:         store,
		enableQAMatch: true,
		enableQALearn: true,
		adminIDs:      make(map[int64]bool),
	}
}

// HandleMessage handles an incoming message with streaming response
func (h *StreamingChatHandler) HandleMessage(ctx context.Context, userID, chatID int64, message string) error {
	// Check for Q&A match first (before AI call)
	if h.enableQAMatch && h.qaMatcher != nil {
		if match, err := h.qaMatcher.FindBestMatch(message); err == nil && match != nil && ai.ShouldUseQAMatch(match, 0.5) {
			// Found a matching Q&A pair - use it directly
			response := ai.FormatQAResponse(match.QAPair, match.Similarity, true)
			_, err := h.tgClient.SendMessage(chatID, response, "", nil)
			if err == nil {
				// Track usage
				_ = h.store.UpdateQAUsage(match.QAPair.ID, true)
				log.Printf("[StreamingChat] Used Q&A match (similarity: %.2f, type: %s)", match.Similarity, match.MatchType)
				return nil
			}
		}
	}

	// Determine chat type
	chatType := h.getChatType(chatID)

	// Get or create conversation
	conv, err := h.convMgr.GetOrCreate(ctx, userID, chatID, chatType)
	if err != nil {
		return fmt.Errorf("failed to get conversation: %w", err)
	}

	// Add user message to conversation
	if err := h.convMgr.AddAndSaveMessage(conv.ID, "user", message); err != nil {
		log.Printf("[StreamingChat] Failed to save user message: %v", err)
	}

	// Compact if needed
	if err := h.convMgr.CompactIfNeeded(conv.ID); err != nil {
		log.Printf("[StreamingChat] Failed to compact conversation: %v", err)
	}

	// Cancel any existing session for this chat
	h.cancelSession(chatID)

	// Send "thinking" message
	thinkingMsg := "💭 小凛正在思考..."
	sentMsg, err := h.tgClient.SendMessage(chatID, thinkingMsg, "", nil)
	if err != nil {
		return fmt.Errorf("failed to send thinking message: %w", err)
	}

	// Get message ID from the response
	msgID := sentMsg.MessageID

	// Create context with cancel for this session
	streamCtx, cancel := context.WithCancel(ctx)

	// Store session
	h.mu.Lock()
	h.sessions[chatID] = &StreamingSession{
		ConversationID: conv.ID,
		MessageID:      msgID,
		StartedAt:      time.Now(),
		cancel:         cancel,
	}
	h.mu.Unlock()

	// Start streaming in background
	go h.streamResponse(streamCtx, chatID, userID, conv, msgID)

	return nil
}

// streamResponse handles the actual streaming of the AI response
func (h *StreamingChatHandler) streamResponse(ctx context.Context, chatID, userID int64, conv *ai.Conversation, msgID int64) {
	// Build chat request
	req := conv.ToChatRequest(h.agent.GetSystemPrompt(), h.agent.GetMaxTokens(), h.agent.GetTemperature())

	// Get messages from conversation
	req.Messages = conv.GetMessages()

	// Stream from AI provider
	chunkChan, err := h.agent.Stream(ctx, req)
	if err != nil {
		log.Printf("[StreamingChat] Failed to start stream: %v", err)
		h.handleError(chatID, msgID, err)
		h.cleanupSession(chatID)
		return
	}

	// Process stream
	processor := ai.NewStreamProcessor(50, 1000*time.Millisecond)
	var fullContent strings.Builder
	var lastEdit time.Time
	editInterval := 1500 * time.Millisecond // Minimum time between edits (Telegram group limit)

	for {
		select {
		case <-ctx.Done():
			// Context cancelled - finalize and exit
			h.finalizeMessage(chatID, msgID, fullContent.String())
			h.cleanupSession(chatID)
			return

		case chunk, ok := <-chunkChan:
			if !ok {
				// Channel closed - finalize
				h.finalizeMessage(chatID, msgID, fullContent.String())
				h.cleanupSession(chatID)
				return
			}

			if chunk.Err != nil {
				log.Printf("[StreamingChat] Stream error: %v", chunk.Err)
				h.handleError(chatID, msgID, chunk.Err)
				h.cleanupSession(chatID)
				return
			}

			// Process chunk
			shouldFlush, content, _ := processor.ProcessChunk(&chunk)

			if shouldFlush && content != "" {
				fullContent.WriteString(content)

				// Rate limit edits
				timeSinceLastEdit := time.Since(lastEdit)
				if timeSinceLastEdit >= editInterval || chunk.Done {
					// Try to edit message with retry on rate limit
					var editErr error
					maxRetries := 3
					for retry := 0; retry < maxRetries; retry++ {
						_, editErr = h.tgClient.EditMessage(chatID, msgID, content, "", nil)
						if editErr == nil {
							lastEdit = time.Now()
							break
						}
						// Check if it's a rate limit error
						if strings.Contains(editErr.Error(), "Too Many Requests") {
							waitTime := 3 * time.Second
							log.Printf("[StreamingChat] Rate limited, waiting %v...", waitTime)
							time.Sleep(waitTime)
							continue
						}
						// Other errors - log and break
						log.Printf("[StreamingChat] Failed to edit message: %v", editErr)
						break
					}
				}
			}

			if chunk.Done {
				// Flush any remaining content
				remaining, _ := processor.Flush()
				if remaining != "" {
					fullContent.WriteString(remaining)
				}
				h.finalizeMessage(chatID, msgID, fullContent.String())
				h.cleanupSession(chatID)
				return
			}
		}
	}
}

// finalizeMessage finalizes the streaming message and saves to conversation
func (h *StreamingChatHandler) finalizeMessage(chatID int64, msgID int64, content string) {
	if content == "" {
		content = "抱歉，我遇到了一些问题，请稍后再试。"
	}

	// Final edit to ensure message is complete
	if _, err := h.tgClient.EditMessage(chatID, msgID, content, "", nil); err != nil {
		log.Printf("[StreamingChat] Failed to finalize message: %v", err)
	}

	// Get session for conversation ID
	h.mu.RLock()
	session := h.sessions[chatID]
	h.mu.RUnlock()

	if session != nil {
		// Save assistant message to conversation
		if err := h.convMgr.AddAndSaveMessage(session.ConversationID, "assistant", content); err != nil {
			log.Printf("[StreamingChat] Failed to save assistant message: %v", err)
		}
	}
}

// handleError handles an error during streaming
func (h *StreamingChatHandler) handleError(chatID int64, msgID int64, err error) {
	errorMsg := "抱歉，我遇到了一些问题，请稍后再试。"
	if err != nil {
		log.Printf("[StreamingChat] Error: %v", err)
	}

	// Try to update the message with error info
	_, _ = h.tgClient.EditMessage(chatID, msgID, errorMsg, "", nil)
}

// cancelSession cancels an active streaming session for a chat
func (h *StreamingChatHandler) cancelSession(chatID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, exists := h.sessions[chatID]; exists {
		if session.cancel != nil {
			session.cancel()
		}
		delete(h.sessions, chatID)
	}
}

// cleanupSession removes a completed session
func (h *StreamingChatHandler) cleanupSession(chatID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.sessions, chatID)
}

// getChatType determines the type of chat (private, group, supergroup)
func (h *StreamingChatHandler) getChatType(chatID int64) string {
	// Private chats have negative IDs in Telegram
	if chatID > 0 {
		return "private"
	}
	// Group chats have negative IDs
	// -100 is the prefix for supergroups/channels
	if chatID < -1000000000 {
		return "supergroup"
	}
	return "group"
}

// CancelStream cancels any active stream for the given chat
func (h *StreamingChatHandler) CancelStream(chatID int64) {
	h.cancelSession(chatID)
}

// GetActiveSessions returns the number of active streaming sessions
func (h *StreamingChatHandler) GetActiveSessions() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.sessions)
}

// IsStreaming checks if a chat currently has an active streaming session
func (h *StreamingChatHandler) IsStreaming(chatID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, exists := h.sessions[chatID]
	return exists
}

// ============================================================================
// Q&A Learning Integration
// ============================================================================

// ProcessMessageForLearning processes a message for Q&A learning (called for every group message)
func (h *StreamingChatHandler) ProcessMessageForLearning(ctx context.Context, userID, chatID, msgID int64, userName, content string, timestamp time.Time) {
	if !h.enableQALearn || h.qaExtractor == nil {
		return
	}

	msg := &ai.QNAMessage{
		UserID:    userID,
		UserName:  userName,
		Content:   content,
		ChatID:    chatID,
		MessageID: msgID,
		Timestamp: timestamp,
		IsReply:   false,
		IsMention: false,
	}

	candidate, err := h.qaExtractor.ProcessMessage(ctx, msg)
	if err != nil {
		log.Printf("[StreamingChat] Q&A processing error: %v", err)
		return
	}

	// If we found a Q&A candidate, save it
	if candidate != nil {
		qaPair, err := h.qaExtractor.SaveQAPair(candidate)
		if err != nil {
			log.Printf("[StreamingChat] Failed to save Q&A pair: %v", err)
		} else if qaPair != nil {
			log.Printf("[StreamingChat] Learned new Q&A: %s -> truncate(%d)", truncate(candidate.Question, 30), qaPair.ID)
		}
	}
}

// SetAdminIDs sets the admin IDs for Q&A learning
func (h *StreamingChatHandler) SetAdminIDs(adminIDs []int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.adminIDs = make(map[int64]bool)
	for _, id := range adminIDs {
		h.adminIDs[id] = true
	}

	if h.qaExtractor != nil {
		h.qaExtractor.SetAdmins(adminIDs)
	}
}

// SetQAMatchEnabled enables or disables Q&A matching
func (h *StreamingChatHandler) SetQAMatchEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enableQAMatch = enabled
}

// SetQALearnEnabled enables or disables Q&A learning
func (h *StreamingChatHandler) SetQALearnEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enableQALearn = enabled
}

// GetQAStats returns Q&A learning statistics
func (h *StreamingChatHandler) GetQAStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["qa_match_enabled"] = h.enableQAMatch
	stats["qa_learn_enabled"] = h.enableQALearn

	if h.qaExtractor != nil {
		extractorStats := h.qaExtractor.GetStats()
		for k, v := range extractorStats {
			stats[k] = v
		}
	}

	if h.store != nil {
		storeStats, err := h.store.Stats()
		if err == nil {
			stats["qa_pairs_count"] = storeStats.QAPairCount
			stats["pending_questions_count"] = storeStats.PendingQuestionCount
		}
	}

	return stats
}

// truncate truncates a string to a maximum length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
