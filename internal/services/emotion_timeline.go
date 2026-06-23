package services

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ============================================================
//  情绪时间线引擎 (Emotion Timeline Engine)
//  所有游戏功能的底层数据服务
// ============================================================

// ViewRecord 单次观影记录
type ViewRecord struct {
	ID       string
	Name     string
	Type     string    // movie / series
	Genres   []string
	Rating   float64
	Year     int
	WatchedAt time.Time // 观影时间
}

// EmotionData 情绪数据点
type EmotionData struct {
	Date      time.Time
	Intensity float64 // 0-10 情绪强度
	Genres    []string
	Mood      string // 情绪标签
}

// GenreTransition 类型转变事件
type GenreTransition struct {
	From      string
	To        string
	Date      time.Time
	Direction string // "升压" / "降压" / "转向"
}

// ViewingPattern 观影模式
type ViewingPattern struct {
	PeakHour    int    // 高峰观影时间
	PeakPeriod  string // "深夜" / "傍晚" / "午后" / "早晨"
	WeekdayAvg  float64
	WeekendAvg  float64
	IsNightOwl  bool
}

// EmotionalProfile 完整情绪画像
type EmotionalProfile struct {
	// 基础数据
	TotalWatched  int
	MovieCount    int
	SeriesCount   int
	TopGenres     []GenreCount
	WatchDays     int

	// 情绪分析
	EmotionalIntensity float64        // 当前情绪强度 0-10
	EmotionTrend       string         // "上升" / "平稳" / "下降"
	EmotionCurve       []EmotionData  // 近4周情绪曲线
	CurrentMood        string         // 当前情绪描述

	// 模式分析
	Pattern         ViewingPattern
	SignatureGenre  string           // 标志性类型
	GenreTransitions []GenreTransition // 重要类型转变
	WatchStreak     int              // 连续观影天数
	LongestGap      int              // 最长空白天数

	// 叙事元素
	Narrative       string           // 生成的叙事文本
	PersonalityTag  string           // 性格标签
	LifePhase       string           // 当前人生阶段推断
}

// GenreCount 类型计数
type GenreCount struct {
	Genre string
	Count int
}

// EmotionTimelineService 情绪时间线引擎
type EmotionTimelineService struct {
	embyURL    string
	embyAPIKey string
	httpClient *http.Client
}

// NewEmotionTimelineService 创建时间线引擎
func NewEmotionTimelineService(embyURL, embyAPIKey string) *EmotionTimelineService {
	return &EmotionTimelineService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// 情绪强度映射
var genreIntensityMap = map[string]float64{
	"恐怖":   9.0,
	"惊悚":   8.5,
	"动作":   8.0,
	"犯罪":   7.5,
	"战争":   8.0,
	"悬疑":   7.0,
	"科幻":   6.5,
	"冒险":   6.5,
	"奇幻":   6.0,
	"剧情":   5.5,
	"爱情":   5.0,
	"喜剧":   4.0,
	"动画":   4.5,
	"家庭":   3.5,
	"纪录":   3.0,
	"音乐":   3.5,
}

// BuildProfile 构建用户情绪画像
func (s *EmotionTimelineService) BuildProfile(embyUserID, userName string) (*EmotionalProfile, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}

	// 拉取观影数据（电影+剧集，各200条）
	records, err := s.fetchViewRecords(embyUserID, 400)
	if err != nil {
		return nil, fmt.Errorf("获取观影数据失败: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("没有找到观影记录")
	}

	profile := &EmotionalProfile{}
	profile.TotalWatched = len(records)

	// 统计类型分布
	genreCount := make(map[string]int)
	var ratings []float64
	for _, r := range records {
		if r.Type == "movie" {
			profile.MovieCount++
		} else {
			profile.SeriesCount++
		}
		for _, g := range r.Genres {
			genreCount[g]++
		}
		if r.Rating > 0 {
			ratings = append(ratings, r.Rating)
		}
	}

	// Top genres
	profile.TopGenres = s.buildTopGenres(genreCount, 5)
	profile.SignatureGenre = s.findSignatureGenre(genreCount)

	// 情绪曲线
	profile.EmotionCurve = s.buildEmotionCurve(records)
	profile.EmotionalIntensity = s.calculateCurrentIntensity(profile.EmotionCurve)
	profile.EmotionTrend = s.calculateTrend(profile.EmotionCurve)
	profile.CurrentMood = describeMood(profile.EmotionalIntensity, profile.EmotionTrend, profile.SignatureGenre)

	// 观影模式
	profile.Pattern = s.analyzePattern(records)
	profile.WatchStreak = s.calculateStreak(records)
	profile.LongestGap = s.calculateLongestGap(records)

	// 类型转变
	profile.GenreTransitions = s.findTransitions(records)

	// 计算观影天数
	daySet := make(map[string]bool)
	for _, r := range records {
		daySet[r.WatchedAt.Format("2006-01-02")] = true
	}
	profile.WatchDays = len(daySet)

	// 性格标签和人生阶段
	profile.PersonalityTag = s.derivePersonalityTag(profile)
	profile.LifePhase = s.deriveLifePhase(profile)

	return profile, nil
}

// fetchViewRecords 从 Emby 拉取观影记录
func (s *EmotionTimelineService) fetchViewRecords(userID string, limit int) ([]ViewRecord, error) {
	// 使用 Recursive=true 获取所有已观看项目，按观影时间排序
	apiURL := fmt.Sprintf(
		"%s/Users/%s/Items?Recursive=true&SortBy=DatePlayed&SortOrder=Descending&Filters=IsPlayed&Limit=%d&Fields=Genres,CommunityRating,UserData,ProductionYear&api_key=%s",
		s.embyURL, userID, limit, s.embyAPIKey,
	)

	resp, err := s.httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			ID              string   `json:"Id"`
			Name            string   `json:"Name"`
			Type            string   `json:"Type"`
			Genres          []string `json:"Genres"`
			CommunityRating float64  `json:"CommunityRating"`
			ProductionYear  int      `json:"ProductionYear"`
			UserData        struct {
				LastPlayedDate string `json:"LastPlayedDate"`
				PlayCount      int    `json:"PlayCount"`
			} `json:"UserData"`
			SeriesName string `json:"SeriesName"`
		} `json:"Items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var records []ViewRecord
	for _, item := range result.Items {
		name := item.Name
		if item.SeriesName != "" {
			name = item.SeriesName
		}

		// 解析观影时间
		var watchedAt time.Time
		if item.UserData.LastPlayedDate != "" {
			watchedAt, _ = time.Parse(time.RFC3339, item.UserData.LastPlayedDate)
		}
		if watchedAt.IsZero() {
			continue // 没有时间记录的跳过
		}

		records = append(records, ViewRecord{
			ID:        item.ID,
			Name:      name,
			Type:      strings.ToLower(item.Type),
			Genres:    item.Genres,
			Rating:    item.CommunityRating,
			Year:      item.ProductionYear,
			WatchedAt: watchedAt,
		})
	}

	logger.Info("[Timeline] Fetched %d view records for user %s", len(records), userID)
	return records, nil
}

// buildTopGenres 构建类型排行榜
func (s *EmotionTimelineService) buildTopGenres(genreCount map[string]int, limit int) []GenreCount {
	var gcs []GenreCount
	for g, c := range genreCount {
		gcs = append(gcs, GenreCount{g, c})
	}
	sort.Slice(gcs, func(i, j int) bool { return gcs[i].Count > gcs[j].Count })
	if len(gcs) > limit {
		gcs = gcs[:limit]
	}
	return gcs
}

// findSignatureGenre 找到标志性类型（占比最高且超过阈值）
func (s *EmotionTimelineService) findSignatureGenre(genreCount map[string]int) string {
	total := 0
	for _, c := range genreCount {
		total += c
	}
	if total == 0 {
		return "全能"
	}

	top := ""
	topCount := 0
	for g, c := range genreCount {
		if c > topCount {
			topCount = c
			top = g
		}
	}
	if float64(topCount)/float64(total) > 0.2 {
		return top
	}
	return "全能"
}

// buildEmotionCurve 构建近4周情绪曲线
func (s *EmotionTimelineService) buildEmotionCurve(records []ViewRecord) []EmotionData {
	now := time.Now()
	// 按周分组（近4周）
	weeks := make([][]ViewRecord, 4)
	for _, r := range records {
		daysAgo := int(now.Sub(r.WatchedAt).Hours() / 24)
		weekIdx := daysAgo / 7
		if weekIdx >= 0 && weekIdx < 4 {
			weeks[3-weekIdx] = append(weeks[3-weekIdx], r) // 最早的在前
		}
	}

	var curve []EmotionData
	for i, week := range weeks {
		if len(week) == 0 {
			curve = append(curve, EmotionData{
				Date:      now.AddDate(0, 0, -21+i*7),
				Intensity: 0,
				Genres:    nil,
				Mood:      "无观影",
			})
			continue
		}

		// 计算该周情绪强度
		totalIntensity := 0.0
		genreSet := make(map[string]int)
		for _, r := range week {
			for _, g := range r.Genres {
				totalIntensity += genreIntensityMap[g]
				genreSet[g]++
			}
		}
		avgIntensity := totalIntensity / float64(len(week))
		avgIntensity = math.Min(10, math.Max(0, avgIntensity))

		// 该周主要类型
		var topGenre string
		topCount := 0
		for g, c := range genreSet {
			if c > topCount {
				topCount = c
				topGenre = g
			}
		}

		curve = append(curve, EmotionData{
			Date:      now.AddDate(0, 0, -21+i*7),
			Intensity: avgIntensity,
			Genres:    []string{topGenre},
			Mood:      intensityToMood(avgIntensity),
		})
	}

	return curve
}

// calculateCurrentIntensity 计算当前情绪强度（加权近期）
func (s *EmotionTimelineService) calculateCurrentIntensity(curve []EmotionData) float64 {
	if len(curve) == 0 {
		return 5.0
	}
	// 最近一周权重最高
	weights := []float64{0.1, 0.2, 0.3, 0.4} // 从最早到最近
	totalWeight := 0.0
	weightedSum := 0.0
	for i, d := range curve {
		if i < len(weights) {
			weightedSum += d.Intensity * weights[i]
			totalWeight += weights[i]
		}
	}
	if totalWeight == 0 {
		return 5.0
	}
	return weightedSum / totalWeight
}

// calculateTrend 计算情绪趋势
func (s *EmotionTimelineService) calculateTrend(curve []EmotionData) string {
	if len(curve) < 2 {
		return "平稳"
	}
	last := curve[len(curve)-1].Intensity
	prev := curve[len(curve)-2].Intensity
	diff := last - prev
	if diff > 1.5 {
		return "上升"
	}
	if diff < -1.5 {
		return "下降"
	}
	return "平稳"
}

// describeMood 生成情绪描述
func describeMood(intensity float64, trend, genre string) string {
	base := ""
	switch {
	case intensity >= 8:
		base = "高压状态"
	case intensity >= 6:
		base = "节奏紧凑"
	case intensity >= 4:
		base = "平稳放松"
	default:
		base = "宁静舒缓"
	}

	trendText := ""
	switch trend {
	case "上升":
		trendText = "，情绪在升温"
	case "下降":
		trendText = "，正在寻找平静"
	}

	genreText := ""
	switch genre {
	case "恐怖", "惊悚":
		genreText = "。你在寻找刺激感"
	case "动作", "犯罪":
		genreText = "。你渴望肾上腺素"
	case "爱情", "剧情":
		genreText = "。你在寻找情感共鸣"
	case "喜剧":
		genreText = "。你需要放松和欢笑"
	case "科幻", "奇幻":
		genreText = "。你的想象力在飞驰"
	case "纪录":
		genreText = "。你在追求真实和知识"
	case "动画", "家庭":
		genreText = "。你向往简单纯粹的东西"
	}

	return base + trendText + genreText
}

// analyzePattern 分析观影模式
func (s *EmotionTimelineService) analyzePattern(records []ViewRecord) ViewingPattern {
	pattern := ViewingPattern{}

	// 统计每小时观影次数
	hourCount := make(map[int]int)
	weekdayCount, weekendCount := 0, 0
	weekdayDays, weekendDays := make(map[string]bool), make(map[string]bool)

	for _, r := range records {
		hour := r.WatchedAt.Hour()
		hourCount[hour]++

		day := r.WatchedAt.Weekday()
		dateKey := r.WatchedAt.Format("2006-01-02")
		if day == time.Saturday || day == time.Sunday {
			weekendCount++
			weekendDays[dateKey] = true
		} else {
			weekdayCount++
			weekdayDays[dateKey] = true
		}
	}

	// 找高峰时段
	peakHour := 0
	peakCount := 0
	for h, c := range hourCount {
		if c > peakCount {
			peakCount = c
			peakHour = h
		}
	}
	pattern.PeakHour = peakHour

	switch {
	case peakHour >= 22 || peakHour < 4:
		pattern.PeakPeriod = "深夜"
		pattern.IsNightOwl = true
	case peakHour >= 18:
		pattern.PeakPeriod = "傍晚"
	case peakHour >= 12:
		pattern.PeakPeriod = "午后"
	default:
		pattern.PeakPeriod = "早晨"
	}

	// 工作日 vs 周末平均
	if len(weekdayDays) > 0 {
		pattern.WeekdayAvg = float64(weekdayCount) / float64(len(weekdayDays))
	}
	if len(weekendDays) > 0 {
		pattern.WeekendAvg = float64(weekendCount) / float64(len(weekendDays))
	}

	return pattern
}

// calculateStreak 计算连续观影天数
func (s *EmotionTimelineService) calculateStreak(records []ViewRecord) int {
	if len(records) == 0 {
		return 0
	}

	// 收集所有观影日期
	daySet := make(map[string]bool)
	for _, r := range records {
		daySet[r.WatchedAt.Format("2006-01-02")] = true
	}

	// 从今天往前数连续天数
	streak := 0
	now := time.Now()
	for i := 0; i < 365; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		if daySet[date] {
			streak++
		} else {
			break
		}
	}
	return streak
}

// calculateLongestGap 计算最长空白期
func (s *EmotionTimelineService) calculateLongestGap(records []ViewRecord) int {
	if len(records) < 2 {
		return 0
	}

	// 按时间排序
	sorted := make([]ViewRecord, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].WatchedAt.Before(sorted[j].WatchedAt)
	})

	maxGap := 0
	for i := 1; i < len(sorted); i++ {
		gap := int(sorted[i].WatchedAt.Sub(sorted[i-1].WatchedAt).Hours() / 24)
		if gap > maxGap {
			maxGap = gap
		}
	}
	return maxGap
}

// findTransitions 发现重要类型转变
func (s *EmotionTimelineService) findTransitions(records []ViewRecord) []GenreTransition {
	if len(records) < 10 {
		return nil
	}

	// 按时间排序
	sorted := make([]ViewRecord, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].WatchedAt.Before(sorted[j].WatchedAt)
	})

	// 将记录按周分组，找每周的主要类型
	weekGenres := make(map[string]string) // weekKey -> topGenre
	weekRecords := make(map[string][]ViewRecord)

	for _, r := range sorted {
		year, week := r.WatchedAt.ISOWeek()
		weekKey := fmt.Sprintf("%d-W%02d", year, week)
		weekRecords[weekKey] = append(weekRecords[weekKey], r)
	}

	for weekKey, recs := range weekRecords {
		genreCount := make(map[string]int)
		for _, r := range recs {
			for _, g := range r.Genres {
				genreCount[g]++
			}
		}
		top := ""
		topC := 0
		for g, c := range genreCount {
			if c > topC {
				topC = c
				top = g
			}
		}
		weekGenres[weekKey] = top
	}

	// 按时间顺序找转变
	var keys []string
	for k := range weekGenres {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var transitions []GenreTransition
	for i := 1; i < len(keys); i++ {
		prev := weekGenres[keys[i-1]]
		curr := weekGenres[keys[i]]
		if prev != curr && prev != "" && curr != "" {
			// 判断转变方向
			prevI := genreIntensityMap[prev]
			currI := genreIntensityMap[curr]
			direction := "转向"
			if currI > prevI+1 {
				direction = "升压"
			} else if currI < prevI-1 {
				direction = "降压"
			}

			transitions = append(transitions, GenreTransition{
				From:      prev,
				To:        curr,
				Direction: direction,
			})
		}
	}

	// 只保留最近3个转变
	if len(transitions) > 3 {
		transitions = transitions[len(transitions)-3:]
	}
	return transitions
}

// derivePersonalityTag 推导性格标签
func (s *EmotionTimelineService) derivePersonalityTag(profile *EmotionalProfile) string {
	genre := profile.SignatureGenre
	intensity := profile.EmotionalIntensity
	pattern := profile.Pattern

	// 基于标志性类型 + 情绪强度 + 观影模式组合
	switch {
	case genre == "恐怖" && intensity >= 7:
		return "暗夜猎手"
	case genre == "恐怖" && pattern.IsNightOwl:
		return "午夜行者"
	case genre == "科幻" && intensity >= 6:
		return "星际探索者"
	case genre == "科幻":
		return "未来思考者"
	case genre == "动作" && intensity >= 7:
		return "肾上腺素瘾者"
	case genre == "动作":
		return "速度追求者"
	case genre == "剧情" && intensity <= 4:
		return "深夜沉思者"
	case genre == "剧情":
		return "人生观察家"
	case genre == "喜剧" && pattern.WeekendAvg > pattern.WeekdayAvg*1.5:
		return "周末狂欢者"
	case genre == "喜剧":
		return "快乐制造机"
	case genre == "爱情":
		return "浪漫主义者"
	case genre == "纪录" && intensity <= 4:
		return "安静的求知者"
	case genre == "纪录":
		return "真相猎人"
	case genre == "动画":
		return "永恒少年"
	case genre == "犯罪" || genre == "悬疑":
		return "谜题破解者"
	case genre == "战争" || genre == "历史":
		return "历史回望者"
	case genre == "全能" && profile.TotalWatched >= 100:
		return "阅片无数的杂食者"
	case genre == "全能":
		return "好奇的探索者"
	default:
		return "独特的观影者"
	}
}

// deriveLifePhase 推断当前人生阶段
func (s *EmotionTimelineService) deriveLifePhase(profile *EmotionalProfile) string {
	intensity := profile.EmotionalIntensity
	trend := profile.EmotionTrend
	genre := profile.SignatureGenre
	gap := profile.LongestGap

	switch {
	case gap >= 14:
		return "沉寂期 — 你离开了屏幕一段时间，生活里一定有更重要的事在发生"
	case intensity >= 7 && trend == "上升":
		return "高压期 — 你在电影里寻找出口，密集的观影是你释放压力的方式"
	case intensity >= 7 && trend == "下降":
		return "转折期 — 高强度的观影正在退潮，你似乎在找到新的平衡"
	case intensity <= 3 && genre == "爱情":
		return "柔软期 — 你的心正在变得柔软，或者它本来就很柔软"
	case intensity <= 3:
		return "沉淀期 — 你在用平静的电影包裹自己，这是一种自我修复"
	case profile.Pattern.IsNightOwl && profile.WatchStreak >= 7:
		return "熬夜期 — 连续的深夜观影，屏幕的光是你夜晚唯一的陪伴"
	case trend == "上升":
		return "探索期 — 你在尝试更刺激的内容，好奇心在驱动你走向未知"
	default:
		return "平稳期 — 你的观影节奏很健康，电影是生活的一部分而不是全部"
	}
}

// intensityToMood 强度转情绪标签
func intensityToMood(intensity float64) string {
	switch {
	case intensity >= 8:
		return "🔥 炽热"
	case intensity >= 6:
		return "⚡ 激烈"
	case intensity >= 4:
		return "🌊 平静"
	case intensity >= 2:
		return "🌿 舒缓"
	default:
		return "🌙 宁静"
	}
}

// GenerateNarrative 生成叙事文本（用于时光放映机）
func (s *EmotionTimelineService) GenerateNarrative(profile *EmotionalProfile, userName string) string {
	var parts []string

	// 开场
	parts = append(parts, fmt.Sprintf("📽️ %s 的观影故事", userName))
	parts = append(parts, "")

	// 人生阶段
	parts = append(parts, fmt.Sprintf("📍 %s", profile.LifePhase))
	parts = append(parts, "")

	// 标志性类型
	if profile.SignatureGenre != "全能" {
		parts = append(parts, fmt.Sprintf("🎭 你的标志性类型是「%s」，在你看过的作品中占比最高。", profile.SignatureGenre))
	} else {
		parts = append(parts, "🎭 你是一个杂食型观众，没有固定的偏好类型，什么都看。")
	}

	// 情绪曲线叙事
	if len(profile.EmotionCurve) >= 2 {
		curve := profile.EmotionCurve
		first := curve[0]
		last := curve[len(curve)-1]

		if last.Intensity > first.Intensity+1.5 {
			parts = append(parts, fmt.Sprintf("📈 这一个月，你的观影情绪从 %s 一路走到了 %s。你正在寻找更强烈的东西。", first.Mood, last.Mood))
		} else if last.Intensity < first.Intensity-1.5 {
			parts = append(parts, fmt.Sprintf("📉 这一个月，你从 %s 慢慢退回到了 %s。你正在寻找平静。", first.Mood, last.Mood))
		} else {
			parts = append(parts, fmt.Sprintf("📊 这一个月你的情绪曲线很平稳，始终保持在 %s 左右。", last.Mood))
		}
	}

	// 类型转变
	if len(profile.GenreTransitions) > 0 {
		parts = append(parts, "")
		parts = append(parts, "🔄 你经历了这些转变：")
		for _, t := range profile.GenreTransitions {
			parts = append(parts, fmt.Sprintf("  %s → %s（%s）", t.From, t.To, t.Direction))
		}
	}

	// 观影模式
	parts = append(parts, "")
	if profile.Pattern.IsNightOwl {
		parts = append(parts, fmt.Sprintf("🌙 你是一个夜猫子，%s是你最活跃的观影时段。", profile.Pattern.PeakPeriod))
	} else {
		parts = append(parts, fmt.Sprintf("☀️ 你习惯在%s观影，作息很规律。", profile.Pattern.PeakPeriod))
	}

	// 连续观影
	if profile.WatchStreak >= 7 {
		parts = append(parts, fmt.Sprintf("🔥 你已经连续观影 %d 天了，电影已经成为你生活的一部分。", profile.WatchStreak))
	}

	// 空白期
	if profile.LongestGap >= 7 {
		parts = append(parts, fmt.Sprintf("⏸️ 你曾有过一段 %d 天的空白期。那段时间，屏幕外的世界一定很精彩。", profile.LongestGap))
	}

	// 性格标签
	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("🏷️ 你的观影人格：%s", profile.PersonalityTag))

	return strings.Join(parts, "\n")
}
