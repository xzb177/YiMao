package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutboundCopyHasNoStalePhrases(t *testing.T) {
	stale := []string{"已被已批准", "求片已提交", "媒体库中已存在", "首次加载约需"}
	_ = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(data), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "//") {
				continue
			}
			for _, phrase := range stale {
				if strings.Contains(line, phrase) {
					t.Errorf("%s contains stale copy %q: %s", path, phrase, trim)
				}
			}
		}
		return nil
	})
}
