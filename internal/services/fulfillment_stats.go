package services

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ─────────────────────────────────────
// 履约统计：求片 → 入库的耗时样本 + 入库回访反馈存档。
//
// ReviewService 的已完成单据 7 天后会被清理，因此 ETA 统计不能依赖
// reviews 本身，而是在完成瞬间抽一条轻量样本单独持久化。
// ─────────────────────────────────────

// FulfillmentSample 一条履约耗时样本。
type FulfillmentSample struct {
	MediaType   string    `json:"media_type"`     // movie / tv
	Year        int       `json:"year,omitempty"` // 媒体发行年份（新旧片分桶用）
	DurationSec int64     `json:"duration_sec"`   // 提交 → 完成 的秒数
	CompletedAt time.Time `json:"completed_at"`   // 完成时间
}

// WatchFeedback 入库回访的用户回答。
type WatchFeedback struct {
	RequestID string    `json:"request_id"`
	Title     string    `json:"title,omitempty"`
	Answer    string    `json:"answer"` // w=看完了 l=还没看 d=不想看了
	At        time.Time `json:"at"`
}

// CompletionRecord 一条长期保留的入库记录。
// ReviewRequest 会被定期清理，90 天冷片盘点依赖这份轻量账本。
type CompletionRecord struct {
	RequestID   string    `json:"request_id"`
	TelegramID  int64     `json:"telegram_id"`
	Title       string    `json:"title"`
	MediaType   string    `json:"media_type"`
	Year        int       `json:"year,omitempty"`
	Season      int       `json:"season,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

const (
	maxFulfillmentSamples = 400
	maxWatchFeedback      = 500
	maxCompletionRecords  = 5000
	// 超过 180 天的履约耗时多半是迁移/脏历史，不让它污染用户 ETA。
	maxFulfillmentDuration = 180 * 24 * time.Hour
)

// FulfillmentStatsService 管理履约样本与回访反馈的持久化和估算。
type FulfillmentStatsService struct {
	mu             sync.Mutex
	file           string
	fbFile         string
	completionFile string
	samples        []FulfillmentSample
	feedback       []WatchFeedback
	completions    []CompletionRecord
}

// NewFulfillmentStatsService 创建服务并加载历史数据。
func NewFulfillmentStatsService(dataDir string) *FulfillmentStatsService {
	s := &FulfillmentStatsService{
		file:           filepath.Join(dataDir, "fulfillment_stats.json"),
		fbFile:         filepath.Join(dataDir, "watch_feedback.json"),
		completionFile: filepath.Join(dataDir, "completion_records.json"),
	}
	s.load()
	return s
}

func (s *FulfillmentStatsService) load() {
	if data, err := os.ReadFile(s.file); err == nil {
		if err := json.Unmarshal(data, &s.samples); err != nil {
			logger.Info("[FulfillmentStats] 样本文件解析失败（忽略重建）: %v", err)
			s.samples = nil
		}
	}
	if data, err := os.ReadFile(s.fbFile); err == nil {
		if err := json.Unmarshal(data, &s.feedback); err != nil {
			logger.Info("[FulfillmentStats] 回访文件解析失败（忽略重建）: %v", err)
			s.feedback = nil
		}
	}
	if data, err := os.ReadFile(s.completionFile); err == nil {
		if err := json.Unmarshal(data, &s.completions); err != nil {
			logger.Info("[FulfillmentStats] 完成账本解析失败（忽略重建）: %v", err)
			s.completions = nil
		}
	}
}

func (s *FulfillmentStatsService) saveSamplesLocked() {
	data, err := json.MarshalIndent(s.samples, "", "  ")
	if err != nil {
		return
	}
	if err := atomicWriteFile(s.file, data, 0o644); err != nil {
		logger.Info("[FulfillmentStats] 样本写入失败: %v", err)
	}
}

func (s *FulfillmentStatsService) saveFeedbackLocked() {
	data, err := json.MarshalIndent(s.feedback, "", "  ")
	if err != nil {
		return
	}
	if err := atomicWriteFile(s.fbFile, data, 0o644); err != nil {
		logger.Info("[FulfillmentStats] 回访写入失败: %v", err)
	}
}

func (s *FulfillmentStatsService) saveCompletionsLocked() {
	data, err := json.MarshalIndent(s.completions, "", "  ")
	if err != nil {
		return
	}
	if err := atomicWriteFile(s.completionFile, data, 0o644); err != nil {
		logger.Info("[FulfillmentStats] 完成账本写入失败: %v", err)
	}
}

// AddCompletion 记录长期入库账本；同一 requestID 幂等。
func (s *FulfillmentStatsService) AddCompletion(record CompletionRecord) {
	if record.RequestID == "" || record.Title == "" || record.CompletedAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.completions {
		if old.RequestID == record.RequestID {
			return
		}
	}
	s.completions = append(s.completions, record)
	if len(s.completions) > maxCompletionRecords {
		s.completions = s.completions[len(s.completions)-maxCompletionRecords:]
	}
	s.saveCompletionsLocked()
}

// CompletionRecords returns a snapshot for safe reconciliation by ReviewService.
func (s *FulfillmentStatsService) CompletionRecords() []CompletionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CompletionRecord(nil), s.completions...)
}

// StaleUnwatchedTitles 返回已入库超过 days、且没有积极回访回答的片名。
func (s *FulfillmentStatsService) StaleUnwatchedTitles(days, limit int) []string {
	if days < 1 || limit < 1 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answers := make(map[string]string, len(s.feedback))
	for _, fb := range s.feedback {
		answers[fb.RequestID] = fb.Answer
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var out []string
	for i := len(s.completions) - 1; i >= 0 && len(out) < limit; i-- {
		r := s.completions[i]
		if r.CompletedAt.After(cutoff) {
			continue
		}
		if answers[r.RequestID] == "w" {
			continue
		}
		out = append(out, r.Title)
	}
	return out
}

// AddSample 记录一条履约样本（完成时调用）。
func (s *FulfillmentStatsService) AddSample(mediaType string, year int, durationSec int64) {
	if durationSec <= 0 || durationSec > int64(maxFulfillmentDuration.Seconds()) || mediaType == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, FulfillmentSample{
		MediaType:   mediaType,
		Year:        year,
		DurationSec: durationSec,
		CompletedAt: time.Now(),
	})
	if len(s.samples) > maxFulfillmentSamples {
		s.samples = s.samples[len(s.samples)-maxFulfillmentSamples:]
	}
	s.saveSamplesLocked()
}

// Estimate 估算指定类型/年份求片的入库耗时（中位数）。
// 分桶策略：近两年的片子走「新片桶」，更早的走「旧片桶」；
// 桶内样本不足 3 条时回退到同类型全量；仍不足则不给估算。
func (s *FulfillmentStatsService) Estimate(mediaType string, year int) (time.Duration, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fresh := year >= time.Now().Year()-1
	var bucket, all []int64
	for _, sm := range s.samples {
		if sm.MediaType != mediaType {
			continue
		}
		all = append(all, sm.DurationSec)
		if (sm.Year >= time.Now().Year()-1) == fresh {
			bucket = append(bucket, sm.DurationSec)
		}
	}
	pick := bucket
	if len(pick) < 3 {
		pick = all
	}
	if len(pick) < 3 {
		return 0, 0, false
	}
	sort.Slice(pick, func(i, j int) bool { return pick[i] < pick[j] })
	median := pick[len(pick)/2]
	return time.Duration(median) * time.Second, len(pick), true
}

// EstimateText 生成求片回执里的 ETA 参考文案；无足量数据时返回空串。
func (s *FulfillmentStatsService) EstimateText(mediaType string, year int) string {
	d, n, ok := s.Estimate(mediaType, year)
	if !ok {
		return ""
	}
	return fmt.Sprintf("📈 参考：最近 %d 次同类求片，一般 %s 内入库", n, humanizeFulfillment(d))
}

// AddWatchFeedback 记录一条入库回访回答。
func (s *FulfillmentStatsService) AddWatchFeedback(requestID, answer string) {
	s.AddWatchFeedbackTitled(requestID, "", answer)
}

// AddWatchFeedbackTitled 记录一条带片名的入库回访回答（管理端清单展示用）。
func (s *FulfillmentStatsService) AddWatchFeedbackTitled(requestID, title, answer string) {
	if requestID == "" || answer == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// 同一个请求只保留最后一次回答；按钮重试/重复点击不会污染分布。
	for i := range s.feedback {
		if s.feedback[i].RequestID != requestID {
			continue
		}
		s.feedback[i].Answer = answer
		s.feedback[i].At = now
		if title != "" {
			s.feedback[i].Title = title
		}
		s.saveFeedbackLocked()
		return
	}
	s.feedback = append(s.feedback, WatchFeedback{RequestID: requestID, Title: title, Answer: answer, At: now})
	if len(s.feedback) > maxWatchFeedback {
		s.feedback = s.feedback[len(s.feedback)-maxWatchFeedback:]
	}
	s.saveFeedbackLocked()
}

// RecentUnwantedTitles 返回最近回答「不想看了」的片单（管理端清理线索）。
func (s *FulfillmentStatsService) RecentUnwantedTitles(limit int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var titles []string
	for i := len(s.feedback) - 1; i >= 0 && len(titles) < limit; i-- {
		fb := s.feedback[i]
		if fb.Answer == "d" && fb.Title != "" {
			titles = append(titles, fb.Title)
		}
	}
	return titles
}

// WatchFeedbackCounts 汇总回访回答分布（管理端展示用）。
func (s *FulfillmentStatsService) WatchFeedbackCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int, 3)
	for _, fb := range s.feedback {
		counts[fb.Answer]++
	}
	return counts
}

// humanizeFulfillment 把时长转成用户友好的中文粒度。
func humanizeFulfillment(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 1:
		return "几分钟"
	case m < 60:
		return fmt.Sprintf("%d 分钟", m)
	case d.Hours() < 48:
		h := int(math.Round(d.Hours()))
		if h < 1 {
			h = 1
		}
		return fmt.Sprintf("%d 小时", h)
	default:
		days := int(math.Round(d.Hours() / 24))
		return fmt.Sprintf("%d 天", days)
	}
}
