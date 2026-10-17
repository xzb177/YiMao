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
//   - key:   tmdbID + mediaType + season 组合（carpoolKey），电影只按 mediaType:tmdbID，
//     剧集按 tv:tmdbID:s<season>，season=0 表示「任意季」
//   - value: telegram userID 列表（去重）
//   - 所有读写加锁，保证并发安全。
//   - 写盘统一使用 services 包内已有的 atomicWriteFile 工具函数（原子写）。
//
// 历史数据兼容：旧格式 key（tv:<tmdbID> 无季后缀）在 load() 时统一改写为
// tv:<tmdbID>:s0（任意季），不丢数据、不报错。

// normalizeCarpoolSeason 归一化季号：电影不分季（恒为 0），负数按 0 处理。
func normalizeCarpoolSeason(mediaType string, season int) int {
	if mediaType != "tv" || season <= 0 {
		return 0
	}
	return season
}

// carpoolKey 生成存储 key：电影 movie:tmdbID；剧集 tv:tmdbID:s<season>（0=任意季）
func carpoolKey(tmdbID int, mediaType string, season int) string {
	if mediaType == "" {
		mediaType = "movie"
	}
	season = normalizeCarpoolSeason(mediaType, season)
	if mediaType == "tv" {
		return fmt.Sprintf("tv:%d:s%d", tmdbID, season)
	}
	return fmt.Sprintf("%s:%d", mediaType, tmdbID)
}

// parseCarpoolKey 解析存储 key，返回 mediaType、tmdbID、season（ok=false 表示非法）。
// 兼容旧格式：tv:<tmdbID>（无季后缀）解析为 season=0（任意季）。
func parseCarpoolKey(key string) (mediaType string, tmdbID, season int, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) < 2 {
		return "", 0, 0, false
	}
	mediaType = parts[0]
	id, err := strconv.Atoi(parts[1])
	if err != nil || id <= 0 {
		return "", 0, 0, false
	}
	if len(parts) == 2 {
		// 旧格式：tv:<id> → 任意季；movie:<id> 不变。
		return mediaType, id, 0, true
	}
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "s") {
		return "", 0, 0, false
	}
	sn, err := strconv.Atoi(strings.TrimPrefix(parts[2], "s"))
	if err != nil || sn < 0 {
		return "", 0, 0, false
	}
	return mediaType, id, normalizeCarpoolSeason(mediaType, sn), true
}

// SeasonLabel 季号展示文案：0 → 任意季，N → 第N季。
func SeasonLabel(season int) string {
	return seasonLabel(season)
}

// seasonLabel 季号展示文案：0 → 任意季，N → 第N季。
func seasonLabel(season int) string {
	if season <= 0 {
		return "任意季"
	}
	return fmt.Sprintf("第%d季", season)
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
		// 旧格式 key（tv:<id> 无季后缀）改写为 tv:<id>:s0（任意季）。
		// 若目标 key 已有数据（同一文件不可能出现两次旧 key，这里只是防重），合并。
		newKey := key
		if mt, tid, _, ok := parseCarpoolKey(key); ok && mt == "tv" {
			newKey = carpoolKey(tid, "tv", 0)
		}
		if existing, dup := s.entries[newKey]; dup {
			for id := range set {
				existing[id] = true
			}
		} else {
			s.entries[newKey] = set
		}
	}
	for key, meta := range fileData.Metadata {
		newKey := key
		if mt, tid, _, ok := parseCarpoolKey(key); ok && mt == "tv" {
			newKey = carpoolKey(tid, "tv", 0)
		}
		if _, dup := s.metadata[newKey]; !dup {
			s.metadata[newKey] = meta
		}
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
// season：剧集季号（0=任意季），电影忽略。
func (s *CarpoolService) Add(tmdbID int, mediaType string, season int, userID int64) int {
	count, err := s.AddChecked(tmdbID, mediaType, season, userID)
	if err != nil {
		logger.Info("[Carpool] 加入后持久化失败: %v", err)
	}
	return count
}

func (s *CarpoolService) AddChecked(tmdbID int, mediaType string, season int, userID int64) (int, error) {
	return s.AddWithMetadataChecked(tmdbID, mediaType, season, userID, CarpoolMetadata{})
}

func (s *CarpoolService) AddWithMetadataChecked(tmdbID int, mediaType string, season int, userID int64, meta CarpoolMetadata) (int, error) {
	key := carpoolKey(tmdbID, mediaType, season)

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
	Season  int       `json:"season,omitempty"` // 剧集季号（0=任意季），电影恒 0
	Title   string    `json:"title,omitempty"`
	Year    string    `json:"year,omitempty"`
	Poster  string    `json:"poster_path,omitempty"`
	AddedAt time.Time `json:"added_at,omitempty"`
}

// Remove 精确移除某个用户对某部电影或剧集某季的想看记录。
func (s *CarpoolService) Remove(tmdbID int, mediaType string, season int, userID int64) bool {
	removed, err := s.RemoveChecked(tmdbID, mediaType, season, userID)
	if err != nil {
		logger.Info("[Carpool] 移除后持久化失败: %v", err)
	}
	return removed
}

func (s *CarpoolService) RemoveChecked(tmdbID int, mediaType string, season int, userID int64) (bool, error) {
	key := carpoolKey(tmdbID, mediaType, season)
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

// Contains 报告用户是否已将该作品（剧集含季号）加入想看。
func (s *CarpoolService) Contains(tmdbID int, mediaType string, season int, userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[carpoolKey(tmdbID, mediaType, season)][userID]
}

// ListForUser 返回当前用户的想看列表（剧集条目带 Season，可直接展示「第N季」）。
func (s *CarpoolService) ListForUser(userID int64) []CarpoolItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []CarpoolItem{}
	for key, users := range s.entries {
		if !users[userID] {
			continue
		}
		mediaType, id, season, ok := parseCarpoolKey(key)
		if !ok {
			continue
		}
		meta := s.metadata[key]
		items = append(items, CarpoolItem{TMDBID: id, Type: mediaType, Season: season, Title: meta.Title, Year: meta.Year, Poster: meta.Poster, AddedAt: meta.AddedAt})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].AddedAt.Equal(items[j].AddedAt) {
			return items[i].AddedAt.After(items[j].AddedAt)
		}
		if items[i].Type == items[j].Type {
			if items[i].TMDBID == items[j].TMDBID {
				return items[i].Season < items[j].Season
			}
			return items[i].TMDBID < items[j].TMDBID
		}
		return items[i].Type < items[j].Type
	})
	return items
}

// Get 返回某部片某季当前的拼车用户列表（拷贝，调用方可安全使用）。
func (s *CarpoolService) Get(tmdbID int, mediaType string, season int) []int64 {
	key := carpoolKey(tmdbID, mediaType, season)

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

// GetAll 返回某部片全部季的拼车用户（去重），用于剧集热度等整部粒度的统计。
func (s *CarpoolService) GetAll(tmdbID int, mediaType string) []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []int64
	seen := map[int64]bool{}
	for key, set := range s.entries {
		mt, id, _, ok := parseCarpoolKey(key)
		if !ok || mt != mediaType || id != tmdbID {
			continue
		}
		for uid := range set {
			if !seen[uid] {
				seen[uid] = true
				out = append(out, uid)
			}
		}
	}
	return out
}

// CountAll 返回某部片全部季拼车用户总数（去重后的 key 数之和；同一个人拼了两季算两次）。
func (s *CarpoolService) CountAll(tmdbID int, mediaType string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for key, set := range s.entries {
		mt, id, _, ok := parseCarpoolKey(key)
		if !ok || mt != mediaType || id != tmdbID {
			continue
		}
		total += len(set)
	}
	return total
}

// GetAndClear 返回某部片某季的拼车用户列表并清空该季记录。
func (s *CarpoolService) GetAndClear(tmdbID int, mediaType string, season int) []int64 {
	key := carpoolKey(tmdbID, mediaType, season)

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

// clearKeysLocked 清空指定的多个 key（调用方必须持有写锁），返回受影响的用户去重列表。
// 持久化失败时回滚全部 key。
func (s *CarpoolService) clearKeysLocked(keys []string) []int64 {
	saved := map[string]map[int64]bool{}
	savedMeta := map[string]CarpoolMetadata{}
	var notified []int64
	seen := map[int64]bool{}
	for _, key := range keys {
		set, ok := s.entries[key]
		if !ok || len(set) == 0 {
			continue
		}
		saved[key] = set
		if meta, hadMeta := s.metadata[key]; hadMeta {
			savedMeta[key] = meta
		}
		for id := range set {
			if !seen[id] {
				seen[id] = true
				notified = append(notified, id)
			}
		}
		delete(s.entries, key)
		delete(s.metadata, key)
	}
	if len(saved) == 0 {
		return nil
	}
	if err := s.saveLocked(); err != nil {
		for key, set := range saved {
			s.entries[key] = set
		}
		for key, meta := range savedMeta {
			s.metadata[key] = meta
		}
		logger.Info("[Carpool] 清空后持久化失败，已回滚: %v", err)
		return nil
	}
	return notified
}

// arrivalSeasons 返回某季入库（或某季取消）时需要匹配的 key：
// arrival>0 时匹配该季 + 任意季(0)；arrival=0（整部/季未知）时匹配全部季。
func (s *CarpoolService) arrivalSeasons(tmdbID int, mediaType string, arrival int) []string {
	arrival = normalizeCarpoolSeason(mediaType, arrival)
	if arrival > 0 {
		return []string{carpoolKey(tmdbID, mediaType, arrival), carpoolKey(tmdbID, mediaType, 0)}
	}
	// 整部取消或季未知：匹配该部片全部季的 key。
	var keys []string
	for key := range s.entries {
		mt, id, _, ok := parseCarpoolKey(key)
		if ok && mt == mediaType && id == tmdbID {
			keys = append(keys, key)
		}
	}
	return keys
}

// GetArrivalMembers 返回某季入库时应通知的用户：该季 + 任意季(0)（去重），只读不清空。
// 季未知（arrival=0）时只返回任意季成员。
func (s *CarpoolService) GetArrivalMembers(tmdbID int, mediaType string, arrival int) []int64 {
	arrival = normalizeCarpoolSeason(mediaType, arrival)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	if arrival > 0 {
		keys = []string{carpoolKey(tmdbID, mediaType, arrival), carpoolKey(tmdbID, mediaType, 0)}
	} else {
		keys = []string{carpoolKey(tmdbID, mediaType, 0)}
	}
	var out []int64
	seen := map[int64]bool{}
	for _, key := range keys {
		for uid := range s.entries[key] {
			if !seen[uid] {
				seen[uid] = true
				out = append(out, uid)
			}
		}
	}
	return out
}

// GetAndClearArrival 某季入库：返回该季 + 任意季(0) 的拼车用户（去重）并结清这两季，
// 其他季的成员保留不动。季未知（arrival=0）时只结清任意季。
func (s *CarpoolService) GetAndClearArrival(tmdbID int, mediaType string, arrival int) []int64 {
	arrival = normalizeCarpoolSeason(mediaType, arrival)
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	if arrival > 0 {
		keys = []string{carpoolKey(tmdbID, mediaType, arrival), carpoolKey(tmdbID, mediaType, 0)}
	} else {
		keys = []string{carpoolKey(tmdbID, mediaType, 0)}
	}
	return s.clearKeysLocked(keys)
}

// ClearArrival 拼车取消：season>0 时清该季 + 任意季；season=0（整部取消）时清全部季。
// 返回受影响的用户去重列表。
func (s *CarpoolService) ClearArrival(tmdbID int, mediaType string, season int) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearKeysLocked(s.arrivalSeasons(tmdbID, mediaType, season))
}
