package aesthetic

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	tb "github.com/anencod0/telegram-bot-api/v6"
)

type AestheticBot struct {
	db *AestheticDB
	bot *tb.Bot
}

func NewAestheticBot(db *AestheticDB, botToken string) (*AestheticBot, error) {
	bot, err := tb.NewBot(tb.Token{Token: botToken})
	if err != nil {
		return nil, err
	}

	ab := &AestheticBot{db: db, bot: bot}
	ab.setupHandlers()

	return ab, nil
}

func (ab *AestheticBot) setupHandlers() {
	bot := ab.bot

	_, _ = bot.Handle(tb.OnCommand("start", ab.handleStart))
	_, _ = bot.Handle(tb.OnCommand "许愿", ab.handleMakeWish))
	_, _ = bot.Handle(tb.OnCommand("心愿", ab.handleListWishes))
	_, _ = bot.Handle(tb.OnCommand("星火", ab.handleIgnite))
	_, _ = bot.Handle(tb.OnCommand("状态", ab.handleStatus))
	_, _ = bot.Handle(tb.OnCommand("重置", ab.handleReset))

	ab.setupCallbackHandlers()
}

func (ab *AestheticBot) setupCallbackHandlers() {
	bot := ab.bot

	_, _ = bot.Handle(tb.OnCallback([]string{"wish_search"}, ab.handleWishSearch))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_add"}, ab.handleAddWish))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_ignite"}, ab.handleIgniteCallback))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_remove"}, ab.handleRemoveWish))
}

func (ab *AestheticBot) handleStart(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := ab.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	header := BuildPoeticHeader(binding)

	msg := fmt.Sprintf(
		"%s\n欢迎来到愿望星空\n\n──────\n\n用 /许愿 诉说你的期待\n用 /心愿 查看心愿清单\n用 /星火 为心愿注入能量",
		header,
	)

	_, err = bot.Send(user, msg)
	return err
}

func (ab *AestheticBot) handleStatus(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := ab.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	header := BuildPoeticHeader(binding)

	realm := GetRealm(binding.Realm)
	quota := GetQuotaLevel(binding.Points)

	activityMsg := fmt.Sprintf(
		"\n──────\n\n境界 %s\n灵韵 %s\n累积 %d 点",
		realm.Name,
		quota.Name,
		binding.Points,
	)

	msg := header + activityMsg

	_, err = bot.Send(user, msg)
	return err
}

func (ab *AestheticBot) handleMakeWish(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := ab.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	canRequest := binding.MovieQuota > 0

	var quotaMsg string
	if canRequest {
		quotaMsg = fmt.Sprintf("今日还可许愿 %d 次", binding.MovieQuota+binding.TvQuota)
	} else {
		quotaMsg = "今日星火已耗尽\n明日黎明时重置"
	}

	msg := fmt.Sprintf(
		"✧ 许愿仪式 ✧\n\n──────\n\n发送你想看的影视名称\n\n%s\n\n──────\n\n%s",
		quotaMsg,
		BuildPoeticActionPanel("", canRequest, ""),
	)

	keyboard := [][]tb.InlineButton{}
	if canRequest {
		keyboard = append(keyboard, []tb.InlineButton{
			{Text: "继续", Data: "wish_add"},
		})
	}

	opts := &tb.SendOptions{
		ReplyMarkup: tb.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		},
	}

	_, err = bot.Send(user, msg, opts)
	return err
}

func (ab *AestheticBot) handleAddWish(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := ab.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	if binding.MovieQuota <= 0 && binding.TvQuota <= 0 {
		_, _ = bot.Send(user, "◌ 今日星火已耗尽\n\n明日黎明时重置")
		return nil
	}

	_, _ = bot.Send(user, "✧ 请说出你的愿望 ✧\n\n──────\n\n发送影视名称或直接粘贴链接\n\n──────\n\n✦ 系统将自动识别并点亮心愿")

	return nil
}

func (ab *AestheticBot) handleWishSearch(ctx tb.Context) error {
	c := ctx.Callback()

	inline := [][]tb.InlineButton{}
	inline = append(inline, []tb.InlineButton{
		{Text: "✧ 心愿清单", Data: "wish_list"},
		{Text: "✦ 添加心愿", Data: "wish_add"},
	})

	_, err := bot.Edit(c.Message, "✧ 搜索中 ·✧", &tb.SendOptions{
		ReplyMarkup: tb.InlineKeyboardMarkup{
			InlineKeyboard: inline,
		},
	})

	_, _ = bot.Answer(c, "✧")

	return err
}

func (ab *AestheticBot) handleListWishes(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	wishes, err := ab.db.GetUserWishes(user.ID)
	if err != nil {
		return err
	}

	msg := BuildPoeticWishList(wishes)

	_, err = bot.Send(user, msg)
	return err
}

func (ab *AestheticBot) handleIgnite(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	wishes, err := ab.db.GetUserWishes(user.ID)
	if err != nil {
		return err
	}

	if len(wishes) == 0 {
		_, _ = bot.Send(user, "◌ 心愿清单空空如也\n\n用 /许愿 添加期待")
		return nil
	}

	inline := [][]tb.InlineButton{}
	row := []tb.InlineButton{}

	for _, wish := range wishes {
		if wish.Status == WishStatusDormant || wish.Status == WishStatusGlow {
			btn := tb.InlineButton{
				Text: fmt.Sprintf("✦ %s", wish.Title),
				Data: fmt.Sprintf("wish_ignite:%d", wish.ID),
			}
			row = append(row, btn)

			if len(row) >= 2 {
				inline = append(inline, row)
				row = []tb.InlineButton{}
			}
		}
	}

	if len(row) > 0 {
		inline = append(inline, row)
	}

	_, err = bot.Send(user, "✧ 选择要注入能量的心愿 ✧\n\n──────\n\n", &tb.SendOptions{
		ReplyMarkup: tb.InlineKeyboardMarkup{
			InlineKeyboard: inline,
		},
	})

	return err
}

func (ab *AestheticBot) handleIgniteCallback(ctx tb.Context) error {
	c := ctx.Callback()
	parts := strings.Split(c.Data, ":")

	if len(parts) < 2 {
		return nil
	}

	wishID, _ := strconv.Atoi(parts[1])

	wish, err := ab.db.GetWishByID(wishID)
	if err != nil {
		return err
	}

	if wish.TgID != c.Sender.ID {
		_, _ = bot.Answer(c, "◌ 这不是你的心愿")
		return nil
	}

	if wish.Status != WishStatusDormant && wish.Status != WishStatusGlow {
		_, _ = bot.Answer(c, "◌ 心愿状态异常")
		return nil
	}

	err = ab.db.IgniteWish(wishID, c.Sender.ID)
	if err != nil {
		_, _ = bot.Answer(c, "❌ 点燃失败")
		return err
	}

	oldEnergy := wish.Energy
	wish.Energy++
	wish.Status = WishStatusGlow

	msg := fmt.Sprintf(
		"✦ 能量已注入 ✦\n\n──────\n\n%s\n\n%s → %s\n\n──────\n\n愿星光指引",
		BuildPoeticWishCard(wish),
		FormatEnergy(oldEnergy),
		FormatEnergy(wish.Energy),
	)

	_, err = bot.Edit(c.Message, msg)
	if err != nil {
		return err
	}

	_, _ = bot.Answer(c, "✦ 注入成功")

	return nil
}

func (ab *AestheticBot) handleRemoveWish(ctx tb.Context) error {
	c := ctx.Callback()
	parts := strings.Split(c.Data, ":")

	if len(parts) < 2 {
		return nil
	}

	wishID, _ := strconv.Atoi(parts[1])

	wish, err := ab.db.GetWishByID(wishID)
	if err != nil {
		return err
	}

	if wish.TgID != c.Sender.ID {
		_, _ = bot.Answer(c, "◌ 这不是你的心愿")
		return nil
	}

	err = ab.db.DeleteWish(wishID)
	if err != nil {
		_, _ = bot.Answer(c, "❌ 消除失败")
		return err
	}

	_, err = bot.Edit(c.Message, "✧ 心愿已消散 ✧\n\n──────\n\n它将化作星尘回归虚空")
	if err != nil {
		return err
	}

	_, _ = bot.Answer(c, "✧")

	return nil
}

func (ab *AestheticBot) handleReset(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	err := ab.db.RestoreQuota(user.ID)
	if err != nil {
		return err
	}

	binding, _ := ab.db.GetOrCreateBinding(user.ID)
	header := BuildPoeticHeader(binding)

	_, err = bot.Send(user, fmt.Sprintf("%s\n\n──────\n\n星火已重置\n\n愿新的黎明带来好运", header))

	return err
}

type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64  `json:"message_id"`
		From      *struct {
			ID           int64  `json:"id"`
			IsBot        bool   `json:"is_bot"`
			FirstName    string `json:"first_name"`
			LastName     string `json:"last_name"`
			Username     string `json:"username"`
			LanguageCode string `json:"language_code"`
		} `json:"from"`
		Chat *struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
	CallbackQuery *struct {
		ID      string `json:"callback_query_id"`
		From    *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Message *struct {
			MessageID int64 `json:"message_id"`
			Chat      *struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
		Data string `json:"data"`
	} `json:"callback_query"`
}
