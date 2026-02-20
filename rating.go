package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// RatingSystem handles user ratings for media
type RatingSystem struct {
	jellyseerrURL string
	apiKey        string
	httpClient    *http.Client

	// User ratings: userID -> tmdbID -> rating (exported for bridge access)
	UserRatings map[int64]map[int64]float64
	ratingMutex  sync.RWMutex

	// Average ratings: tmdbID -> {sum, count}
	AvgRatings  map[int64]*AvgRating
	avgMutex     sync.RWMutex

	storageFile string
}

type AvgRating struct {
	TmdbID     int64   `json:"tmdbId"`
	Sum        float64 `json:"sum"`
	Count      int     `json:"count"`
	Average    float64 `json:"average"`
	LastUpdate time.Time `json:"lastUpdate"`
}

var ratingSys *RatingSystem

// InitRatingSystem initializes the rating system
func InitRatingSystem() {
	ratingSys = &RatingSystem{
		jellyseerrURL: os.Getenv("JELLYSEERR_URL"),
		apiKey:        os.Getenv("JELLYSEERR_API_KEY"),
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		UserRatings:   make(map[int64]map[int64]float64),
		AvgRatings:    make(map[int64]*AvgRating),
		storageFile:   "user_ratings.json",
	}

	ratingSys.load()

	log.Println("RatingSystem initialized")
}

// RateMedia rates a media item
func (r *RatingSystem) RateMedia(userID int64, tmdbID int64, rating float64, mediaTitle string) error {
	if rating < 1 || rating > 10 {
		return fmt.Errorf("评分必须在1-10之间")
	}

	r.ratingMutex.Lock()
	defer r.ratingMutex.Unlock()

	// Initialize user ratings map if needed
	if r.UserRatings[userID] == nil {
		r.UserRatings[userID] = make(map[int64]float64)
	}

	// Store user rating (overwrite existing)
	oldRating, existed := r.UserRatings[userID][tmdbID]
	r.UserRatings[userID][tmdbID] = rating

	// Update average rating
	r.updateAvgRating(tmdbID, rating, oldRating, existed)

	r.save()

	log.Printf("RatingSystem: User %d rated %d (%s) as %.1f", userID, tmdbID, mediaTitle, rating)

	return nil
}

// GetUserRating gets user's rating for a media
func (r *RatingSystem) GetUserRating(userID int64, tmdbID int64) float64 {
	r.ratingMutex.RLock()
	defer r.ratingMutex.RUnlock()

	if userRatings, ok := r.UserRatings[userID]; ok {
		if rating, exists := userRatings[tmdbID]; exists {
			return rating
		}
	}
	return 0
}

// GetAverageRating gets average rating for a media
func (r *RatingSystem) GetAverageRating(tmdbID int64) (float64, int) {
	r.avgMutex.RLock()
	defer r.avgMutex.RUnlock()

	if avg, ok := r.AvgRatings[tmdbID]; ok {
		return avg.Average, avg.Count
	}
	return 0, 0
}

// updateAvgRating updates the average rating for a media
func (r *RatingSystem) updateAvgRating(tmdbID int64, newRating float64, oldRating float64, existed bool) {
	r.avgMutex.Lock()
	defer r.avgMutex.Unlock()

	avg, ok := r.AvgRatings[tmdbID]
	if !ok {
		avg = &AvgRating{
			TmdbID:     tmdbID,
			Sum:        newRating,
			Count:      1,
			Average:    newRating,
			LastUpdate: time.Now(),
		}
		r.AvgRatings[tmdbID] = avg
		return
	}

	if existed {
		// Replace old rating with new one
		avg.Sum = avg.Sum - oldRating + newRating
	} else {
		avg.Sum += newRating
		avg.Count++
	}
	avg.Average = avg.Sum / float64(avg.Count)
	avg.LastUpdate = time.Now()
}

// save saves rating data
func (r *RatingSystem) save() {
	// Save user ratings
	data, _ := json.MarshalIndent(r.UserRatings, "", "  ")
	os.WriteFile(r.storageFile, data, 0644)

	// Save average ratings
	avgData, _ := json.MarshalIndent(r.AvgRatings, "", "  ")
	os.WriteFile("average_ratings.json", avgData, 0644)
}

// load loads rating data from file
func (r *RatingSystem) load() {
	// Load user ratings
	data, err := os.ReadFile(r.storageFile)
	if err == nil {
		json.Unmarshal(data, &r.UserRatings)
		log.Printf("RatingSystem: Loaded %d user ratings", len(r.UserRatings))
	}

	// Load average ratings
	avgData, err := os.ReadFile("average_ratings.json")
	if err == nil {
		json.Unmarshal(avgData, &r.AvgRatings)
		log.Printf("RatingSystem: Loaded %d average ratings", len(r.AvgRatings))
	}
}

// GetTopRatedMedia returns top rated media
func (r *RatingSystem) GetTopRatedMedia(limit int) []AvgRating {
	r.avgMutex.RLock()
	defer r.avgMutex.RUnlock()

	// Convert map to slice
	ratings := make([]AvgRating, 0, len(r.AvgRatings))
	for _, avg := range r.AvgRatings {
		ratings = append(ratings, *avg)
	}

	// Sort by average (descending)
	for i := 0; i < len(ratings)-1; i++ {
		for j := i + 1; j < len(ratings); j++ {
			if ratings[j].Average > ratings[i].Average {
				ratings[i], ratings[j] = ratings[j], ratings[i]
			}
		}
	}

	if len(ratings) > limit {
		ratings = ratings[:limit]
	}

	return ratings
}

// RLock provides read lock access to userRatings
func (r *RatingSystem) RLock() {
	r.ratingMutex.RLock()
}

// RUnlock provides read unlock access to userRatings
func (r *RatingSystem) RUnlock() {
	r.ratingMutex.RUnlock()
}

// FormatRating displays rating with stars
func FormatRating(rating float64) string {
	stars := "☆"
	fullStars := int(rating)
	for i := 0; i < 5; i++ {
		if i < fullStars {
			stars += "★"
		} else {
			stars += "☆"
		}
	}
	return fmt.Sprintf("%.1f %s", rating, stars)
}

// GetRatingStats returns rating statistics
func (r *RatingSystem) GetRatingStats(tmdbID int64) (avg float64, count int, userRating float64) {
	avg, count = r.GetAverageRating(tmdbID)
	// userRating would need to be passed in
	return
}
