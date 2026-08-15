package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

func TestGameCenterRouletteCallbacksParseAndReachGameHandler(t *testing.T) {
	var entryGenerated bool
	for _, row := range services.BuildGameCenterKeyboard().InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "game_roulette" {
				entryGenerated = true
			}
		}
	}
	if !entryGenerated {
		t.Fatal("BuildGameCenterKeyboard did not generate game_roulette")
	}

	gameHandler := NewGameHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	registry := callback.NewRegistry()
	registry.RegisterFunc("game_roulette", gameHandler.Handle)
	registry.RegisterFunc("game_roulette_spin", gameHandler.Handle)

	for _, callbackData := range []string{"game_roulette", "game_roulette_spin"} {
		t.Run(callbackData, func(t *testing.T) {
			parsed, err := callback.NewParser().Parse(callbackData)
			if err != nil {
				t.Fatalf("Parser rejected %s callback: %v", callbackData, err)
			}

			registered, ok := registry.Get(parsed.Action)
			if !ok {
				t.Fatalf("%s has no registered game handler", callbackData)
			}
			response, err := registered.Handle(&callback.Context{UserID: 101, Callback: parsed})
			if err != nil {
				t.Fatalf("registered %s handler failed: %v", callbackData, err)
			}
			if response == nil || response.CallbackMsg != "轮盘服务未就绪" {
				t.Fatalf("%s reached wrong handler: %#v", callbackData, response)
			}
		})
	}
}
