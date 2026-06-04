package handlers

import "testing"

// TestIsConfidentTitleMatch 覆盖 B6：多结果时判定首条是否高置信，决定是否提示用户精确重搜。
func TestIsConfidentTitleMatch(t *testing.T) {
	cases := []struct {
		query, title string
		want         bool
	}{
		{"沙丘", "沙丘", true},
		{"DUNE", "Dune", true},      // 忽略大小写
		{"  沙丘 ", "沙丘", true},      // 忽略首尾空白
		{"沙丘", "沙丘 2", true},       // 标题包含查询
		{"沙丘 2", "沙丘", true},       // 查询包含标题
		{"沙丘", "盗梦空间", false},      // 完全不相关
		{"", "沙丘", false},          // 空查询
		{"沙丘", "", false},          // 空标题
	}
	for _, c := range cases {
		if got := isConfidentTitleMatch(c.query, c.title); got != c.want {
			t.Errorf("isConfidentTitleMatch(%q,%q)=%v want %v", c.query, c.title, got, c.want)
		}
	}
}
