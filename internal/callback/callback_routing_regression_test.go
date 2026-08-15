package callback

import "testing"

func TestGeneratedCallbacksParseAndReachRegistry(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		params map[string]string
	}{
		{"search trends", "search_trends", map[string]string{"days": "30"}},
		{"popular period", "search_popular", map[string]string{"period": "all"}},
		{"blindbox horror", "game_blindbox_horror", nil},
		{"narrator spoiler", "game_narrate", map[string]string{"spoiler": "1", "name": "Alien"}},
		{"blindbox personality", "game_blindbox_personality", nil},
		{"my requests legacy button", "my_requests", nil},
	}

	parser := NewParser()
	registry := NewRegistry()
	for _, tt := range tests {
		registry.RegisterFunc(tt.action, func(*Context) (*Response, error) { return &Response{}, nil })
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generated := BuildCallback(tt.action, tt.params)
			parsed, err := parser.Parse(generated)
			if err != nil {
				t.Fatalf("Parse(BuildCallback(...)) failed for %q: %v", generated, err)
			}
			if parsed.Action != tt.action {
				t.Fatalf("action = %q, want %q", parsed.Action, tt.action)
			}
			for key, want := range tt.params {
				if got := parsed.Params[key]; got != want {
					t.Errorf("param %q = %q, want %q", key, got, want)
				}
			}
			if _, ok := registry.Get(parsed.Action); !ok {
				t.Errorf("generated action %q has no registry handler", parsed.Action)
			}
		})
	}
}
