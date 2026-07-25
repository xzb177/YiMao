package bot

import "testing"

func TestParseStrictTMDBLink(t *testing.T) {
	tests := []struct {
		name, text, kind string
		id               int
		ok               bool
	}{
		{"movie canonical", "看看 https://www.themoviedb.org/movie/550 谢谢", "movie", 550, true},
		{"tv localized tail", "https://themoviedb.org/tv/1399-foo?language=zh-CN", "tv", 1399, true},
		{"http rejected", "http://www.themoviedb.org/movie/550", "", 0, false},
		{"lookalike rejected", "https://www.themoviedb.org.evil.test/movie/550", "", 0, false},
		{"unknown kind", "https://www.themoviedb.org/person/550", "", 0, false},
		{"embedded token", "xhttps://www.themoviedb.org/movie/550", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, id, ok := ParseStrictTMDBLink(tt.text)
			if kind != tt.kind || id != tt.id || ok != tt.ok {
				t.Fatalf("got (%q,%d,%v), want (%q,%d,%v)", kind, id, ok, tt.kind, tt.id, tt.ok)
			}
		})
	}
}
