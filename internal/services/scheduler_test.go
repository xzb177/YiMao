package services

import "testing"

// TestShuffleAndPick 验证 shuffleAndPick 的纯逻辑：
// 1. 当结果数 <= n 时原样返回（不截断）
// 2. 当结果数 > n 时只返回 n 个，且都来自原集合
func TestShuffleAndPick(t *testing.T) {
	s := &Scheduler{}

	// 输入数量 <= n：原样返回
	in := []SearchResult{{ID: 1}, {ID: 2}}
	got := s.shuffleAndPick(in, 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}

	// 输入数量 > n：截断到 n
	big := make([]SearchResult, 10)
	for i := range big {
		big[i] = SearchResult{ID: i + 1}
	}
	picked := s.shuffleAndPick(big, 3)
	if len(picked) != 3 {
		t.Fatalf("expected 3 items, got %d", len(picked))
	}

	// 选出的元素必须都来自原集合
	valid := map[int]bool{}
	for _, r := range big {
		valid[r.ID] = true
	}
	for _, p := range picked {
		if !valid[p.ID] {
			t.Fatalf("picked item ID=%d not in original set", p.ID)
		}
	}
}
