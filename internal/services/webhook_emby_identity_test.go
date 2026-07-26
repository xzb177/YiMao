package services

import (
	"net/http"
	"testing"
)

func TestSearchEmbyMediaByTMDBRejectsFuzzyTitleCollision(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("AnyProviderIdEquals"); got != "Tmdb.257211" {
			t.Fatalf("provider filter=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"space-intern","Name":"太空实习生","ProductionYear":2013,"Type":"Movie","ProviderIds":{"Tmdb":"999999"}}],"TotalRecordCount":1}`))
	}))

	got, err := svc.SearchEmbyMediaByTMDB(257211, MediaTypeMovie)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("fuzzy collision accepted: %#v", got)
	}
}

func TestSearchEmbyMediaByTMDBReturnsExactProviderMatch(t *testing.T) {
	svc := newWashTargetService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"the-intern","Name":"实习生","ProductionYear":2015,"Type":"Movie","ProviderIds":{"Tmdb":"257211"}}],"TotalRecordCount":1}`))
	}))

	got, err := svc.SearchEmbyMediaByTMDB(257211, MediaTypeMovie)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "实习生" || got.Year != 2015 {
		t.Fatalf("exact match=%#v", got)
	}
}
