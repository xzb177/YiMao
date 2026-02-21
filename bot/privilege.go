// 管理员特权系统
// 核心理念：管理员不应该被配额限制，但需要有防滥用机制
//
// 创新点：
// 1. 基于信任的软限制 - 管理员有无限配额，但会记录使用情况
// 2. 智能异常检测 - 检测管理员账号是否被滥用（异常请求模式）
// 3. 递增式验证 - 可疑操作需要额外验证
// 4. 透明审计 - 所有管理员操作都有完整审计日志

package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PrivilegeLevel 用户权限级别
type PrivilegeLevel int

const (
	PrivilegeUser     PrivilegeLevel = iota // 普通用户
	PrivilegeTrusted                         // 受信任用户
	PrivilegeAdmin                           // 管理员
	PrivilegeSuperAdmin                      // 超级管理员
)

func (p PrivilegeLevel) String() string {
	switch p {
	case PrivilegeUser:
		return "用户"
	case PrivilegeTrusted:
		return "受信任用户"
	case PrivilegeAdmin:
		return "管理员"
	case PrivilegeSuperAdmin:
		return "超级管理员"
	default:
		return "未知"
	}
}

// AdminActivity 管理员活动记录
type AdminActivity struct {
	Timestamp   time.Time `json:"timestamp"`
	UserID      int64     `json:"user_id"`
	UserName    string    `json:"user_name"`
	Action      string    `json:"action"`       // "request", "approve", "decline", etc.
	MediaType   string    `json:"media_type"`   // "movie", "tv"
	MediaTitle  string    `json:"media_title"`
	MediaID     int       `json:"media_id"`
	RequestID   int       `json:"request_id,omitempty"`
	Success     bool      `json:"success"`
	IPAddress   string    `json:"ip_address,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	Source      string    `json:"source"`       // "telegram", "web", "api"
	BypassReason string   `json:"bypass_reason,omitempty"` // 为什么绕过限制
}

// AbuseAbnormality 异常检测指标
type AbuseAbnormality struct {
	RapidRequests      bool      `json:"rapid_requests"`       // 短时间内大量请求
	UnusualPattern     bool      `json:"unusual_pattern"`      // 异常时间模式（如凌晨3点大量请求）
	SameMediaRepeated  bool      `json:"same_media_repeated"`  // 重复请求相同内容
	HighFailureRate    bool      `json:"high_failure_rate"`    // 失败率异常
	FirstDetected      time.Time `json:"first_detected"`
	SuspicionScore     int       `json:"suspicion_score"`      // 0-100, 越高越可疑
}

// AdminProfile 管理员档案
type AdminProfile struct {
	UserID            int64                `json:"user_id"`
	UserName          string               `json:"user_name"`
	PrivilegeLevel    PrivilegeLevel       `json:"privilege_level"`
	TotalRequests     int                  `json:"total_requests"`
	MovieRequests     int                  `json:"movie_requests"`
	TVRequests        int                  `json:"tv_requests"`
	ApprovedCount     int                  `json:"approved_count"`
	DeclinedCount     int                  `json:"declined_count"`
	FirstActivity     time.Time            `json:"first_activity"`
	LastActivity      time.Time            `json:"last_activity"`
	RecentActivities  []AdminActivity      `json:"recent_activities"` // 最近100条活动
	Abnormality       *AbuseAbnormality    `json:"abnormality,omitempty"`
	WhitelistedUntil  *time.Time           `json:"whitelisted_until,omitempty"` // 白名单到期时间
	Metadata          map[string]string    `json:"metadata,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

// PrivilegeManager 特权管理器
type PrivilegeManager struct {
	profiles    map[int64]*AdminProfile
	activities  []AdminActivity // 全局活动日志
	mu          sync.RWMutex
	storagePath string
	isAdminFunc func(int64) bool
	maxRecentActivities int
}

// NewPrivilegeManager 创建特权管理器
func NewPrivilegeManager(storagePath string) *PrivilegeManager {
	pm := &PrivilegeManager{
		profiles:           make(map[int64]*AdminProfile),
		activities:         make([]AdminActivity, 0, 1000),
		storagePath:        storagePath,
		maxRecentActivities: 100,
	}
	pm.load()
	return pm
}

// SetIsAdminFunc 设置管理员检查函数
func (pm *PrivilegeManager) SetIsAdminFunc(fn func(int64) bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.isAdminFunc = fn
}

// GetPrivilegeLevel 获取用户权限级别
func (pm *PrivilegeManager) GetPrivilegeLevel(userID int64) PrivilegeLevel {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 检查是否是管理员
	if pm.isAdminFunc != nil && pm.isAdminFunc(userID) {
		return PrivilegeAdmin
	}

	// 检查档案
	if profile, exists := pm.profiles[userID]; exists {
		return profile.PrivilegeLevel
	}

	return PrivilegeUser
}

// CanBypassQuota 检查用户是否可以绕过配额限制
// 返回: (canBypass bool, reason string, activityRequired bool)
func (pm *PrivilegeManager) CanBypassQuota(userID int64, userName string, mediaType, mediaTitle string, mediaID int) (bool, string, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	privilege := PrivilegeUser
	if pm.isAdminFunc != nil && pm.isAdminFunc(userID) {
		privilege = PrivilegeAdmin
	}

	// 只有管理员可以绕过
	if privilege != PrivilegeAdmin {
		return false, "", false
	}

	// 获取或创建档案
	profile := pm.getOrCreateProfile(userID, userName, privilege)

	// 检查异常标记
	if profile.Abnormality != nil && profile.Abnormality.SuspicionScore > 70 {
		log.Printf("[Privilege] 用户 %d (%s) 可疑分数过高 (%d)，需要额外验证", userID, userName, profile.Abnormality.SuspicionScore)
		return false, fmt.Sprintf("⚠️ 账号活动异常，请稍后再试\n\n如需帮助，请联系超级管理员"), true
	}

	// 检查白名单
	if profile.WhitelistedUntil != nil && time.Now().Before(*profile.WhitelistedUntil) {
		return true, "白名单用户", false
	}

	// 记录活动
	pm.recordActivityLocked(profile, AdminActivity{
		Timestamp:  time.Now(),
		UserID:     userID,
		UserName:   userName,
		Action:     "bypass_quota",
		MediaType:  mediaType,
		MediaTitle: mediaTitle,
		MediaID:    mediaID,
		Success:    true,
		Source:     "telegram",
		BypassReason: "管理员特权",
	})

	// 更新统计
	profile.TotalRequests++
	if mediaType == "movie" {
		profile.MovieRequests++
	} else {
		profile.TVRequests++
	}
	profile.UpdatedAt = time.Now()

	pm.save()

	return true, "管理员特权", false
}

// HasUnlimitedQuota 检查用户是否有无限配额
func (pm *PrivilegeManager) HasUnlimitedQuota(userID int64) bool {
	privilege := pm.GetPrivilegeLevel(userID)
	return privilege == PrivilegeAdmin || privilege == PrivilegeSuperAdmin
}

// RecordRequest 记录请求活动
func (pm *PrivilegeManager) RecordRequest(userID int64, userName string, mediaType, mediaTitle string, mediaID int, success bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	privilege := PrivilegeUser
	if pm.isAdminFunc != nil && pm.isAdminFunc(userID) {
		privilege = PrivilegeAdmin
	}

	profile := pm.getOrCreateProfile(userID, userName, privilege)

	activity := AdminActivity{
		Timestamp:  time.Now(),
		UserID:     userID,
		UserName:   userName,
		Action:     "request",
		MediaType:  mediaType,
		MediaTitle: mediaTitle,
		MediaID:    mediaID,
		Success:    success,
		Source:     "telegram",
	}

	pm.recordActivityLocked(profile, activity)

	if success {
		profile.TotalRequests++
		if mediaType == "movie" {
			profile.MovieRequests++
		} else {
			profile.TVRequests++
		}
	}
	profile.UpdatedAt = time.Now()

	// 异步检查异常
	go pm.checkAbnormalities(userID)
}

// RecordAdminAction 记录管理员操作（批准/拒绝）
func (pm *PrivilegeManager) RecordAdminAction(userID int64, userName string, action string, requestID int, mediaTitle string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	privilege := PrivilegeUser
	if pm.isAdminFunc != nil && pm.isAdminFunc(userID) {
		privilege = PrivilegeAdmin
	}

	profile := pm.getOrCreateProfile(userID, userName, privilege)

	pm.recordActivityLocked(profile, AdminActivity{
		Timestamp:  time.Now(),
		UserID:     userID,
		UserName:   userName,
		Action:     action,
		MediaTitle: mediaTitle,
		RequestID:  requestID,
		Success:    true,
		Source:     "telegram",
	})

	if action == "approve" {
		profile.ApprovedCount++
	} else if action == "decline" {
		profile.DeclinedCount++
	}
	profile.UpdatedAt = time.Now()
}

// getOrCreateProfile 获取或创建用户档案
func (pm *PrivilegeManager) getOrCreateProfile(userID int64, userName string, privilege PrivilegeLevel) *AdminProfile {
	profile, exists := pm.profiles[userID]
	if !exists {
		profile = &AdminProfile{
			UserID:           userID,
			UserName:         userName,
			PrivilegeLevel:   privilege,
			FirstActivity:    time.Now(),
			LastActivity:     time.Now(),
			RecentActivities: make([]AdminActivity, 0, pm.maxRecentActivities),
			Metadata:         make(map[string]string),
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		pm.profiles[userID] = profile
		log.Printf("[Privilege] 创建新档案: userID=%d, userName=%s, privilege=%s", userID, userName, privilege)
	}

	// 更新用户名（可能变化）
	if userName != "" && profile.UserName != userName {
		profile.UserName = userName
	}

	// 更新权限级别（可能升级）
	if privilege > profile.PrivilegeLevel {
		profile.PrivilegeLevel = privilege
		log.Printf("[Privilege] 用户 %d 权限升级: %s -> %s", userID, profile.PrivilegeLevel, privilege)
	}

	profile.LastActivity = time.Now()
	return profile
}

// recordActivityLocked 记录活动（已加锁）
func (pm *PrivilegeManager) recordActivityLocked(profile *AdminProfile, activity AdminActivity) {
	// 添加到用户档案
	if len(profile.RecentActivities) >= pm.maxRecentActivities {
		profile.RecentActivities = profile.RecentActivities[1:]
	}
	profile.RecentActivities = append(profile.RecentActivities, activity)

	// 添加到全局日志
	if len(pm.activities) >= 1000 {
		pm.activities = pm.activities[100:]
	}
	pm.activities = append(pm.activities, activity)
}

// checkAbnormalities 检查异常模式
func (pm *PrivilegeManager) checkAbnormalities(userID int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	profile, exists := pm.profiles[userID]
	if !exists {
		return
	}

	// 初始化异常标记
	if profile.Abnormality == nil {
		profile.Abnormality = &AbuseAbnormality{
			SuspicionScore: 0,
		}
	}

	ab := profile.Abnormality
	score := 0

	// 检查1: 短时间内大量请求（最近5分钟超过10个）
	recentCount := 0
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	for _, act := range profile.RecentActivities {
		if act.Timestamp.After(fiveMinutesAgo) && act.Action == "request" {
			recentCount++
		}
	}
	if recentCount > 10 {
		ab.RapidRequests = true
		score += 30
		if ab.FirstDetected.IsZero() {
			ab.FirstDetected = time.Now()
		}
		log.Printf("[Privilege] 检测到快速请求: userID=%d, count=%d", userID, recentCount)
	} else {
		ab.RapidRequests = false
	}

	// 检查2: 异常时间模式（凌晨2-6点大量请求）
	earlyMorningCount := 0
	for _, act := range profile.RecentActivities {
		hour := act.Timestamp.Hour()
		if hour >= 2 && hour <= 6 && act.Action == "request" {
			earlyMorningCount++
		}
	}
	if earlyMorningCount > 5 {
		ab.UnusualPattern = true
		score += 20
		log.Printf("[Privilege] 检测到异常时间模式: userID=%d, earlyMorning=%d", userID, earlyMorningCount)
	} else {
		ab.UnusualPattern = false
	}

	// 检查3: 重复请求相同内容
	mediaCounts := make(map[string]int)
	for _, act := range profile.RecentActivities {
		if act.Action == "request" {
			key := fmt.Sprintf("%s:%d", act.MediaType, act.MediaID)
			mediaCounts[key]++
		}
	}
	for _, count := range mediaCounts {
		if count > 3 {
			ab.SameMediaRepeated = true
			score += 25
			log.Printf("[Privilege] 检测到重复请求: userID=%d", userID)
			break
		}
	}

	// 检查4: 高失败率
	failedCount := 0
	totalCount := 0
	for _, act := range profile.RecentActivities {
		if act.Action == "request" {
			totalCount++
			if !act.Success {
				failedCount++
			}
		}
	}
	if totalCount > 10 && float64(failedCount)/float64(totalCount) > 0.5 {
		ab.HighFailureRate = true
		score += 25
		log.Printf("[Privilege] 检测到高失败率: userID=%d, rate=%.2f", userID, float64(failedCount)/float64(totalCount))
	} else {
		ab.HighFailureRate = false
	}

	// 更新可疑分数
	ab.SuspicionScore = score

	// 如果没有异常，重置标记
	if score == 0 {
		profile.Abnormality = nil
	}

	pm.save()
}

// GetProfile 获取用户档案
func (pm *PrivilegeManager) GetProfile(userID int64) *AdminProfile {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.profiles[userID]
}

// GetStats 获取统计信息
func (pm *PrivilegeManager) GetStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	totalRequests := 0
	adminRequests := 0
	for _, p := range pm.profiles {
		totalRequests += p.TotalRequests
		if p.PrivilegeLevel >= PrivilegeAdmin {
			adminRequests += p.TotalRequests
		}
	}

	return map[string]interface{}{
		"total_profiles":      len(pm.profiles),
		"total_activities":    len(pm.activities),
		"total_requests":      totalRequests,
		"admin_requests":      adminRequests,
		"suspicious_profiles": pm.countSuspiciousProfiles(),
	}
}

// countSuspiciousProfiles 统计可疑档案数量
func (pm *PrivilegeManager) countSuspiciousProfiles() int {
	count := 0
	for _, p := range pm.profiles {
		if p.Abnormality != nil && p.Abnormality.SuspicionScore > 50 {
			count++
		}
	}
	return count
}

// SetWhitelist 设置白名单
func (pm *PrivilegeManager) SetWhitelist(userID int64, duration time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if profile, exists := pm.profiles[userID]; exists {
		until := time.Now().Add(duration)
		profile.WhitelistedUntil = &until
		profile.UpdatedAt = time.Now()
		pm.save()
		log.Printf("[Privilege] 用户 %d 已添加到白名单，到期时间: %s", userID, until)
	}
}

// ClearSuspicion 清除可疑标记（需要超级管理员权限）
func (pm *PrivilegeManager) ClearSuspicion(userID int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if profile, exists := pm.profiles[userID]; exists {
		profile.Abnormality = nil
		profile.UpdatedAt = time.Now()
		pm.save()
		log.Printf("[Privilege] 已清除用户 %d 的可疑标记", userID)
	}
}

// load 加载数据
func (pm *PrivilegeManager) load() {
	if pm.storagePath == "" {
		return
	}

	// 确保目录存在
	dir := filepath.Dir(pm.storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[Privilege] 创建目录失败: %v", err)
		return
	}

	// 加载档案
	profilesPath := filepath.Join(dir, "admin_profiles.json")
	data, err := os.ReadFile(profilesPath)
	if err != nil {
		log.Printf("[Privilege] 档案文件不存在，将创建新文件")
		return
	}

	var profiles map[int64]*AdminProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		log.Printf("[Privilege] 加载档案失败: %v", err)
		return
	}

	pm.profiles = profiles
	log.Printf("[Privilege] 已加载 %d 个用户档案", len(profiles))
}

// save 保存数据
func (pm *PrivilegeManager) save() {
	if pm.storagePath == "" {
		return
	}

	dir := filepath.Dir(pm.storagePath)
	profilesPath := filepath.Join(dir, "admin_profiles.json")

	data, err := json.MarshalIndent(pm.profiles, "", "  ")
	if err != nil {
		log.Printf("[Privilege] 序列化档案失败: %v", err)
		return
	}

	if err := os.WriteFile(profilesPath, data, 0644); err != nil {
		log.Printf("[Privilege] 保存档案失败: %v", err)
	}
}
