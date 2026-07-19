package services

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultRequestHeatWindow = 7 * 24 * time.Hour

// RequestHeatItem is an anonymous aggregate of real request and carpool activity.
type RequestHeatItem struct {
	TMDBID    int
	MediaType string
	Title     string
	Year      int
	Count     int
	LastAt    time.Time
}

// RequestHeatService builds discovery data from real YiMao requests and carpool joins.
type RequestHeatService struct {
	reviews *ReviewService
	carpool *CarpoolService
	now     func() time.Time
}

func NewRequestHeatService(reviews *ReviewService, carpool *CarpoolService) *RequestHeatService {
	return &RequestHeatService{reviews: reviews, carpool: carpool, now: time.Now}
}

type requestHeatAggregate struct {
	item  RequestHeatItem
	users map[int64]struct{}
}

// Recent returns active titles requested during the window, ranked by unique users.
// Carpool has no timestamps, so it only augments titles anchored by a recent request.
func (s *RequestHeatService) Recent(window time.Duration, limit int) []RequestHeatItem {
	if s == nil || s.reviews == nil || limit <= 0 {
		return nil
	}
	if window <= 0 {
		window = defaultRequestHeatWindow
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	cutoff := now.Add(-window)
	aggregates := make(map[string]*requestHeatAggregate)

	for _, review := range s.reviews.GetAllRequests() {
		if !requestHeatEligible(review, cutoff) {
			continue
		}
		mediaType := normalizeRequestHeatMediaType(string(review.MediaType))
		if mediaType == "" {
			continue
		}
		key := mediaType + ":" + strconv.Itoa(review.TmdbID)
		agg := aggregates[key]
		if agg == nil {
			agg = &requestHeatAggregate{
				item:  RequestHeatItem{TMDBID: review.TmdbID, MediaType: mediaType},
				users: make(map[int64]struct{}),
			}
			aggregates[key] = agg
		}
		if review.TelegramID != 0 {
			agg.users[review.TelegramID] = struct{}{}
		}
		if review.CreatedAt.After(agg.item.LastAt) {
			agg.item.LastAt = review.CreatedAt
			agg.item.Title = strings.TrimSpace(review.MediaTitle)
			agg.item.Year = review.MediaYear
		}
	}

	items := make([]RequestHeatItem, 0, len(aggregates))
	for _, agg := range aggregates {
		if s.carpool != nil {
			for _, userID := range s.carpool.Get(agg.item.TMDBID, agg.item.MediaType) {
				if userID != 0 {
					agg.users[userID] = struct{}{}
				}
			}
		}
		agg.item.Count = len(agg.users)
		if agg.item.Count > 0 && agg.item.Title != "" {
			items = append(items, agg.item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		if !items[i].LastAt.Equal(items[j].LastAt) {
			return items[i].LastAt.After(items[j].LastAt)
		}
		if items[i].MediaType != items[j].MediaType {
			return items[i].MediaType < items[j].MediaType
		}
		return items[i].TMDBID < items[j].TMDBID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func requestHeatEligible(review *ReviewRequest, cutoff time.Time) bool {
	if review == nil || review.TmdbID <= 0 || review.CreatedAt.IsZero() || review.CreatedAt.Before(cutoff) {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(review.Status))
	if status == "rejected" || status == "cancelled" {
		return false
	}
	if status != "pending" && status != "approved" && !review.Stuck {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(review.SubscriptionState)) {
	case StateCompleted, StateFailed, StateCancelled:
		return false
	}
	return true
}

func normalizeRequestHeatMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "movie", "电影":
		return "movie"
	case "tv", "series", "电视剧", "剧集":
		return "tv"
	default:
		return ""
	}
}
