package services

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/xzb177/yimao/pkg/logger"
)

// 本文件实现「拼车 +1」功能的持久化存储（Batch A #3）。
//
// 设计参考 quota.go 的 JSON 持久化 + RWMutex + atomicWriteFile 模式：
//   - key:   tmdbID + mediaType 组合（carpoolKey），用于区分电影/剧集同 ID 的情况
//   - value: telegram userID 列表（去重）
//   - 所有读写加锁，保证并发安全。
//   - 写盘统一使用 services 包内已有的 atomicWriteFile 工具函数（原子写）。

// carpoolKey 生成存储 key：mediaType:tmdbID
func carpoolKey(tmdbID int, mediaType string) string {
	if mediaType == "" {
		mediaType = "movie"
	}
	return fmt.Sprintf("%s:%d", mediaType, tmdbID)
}

// CarpoolService 管理「拼车」列表（谁也想看某部片）。
type CarpoolService struct {
	dataFile string
	// key -> set of telegram userIDs（用 map 去重）
	entries map[string]map[int64]bool
	mu      sync.RWMutex
}

// NewCarpoolService 创建拼车服务并从磁盘加载已有数据。
func NewCarpoolService(dataDir string) *CarpoolService {
	s := &CarpoolService{
		dataFile: fmt.Sprintf("%s/carpool.json", dataDir),
		entries:  make(map[string]map[int64]bool),
	}
	s.load()
	return s
}

// carpoolFileData 是落盘格式：key -> userID 列表
type carpoolFileData struct {
	Entries map[string][]int64 `json:"entries"`
}

// load 从文件加载（仅在构造时调用，内部加锁）。
func (s *CarpoolService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.dataFile)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Info("[Carpool] 读取拼车数据失败: %v", err)
		}
		return
	}

	var fileData carpoolFileData
	if err := json.Unmarshal(data, &fileData); err != nil {
		logger.Info("[Carpool] 解析拼车数据失败: %v", err)
		return
	}

	for key, ids := range fileData.Entries {
		set := make(map[int64]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		s.entries[key] = set
	}
	logger.Info("[Carpool] 已加载 %d 条拼车记录", len(s.entries))
}

// saveLocked 写盘（调用方必须持有锁）。复制出落盘结构后调用原子写。
func (s *CarpoolService) saveLocked() {
	fileData := carpoolFileData{Entries: make(map[string][]int64, len(s.entries))}
	for key, set := range s.entries {
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		fileData.Entries[key] = ids
	}

	data, err := json.MarshalIndent(fileData, "", "  ")
	if err != nil {
		logger.Info("[Carpool] 序列化拼车数据失败: %v", err)
		return
	}

	if err := atomicWriteFile(s.dataFile, data, 0644); err != nil {
		logger.Info("[Carpool] 保存拼车数据失败: %v", err)
	}
}

// Add 把一个用户加入某部片的拼车列表（去重），返回当前想看的总人数。
func (s *CarpoolService) Add(tmdbID int, mediaType string, userID int64) int {
	key := carpoolKey(tmdbID, mediaType)

	s.mu.Lock()
	defer s.mu.Unlock()

	set, ok := s.entries[key]
	if !ok {
		set = make(map[int64]bool)
		s.entries[key] = set
	}
	set[userID] = true

	s.saveLocked()
	return len(set)
}

// Get 返回某部片当前的拼车用户列表（拷贝，调用方可安全使用）。
func (s *CarpoolService) Get(tmdbID int, mediaType string) []int64 {
	key := carpoolKey(tmdbID, mediaType)

	s.mu.RLock()
	defer s.mu.RUnlock()

	set, ok := s.entries[key]
	if !ok || len(set) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}

// GetAndClear 返回拼车用户列表并清空该片的记录（入库通知 @ 完之后调用）。
func (s *CarpoolService) GetAndClear(tmdbID int, mediaType string) []int64 {
	key := carpoolKey(tmdbID, mediaType)

	s.mu.Lock()
	defer s.mu.Unlock()

	set, ok := s.entries[key]
	if !ok || len(set) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}

	delete(s.entries, key)
	s.saveLocked()
	return ids
}
