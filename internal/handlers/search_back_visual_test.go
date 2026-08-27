package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

func TestRestoredSearchResultsPreserveOverviewAndReliableStatus(t *testing.T) {
	items := []session.SearchItem{{
		ID: "276567", Title: "夫妻的博弈", Year: 2026, Type: "tv",
		Poster: "/poster.jpg", Rating: 8.7,
		Overview: "这是一段返回搜索结果后仍应显示的完整简介。",
		Status:   "已在库",
	}}

	results, statuses := restoreSearchCardSnapshot(items)
	if len(results) != 1 {
		t.Fatalf("results=%d", len(results))
	}
	if results[0].Overview != items[0].Overview {
		t.Fatalf("overview lost: %q", results[0].Overview)
	}
	key := services.MediaStatusKey(results[0].ID, results[0].Type)
	if statuses[key] != "已在库" {
		t.Fatalf("status lost: key=%q statuses=%+v", key, statuses)
	}
}

func TestRestoredSearchResultsUseConservativeStatusWhenLegacySnapshotHasNone(t *testing.T) {
	items := []session.SearchItem{{ID: "124003", Title: "完美世界", Year: 2021, Type: "tv", Overview: "简介"}}
	results, statuses := restoreSearchCardSnapshot(items)
	key := services.MediaStatusKey(results[0].ID, results[0].Type)
	if statuses[key] != "状态暂未确认" {
		t.Fatalf("legacy status must be conservative, got %q", statuses[key])
	}
}
