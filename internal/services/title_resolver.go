package services

import (
	"fmt"
	"regexp"
	"strings"
)

// TitleResolver resolves display titles from media metadata
type TitleResolver struct{}

// NewTitleResolver creates a new title resolver
func NewTitleResolver() *TitleResolver {
	return &TitleResolver{}
}

// 预编译正则（避免每次调用重新编译）
var (
	// 技术标签清理
	reResolution = regexp.MustCompile(`\.(?:2160p|1080p|720p|480p|4K)\b`)
	reCodec      = regexp.MustCompile(`\.(?:x265|x264|h264|h265|hevc|avc)\b`)
	reBitDepth   = regexp.MustCompile(`\.(?:10bit|8bit)\b`)
	reSource     = regexp.MustCompile(`\.(?:BluRay|WEBDL|WEB-DL|WEBRip|DVDRip|DVDR|HDTV|PDTV|SDR)\b`)
	reRemux      = regexp.MustCompile(`\.(?:Remux|REMUX)\b`)
	rePlatform   = regexp.MustCompile(`\.(?:AMZN|NF|Disney|Hulu|HBO|Max)\b`)
	reAudio      = regexp.MustCompile(`\.(?:DTS|DDP|Atmos|TrueHD|AAC|MP3|FLAC|Opus)\b`)
	reCodecProf  = regexp.MustCompile(`\.(?:Hi10P|Hi10)\b`)
	reRelGroup   = regexp.MustCompile(`-[\w\.\-]+$`)
	reCnGroup    = regexp.MustCompile(`-\s*[^\s]+\s*$`)
	reBracket    = regexp.MustCompile(`\[.*?\]`)
	reCnBracket  = regexp.MustCompile(`【.*?】`)
	reParen      = regexp.MustCompile(`\(.*?\)`)
	reDot        = regexp.MustCompile(`\.`)
	reUnderscore = regexp.MustCompile(`_`)
	reSpaces     = regexp.MustCompile(`\s+`)
	reYear       = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
)

// ResolveMovieTitle resolves the best possible display title for a movie
// Following priority:
// 1. title field
// 2. name field
// 3. original_title field
// 4. nested media/title field
// 5. Year fallback
// 6. Filename extraction
// 7. Final fallback
func (r *TitleResolver) ResolveMovieTitle(item *MediaItem, filename string) string {
	// Priority 1: Title field
	if item.Title != "" && item.Title != "[未命名电影]" {
		return item.Title
	}

	// Priority 2: source-specific alternative fields are normalized before MediaItem reaches this resolver.
	// Fall back to robust filename extraction here.

	// Priority 3: Try filename extraction
	if filename != "" {
		if extractedTitle := r.extractFromFilename(filename); extractedTitle != "" {
			return extractedTitle
		}
	}

	// Priority 4: Year fallback
	if item.Year > 1900 && item.Year < 2100 {
		return r.formatYearTitle(item.Year)
	}

	// Priority 5: Final fallback
	return "未识别电影"
}

// extractFromFilename extracts a clean title from a filename
// Examples:
//
//	"Oppenheimer.2023.1080p.BluRay.x265.mkv" -> "Oppenheimer (2023)"
//	"Dune.Part.Two.2024.2160p.WEB-DL.HEVC.mkv" -> "Dune Part Two (2024)"
//	"流浪地球2.2023.4K.mkv" -> "流浪地球2 (2023)"
func (r *TitleResolver) extractFromFilename(filename string) string {
	// Get base filename (remove path and extension)
	base := filename
	if idx := strings.LastIndex(base, "/"); idx != -1 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx != -1 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx != -1 {
		base = base[:idx]
	}

	// Remove common technical tags/patterns（使用预编译正则）
	cleaned := reResolution.ReplaceAllString(base, "")
	cleaned = reCodec.ReplaceAllString(cleaned, "")
	cleaned = reBitDepth.ReplaceAllString(cleaned, "")
	cleaned = reSource.ReplaceAllString(cleaned, "")
	cleaned = reRemux.ReplaceAllString(cleaned, "")
	cleaned = rePlatform.ReplaceAllString(cleaned, "")
	cleaned = reAudio.ReplaceAllString(cleaned, "")
	cleaned = reCodecProf.ReplaceAllString(cleaned, "")
	cleaned = reRelGroup.ReplaceAllString(cleaned, "")
	cleaned = reCnGroup.ReplaceAllString(cleaned, "")
	cleaned = reBracket.ReplaceAllString(cleaned, "")
	cleaned = reCnBracket.ReplaceAllString(cleaned, "")
	cleaned = reParen.ReplaceAllString(cleaned, " ")
	cleaned = reDot.ReplaceAllString(cleaned, " ")
	cleaned = reUnderscore.ReplaceAllString(cleaned, " ")
	cleaned = reSpaces.ReplaceAllString(cleaned, " ")

	cleaned = strings.TrimSpace(cleaned)

	// Extract year if present (support 1900-2099)
	yearMatch := reYear.FindStringSubmatch(cleaned)
	year := ""
	if len(yearMatch) > 1 {
		year = yearMatch[1]
		cleaned = reYear.ReplaceAllString(cleaned, "")
		cleaned = strings.TrimSpace(cleaned)
	}

	cleaned = strings.TrimSpace(cleaned)
	if cleaned != "" {
		if year != "" {
			return r.formatTitleWithYear(cleaned, year)
		}
		return cleaned
	}

	return ""
}

// formatTitleWithYear formats a title with year
func (r *TitleResolver) formatTitleWithYear(title, year string) string {
	// Check if title already contains year in parentheses
	if strings.Contains(title, "("+year+")") || strings.Contains(title, " ("+year) {
		return title
	}
	return title + " (" + year + ")"
}

// formatYearTitle creates a title from just the year
func (r *TitleResolver) formatYearTitle(year int) string {
	return "[" + r.formatYearInt(year) + "年电影]"
}

// formatYearInt formats a year as integer
func (r *TitleResolver) formatYearInt(year int) string {
	if year > 1900 && year < 2000 {
		return string(rune('0'+(year-1900)/100)) + string(rune('0'+(year-1900)%100))
	}
	if year >= 2000 && year < 2100 {
		return "20" + string(rune('0'+(year-2000)/10)) + string(rune('0'+(year-2000)%10))
	}
	return "????"
}

// SeriesAggregationKey creates a unique key for aggregating series episodes
type SeriesAggregationKey struct {
	SeriesName   string
	SeasonNumber int
}

// String returns a string representation of the key
func (k SeriesAggregationKey) String() string {
	return k.SeriesName + "_S" + string(rune('0'+k.SeasonNumber))
}

// AggregatedSeries represents aggregated episodes for a series season
type AggregatedSeries struct {
	SeriesName   string
	SeasonNumber int
	Episodes     []int // Sorted list of episode numbers
	MinEpisode   int
	MaxEpisode   int
	Count        int
	IsComplete   bool
	LibraryName  string
	Year         int
}

// NewAggregatedSeries creates a new aggregated series from items
func NewAggregatedSeries(items []*MediaItem) *AggregatedSeries {
	if len(items) == 0 {
		return nil
	}

	// Use first item as base
	base := items[0]
	agg := &AggregatedSeries{
		SeriesName:   base.SeriesName,
		SeasonNumber: base.SeasonNumber,
		LibraryName:  base.LibraryName,
		Year:         base.Year,
		Episodes:     make([]int, 0, len(items)),
	}

	// Collect all episodes
	for _, item := range items {
		if item.EpisodeStart > 0 {
			agg.Episodes = append(agg.Episodes, item.EpisodeStart)
		}
		if item.EpisodeEnd > 0 && item.EpisodeEnd != item.EpisodeStart {
			// Add range
			for ep := item.EpisodeStart + 1; ep <= item.EpisodeEnd; ep++ {
				agg.Episodes = append(agg.Episodes, ep)
			}
		}
	}

	// Deduplicate and sort episodes
	agg.Episodes = uniqueSortedInts(agg.Episodes)

	// Set min, max, count
	if len(agg.Episodes) > 0 {
		agg.MinEpisode = agg.Episodes[0]
		agg.MaxEpisode = agg.Episodes[len(agg.Episodes)-1]
	}
	agg.Count = len(agg.Episodes)

	// Check if complete (if any item was marked complete)
	for _, item := range items {
		if item.IsCompleted {
			agg.IsComplete = true
			break
		}
	}

	return agg
}

// FormatForSummary formats the aggregated series for daily summary display
func (a *AggregatedSeries) FormatForSummary() string {
	title := a.SeriesName

	// Add season
	if a.SeasonNumber > 0 {
		title += fmt.Sprintf(" 第%d季", a.SeasonNumber)
	}

	// Add episode info
	if a.Count == 1 {
		title += fmt.Sprintf("：EP%02d", a.MinEpisode)
	} else if a.IsConsecutive() {
		// Continuous range - show as range
		title += fmt.Sprintf("：EP%02d-EP%02d", a.MinEpisode, a.MaxEpisode)
	} else {
		// Non-continuous - show max
		title += fmt.Sprintf("：更新至 EP%02d", a.MaxEpisode)
	}

	// Add completion marker
	if a.IsComplete {
		title += "（完结）"
	}

	return title
}

// IsConsecutive checks if episodes form a continuous sequence
func (a *AggregatedSeries) IsConsecutive() bool {
	if len(a.Episodes) <= 1 {
		return true
	}

	for i := 1; i < len(a.Episodes); i++ {
		if a.Episodes[i] != a.Episodes[i-1]+1 {
			return false
		}
	}
	return true
}

// uniqueSortedInts returns sorted unique integers from a slice
func uniqueSortedInts(nums []int) []int {
	seen := make(map[int]bool)
	result := make([]int, 0, len(nums))

	for _, num := range nums {
		if !seen[num] {
			seen[num] = true
			result = append(result, num)
		}
	}

	// Simple insertion sort (small slices)
	for i := 1; i < len(result); i++ {
		key := result[i]
		j := i - 1
		for j >= 0 && result[j] > key {
			result[j+1] = result[j]
			j--
		}
		result[j+1] = key
	}

	return result
}

// MovieAggregationKey creates a unique key for aggregating movies
type MovieAggregationKey struct {
	Title string
	Year  int
}

// String returns a string representation of the key
func (k MovieAggregationKey) String() string {
	if k.Year > 0 {
		return k.Title + "_" + string(rune('0'+k.Year/1000)) + string(rune('0'+(k.Year%1000)/100))
	}
	return k.Title
}

// AggregatedMovie represents an aggregated movie
type AggregatedMovie struct {
	Title       string
	Year        int
	LibraryName string
	Count       int // Number of records (should be 1 after aggregation)
}

// FormatForSummary formats the aggregated movie for display
func (a *AggregatedMovie) FormatForSummary() string {
	if a.Year > 1900 && a.Year < 2100 {
		return fmt.Sprintf("%s (%d)", a.Title, a.Year)
	}
	return a.Title
}
