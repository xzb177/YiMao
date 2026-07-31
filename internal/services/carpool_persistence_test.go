package services

import "testing"

func TestCarpoolAddCheckedRollsBackWhenPersistenceFails(t *testing.T) {
	svc := NewCarpoolService(t.TempDir())
	svc.dataFile = t.TempDir() + "/missing/carpool.json"
	if _, err := svc.AddChecked(550, "movie", 7); err == nil {
		t.Fatal("expected persistence error")
	}
	if svc.Contains(550, "movie", 7) {
		t.Fatal("failed add remained in memory")
	}
}

func TestCarpoolRemoveCheckedRollsBackWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	svc := NewCarpoolService(dir)
	if _, err := svc.AddChecked(550, "movie", 7); err != nil {
		t.Fatal(err)
	}
	svc.dataFile = t.TempDir() + "/missing/carpool.json"
	if _, err := svc.RemoveChecked(550, "movie", 7); err == nil {
		t.Fatal("expected persistence error")
	}
	if !svc.Contains(550, "movie", 7) {
		t.Fatal("failed remove was not rolled back")
	}
}
