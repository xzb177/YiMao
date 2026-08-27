package richmessage

import (
	_ "embed"
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

//go:embed assets/welcome_hero.png
var welcomeHeroPNG []byte

func WelcomeHeroPNG() []byte { return welcomeHeroPNG }

func WelcomeCaption() string {
	return strings.Join([]string{copyKickerCinema, copyWelcomeH1, copyWelcomeTag, copyWelcomeBody, copyWelcomeStat}, "\n")
}

func WelcomeInlineKeyboard() *types.TelegramInlineKeyboard {
	return InlineKeyboardFromBlocks(BuildWelcomeCard("", WelcomeOptions{}).Input())
}

func IsWelcomeHero(rich *types.TelegramInputRichMessage) bool {
	if rich == nil {
		return false
	}
	for _, media := range rich.Media {
		if media.ID == "welcome_hero" {
			return true
		}
	}
	return false
}

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
