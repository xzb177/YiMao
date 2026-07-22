package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestBuildVisualSearchMessageUsesPhotoBlockForSingleResult(t *testing.T) {
	results := []services.SearchResult{{ID: 1, Title: "单结果", Year: 2026, Type: "tv", Overview: "简介必须在视觉卡中。"}}
	cards := []services.SearchVisualCard{{ResultIndex: 0, JPEG: []byte("jpeg")}}
	rich := buildVisualSearchMessageFromCards("单结果", 1, results, cards)
	if rich == nil || len(rich.Blocks) != 1 {
		t.Fatalf("rich=%+v", rich)
	}
	if rich.Blocks[0].Type != "photo" || rich.Blocks[0].Photo == nil {
		t.Fatalf("single result must be a photo block, got %+v", rich.Blocks[0])
	}
	if len(rich.Media) != 1 || len(rich.Media[0].Upload) == 0 {
		t.Fatalf("single visual upload missing: %+v", rich.Media)
	}
}
