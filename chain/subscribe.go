package chain

import (
	"fmt"
	"log"
)

// SubscribeChain handles subscription operations
type SubscribeChain struct {
	*ChainBase
}

// NewSubscribeChain creates a new subscribe chain
func NewSubscribeChain(jellyseerrURL, apiKey string) *SubscribeChain {
	return &SubscribeChain{
		ChainBase: NewChainBase(jellyseerrURL, apiKey),
	}
}

// SubscribeRequest represents a subscription request
type SubscribeRequest struct {
	MediaID         int    `json:"mediaId"`
	MediaType       string `json:"mediaType"`
	Seasons         []int  `json:"seasons,omitempty"`
	Is4K            bool   `json:"is4K,omitempty"`
	ServerID        int    `json:"serverId,omitempty"`
	RootFolder      string `json:"rootFolder,omitempty"`
	QualityProfileID int   `json:"qualityProfileId,omitempty"`
	UserID          int    `json:"userId,omitempty"`
}

// SubscribeResult represents the result of a subscription
type SubscribeResult struct {
	ID          int    `json:"id"`
	MediaID     int    `json:"mediaId"`
	Status      int    `json:"status"`
	StatusStr   string `json:"statusStr,omitempty"`
	RequestType string `json:"requestType"`
	CreatedAt   string `json:"createdAt"`
}

// Subscribe creates a new subscription
func (s *SubscribeChain) Subscribe(mediaID int, mediaType string, seasons []int) (*SubscribeResult, error) {
	return s.SubscribeWithUser(mediaID, mediaType, seasons, 0)
}

// SubscribeWithUser creates a new subscription with a specific user ID
func (s *SubscribeChain) SubscribeWithUser(mediaID int, mediaType string, seasons []int, userID int) (*SubscribeResult, error) {
	endpoint := "/api/v1/request"

	payload := SubscribeRequest{
		MediaID:   mediaID,
		MediaType: mediaType,
		UserID:    userID,
	}

	if mediaType == "tv" && len(seasons) > 0 {
		payload.Seasons = seasons
	}

	log.Printf("[SubscribeChain] Creating request: mediaID=%d, mediaType=%s, userID=%d",
		mediaID, mediaType, userID)

	var result SubscribeResult
	err := s.postJellyseerrRequest(endpoint, payload, &result)
	if err != nil {
		log.Printf("[SubscribeChain] Request failed: %v", err)
		return nil, err
	}

	log.Printf("[SubscribeChain] Created subscription: ID=%d, MediaID=%d, Type=%s, UserID=%d",
		result.ID, result.MediaID, result.RequestType, userID)

	return &result, nil
}

// GetPendingRequests gets all pending requests
func (s *SubscribeChain) GetPendingRequests() ([]RequestInfo, error) {
	endpoint := "/api/v1/request?take=20&sort=added&filter=pending"

	var response struct {
		PageInfo struct {
			Page  int `json:"page"`
			Pages int `json:"pages"`
			Total int `json:"total"`
		} `json:"pageInfo"`
		Results []RequestInfo `json:"results"`
	}

	err := s.makeJellyseerrRequest(endpoint, &response)
	if err != nil {
		return nil, err
	}

	return response.Results, nil
}

// ApproveRequest approves a pending request
func (s *SubscribeChain) ApproveRequest(requestID int) error {
	endpoint := fmt.Sprintf("/api/v1/request/%d/approve", requestID)

	var result map[string]interface{}
	err := s.postJellyseerrRequest(endpoint, nil, &result)
	if err != nil {
		return fmt.Errorf("failed to approve request: %w", err)
	}

	log.Printf("[SubscribeChain] Approved request ID=%d", requestID)

	return nil
}

// DeclineRequest declines a pending request
func (s *SubscribeChain) DeclineRequest(requestID int) error {
	endpoint := fmt.Sprintf("/api/v1/request/%d/decline", requestID)

	var result map[string]interface{}
	err := s.postJellyseerrRequest(endpoint, nil, &result)
	if err != nil {
		return fmt.Errorf("failed to decline request: %w", err)
	}

	log.Printf("[SubscribeChain] Declined request ID=%d", requestID)

	return nil
}
