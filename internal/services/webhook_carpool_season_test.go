package services

import "testing"

func TestReviewMatchesLibraryAddUsesExactTVSeason(t *testing.T) {
	base := ReviewRequest{Status: "approved", TmdbID: 131033, MediaType: MediaTypeTV}
	for _, tc := range []struct {
		name          string
		reviewSeason  int
		webhookSeason int
		want          bool
	}{
		{name: "same season", reviewSeason: 2, webhookSeason: 2, want: true},
		{name: "different season", reviewSeason: 3, webhookSeason: 2, want: false},
		{name: "unknown webhook does not complete scoped review", reviewSeason: 2, webhookSeason: 0, want: false},
		{name: "legacy unscoped review matches unknown webhook", reviewSeason: 0, webhookSeason: 0, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := base
			rv.Season = tc.reviewSeason
			if got := reviewMatchesLibraryAdd(&rv, 131033, "tv", tc.webhookSeason); got != tc.want {
				t.Fatalf("match=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestEmbyWebhookSeasonUsesEpisodeParentIndex(t *testing.T) {
	season, episode := 2, 7
	payload := EmbyWebhookPayload{Season: 1, Item: &EmbyItem{Type: "Episode", ParentIndexNumber: &season, IndexNumber: &episode}}
	if got := embyWebhookSeason(payload); got != 2 {
		t.Fatalf("season=%d want=2", got)
	}
}
