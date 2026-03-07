package services

import (
	"math"
	"strings"
	"unicode"
)

// SimilarityChecker 相似度检查器
type SimilarityChecker struct {
	threshold float64
}

// NewSimilarityChecker 创建相似度检查器
func NewSimilarityChecker(threshold float64) *SimilarityChecker {
	return &SimilarityChecker{
		threshold: threshold,
	}
}

// CheckSimilarity 检查两个反馈的相似度
func (sc *SimilarityChecker) CheckSimilarity(fb1, fb2 *Feedback) float64 {
	// 问题类型相同
	if fb1.IssueType != fb2.IssueType {
		return 0.0
	}

	// 相似度 = 标题相似度 * 0.4 + 描述相似度 * 0.6
	titleSim := sc.calculateSimilarity(fb1.Title, fb2.Title)
	descSim := sc.calculateSimilarity(fb1.Description, fb2.Description)

	similarity := titleSim*0.4 + descSim*0.6

	// 如果是同一影片，增加相似度
	if fb1.MediaID != "" && fb1.MediaID == fb2.MediaID {
		similarity += 0.2
	}
	if fb1.TmdbID > 0 && fb1.TmdbID == fb2.TmdbID {
		similarity += 0.2
	}

	// 限制在 0-1 之间
	if similarity > 1.0 {
		similarity = 1.0
	}

	return similarity
}

// IsSimilar 判断是否相似
func (sc *SimilarityChecker) IsSimilar(fb1, fb2 *Feedback) bool {
	return sc.CheckSimilarity(fb1, fb2) >= sc.threshold
}

// calculateSimilarity 计算两个字符串的相似度（Jaccard相似度）
func (sc *SimilarityChecker) calculateSimilarity(s1, s2 string) float64 {
	// 转换为小写并分词
	words1 := sc.tokenize(s1)
	words2 := sc.tokenize(s2)

	// 计算交集和并集
	intersection := make(map[string]bool)
	union := make(map[string]bool)

	for _, word := range words1 {
		if len(word) > 0 {
			union[word] = true
		}
	}
	for _, word := range words2 {
		if len(word) > 0 {
			union[word] = true
			if union[word] && contains(words1, word) {
				intersection[word] = true
			}
		}
	}

	// Jaccard 相似度
	if len(union) == 0 {
		return 0.0
	}

	return float64(len(intersection)) / float64(len(union))
}

// tokenize 分词（支持中文和英文）
func (sc *SimilarityChecker) tokenize(s string) []string {
	var words []string
	var currentWord strings.Builder

	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			// 中文字符：每个字作为一个词
			if currentWord.Len() > 0 {
				words = append(words, strings.ToLower(currentWord.String()))
				currentWord.Reset()
			}
			words = append(words, string(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			// 英文字符或数字：累加
			currentWord.WriteRune(unicode.ToLower(r))
		} else {
			// 分隔符：保存当前词
			if currentWord.Len() > 0 {
				words = append(words, strings.ToLower(currentWord.String()))
				currentWord.Reset()
			}
		}
	}

	// 保存最后一个词
	if currentWord.Len() > 0 {
		words = append(words, strings.ToLower(currentWord.String()))
	}

	return words
}

// contains 检查切片是否包含元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// FindMostSimilar 找出最相似的反馈
func (sc *SimilarityChecker) FindMostSimilar(target *Feedback, feedbacks []*Feedback) (*Feedback, float64) {
	if len(feedbacks) == 0 {
		return nil, 0.0
	}

	var maxSimilarity float64
	var mostSimilar *Feedback

	for _, fb := range feedbacks {
		sim := sc.CheckSimilarity(target, fb)
		if sim > maxSimilarity {
			maxSimilarity = sim
			mostSimilar = fb
		}
	}

	return mostSimilar, maxSimilarity
}

// FindAllSimilar 找出所有相似的反馈
func (sc *SimilarityChecker) FindAllSimilar(target *Feedback, feedbacks []*Feedback) []*SimilarFeedback {
	var similar []*SimilarFeedback

	for _, fb := range feedbacks {
		sim := sc.CheckSimilarity(target, fb)
		if sim >= sc.threshold {
			similar = append(similar, &SimilarFeedback{
				Feedback:     fb,
				Similarity:  sim,
				MatchedField: sc.getMatchedField(target, fb),
			})
		}
	}

	// 按相似度排序
	for i := 0; i < len(similar)-1; i++ {
		for j := i + 1; j < len(similar); j++ {
			if similar[i].Similarity < similar[j].Similarity {
				similar[i], similar[j] = similar[j], similar[i]
			}
		}
	}

	return similar
}

// SimilarFeedback 相似反馈
type SimilarFeedback struct {
	Feedback     *Feedback
	Similarity  float64
	MatchedField string
}

// getMatchedField 获取匹配的主要字段
func (sc *SimilarityChecker) getMatchedField(fb1, fb2 *Feedback) string {
	titleSim := sc.calculateSimilarity(fb1.Title, fb2.Title)
	descSim := sc.calculateSimilarity(fb1.Description, fb2.Description)

	if titleSim > descSim {
		return "标题"
	} else if descSim > titleSim {
		return "描述"
	}
	return "标题和描述"
}

// SuggestPriority 建议优先级（基于规则）
func (sc *SimilarityChecker) SuggestPriority(fb *Feedback, allFeedbacks []*Feedback) string {
	// 1. 检查是否有重复反馈（相似度 > 0.7）
	duplicateCount := 0
	for _, other := range allFeedbacks {
		if fb.ID != other.ID {
			sim := sc.CheckSimilarity(fb, other)
			if sim > 0.7 {
				duplicateCount++
			}
		}
	}

	// 重复 3 次以上：紧急
	if duplicateCount >= 3 {
		return "urgent"
	}

	// 2. 检查问题类型
	urgentTypes := []string{"video_quality", "audio_quality", "playback"}
	for _, ut := range urgentTypes {
		if fb.IssueType == ut {
			return "high"
		}
	}

	// 3. 检查是否是新用户的第一条反馈
	// 需要从数据库查询，这里简化处理

	// 4. 默认中等
	return "medium"
}

// CalculateEditDistance 计算编辑距离（可选的更精确算法）
func (sc *SimilarityChecker) CalculateEditDistance(s1, s2 string) int {
	r1 := []rune(s1)
	r2 := []rune(s2)

	m := len(r1)
	n := len(r2)

	// 创建 DP 表
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	// 初始化
	for i := 0; i <= m; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}

	// 填充 DP 表
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if r1[i-1] == r2[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(
					dp[i-1][j],   // 删除
					dp[i][j-1],   // 插入
					dp[i-1][j-1], // 替换
				)
			}
		}
	}

	return dp[m][n]
}

// min3 返回三个数中的最小值
func min3(a, b, c int) int {
	min := a
	if b < min {
		min = b
	}
	if c < min {
		min = c
	}
	return min
}

// CalculateCosineSimilarity 计算余弦相似度（可选算法）
func (sc *SimilarityChecker) CalculateCosineSimilarity(s1, s2 string) float64 {
	vec1 := sc.toVector(s1)
	vec2 := sc.toVector(s2)

	// 计算点积
	dot := 0.0
	for word, count1 := range vec1 {
		dot += float64(count1 * vec2[word])
	}

	// 计算模
	var norm1, norm2 float64
	for _, count := range vec1 {
		norm1 += float64(count * count)
	}
	for _, count := range vec2 {
		norm2 += float64(count * count)
	}

	norm1 = math.Sqrt(norm1)
	norm2 = math.Sqrt(norm2)

	if norm1 == 0 || norm2 == 0 {
		return 0.0
	}

	return dot / (norm1 * norm2)
}

// toVector 将字符串转换为词向量
func (sc *SimilarityChecker) toVector(s string) map[string]int {
	vector := make(map[string]int)
	words := sc.tokenize(s)

	for _, word := range words {
		vector[word]++
	}

	return vector
}
