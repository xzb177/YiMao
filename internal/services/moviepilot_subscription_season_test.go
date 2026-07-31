package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindExistingSubscriptionTVRequiresExactSeason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/subscribe/" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]SubscribeStatus{
			{ID: 8427, Name: "猥城琐事", Type: "电视剧", MediaID: FlexibleInt64(131033), Season: 1, State: StateRecycled, TotalEpisode: 23},
		})
	}))
	defer server.Close()
	client := NewMoviePilotClient(server.URL, "unused", "")
	if item, found, err := client.FindExistingSubscription(131033, MediaTypeTV, 2); err != nil || found || item != nil {
		t.Fatalf("season 1 blocked season 2: item=%+v found=%v err=%v", item, found, err)
	}
	if item, found, err := client.FindExistingSubscription(131033, MediaTypeTV, 1); err != nil || !found || item == nil || item.ID != 8427 {
		t.Fatalf("exact season not found: item=%+v found=%v err=%v", item, found, err)
	}
}

func TestFindExistingSubscriptionTVRejectsMissingSeasonFromOlderMoviePilot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]SubscribeStatus{
			{ID: 8428, Name: "旧版响应", Type: "电视剧", MediaID: FlexibleInt64(131033), State: StateSearching},
		})
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "unused", "")
	item, found, err := client.FindExistingSubscription(131033, MediaTypeTV, 2)
	if err != nil || found || item != nil {
		t.Fatalf("missing season was treated as an exact match: item=%+v found=%v err=%v", item, found, err)
	}
}
