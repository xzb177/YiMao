package callback

import (
	"fmt"
	"testing"
)

func TestResourceCallbacks(t *testing.T) {
	parser := NewParser()
	
	tests := []struct {
		input    string
		action   Action
		params   map[string]string
	}{
		{
			"res_list:id:123:type:movie",
			ActionResourceList,
			map[string]string{"id": "123", "type": "movie"},
		},
		{
			"res_pick:idx:0",
			ActionResourcePick,
			map[string]string{"idx": "0"},
		},
		{
			"rp:0",
			"rp",
			nil, // "rp" has no params
		},
		{
			"res_sort:by:seeders",
			ActionResourceSort,
			map[string]string{"by": "seeders"},
		},
		{
			"res_prev",
			ActionResourcePrev,
			nil,
		},
		{
			"res_next",
			ActionResourceNext,
			nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed, err := parser.Parse(tt.input)
			if err != nil {
				t.Errorf("Parse(%q) error = %v", tt.input, err)
				return
			}
			
			if parsed.Action != tt.action {
				t.Errorf("Parse(%q).Action = %q, want %q", tt.input, parsed.Action, tt.action)
			}
			
			if tt.params != nil {
				for k, v := range tt.params {
					if parsed.Params[k] != v {
						t.Errorf("Parse(%q).Params[%q] = %q, want %q", tt.input, k, parsed.Params[k], v)
					}
				}
			}
			
			fmt.Printf("✓ %s -> Action=%s, Params=%v\n", tt.input, parsed.Action, parsed.Params)
		})
	}
}

func TestBuildCallback(t *testing.T) {
	parser := NewParser()
	
	// 测试 BuildCallback 生成
	cb1 := BuildCallback(ActionResourceList, map[string]string{"id": "123", "type": "movie"})
	fmt.Printf("BuildCallback(res_list, {id:123, type:movie}) = %s\n", cb1)
	
	parsed, _ := parser.Parse(cb1)
	if parsed.Action != ActionResourceList {
		t.Errorf("Generated callback action = %q, want %q", parsed.Action, ActionResourceList)
	}
	if parsed.Params["id"] != "123" || parsed.Params["type"] != "movie" {
		t.Errorf("Generated callback params = %v, want map[id:123 type:movie]", parsed.Params)
	}
	fmt.Printf("✓ Round-trip successful\n")
}
