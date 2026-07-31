package services

import (
	"testing"
	"time"
)

func TestFindCachedSubscriptionNeverCallsMoviePilotOnFreshCache(t *testing.T) {
	client := NewMoviePilotClient("http://127.0.0.1:1", "unused", "")
	client.subsCacheData = []SubscribeItem{{TMDBID: 550, Type: "电影", State: StateSearching}}
	client.subsCacheTime = time.Now()

	item, found, ready := client.FindCachedSubscription(550, MediaTypeMovie)
	if !ready || !found || item == nil || item.State != StateSearching {
		t.Fatalf("unexpected cached result: item=%+v found=%v ready=%v", item, found, ready)
	}
}

func TestFindCachedSubscriptionReturnsUnknownForStaleCache(t *testing.T) {
	client := NewMoviePilotClient("http://127.0.0.1:1", "unused", "")
	client.subsCacheData = []SubscribeItem{{TMDBID: 550, Type: "电影", State: StateSearching}}
	client.subsCacheTime = time.Now().Add(-client.subsCacheTTL - time.Second)

	item, found, ready := client.FindCachedSubscription(550, MediaTypeMovie)
	if ready || found || item != nil {
		t.Fatalf("stale cache must be unknown: item=%+v found=%v ready=%v", item, found, ready)
	}
}

func TestFindCachedSubscriptionKeepsMovieAndTVNamespacesSeparate(t *testing.T) {
	client := NewMoviePilotClient("http://127.0.0.1:1", "unused", "")
	client.subsCacheData = []SubscribeItem{{TMDBID: 100, Type: "电视剧", State: StateDownloading}}
	client.subsCacheTime = time.Now()

	if _, found, ready := client.FindCachedSubscription(100, MediaTypeMovie); !ready || found {
		t.Fatalf("TV cache entry leaked into movie namespace: found=%v ready=%v", found, ready)
	}
}
