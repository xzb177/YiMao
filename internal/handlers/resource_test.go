package handlers

import (
	"testing"

	"emby-telegram-bot/internal/callback"
)

func TestResourceHandlerActions(t *testing.T) {
	parser := callback.NewParser()
	
	tests := []struct {
		name   string
		input  string
		action callback.Action
	}{
		{"ResourceList", "res_list:id:123:type:movie", callback.ActionResourceList},
		{"ResourcePick", "res_pick:idx:0", callback.ActionResourcePick},
		{"ResourceSort", "res_sort:by:seeders", callback.ActionResourceSort},
		{"ResourcePrev", "res_prev", callback.ActionResourcePrev},
		{"ResourceNext", "res_next", callback.ActionResourceNext},
		{"ShortPick", "rp:0", "rp"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb, err := parser.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.input, err)
			}
			
			// 验证 action 是否正确匹配
			if cb.Action != tt.action {
				t.Errorf("Action = %q, want %q", cb.Action, tt.action)
			}
			
			// 验证 params 是否正确解析
			if tt.name == "ResourceList" {
				if cb.Params["id"] != "123" {
					t.Errorf("Params[id] = %q, want 123", cb.Params["id"])
				}
				if cb.Params["type"] != "movie" {
					t.Errorf("Params[type] = %q, want movie", cb.Params["type"])
				}
			}
			if tt.name == "ResourcePick" || tt.name == "ShortPick" {
				idx := cb.Params["idx"]
				if tt.name == "ResourcePick" && idx != "0" {
					t.Errorf("Params[idx] = %q, want 0", idx)
				}
			}
			if tt.name == "ResourceSort" {
				if cb.Params["by"] != "seeders" {
					t.Errorf("Params[by] = %q, want seeders", cb.Params["by"])
				}
			}
			
			t.Logf("✓ %s: action=%s, params=%v", tt.name, cb.Action, cb.Params)
		})
	}
}
