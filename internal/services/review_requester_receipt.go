package services

import "fmt"

// SetRequesterReceipt records where the requester-side submission receipt card
// lives so review outcomes can update that exact message in place instead of
// only editing the administrator review message.
func (s *ReviewService) SetRequesterReceipt(requestID string, chatID, messageID int64) error {
	if requestID == "" || chatID == 0 || messageID == 0 {
		return fmt.Errorf("invalid requester receipt coordinates")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	review, exists := s.reviews[requestID]
	if !exists || review == nil {
		return fmt.Errorf("review request not found: %s", requestID)
	}
	if review.RequesterChatID == chatID && review.RequesterReceiptMsgID == messageID {
		return nil
	}
	previousChat, previousMessage := review.RequesterChatID, review.RequesterReceiptMsgID
	review.RequesterChatID = chatID
	review.RequesterReceiptMsgID = messageID
	if err := s.saveLocked(); err != nil {
		review.RequesterChatID, review.RequesterReceiptMsgID = previousChat, previousMessage
		return err
	}
	return nil
}
