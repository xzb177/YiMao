package services

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	entries  map[string]map[int64]bool
	metadata map[string]CarpoolMetadata
	mu       sync.RWMutex
}

// NewCarpoolService 创建拼车服务并从磁盘加载已有数据。
func NewCarpoolService(dataDir string) *CarpoolService {
	s := &CarpoolService{
		dataFile: fmt.Sprintf("%s/carpool.json", dataDir),
		entries:  make(map[string]map[int64]bool),
		metadata: make(map[string]CarpoolMetadata),
	}
	s.load()
	return s
}

// carpoolFileData 是落盘格式：key -> userID 列表
type carpoolFileData struct {
	Entries  map[string][]int64         `json:"entries"`
	Metadata map[string]CarpoolMetadata `json:"metadata,omitempty"`
}

type CarpoolMetadata struct {
	Title   string    `json:"title,omitempty"`
	Year    string    `json:"year,omitempty"`
	Poster  string    `json:"poster,omitempty"`
	AddedAt time.Time `json:"added_at,omitempty"`
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
	for key, meta := range fileData.Metadata {
		s.metadata[key] = meta
	}
	logger.Info("[Carpool] 已加载 %d 条拼车记录", len(s.entries))
}

// saveLocked 写盘（调用方必须持有锁）。复制出落盘结构后调用原子写。
func (s *CarpoolService) saveLocked() error {
	fileData := carpoolFileData{Entries: make(map[string][]int64, len(s.entries)), Metadata: make(map[string]CarpoolMetadata, len(s.metadata))}
	for key, set := range s.entries {
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		fileData.Entries[key] = ids
	}
	for key, meta := range s.metadata {
		fileData.Metadata[key] = meta
	}

	data, err := json.MarshalIndent(fileData, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化拼车数据: %w", err)
	}

	if err := atomicWriteFile(s.dataFile, data, 0644); err != nil {
		return fmt.Errorf("保存拼车数据: %w", err)
	}
	return nil
}

// Add 把一个用户加入某部片的拼车列表（去重），返回当前想看的总人数。
func (s *CarpoolService) Add(tmdbID int, mediaType string, userID int64) int {
	count, err := s.AddChecked(tmdbID, mediaType, userID)
	if err != nil {
		logger.Info("[Carpool] 加入后持久化失败: %v", err)
	}
	return count
}

func (s *CarpoolService) AddChecked(tmdbID int, mediaType string, userID int64) (int, error) {
	return s.AddWithMetadataChecked(tmdbID, mediaType, userID, CarpoolMetadata{})
}

func (s *CarpoolService) AddWithMetadataChecked(tmdbID int, mediaType string, userID int64, meta CarpoolMetadata) (int, error) {
	key := carpoolKey(tmdbID, mediaType)

	s.mu.Lock()
	defer s.mu.Unlock()

	set, ok := s.entries[key]
	if !ok {
		set = make(map[int64]bool)
		s.entries[key] = set
	}
	previous := set[userID]
	previousMeta, hadMeta := s.metadata[key]
	set[userID] = true
	if meta.Title != "" || meta.Year != "" || meta.Poster != "" || !meta.AddedAt.IsZero() {
		s.metadata[key] = meta
	}
	count := len(set)
	if err := s.saveLocked(); err != nil {
		if previous {
			set[userID] = true
		} else {
			delete(set, userID)
			if len(set) == 0 {
				delete(s.entries, key)
			}
		}
		if hadMeta {
			s.metadata[key] = previousMeta
		} else {
			delete(s.metadata, key)
		}
		return count, err
	}
	return count, nil
}

type CarpoolItem struct {
	TMDBID  int       `json:"tmdb_id"`
	Type    string    `json:"type"`
	Title   string    `json:"title,omitempty"`
	Year    string    `json:"year,omitempty"`
	Poster  string    `json:"poster_path,omitempty"`
	AddedAt time.Time `json:"added_at,omitempty"`
}

// Remove 精确移除某个用户对某部电影或剧集的想看记录。
func (s *CarpoolService) Remove(tmdbID int, mediaType string, userID int64) bool {
	removed, err := s.RemoveChecked(tmdbID, mediaType, userID)
	if err != nil {
		logger.Info("[Carpool] 移除后持久化失败: %v", err)
	}
	return removed
}

func (s *CarpoolService) RemoveChecked(tmdbID int, mediaType string, userID int64) (bool, error) {
	key := carpoolKey(tmdbID, mediaType)
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.entries[key]
	if !set[userID] {
		return false, nil
	}
	delete(set, userID)
	previousMeta, hadMeta := s.metadata[key]
	if len(set) == 0 {
		delete(s.entries, key)
		delete(s.metadata, key)
	}
	if err := s.saveLocked(); err != nil {
		set = s.entries[key]
		if set == nil {
			set = make(map[int64]bool)
			s.entries[key] = set
		}
		set[userID] = true
		if hadMeta {
			s.metadata[key] = previousMeta
		}
		return false, err
	}
	return true, nil
}

// Contains 报告用户是否已将该作品加入想看。
func (s *CarpoolService) Contains(tmdbID int, mediaType string, userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[carpoolKey(tmdbID, mediaType)][userID]
}

// ListForUser 返回当前用户的作品级想看列表。Carpool 的既有存储不含季信息，
// 因此这里不会把剧集记录伪装成季级提醒。
func (s *CarpoolService) ListForUser(userID int64) []CarpoolItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []CarpoolItem{}
	for key, users := range s.entries {
		if !users[userID] {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		id, err := strconv.Atoi(parts[1])
		if err == nil && id > 0 {
			meta := s.metadata[key]
			items = append(items, CarpoolItem{TMDBID: id, Type: parts[0], Title: meta.Title, Year: meta.Year, Poster: meta.Poster, AddedAt: meta.AddedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].AddedAt.Equal(items[j].AddedAt) {
			return items[i].AddedAt.After(items[j].AddedAt)
		}
		if items[i].Type == items[j].Type {
			return items[i].TMDBID < items[j].TMDBID
		}
		return items[i].Type < items[j].Type
	})
	return items
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
	meta, hadMeta := s.metadata[key]
	delete(s.metadata, key)
	if err := s.saveLocked(); err != nil {
		s.entries[key] = set
		if hadMeta {
			s.metadata[key] = meta
		}
		logger.Info("[Carpool] 清空后持久化失败，已回滚: %v", err)
		return nil
	}
	return ids
}
