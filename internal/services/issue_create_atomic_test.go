package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateIssueRollsBackMemoryAndNextIDWhenSaveFails(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	svc := NewIssueService(dataDir)

	if issue, err := svc.CreateIssue(42, "tester", "播放问题", "无法播放", "movie", "123", "测试片"); err == nil || issue != nil {
		t.Fatalf("CreateIssue() = (%#v, %v), want persistence error", issue, err)
	}
	if issues := svc.GetUserIssues(42); len(issues) != 0 {
		t.Fatalf("failed issue leaked into memory: %#v", issues)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	issue, err := svc.CreateIssue(42, "tester", "播放问题", "重试", "movie", "123", "测试片")
	if err != nil {
		t.Fatalf("retry CreateIssue(): %v", err)
	}
	if issue.ID != 1 {
		t.Fatalf("retry issue ID = %d, want 1", issue.ID)
	}
}
