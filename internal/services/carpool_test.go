package services

import "testing"

func TestCarpoolRemoveAndListForUserPersist(t *testing.T) {
	dir := t.TempDir()
	svc := NewCarpoolService(dir)
	svc.Add(20, "tv", 7)
	svc.Add(10, "movie", 7)
	svc.Add(10, "movie", 8)

	items := svc.ListForUser(7)
	if len(items) != 2 || items[0].Type != "movie" || items[0].TMDBID != 10 || items[1].Type != "tv" || items[1].TMDBID != 20 {
		t.Fatalf("unexpected sorted items: %#v", items)
	}
	if !svc.Remove(10, "movie", 7) || svc.Contains(10, "movie", 7) {
		t.Fatal("exact user removal failed")
	}
	if !svc.Contains(10, "movie", 8) {
		t.Fatal("removing one user removed another user's entry")
	}
	if svc.Remove(10, "movie", 7) {
		t.Fatal("second removal should report false")
	}

	reloaded := NewCarpoolService(dir)
	if reloaded.Contains(10, "movie", 7) || !reloaded.Contains(10, "movie", 8) {
		t.Fatal("removal was not persisted exactly")
	}
}
