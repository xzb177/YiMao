package miniapp

import "testing"

func TestFillDynamicPosterUsesStableTMDBIdentity(t *testing.T) {
	item := dynamicMedia{TMDBID: 203164, Type: "tv", Title: "大力女子姜南顺"}
	calls := 0
	fillDynamicPoster(&item, func(tmdbID int, mediaType string) (string, error) {
		calls++
		if tmdbID != 203164 || mediaType != "tv" {
			t.Fatalf("unexpected lookup: id=%d type=%s", tmdbID, mediaType)
		}
		return "https://image.tmdb.org/t/p/w500/poster.jpg", nil
	})
	if calls != 1 || item.Poster != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Fatalf("poster not filled: calls=%d item=%+v", calls, item)
	}
}

func TestFillDynamicPosterPreservesPersistedPoster(t *testing.T) {
	item := dynamicMedia{TMDBID: 550, Type: "movie", Poster: "https://existing.example/poster.jpg"}
	fillDynamicPoster(&item, func(int, string) (string, error) {
		t.Fatal("lookup should not run when poster already exists")
		return "", nil
	})
	if item.Poster != "https://existing.example/poster.jpg" {
		t.Fatalf("existing poster changed: %+v", item)
	}
}

func TestFillDynamicPosterLeavesFallbackOnLookupFailure(t *testing.T) {
	item := dynamicMedia{TMDBID: 0, Type: "movie", Title: "无稳定标识"}
	fillDynamicPoster(&item, func(int, string) (string, error) {
		t.Fatal("lookup should not run without a TMDB ID")
		return "", nil
	})
	if item.Poster != "" {
		t.Fatalf("unexpected fabricated poster: %+v", item)
	}
}
