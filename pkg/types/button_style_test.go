package types

import "testing"

func TestButtonStyleForWelcomeGrid(t *testing.T) {
	cases := []struct{ text, cb, want string }{
		{"搜索求片", "search:menu", ButtonStyleSuccess},
		{"求片进度", "requests", ButtonStylePrimary},
		{"帮助", "help", ButtonStylePrimary},
		{"更多", "start_more", ButtonStylePrimary},
		{"返回", "start", ButtonStylePrimary},
		{"洗版", "wash", ButtonStyleSuccess},
		{"立即求片", "request:id:1", ButtonStyleSuccess},
	}
	for _, c := range cases {
		if got := ButtonStyleFor(c.text, c.cb); got != c.want {
			t.Errorf("%s/%s = %q want %q", c.text, c.cb, got, c.want)
		}
	}
}
