package ai

import (
	"fmt"
	"strings"
	"unicode"
)

// QNAMatcher handles similarity matching for questions
type QNAMatcher struct {
	store *Store
}

// NewQNAMatcher creates a new Q&A matcher
func NewQNAMatcher(store *Store) *QNAMatcher {
	return &QNAMatcher{store: store}
}

// FindBestMatch finds the best matching Q&A pair for a question
func (m *QNAMatcher) FindBestMatch(question string) (*QAMatchResult, error) {
	normalized := normalizeText(question)
	keywords := extractKeywordMap(question)

	// Strategy 1: Exact normalized match (highest priority)
	if exact, err := m.store.FindQAByNormalized(normalized); err == nil && exact != nil {
		return &QAMatchResult{
			QAPair:     exact,
			Similarity: 1.0,
			MatchType:  "exact",
		}, nil
	}

	// Strategy 2: Keyword overlap matching
	if len(keywords) > 0 {
		keywordMatches, err := m.store.FindQAByKeywords(keywordList(keywords))
		if err == nil && len(keywordMatches) > 0 {
			best := m.rankByKeywordOverlap(question, keywords, keywordMatches)
			if best.Similarity >= 0.6 {
				return best, nil
			}
		}
	}

	// Strategy 3: Fuzzy text similarity for recent/popular Q&A
	fuzzyMatches, err := m.store.GetRecentQAPairs(50)
	if err == nil && len(fuzzyMatches) > 0 {
		best := m.rankByTextSimilarity(question, fuzzyMatches)
		if best.Similarity >= 0.5 {
			return best, nil
		}
	}

	return nil, nil
}

// rankByKeywordOverlap ranks candidates by keyword overlap
func (m *QNAMatcher) rankByKeywordOverlap(question string, questionKeywords map[string]bool, candidates []*QAPair) *QAMatchResult {
	var best *QAMatchResult

	for _, pair := range candidates {
		pairKeywords, _ := m.store.GetQAKeywords(pair.ID)

		overlap := 0
		for _, kw := range pairKeywords {
			if questionKeywords[kw] {
				overlap++
			}
		}

		// Jaccard similarity
		totalKeywords := len(questionKeywords) + len(pairKeywords)
		if totalKeywords == 0 {
			continue
		}

		similarity := float64(overlap*2) / float64(totalKeywords)

		// Boost for admin answers and high usage
		if pair.IsAdminAnswer {
			similarity *= 1.2
		}
		if pair.UsageCount > 10 {
			similarity *= 1.1
		}
		if pair.SuccessCount > pair.FailCount*2 {
			similarity *= 1.15
		}

		// Cap at 1.0
		if similarity > 1.0 {
			similarity = 1.0
		}

		if best == nil || similarity > best.Similarity {
			best = &QAMatchResult{
				QAPair:     pair,
				Similarity: similarity,
				MatchType:  "keyword",
			}
		}
	}

	return best
}

// rankByTextSimilarity ranks candidates by text similarity (Jaro-Winkler)
func (m *QNAMatcher) rankByTextSimilarity(question string, candidates []*QAPair) *QAMatchResult {
	var best *QAMatchResult

	questionNorm := normalizeText(question)

	for _, pair := range candidates {
		pairNorm := normalizeText(pair.Question)
		similarity := jaroWinklerSimilarity(questionNorm, pairNorm)

		// Boost for exact substring match
		if strings.Contains(questionNorm, pairNorm) || strings.Contains(pairNorm, questionNorm) {
			similarity *= 1.3
		}

		// Boost for admin answers
		if pair.IsAdminAnswer {
			similarity *= 1.1
		}

		// Boost for high success rate
		if pair.UsageCount > 5 && pair.SuccessCount > pair.FailCount {
			similarity *= 1.1
		}

		// Cap at 1.0
		if similarity > 1.0 {
			similarity = 1.0
		}

		if best == nil || similarity > best.Similarity {
			best = &QAMatchResult{
				QAPair:     pair,
				Similarity: similarity,
				MatchType:  "fuzzy",
			}
		}
	}

	return best
}

// normalizeText normalizes text for comparison
func normalizeText(text string) string {
	// Convert to lowercase
	text = strings.ToLower(text)

	// Remove punctuation
	text = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, text)

	// Normalize whitespace
	return strings.Join(strings.Fields(text), " ")
}

// extractKeywordMap extracts keywords from text
func extractKeywordMap(text string) map[string]bool {
	words := strings.Fields(normalizeText(text))
	keywords := make(map[string]bool)

	for _, word := range words {
		word = strings.Trim(word, "。，、；：？！\"'（）【】")
		if len(word) >= 2 && !stopWords[word] {
			keywords[word] = true

			// Also add 2-char bigrams for Chinese
			runes := []rune(word)
			for i := 0; i < len(runes)-1; i++ {
				bigram := string(runes[i : i+2])
				if !stopWords[bigram] {
					keywords[bigram] = true
				}
			}
		}
	}

	return keywords
}

// keywordList converts keyword map to slice
func keywordList(keywords map[string]bool) []string {
	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}
	return result
}

// jaroWinklerSimilarity calculates Jaro-Winkler string similarity
func jaroWinklerSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	len1, len2 := len(s1), len(s2)

	// Empty string cases
	if len1 == 0 || len2 == 0 {
		return 0.0
	}

	// Match distance
	matchDistance := max(len1, len2)/2 - 1
	if matchDistance < 0 {
		matchDistance = 0
	}

	// Find matches
	s1Matches := make([]bool, len1)
	s2Matches := make([]bool, len2)

	matches := 0
	transpositions := 0

	for i := 0; i < len1; i++ {
		start := max(0, i-matchDistance)
		end := min(len2, i+matchDistance+1)

		for j := start; j < end; j++ {
			if s2Matches[j] || s1[i] != s2[j] {
				continue
			}
			s1Matches[i] = true
			s2Matches[j] = true
			matches++
			break
		}
	}

	if matches == 0 {
		return 0.0
	}

	// Count transpositions
	j := 0
	for i := 0; i < len1; i++ {
		if !s1Matches[i] {
			continue
		}
		for !s2Matches[j] {
			j++
		}
		if s1[i] != s2[j] {
			transpositions++
		}
		j++
	}

	// Jaro similarity
	jaro := float64(matches) / float64(len1)
	jaro += float64(matches) / float64(len2)
	jaro += float64(matches-(transpositions/2)) / float64(matches)
	jaro /= 3.0

	// Winkler modification (prefix bonus)
	prefix := 0
	for i := 0; i < min(min(len1, len2), 4); i++ {
		if s1[i] == s2[i] {
			prefix++
		} else {
			break
		}
	}

	jaroWinkler := jaro + float64(prefix)*0.1*(1.0-jaro)

	return jaroWinkler
}

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CalculateSimilarity calculates similarity between two strings (exported utility)
func CalculateSimilarity(s1, s2 string) float64 {
	return jaroWinklerSimilarity(normalizeText(s1), normalizeText(s2))
}

// FormatQAResponse formats a Q&A response with confidence indication
func FormatQAResponse(qa *QAPair, similarity float64, includeMeta bool) string {
	response := qa.Answer

	if includeMeta {
		if similarity >= 0.9 {
			response = "💡 " + response
		} else if similarity >= 0.7 {
			response = "💡 " + response
		} else {
			response = "💡 " + response + "\n\n(参考答案，可能不完全匹配你的问题)"
		}
	}

	return response
}

// ShouldUseQAMatch determines if a Q&A match should be used based on confidence
func ShouldUseQAMatch(result *QAMatchResult, threshold float64) bool {
	if result == nil || result.QAPair == nil {
		return false
	}

	// Always use exact or high-confidence matches
	if result.MatchType == "exact" || result.Similarity >= 0.9 {
		return true
	}

	// Use keyword matches above threshold
	if result.MatchType == "keyword" && result.Similarity >= threshold {
		return true
	}

	// Only use fuzzy matches if they're very confident
	if result.MatchType == "fuzzy" && result.Similarity >= threshold+0.1 {
		return true
	}

	return false
}

// GetMatchReason returns a human-readable reason for the match
func GetMatchReason(result *QAMatchResult) string {
	if result == nil {
		return ""
	}

	switch result.MatchType {
	case "exact":
		return "完全匹配"
	case "keyword":
		return fmt.Sprintf("关键词匹配 (%.0f%%)", result.Similarity*100)
	case "fuzzy":
		return fmt.Sprintf("相似问题 (%.0f%%)", result.Similarity*100)
	default:
		return ""
	}
}
