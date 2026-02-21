// RequestApprovalToken - 版本化请求批准令牌系统
// 核心思想：使用 MVCC 风格的版本号实现幂等性
// 问题: 管理员点击"批准"按钮时收到"请求已过期"错误
// 解决: 使用持久化令牌 + CAS 机制 + 状态同步

package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// RequestState 表示请求的当前状态
type RequestState int

const (
	StateUnknown RequestState = iota
	StatePending // 待处理
	StateApproved // 已批准
	StateDeclined // 已拒绝
	StateAvailable // 已可用
)

func (s RequestState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateApproved:
		return "approved"
	case StateDeclined:
		return "declined"
	case StateAvailable:
		return "available"
	default:
		return "unknown"
	}
}

// ApprovalToken 批准令牌结构
type ApprovalToken struct {
	TokenID        string            `json:"token_id"`         // 唯一令牌ID
	RequestID      int               `json:"request_id"`       // Jellyseerr请求ID
	Version        int64             `json:"version"`          // 版本号（纳秒时间戳）
	State          RequestState      `json:"state"`            // 当前状态
	CreatedAt      time.Time         `json:"created_at"`
	ExpiresAt      time.Time         `json:"expires_at"`
	MediaTitle     string            `json:"media_title"`
	MediaType      string            `json:"media_type"`
	Username       string            `json:"username"`
	NotifiedAdmins map[string]bool   `json:"notified_admins"` // 已通知的管理员
}

// ApprovalResult 批准操作结果
type ApprovalResult struct {
	Success       bool         // 操作是否成功
	Message       string       // 返回给用户的消息
	State         RequestState // 操作后的状态
	WasFirst      bool         // 是否是第一个成功操作的操作者
	PreviousState RequestState // 操作前的状态
}

// TokenManager 令牌管理器
type TokenManager struct {
	tokens       map[string]*ApprovalToken // tokenID -> token
	requestIndex map[int]string            // requestID -> tokenID (反向索引)
	mutex        sync.RWMutex
	storagePath  string
}

// NewTokenManager 创建新的令牌管理器
func NewTokenManager(storagePath string) *TokenManager {
	tm := &TokenManager{
		tokens:       make(map[string]*ApprovalToken),
		requestIndex: make(map[int]string),
		storagePath:  storagePath,
	}
	tm.loadPersistedTokens()
	return tm
}

// GenerateToken 为请求生成新的批准令牌
func (tm *TokenManager) GenerateToken(requestID int, mediaTitle, mediaType, username string, ttl time.Duration) (*ApprovalToken, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// 检查是否已存在有效令牌
	if existingTokenID, exists := tm.requestIndex[requestID]; exists {
		if token, ok := tm.tokens[existingTokenID]; ok && time.Now().Before(token.ExpiresAt) {
			log.Printf("[TokenManager] 复用现有令牌 %s for request %d", existingTokenID, requestID)
			return token, nil
		}
		// 清理过期令牌
		delete(tm.tokens, existingTokenID)
	}

	// 生成新令牌
	tokenID := tm.generateTokenID(requestID)
	now := time.Now()

	token := &ApprovalToken{
		TokenID:        tokenID,
		RequestID:      requestID,
		Version:        now.UnixNano(), // 使用纳秒时间戳作为版本号
		State:          StatePending,
		CreatedAt:      now,
		ExpiresAt:      now.Add(ttl),
		MediaTitle:     mediaTitle,
		MediaType:      mediaType,
		Username:       username,
		NotifiedAdmins: make(map[string]bool),
	}

	tm.tokens[tokenID] = token
	tm.requestIndex[requestID] = tokenID
	tm.saveTokens()

	log.Printf("[TokenManager] 生成令牌 %s for request %d (version: %d)", tokenID, requestID, token.Version)
	return token, nil
}

// generateTokenID 生成唯一的令牌ID
// 格式: "r" + base36(requestID + timestamp的低16位 + 随机数)
// 保证在64字节以内（Telegram callback_data 限制）
func (tm *TokenManager) generateTokenID(requestID int) string {
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 3)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to time-based randomness
		randomBytes = []byte{byte(timestamp >> 8), byte(timestamp), byte(timestamp >> 16)}
	}

	// 使用 base36 编码更紧凑
	// 格式: requestID (最多7位) + 时间戳低16位 (4位) + 随机数 (3位)
	// 例如: r12345_abc_1x2y3z
	chars := "0123456789abcdefghijklmnopqrstuvwxyz"
	encodeBase36 := func(n int64) string {
		if n == 0 {
			return "0"
		}
		s := ""
		for n > 0 {
			s = string(chars[n%36]) + s
			n /= 36
		}
		return s
	}

	// 组合: 时间戳低20位 + requestID + 随机数
	timePart := encodeBase36(timestamp & 0xFFFFF)
	idPart := encodeBase36(int64(requestID))

	// 随机部分转hex
	randomPart := fmt.Sprintf("%02x%02x%02x", randomBytes[0], randomBytes[1], randomBytes[2])

	tokenID := "r" + timePart + "_" + idPart + "_" + randomPart

	// 确保不超过40字节
	if len(tokenID) > 40 {
		tokenID = tokenID[:40]
	}

	return tokenID
}

// GetToken 获取令牌
func (tm *TokenManager) GetToken(tokenID string) *ApprovalToken {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	return tm.tokens[tokenID]
}

// GetTokenByRequestID 通过请求ID获取令牌
func (tm *TokenManager) GetTokenByRequestID(requestID int) *ApprovalToken {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	if tokenID, exists := tm.requestIndex[requestID]; exists {
		return tm.tokens[tokenID]
	}
	return nil
}

// ValidateToken 验证令牌并检查实际状态
// 返回: (valid, token, error)
func (tm *TokenManager) ValidateToken(tokenID string) (bool, *ApprovalToken, string) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	// 检查令牌格式 - 新格式以 "r" 开头
	if !strings.HasPrefix(tokenID, "r") {
		return false, nil, "无效的令牌格式"
	}

	token, exists := tm.tokens[tokenID]
	if !exists {
		// 尝试从存储加载（可能服务刚重启）
		tm.mutex.RUnlock()
		tm.mutex.Lock()
		tm.loadPersistedTokens()
		tm.mutex.Unlock()
		tm.mutex.RLock()

		token, exists = tm.tokens[tokenID]
		if !exists {
			return false, nil, "令牌不存在或已过期"
		}
	}

	// 检查过期
	if time.Now().After(token.ExpiresAt) {
		return false, token, "令牌已过期"
	}

	return true, token, ""
}

// ApproveRequest 使用CAS机制批准请求
func (tm *TokenManager) ApproveRequest(tokenID string, expectedVersion int64) *ApprovalResult {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	token, exists := tm.tokens[tokenID]
	if !exists {
		return &ApprovalResult{
			Success: false,
			Message: "令牌不存在",
		}
	}

	previousState := token.State

	// 幂等性检查：如果已经批准，返回成功但不重复执行
	if token.State == StateApproved || token.State == StateAvailable {
		return &ApprovalResult{
			Success:       true,
			WasFirst:      false,
			State:         token.State,
			PreviousState: previousState,
			Message:       fmt.Sprintf("请求已被其他管理员批准\n\n媒体: %s", token.MediaTitle),
		}
	}

	if token.State == StateDeclined {
		return &ApprovalResult{
			Success: false,
			State:   token.State,
			Message: fmt.Sprintf("请求已被拒绝\n\n媒体: %s", token.MediaTitle),
		}
	}

	// CAS检查：版本号必须匹配
	if expectedVersion > 0 && token.Version != expectedVersion {
		return &ApprovalResult{
			Success: false,
			State:   token.State,
			Message: "版本号不匹配 - 请求状态可能已被修改",
		}
	}

	// 调用Jellyseerr API批准请求
	if jellyseerrClient == nil {
		return &ApprovalResult{
			Success: false,
			State:   StatePending,
			Message: "Jellyseerr API 未配置",
		}
	}

	if err := jellyseerrClient.ApproveRequest(token.RequestID); err != nil {
		return &ApprovalResult{
			Success: false,
			State:   StatePending,
			Message: fmt.Sprintf("批准失败: %v", err),
		}
	}

	// 更新令牌状态（原子操作）
	token.State = StateApproved
	token.Version = time.Now().UnixNano()
	tm.saveTokens()

	log.Printf("[TokenManager] 请求 %d 已批准 (version: %d -> %d)", token.RequestID, expectedVersion, token.Version)

	return &ApprovalResult{
		Success:       true,
		WasFirst:      true,
		State:         StateApproved,
		PreviousState: previousState,
		Message:       fmt.Sprintf("已批准: %s\n\nJellyseerr 将自动下载此内容", token.MediaTitle),
	}
}

// DeclineRequest 使用CAS机制拒绝请求
func (tm *TokenManager) DeclineRequest(tokenID string, expectedVersion int64) *ApprovalResult {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	token, exists := tm.tokens[tokenID]
	if !exists {
		return &ApprovalResult{
			Success: false,
			Message: "令牌不存在",
		}
	}

	previousState := token.State

	// 幂等性检查
	if token.State == StateDeclined {
		return &ApprovalResult{
			Success:       true,
			WasFirst:      false,
			State:         StateDeclined,
			PreviousState: previousState,
			Message:       fmt.Sprintf("请求已被其他管理员拒绝\n\n媒体: %s", token.MediaTitle),
		}
	}

	if token.State == StateApproved || token.State == StateAvailable {
		return &ApprovalResult{
			Success: false,
			State:   token.State,
			Message: fmt.Sprintf("无法拒绝 - 请求已被批准\n\n媒体: %s", token.MediaTitle),
		}
	}

	// CAS检查
	if expectedVersion > 0 && token.Version != expectedVersion {
		return &ApprovalResult{
			Success: false,
			State:   token.State,
			Message: "版本号不匹配 - 请求状态可能已被修改",
		}
	}

	// 调用Jellyseerr API拒绝请求
	if jellyseerrClient == nil {
		return &ApprovalResult{
			Success: false,
			State:   StatePending,
			Message: "Jellyseerr API 未配置",
		}
	}

	if err := jellyseerrClient.DeclineRequest(token.RequestID); err != nil {
		return &ApprovalResult{
			Success: false,
			State:   StatePending,
			Message: fmt.Sprintf("拒绝失败: %v", err),
		}
	}

	// 更新令牌状态
	token.State = StateDeclined
	token.Version = time.Now().UnixNano()
	tm.saveTokens()

	log.Printf("[TokenManager] 请求 %d 已拒绝 (version: %d -> %d)", token.RequestID, expectedVersion, token.Version)

	return &ApprovalResult{
		Success:       true,
		WasFirst:      true,
		State:         StateDeclined,
		PreviousState: previousState,
		Message:       fmt.Sprintf("已拒绝: %s", token.MediaTitle),
	}
}

// SyncRequestState 同步请求的实际状态（从Jellyseerr）
func (tm *TokenManager) SyncRequestState(requestID int) RequestState {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	tokenID, exists := tm.requestIndex[requestID]
	if !exists {
		return StateUnknown
	}

	token, ok := tm.tokens[tokenID]
	if !ok {
		return StateUnknown
	}

	// 从Jellyseerr获取实际状态
	if jellyseerrClient == nil {
		return token.State
	}

	request, err := jellyseerrClient.GetRequest(requestID)
	if err != nil {
		log.Printf("[TokenManager] 同步状态失败: %v", err)
		return token.State
	}

	actualState := determineStateFromJellyseerr(request)
	if actualState != token.State {
		token.State = actualState
		token.Version = time.Now().UnixNano()
		tm.saveTokens()
		log.Printf("[TokenManager] 更新令牌 %s 状态: %s -> %s", tokenID, token.State, actualState)
	}

	return actualState
}

// determineStateFromJellyseerr 从Jellyseerr响应确定状态
func determineStateFromJellyseerr(request *JellyseerrRequest) RequestState {
	if request == nil || request.Status == nil {
		return StateUnknown
	}

	switch v := request.Status.(type) {
	case string:
		switch v {
		case "pending":
			return StatePending
		case "approved":
			return StateApproved
		case "available":
			return StateAvailable
		case "declined":
			return StateDeclined
		}
	case float64:
		switch int(v) {
		case 1:
			return StatePending
		case 2:
			return StateApproved
		case 3:
			return StateAvailable
		case 4:
			return StateDeclined
		}
	}

	return StateUnknown
}

// loadPersistedTokens 从文件加载令牌
func (tm *TokenManager) loadPersistedTokens() {
	data, err := os.ReadFile(tm.storagePath)
	if err != nil {
		// 文件不存在是正常情况，首次运行时
		return
	}

	var tokens map[string]*ApprovalToken
	if err := json.Unmarshal(data, &tokens); err != nil {
		log.Printf("[TokenManager] 加载令牌失败: %v", err)
		return
	}

	// 清理过期令牌
	now := time.Now()
	for tokenID, token := range tokens {
		if now.Before(token.ExpiresAt) {
			tm.tokens[tokenID] = token
			tm.requestIndex[token.RequestID] = tokenID
		}
	}

	log.Printf("[TokenManager] 加载了 %d 个有效令牌", len(tm.tokens))
}

// saveTokens 保存令牌到文件
func (tm *TokenManager) saveTokens() {
	data, err := json.MarshalIndent(tm.tokens, "", "  ")
	if err != nil {
		log.Printf("[TokenManager] 序列化令牌失败: %v", err)
		return
	}

	if err := os.WriteFile(tm.storagePath, data, 0644); err != nil {
		log.Printf("[TokenManager] 保存令牌失败: %v", err)
	}
}

// CleanupExpiredTokens 清理过期令牌
func (tm *TokenManager) CleanupExpiredTokens() int {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	now := time.Now()
	expired := 0

	for tokenID, token := range tm.tokens {
		if now.After(token.ExpiresAt) {
			delete(tm.tokens, tokenID)
			delete(tm.requestIndex, token.RequestID)
			expired++
		}
	}

	if expired > 0 {
		tm.saveTokens()
		log.Printf("[TokenManager] 清理了 %d 个过期令牌", expired)
	}

	return expired
}
