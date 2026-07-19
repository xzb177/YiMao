package services

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const transferHistoryTimeLayout = "2006-01-02 15:04:05"

// TransferHistoryItem is the subset of MoviePilot transfer history used by the daily summary.
type TransferHistoryItem struct {
	ID       int          `json:"id"`
	Title    string       `json:"title"`
	Year     FlexibleYear `json:"year"`
	Type     string       `json:"type"`
	Seasons  string       `json:"seasons"`
	Episodes string       `json:"episodes"`
	Date     string       `json:"date"`
	Status   bool         `json:"status"`
}

type TransferSeriesSummary struct {
	Title    string
	Year     int
	Type     string
	Season   int
	Episodes []int
	Files    int
	FirstAt  time.Time
}

func (s TransferSeriesSummary) EpisodeDisplay() string {
	if len(s.Episodes) == 0 {
		return fmt.Sprintf("%d 个文件", s.Files)
	}
	parts := make([]string, 0)
	for start := 0; start < len(s.Episodes); {
		end := start
		for end+1 < len(s.Episodes) && s.Episodes[end+1] == s.Episodes[end]+1 {
			end++
		}
		if end > start {
			parts = append(parts, fmt.Sprintf("E%02d-E%02d", s.Episodes[start], s.Episodes[end]))
		} else {
			parts = append(parts, fmt.Sprintf("E%02d", s.Episodes[start]))
		}
		start = end + 1
	}
	return strings.Join(parts, ",")
}

func (s TransferSeriesSummary) DisplayTitle() string {
	title := s.Title
	if s.Year > 0 {
		title += fmt.Sprintf(" (%d)", s.Year)
	}
	if s.Season >= 0 {
		title += fmt.Sprintf(" S%02d", s.Season)
	}
	return title + "：" + s.EpisodeDisplay()
}

type TransferDailySummary struct {
	Day         time.Time
	Movies      []TransferSeriesSummary
	Series      []TransferSeriesSummary
	MovieCount  int
	SeriesCount int
	FileCount   int
	FirstAt     time.Time
	LastAt      time.Time
}

func parseTransferNumber(value string, prefix byte) int {
	value = strings.TrimSpace(strings.ToUpper(value))
	if len(value) < 2 || value[0] != prefix {
		return -1
	}
	n, err := strconv.Atoi(value[1:])
	if err != nil {
		return -1
	}
	return n
}

// SummarizeTransferHistory aggregates successful records from exactly one natural day.
// FileCount intentionally counts every successful transfer record; episode display is deduplicated.
func SummarizeTransferHistory(rows []TransferHistoryItem, day time.Time) TransferDailySummary {
	loc := day.Location()
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	type key struct {
		title  string
		year   int
		typeID string
		season int
	}
	groups := make(map[key]*TransferSeriesSummary)
	result := TransferDailySummary{Day: start}

	for _, row := range rows {
		if !row.Status || strings.TrimSpace(row.Title) == "" {
			continue
		}
		at, err := time.ParseInLocation(transferHistoryTimeLayout, row.Date, loc)
		if err != nil || at.Before(start) || !at.Before(end) {
			continue
		}
		typeID := strings.TrimSpace(row.Type)
		season := parseTransferNumber(row.Seasons, 'S')
		if isTransferMovie(typeID) {
			season = -1
		}
		k := key{title: strings.TrimSpace(row.Title), year: row.Year.Int(), typeID: typeID, season: season}
		group := groups[k]
		if group == nil {
			group = &TransferSeriesSummary{Title: k.title, Year: k.year, Type: typeID, Season: season, FirstAt: at}
			groups[k] = group
		}
		group.Files++
		if at.Before(group.FirstAt) {
			group.FirstAt = at
		}
		if episode := parseTransferNumber(row.Episodes, 'E'); episode >= 0 {
			group.Episodes = append(group.Episodes, episode)
		}
		result.FileCount++
		if result.FirstAt.IsZero() || at.Before(result.FirstAt) {
			result.FirstAt = at
		}
		if result.LastAt.IsZero() || at.After(result.LastAt) {
			result.LastAt = at
		}
	}

	for _, group := range groups {
		group.Episodes = uniqueSortedInts(group.Episodes)
		if isTransferMovie(group.Type) {
			result.Movies = append(result.Movies, *group)
		} else {
			result.Series = append(result.Series, *group)
		}
	}
	sortTransferSummaries(result.Movies)
	sortTransferSummaries(result.Series)
	result.MovieCount = len(result.Movies)
	result.SeriesCount = len(result.Series)
	return result
}

func isTransferMovie(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return mediaType == "电影" || mediaType == "movie"
}

func sortTransferSummaries(items []TransferSeriesSummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].FirstAt.Equal(items[j].FirstAt) {
			return items[i].Title < items[j].Title
		}
		return items[i].FirstAt.Before(items[j].FirstAt)
	})
}
