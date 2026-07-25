package services

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateStatusWithNotifyRollsBackWhenPersistenceFails(t *testing.T) {
	now := time.Now().Add(-time.Hour)
	issue := &Issue{ID: 1, Status: IssueStatusOpen, UpdatedAt: now}
	svc := &IssueService{
		issuesFile: filepath.Join(t.TempDir(), "missing", "feedback.json"),
		issues:     map[int64]*Issue{1: issue},
	}
	if err := svc.UpdateStatusWithNotify(1, IssueStatusFixed); err == nil {
		t.Fatal("expected persistence error")
	}
	if issue.Status != IssueStatusOpen || !issue.UpdatedAt.Equal(now) || issue.ResolvedAt != nil {
		t.Fatalf("issue was not rolled back: %#v", issue)
	}
}
