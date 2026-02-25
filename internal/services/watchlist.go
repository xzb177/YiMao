package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// WatchlistItem represents an item in a user's watchlist
type WatchlistItem struct {
	TmdbID      int       `json:"tmdb_id"`
	Title       string    `json:"title"`
	Year        int       `json:"year"`
	MediaType   string    `json:"media_type"`   // movie, tv
	Poster      string    `json:"poster,omitempty"`
	Overview    string    `json:"overview,omitempty"`
	Rating      float64   `json:"rating,omitempty"`
	AddedAt     time.Time `json:"added_at"`
	Notes       string    `json:"notes,omitempty"`       // User's personal notes
	Tags        []string  `json:"tags,omitempty"`        // Custom tags for organization
}

// WatchlistCollection represents a named collection of watchlist items
type WatchlistCollection struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	Items       map[int]*WatchlistItem // tmdb_id -> item
}

// WatchlistService manages user watchlists
type WatchlistService struct {
	watchlistsFile string
	// telegramID -> user's watchlist (main watchlist)
	watchlists    map[int64]map[int]*WatchlistItem
	// telegramID -> named collections
	collections   map[int64][]*WatchlistCollection
	mu            sync.RWMutex
}

// NewWatchlistService creates a new watchlist service
func NewWatchlistService(dataDir string) *WatchlistService {
	watchlistsFile := fmt.Sprintf("%s/watchlists.json", dataDir)

	service := &WatchlistService{
		watchlistsFile: watchlistsFile,
		watchlists:    make(map[int64]map[int]*WatchlistItem),
		collections:   make(map[int64][]*WatchlistCollection),
	}

	service.load()

	return service
}

// load loads watchlists from file
func (s *WatchlistService) load() error {
	data, err := os.ReadFile(s.watchlistsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Create empty file
			return s.save()
		}
		return err
	}

	var fileData struct {
		Watchlists  map[string]map[int]*WatchlistItem       `json:"watchlists"`
		Collections map[string][]*WatchlistCollection `json:"collections"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		// Try legacy format
		var legacyData map[string]map[int]*WatchlistItem
		if err := json.Unmarshal(data, &legacyData); err == nil {
			s.watchlists = convertStringKeyToInt(legacyData)
		}
	} else {
		s.watchlists = convertStringKeyToInt(fileData.Watchlists)
		s.collections = convertStringKeyCollectionsToInt(fileData.Collections)
	}

	log.Printf("[Watchlist] Loaded %d user watchlists and %d collections", len(s.watchlists), countCollections(s.collections))

	return nil
}

// save saves watchlists to file
func (s *WatchlistService) save() error {
	data := map[string]interface{}{
		"watchlists":  convertIntKeyToString(s.watchlists),
		"collections": convertIntKeyCollectionsToString(s.collections),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.watchlistsFile, jsonData, 0644)
}

// convertStringKeyToInt converts map[string]V to map[int64]V
func convertStringKeyToInt(m map[string]map[int]*WatchlistItem) map[int64]map[int]*WatchlistItem {
	result := make(map[int64]map[int]*WatchlistItem)
	for k, v := range m {
		var userID int64
		fmt.Sscanf(k, "%d", &userID)
		result[userID] = v
	}
	return result
}

// convertIntKeyToString converts map[int64]V to map[string]V
func convertIntKeyToString(m map[int64]map[int]*WatchlistItem) map[string]map[int]*WatchlistItem {
	result := make(map[string]map[int]*WatchlistItem)
	for k, v := range m {
		result[fmt.Sprintf("%d", k)] = v
	}
	return result
}

// convertStringKeyCollectionsToInt converts string-keyed collections to int64-keyed
func convertStringKeyCollectionsToInt(m map[string][]*WatchlistCollection) map[int64][]*WatchlistCollection {
	result := make(map[int64][]*WatchlistCollection)
	for k, v := range m {
		var userID int64
		fmt.Sscanf(k, "%d", &userID)
		result[userID] = v
	}
	return result
}

// convertIntKeyCollectionsToString converts int64-keyed collections to string-keyed
func convertIntKeyCollectionsToString(m map[int64][]*WatchlistCollection) map[string][]*WatchlistCollection {
	result := make(map[string][]*WatchlistCollection)
	for k, v := range m {
		result[fmt.Sprintf("%d", k)] = v
	}
	return result
}

func countCollections(m map[int64][]*WatchlistCollection) int {
	count := 0
	for _, collections := range m {
		count += len(collections)
	}
	return count
}

// AddToWatchlist adds an item to user's main watchlist
func (s *WatchlistService) AddToWatchlist(telegramID int64, item *WatchlistItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watchlists[telegramID] == nil {
		s.watchlists[telegramID] = make(map[int]*WatchlistItem)
	}

	// Check if already exists
	if existing, exists := s.watchlists[telegramID][item.TmdbID]; exists {
		// Update notes/tags if provided
		if item.Notes != "" {
			existing.Notes = item.Notes
		}
		if len(item.Tags) > 0 {
			existing.Tags = append(existing.Tags, item.Tags...)
		}
		return s.save()
	}

	item.AddedAt = time.Now()
	s.watchlists[telegramID][item.TmdbID] = item

	log.Printf("[Watchlist] Added %s to user %d watchlist", item.Title, telegramID)

	return s.save()
}

// RemoveFromWatchlist removes an item from user's watchlist
func (s *WatchlistService) RemoveFromWatchlist(telegramID int64, tmdbID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watchlists[telegramID] == nil {
		return fmt.Errorf("watchlist is empty for user %d", telegramID)
	}

	if _, exists := s.watchlists[telegramID][tmdbID]; !exists {
		return fmt.Errorf("item %d not found in watchlist", tmdbID)
	}

	delete(s.watchlists[telegramID], tmdbID)

	return s.save()
}

// GetWatchlist returns user's watchlist
func (s *WatchlistService) GetWatchlist(telegramID int64) []*WatchlistItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	watchlist := s.watchlists[telegramID]
	if watchlist == nil {
		return nil
	}

	// Convert map to slice and sort by added date (newest first)
	items := make([]*WatchlistItem, 0, len(watchlist))
	for _, item := range watchlist {
		items = append(items, item)
	}

	// Sort by added date descending
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].AddedAt.After(items[j].AddedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	return items
}

// IsInWatchlist checks if an item is in user's watchlist
func (s *WatchlistService) IsInWatchlist(telegramID int64, tmdbID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.watchlists[telegramID] == nil {
		return false
	}

	_, exists := s.watchlists[telegramID][tmdbID]
	return exists
}

// CreateCollection creates a new named collection for a user
func (s *WatchlistService) CreateCollection(telegramID int64, name, description string) (*WatchlistCollection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	collection := &WatchlistCollection{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		Items:       make(map[int]*WatchlistItem),
	}

	s.collections[telegramID] = append(s.collections[telegramID], collection)

	log.Printf("[Watchlist] Created collection '%s' for user %d", name, telegramID)

	return collection, s.save()
}

// AddToCollection adds an item to a named collection
func (s *WatchlistService) AddToCollection(telegramID int64, collectionName string, item *WatchlistItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	collections := s.collections[telegramID]
	for _, collection := range collections {
		if collection.Name == collectionName {
			item.AddedAt = time.Now()
			collection.Items[item.TmdbID] = item
			return s.save()
		}
	}

	return fmt.Errorf("collection '%s' not found for user %d", collectionName, telegramID)
}

// GetCollections returns all collections for a user
func (s *WatchlistService) GetCollections(telegramID int64) []*WatchlistCollection {
	s.mu.RLock()
	defer s.mu.RUnlock()

	collections := s.collections[telegramID]
	if collections == nil {
		return nil
	}

	return collections
}

// UpdateWatchlistItem updates notes or tags for a watchlist item
func (s *WatchlistService) UpdateWatchlistItem(telegramID int64, tmdbID int, notes string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watchlists[telegramID] == nil {
		return fmt.Errorf("watchlist is empty for user %d", telegramID)
	}

	item, exists := s.watchlists[telegramID][tmdbID]
	if !exists {
		return fmt.Errorf("item %d not found in watchlist", tmdbID)
	}

	if notes != "" {
		item.Notes = notes
	}
	if len(tags) > 0 {
		item.Tags = tags
	}

	return s.save()
}

// GetWatchlistStats returns statistics about a user's watchlist
func (s *WatchlistService) GetWatchlistStats(telegramID int64) map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"total_items": 0,
		"movies":      0,
		"tv_shows":    0,
		"collections": len(s.collections[telegramID]),
	}

	watchlist := s.watchlists[telegramID]
	if watchlist != nil {
		stats["total_items"] = len(watchlist)
		for _, item := range watchlist {
			if item.MediaType == "movie" {
				stats["movies"] = stats["movies"].(int) + 1
			} else if item.MediaType == "tv" {
				stats["tv_shows"] = stats["tv_shows"].(int) + 1
			}
		}
	}

	return stats
}

// Now returns the current time, useful for testing
func (s *WatchlistService) Now() time.Time {
	return time.Now()
}
