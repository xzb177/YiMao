// Package search provides search orchestration services.
package search

import (
	"fmt"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/internal/session"
)

// Orchestrator coordinates the search flow.
type Orchestrator struct {
	mp            *services.MoviePilotClient
	fallback      *services.SearchFallbackService
	searchHistory *services.SearchHistoryDB
	sessMgr       *session.Manager
}

// NewOrchestrator creates a new search orchestrator.
func NewOrchestrator(
	mp *services.MoviePilotClient,
	fallback *services.SearchFallbackService,
	searchHistory *services.SearchHistoryDB,
	sessMgr *session.Manager,
) *Orchestrator {
	return &Orchestrator{
		mp:            mp,
		fallback:      fallback,
		searchHistory: searchHistory,
		sessMgr:       sessMgr,
	}
}

// SearchResult represents the result of a search operation.
type SearchResult struct {
	Results []services.SearchResult
	Query   string
	FallbackUsed bool
	FallbackQuery string
}

// Search performs a search with fallback support.
func (o *Orchestrator) Search(userID int64, query string) (*SearchResult, error) {
	logger.Info("[Orchestrator] Search query: %s", query)

	// Perform search
	results, err := o.mp.SearchMedia(query, 1)
	if err != nil {
		logger.Info("[Orchestrator] Search failed: %v", err)
		return nil, err
	}

	// Check for empty results
	if results == nil || results.Results == nil {
		logger.Info("[Orchestrator] Search results is nil for query: %s", query)
		return &SearchResult{Results: []services.SearchResult{}, Query: query}, nil
	}

	logger.Info("[Orchestrator] Search results count: %d for query: %s", len(results.Results), query)

	if len(results.Results) == 0 {
		// Try fallback search
		fallbackResults, fallbackQuery, fbErr := o.tryFallback(query)
		if fbErr != nil {
			logger.Info("[Orchestrator] Fallback search failed: %v", fbErr)
			return &SearchResult{Results: []services.SearchResult{}, Query: query}, nil
		}
		if fallbackResults != nil && len(fallbackResults) > 0 {
			logger.Info("[Orchestrator] Fallback hit: query=%s -> fallback=%s, count=%d", query, fallbackQuery, len(fallbackResults))
			return &SearchResult{
				Results:       fallbackResults,
				Query:         query,
				FallbackUsed:  true,
				FallbackQuery: fallbackQuery,
			}, nil
		}
		return &SearchResult{Results: []services.SearchResult{}, Query: query}, nil
	}

	// Add to search history
	if o.searchHistory != nil {
		o.searchHistory.AddSearch(userID, query)
	}

	return &SearchResult{
		Results: results.Results,
		Query:   query,
	}, nil
}

// SaveToSession saves search results to session.
func (o *Orchestrator) SaveToSession(userID int64, results []services.SearchResult, source string) {
	sess := o.sessMgr.GetOrCreate(userID)
	searchItems := make([]session.SearchItem, 0, len(results))

	displayCount := len(results)
	if displayCount > 8 {
		displayCount = 8
	}

	for _, item := range results[:displayCount] {
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}

		searchItems = append(searchItems, session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Rating:   item.Rating,
			Poster:   item.Poster,
			Overview: item.Overview,
		})
	}

	sess.SetSearchResults(searchItems, 1, source)
}

// tryFallback tries multiple fallback search strategies.
func (o *Orchestrator) tryFallback(query string) ([]services.SearchResult, string, error) {
	if o.fallback == nil {
		return nil, "", nil
	}
	return o.fallback.TryFallback(query)
}

// HasResults checks if a search result has any items.
func (o *Orchestrator) HasResults(result *SearchResult) bool {
	return result != nil && len(result.Results) > 0
}

// GetDisplayCount returns the number of results to display (max 8).
func (o *Orchestrator) GetDisplayCount(result *SearchResult) int {
	count := len(result.Results)
	if count > 8 {
		return 8
	}
	return count
}
