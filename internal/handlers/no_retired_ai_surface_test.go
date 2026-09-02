package handlers

import (
	"os"
	"strings"
	"testing"
)

func TestRetiredAICodePathsAreAbsent(t *testing.T) {
	files := []string{"callback.go", "request.go", "../session/manager.go", "../richmessage/card_builder.go", "../callback/types.go", "../../cmd/bot/main.go"}
	retired := []string{"ai_recommendation", "AIRecommendationItem", "CacheAI", "CachedAI", "ai_cache", "AI recommendation", "AI recommendations", "ActionAI", "start_ai"}
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, value := range retired {
			if strings.Contains(text, value) {
				t.Errorf("retired AI code marker %q remains in %s", value, name)
			}
		}
	}
}
