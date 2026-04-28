package services

import (
	"emby-telegram-bot/pkg/logger"
	"strconv"
	"strings"
)

// SearchFallbackService provides fallback search strategies
type SearchFallbackService struct {
	moviepilot *MoviePilotClient
}

// NewSearchFallbackService creates a new fallback service
func NewSearchFallbackService(moviepilot *MoviePilotClient) *SearchFallbackService {
	return &SearchFallbackService{
		moviepilot: moviepilot,
	}
}

// TryFallback attempts to find results using fallback strategies
// Returns (results, actualQueryUsed, error)
func (s *SearchFallbackService) TryFallback(query string) ([]SearchResult, string, error) {
	candidates := BuildFallbackQueries(query)
	logger.Info("[SearchFallback] Trying fallback for query='%s', candidates=%d", query, len(candidates))
	for _, q := range candidates {
		if q == "" || q == query {
			continue
		}
		logger.Info("[SearchFallback] Trying fallback query: '%s'", q)
		results, err := s.moviepilot.SearchMedia(q, 1)
		if err != nil || results == nil {
			logger.Info("[SearchFallback] Query '%s' failed: %v", q, err)
			continue
		}
		if len(results.Results) > 0 {
			logger.Info("[SearchFallback] Fallback hit: query='%s' -> fallback='%s', count=%d", query, q, len(results.Results))
			return results.Results, q, nil
		}
		logger.Info("[SearchFallback] Query '%s' returned no results", q)
	}
	logger.Info("[SearchFallback] No fallback worked for query='%s'", query)
	return nil, "", nil
}

// BuildFallbackQueries generates alternative query strings for fallback search
func BuildFallbackQueries(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	seen := map[string]bool{q: true}
	add := func(list *[]string, s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		*list = append(*list, s)
	}

	var out []string

	// 1) remove common suffix words in Chinese titles
	suffixes := []string{"电影", "电视剧", "剧", "动画", "动漫", "第1季", "第一季", "第2季", "第二季", "国语", "中字", "完整版"}
	trimmed := q
	for _, s := range suffixes {
		trimmed = strings.ReplaceAll(trimmed, s, "")
	}
	trimmed = strings.TrimSpace(trimmed)
	add(&out, trimmed)

	// 2) keep only Chinese chars and digits to reduce noise
	onlyCore := ExtractCoreKeyword(q)
	add(&out, onlyCore)

	// 3) if contains year info, split to title-only
	for _, r := range []string{"（", "("} {
		if idx := strings.Index(q, r); idx > 0 {
			add(&out, strings.TrimSpace(q[:idx]))
		}
	}

	// 4) fallback to year-only search when title contains 4-digit year
	if y := ExtractYear(q); y != "" {
		add(&out, y)
	}

	return out
}

// ExtractCoreKeyword keeps only Chinese characters, digits, and letters
func ExtractCoreKeyword(s string) string {
	runes := []rune(strings.TrimSpace(s))
	keep := make([]rune, 0, len(runes))
	for _, r := range runes {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0x4e00 && r <= 0x9fff) {
			keep = append(keep, r)
		}
	}
	return strings.TrimSpace(string(keep))
}

// ExtractYear extracts a 4-digit year from a string
func ExtractYear(s string) string {
	runes := []rune(s)
	for i := 0; i+3 < len(runes); i++ {
		chunk := string(runes[i : i+4])
		y, err := strconv.Atoi(chunk)
		if err == nil && y >= 1900 && y <= 2099 {
			return chunk
		}
	}
	return ""
}
