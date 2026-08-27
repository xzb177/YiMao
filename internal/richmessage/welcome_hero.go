package richmessage

import (
	_ "embed"

	"github.com/xzb177/yimao/pkg/types"
)

//go:embed assets/welcome_hero.png
var welcomeHeroPNG []byte

func welcomeHeroMedia() []types.TelegramInputRichMessageMedia {
	if len(welcomeHeroPNG) == 0 {
		return nil
	}
	return []types.TelegramInputRichMessageMedia{{
		ID:       "welcome_hero",
		Media:    types.TelegramRichPhoto{Type: "photo", Media: "attach://welcome_hero"},
		Upload:   welcomeHeroPNG,
		Filename: "welcome_hero.png",
	}}
}
