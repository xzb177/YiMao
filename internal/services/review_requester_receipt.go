package services

import "fmt"

// SetRequesterReceipt records where the requester-side submission receipt card
// lives so review outcomes can update that exact message in place instead of
// only editing the administrator review message.
func (s *ReviewService) SetRequesterReceipt(requestID string, chatID, messageID int64) error {
	return s.setRequesterReceipt(requestID, chatID, messageID, 0)
}

func (s *ReviewService) SetRequesterEphemeralReceipt(requestID string, chatID, ephemeralMessageID int64) error {
	if ephemeralMessageID == 0 {
		return fmt.Errorf("invalid requester receipt coordinates")
	}
	return s.setRequesterReceipt(requestID, chatID, 0, ephemeralMessageID)
}

func (s *ReviewService) setRequesterReceipt(requestID string, chatID, messageID, ephemeralMessageID int64) error {
	if requestID == "" || chatID == 0 || (messageID == 0 && ephemeralMessageID == 0) {
		return fmt.Errorf("invalid requester receipt coordinates")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	review, exists := s.reviews[requestID]
	if !exists || review == nil {
		return fmt.Errorf("review request not found: %s", requestID)
	}
	if review.RequesterChatID == chatID && review.RequesterReceiptMsgID == messageID && review.RequesterReceiptEphemeralID == ephemeralMessageID {
		return nil
	}
	prevChat, prevMsg, prevEph := review.RequesterChatID, review.RequesterReceiptMsgID, review.RequesterReceiptEphemeralID
	review.RequesterChatID = chatID
	review.RequesterReceiptMsgID = messageID
	review.RequesterReceiptEphemeralID = ephemeralMessageID
	if err := s.saveLocked(); err != nil {
		review.RequesterChatID, review.RequesterReceiptMsgID, review.RequesterReceiptEphemeralID = prevChat, prevMsg, prevEph
		return err
	}
	return nil
}
