package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCarpoolMetadataSnapshotPersistsAndLegacyFileStillLoads(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"entries":{"movie:550":[7]}}`
	if err := os.WriteFile(filepath.Join(dir, "carpool.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewCarpoolService(dir)
	items := svc.ListForUser(7)
	if len(items) != 1 || items[0].TMDBID != 550 || items[0].Title != "" {
		t.Fatalf("legacy items=%+v", items)
	}
	added := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	meta := CarpoolMetadata{Title: "搏击俱乐部", Year: "1999", Poster: "https://image.test/poster.jpg", AddedAt: added}
	if _, err := svc.AddWithMetadataChecked(550, "movie", 7, meta); err != nil {
		t.Fatal(err)
	}
	reloaded := NewCarpoolService(dir)
	items = reloaded.ListForUser(7)
	if len(items) != 1 || items[0].Title != meta.Title || items[0].Year != meta.Year || items[0].Poster != meta.Poster || !items[0].AddedAt.Equal(added) {
		t.Fatalf("snapshot not persisted: %+v", items)
	}
}

func TestCarpoolMetadataWriteFailureRollsBackMetadata(t *testing.T) {
	dir := t.TempDir()
	svc := NewCarpoolService(dir)
	if _, err := svc.AddWithMetadataChecked(550, "movie", 7, CarpoolMetadata{Title: "旧标题"}); err != nil {
		t.Fatal(err)
	}
	svc.dataFile = t.TempDir() + "/missing/carpool.json"
	if _, err := svc.AddWithMetadataChecked(550, "movie", 7, CarpoolMetadata{Title: "新标题"}); err == nil {
		t.Fatal("expected persistence failure")
	}
	items := svc.ListForUser(7)
	if len(items) != 1 || items[0].Title != "旧标题" {
		t.Fatalf("metadata rollback failed: %+v", items)
	}
}
