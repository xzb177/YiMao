package richmessage

import (
	_ "embed"
	"strings"
	"sync"

	"github.com/xzb177/yimao/pkg/types"
)

//go:embed assets/welcome_hero.png
var welcomeHeroPNG []byte

var (
	liveHeroMu sync.RWMutex
	liveHeroFn func() ([]byte, string)
)

func SetLiveWelcomeHero(fn func() ([]byte, string)) {
	liveHeroMu.Lock()
	liveHeroFn = fn
	liveHeroMu.Unlock()
}

func WelcomeHeroPNG() []byte { return welcomeHeroPNG }

func WelcomeHeroFile() ([]byte, string) {
	liveHeroMu.RLock()
	fn := liveHeroFn
	liveHeroMu.RUnlock()
	if fn != nil {
		if data, name := fn(); len(data) > 0 {
			if name == "" {
				name = "welcome_hero.jpg"
			}
			return data, name
		}
	}
	return welcomeHeroPNG, "welcome_hero.png"
}

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
	data, name := WelcomeHeroFile()
	if len(data) == 0 {
		return nil
	}
	return []types.TelegramInputRichMessageMedia{{
		ID:       "welcome_hero",
		Media:    types.TelegramRichPhoto{Type: "photo", Media: "attach://welcome_hero"},
		Upload:   data,
		Filename: name,
	}}
}
