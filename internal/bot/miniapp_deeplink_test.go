package bot

import "testing"

func TestParseMiniAppStartPayload(t *testing.T) {
	tests := []struct {
		payload string
		ok      bool
		typeVal string
		id      int
		season  int
	}{
		{"yh_m_1273472_0", true, "movie", 1273472, 0},
		{"yh_t_302051_1", true, "tv", 302051, 1},
		{"yh_t_302051_0", false, "", 0, 0},
		{"yh_m_12_1", false, "", 0, 0},
		{"yh_x_12_0", false, "", 0, 0},
		{"yh_m_-1_0", false, "", 0, 0},
		{"bad", false, "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.payload, func(t *testing.T) {
			got, ok := parseMiniAppStartPayload(tt.payload)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v", ok, tt.ok)
			}
			if ok && (got.Type != tt.typeVal || got.TMDBID != tt.id || got.Season != tt.season) {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}
