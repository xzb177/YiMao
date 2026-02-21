package chain

import (
	"fmt"
	"log"
	"strings"
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
// Returns the request ID and any error, or (0, nil) if successful but ID unavailable
// 🛡️ 创新防御方法：智能错误处理与友好提示
func (s *SubscribeChain) SubscribeWithUser(mediaID int, mediaType string, seasons []int, userID int) (*SubscribeResult, error) {
	log.Printf("[SubscribeChain] SubscribeWithUser called: mediaID=%d, mediaType=%s, userID=%d",
		mediaID, mediaType, userID)

	endpoint := "/request"

	payload := SubscribeRequest{
		MediaID:   mediaID,
		MediaType: mediaType,
		UserID:    userID,
	}

	if mediaType == "tv" && len(seasons) > 0 {
		payload.Seasons = seasons
	}

	log.Printf("[SubscribeChain] Creating request: mediaID=%d, mediaType=%s, userID=%d, seasons=%v",
		mediaID, mediaType, userID, seasons)

	// First, try to post and get the result
	var result SubscribeResult
	err := s.postJellyseerrRequest(endpoint, payload, &result)
	if err != nil {
		log.Printf("[SubscribeChain] Request failed: %v", err)

		// 🛡️ 创新防御：解析错误并提供友好提示
		errStr := err.Error()

		// 检查 "No seasons available" 错误
		if strings.Contains(errStr, "No seasons") {
			return nil, fmt.Errorf("📺 该剧集暂无可用的季\n\n💡 可能原因：\n• 剧集信息尚未完全同步\n• 该剧集已全部下架\n• 需要先在 Jellyfin 中添加")
		}

		// 检查 500 "filter" 错误 (Jellyseerr bug)
		if strings.Contains(errStr, "500") && strings.Contains(errStr, "filter") {
			return nil, fmt.Errorf("📺 该剧集暂无可用的季\n\n💡 这是 Jellyseerr 的已知问题，建议：\n• 先在 Jellyfin 中确认该剧集可用\n• 联系管理员手动添加")
		}

		// 检查 "Media does not exist" 错误
		if strings.Contains(errStr, "Media does not exist") {
			return nil, fmt.Errorf("🎬 该媒体在 Jellyseerr 中不存在\n\n💡 建议：\n• 检查媒体 ID 是否正确\n• 先在 Jellyseerr 中搜索该媒体")
		}

		return nil, err
	}

	// Log success with all details
	log.Printf("[SubscribeChain] Created subscription: ID=%d, MediaID=%d, Type=%s, UserID=%d",
		result.ID, result.MediaID, result.RequestType, userID)

	// Validate that we got a valid request ID
	if result.ID == 0 {
		log.Printf("[SubscribeChain] Warning: Request created but got ID=0 from Jellyseerr API")
	}

	return &result, nil
}

// GetPendingRequests gets all pending requests
func (s *SubscribeChain) GetPendingRequests() ([]RequestInfo, error) {
	endpoint := "/request?take=20&sort=added&filter=pending"

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
	endpoint := fmt.Sprintf("/request/%d/approve", requestID)

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
	endpoint := fmt.Sprintf("/request/%d/decline", requestID)

	var result map[string]interface{}
	err := s.postJellyseerrRequest(endpoint, nil, &result)
	if err != nil {
		return fmt.Errorf("failed to decline request: %w", err)
	}

	log.Printf("[SubscribeChain] Declined request ID=%d", requestID)

	return nil
}
