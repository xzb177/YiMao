package dedupfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	in := map[string]int64{"a": time.Now().Unix(), "b": time.Now().Unix()}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	got := Load(path, 24*time.Hour)
	if len(got) != 2 || got["a"] != in["a"] || got["b"] != in["b"] {
		t.Fatalf("round trip mismatch: %v", got)
	}
}

func TestLoadPrunesExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	old := time.Now().Add(-48 * time.Hour).Unix()
	fresh := time.Now().Unix()
	if err := Save(path, map[string]int64{"old": old, "fresh": fresh}); err != nil {
		t.Fatal(err)
	}
	got := Load(path, 24*time.Hour)
	if _, ok := got["old"]; ok {
		t.Fatal("expired entry must be pruned on load")
	}
	if got["fresh"] != fresh {
		t.Fatal("fresh entry must survive")
	}
}

func TestLoadMissingAndCorruptFailOpen(t *testing.T) {
	if got := Load(filepath.Join(t.TempDir(), "nope.json"), time.Hour); len(got) != 0 {
		t.Fatal("missing file must yield empty map")
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(path, time.Hour); len(got) != 0 {
		t.Fatal("corrupt file must yield empty map (fail-open)")
	}
}
