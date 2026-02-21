package aesthetic

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	tb "github.com/anencod0/telegram-bot-api/v6"
)

type AestheticSystem struct {
	db              *AestheticDB
	bot             *tb.Bot
	jellyseerrURL   string
	jellyseerrKey   string
	tmdbAPIKey      string
}

func NewAestheticSystem(dbPath, botToken, jellyseerrURL, jellyseerrKey, tmdbKey string) (*AestheticSystem, error) {
	db, err := NewAestheticDB(dbPath)
	if err != nil {
		return nil, err
	}

	bot, err := tb.NewBot(tb.Token{Token: botToken})
	if err != nil {
		db.Close()
		return nil, err
	}

	as := &AestheticSystem{
		db:            db,
		bot:           bot,
		jellyseerrURL: jellyseerrURL,
		jellyseerrKey:  jellyseerrKey,
		tmdbAPIKey:    tmdbKey,
	}

	as.setupHandlers()
	go as.StartPolling()

	return as, nil
}

func (as *AestheticSystem) setupHandlers() {
	bot := as.bot

	_, _ = bot.Handle(tb.OnCommand("start", as.handleStart))
	_, _ = bot.Handle(tb.OnCommand("许愿", as.handleMakeWish))
	_, _ = bot.Handle(tb.OnCommand("心愿", as.handleListWishes))
	_, _ = bot.Handle(tb.OnCommand("星火", as.handleIgnite))
	_, _ = bot.Handle(tb.OnCommand("境界", as.handleRealm))
	_, _ = bot.Handle(tb.OnCommand("重置", as.handleReset))

	as.setupTextHandler()
	as.setupCallbackHandlers()
}

func (as *AestheticSystem) setupTextHandler() {
	_, _ = as.bot.Handle(tb.OnText, as.handleTextMessage)
}

func (as *AestheticSystem) setupCallbackHandlers() {
	bot := as.bot

	_, _ = bot.Handle(tb.OnCallback([]string{"wish_search"}, as.handleWishSearch))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_add"}, as.handleAddWish))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_confirm"}, as.handleConfirmWish))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_ignite"}, as.handleIgniteCallback))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_remove"}, as.handleRemoveWish))
	_, _ = bot.Handle(tb.OnCallback([]string{"wish_cancel"}, as.handleCancelWish))
}

func (as *AestheticSystem) handleStart(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := as.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	header := BuildPoeticHeader(binding)

	msg := fmt.Sprintf(
		"%s\n\n欢迎来到愿望星空\n\n──────\n\n· /许愿  · 诉说你的期待\n· /心愿  ·  查看愿望清单\n· /星火  ·  为心愿注入能量\n· /境界  ·  查看修行进度\n\n──────\n\n✧ 愿星光指引 ✧",
		header,
	)

	_, err = as.bot.Send(user, msg)
	return err
}

func (as *AestheticSystem) handleRealm(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := as.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	header := BuildPoeticHeader(binding)

	realm := GetRealm(binding.Realm)
	quota := GetQuotaLevel(binding.Points)

	nextPoints := 20
	if binding.Points >= 20 {
		nextPoints = 50
	}
	if binding.Points >= 50 {
		nextPoints = 100
	}

	progress := ""
	if binding.Points > 0 {
		progress = fmt.Sprintf("\n\n距离下一境界还需 %d 点", nextPoints-binding.Points)
	}

	msg := fmt.Sprintf(
		"%s\n──────\n\n境界 %s\n灵韵 %s\n累积 %d 点%s\n\n──────\n\n✦ 每一次点燃都会积累灵韵",
		header,
		realm.Name,
		quota.Name,
		binding.Points,
		progress,
	)

	_, err = as.bot.Send(user, msg)
	return err
}

func (as *AestheticSystem) handleMakeWish(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := as.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	canRequest := binding.MovieQuota > 0 || binding.TvQuota > 0

	var quotaMsg string
	if canRequest {
		remaining := binding.MovieQuota + binding.TvQuota
		quotaMsg = fmt.Sprintf("✦ 今日还可许愿 %d 次", remaining)
	} else {
		quotaMsg = "◌ 今日星火已耗尽\n\n明日黎明时重置"
	}

	msg := fmt.Sprintf(
		"✧ 许愿仪式 ✧\n\n──────\n\n%s\n\n──────\n\n请发送你想看的影视名称",
		quotaMsg,
	)

	if canRequest {
		msg += "\n\n· 或直接粘贴 Jellyfin 链接 ·"
	}

	opts := &tb.SendOptions{
		ReplyMarkup: tb.InlineKeyboardMarkup{
			InlineKeyboard: [][]tb.InlineButton{
				{{Text: "✧ 心愿清单", Data: "wish_list"}},
			},
		},
	}

	_, err = as.bot.Send(user, msg, opts)
	return err
}

func (as *AestheticSystem) handleTextMessage(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	text := ctx.Message().Text

	if strings.HasPrefix(text, "http") {
		return as.handleLink(ctx)
	}

	return as.handleWishByName(ctx, text)
}

func (as *AestheticSystem) handleLink(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	link := ctx.Message().Text

	binding, err := as.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	if binding.MovieQuota <= 0 && binding.TvQuota <= 0 {
		_, _ = as.bot.Send(user, "◌ 今日星火已耗尽\n\n明日黎明时重置")
		return nil
	}

	tmdbID, mediaType, title := as.parseJellyfinLink(link)
	if tmdbID == 0 {
		_, _ = as.bot.Send(user, "◌ 链接解析失败\n\n──────\n\n请发送正确的 Jellyfin 链接")
		return nil
	}

	var quotaColumn string
	if mediaType == "movie" {
		quotaColumn = "movie_quota"
	} else {
		quotaColumn = "tv_quota"
	}

	if err := as.db.ConsumeQuota(user.ID, mediaType); err != nil {
		_, _ = as.bot.Send(user, "❌ 许愿失败")
		return err
	}

	wish := &Wish{
		TgID:      user.ID,
		Title:     title,
		Category:  mediaType,
		Energy:    1,
		Status:    WishStatusDormant,
		TmdbID:    tmdbID,
		MediaType: mediaType,
	}

	if err := as.db.CreateWish(wish); err != nil {
		_, _ = as.bot.Send(user, "❌ 心愿记录失败")
		return err
	}

	_, _ = as.bot.Answer(ctx.Callback(), "✧ 许愿成功")

	msg := fmt.Sprintf(
		"✧ 愿望已记录 ✧\n\n──────\n\n%s\n\n──────\n\n✦ 用 /星火 为心愿注入能量 ✦\n\n心火点燃时，星空会回应",
		BuildPoeticWishCard(wish),
	)

	_, err = as.bot.Send(user, msg)
	return err
}

func (as *AestheticSystem) handleWishByName(ctx tb.Context, title string) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	binding, err := as.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	if binding.MovieQuota <= 0 && binding.TvQuota <= 0 {
		_, _ = as.bot.Send(user, "◌ 今日星火已耗尽\n\n明日黎明时重置")
		return nil
	}

	tmdbID, mediaType, overview, year := as.searchTMDB(title)
	if tmdbID == 0 {
		_, _ = as.bot.Send(user, "◌ 星空未找到此作品\n\n──────\n\n请确认名称后重试")
		return nil
	}

	var quotaColumn string
	if mediaType == "movie" {
		quotaColumn = "movie_quota"
	} else {
		quotaColumn = "tv_quota"
	}

	if err := as.db.ConsumeQuota(user.ID, mediaType); err != nil {
		_, _ = as.bot.Send(user, "❌ 许愿失败")
		return err
	}

	wish := &Wish{
		TgID:      user.ID,
		Title:     title,
		Category:  mediaType,
		Energy:    1,
		Status:    WishStatusDormant,
		TmdbID:    tmdbID,
		MediaType: mediaType,
	}

	if err := as.db.CreateWish(wish); err != nil {
		_, _ = as.bot.Send(user, "❌ 心愿记录失败")
		return err
	}

	detailMsg := BuildPoeticDetail(title, overview, year, 0)

	_, _ = as.bot.Answer(ctx.Callback(), "✧")

	msg := fmt.Sprintf(
		"✧ 愿望已记录 ✧\n\n%s\n──────\n\n✦ 用 /星火 为心愿注入能量 ✦",
		detailMsg,
	)

	_, err = as.bot.Send(user, msg)
	return err
}

func (as *AestheticSystem) handleWishSearch(ctx tb.Context) error {
	c := ctx.Callback()

	wishes, err := as.db.GetUserWishes(c.Sender.ID)
	if err != nil {
		return err
	}

	msg := BuildPoeticWishList(wishes)

	_, err = as.bot.Edit(c.Message, msg)
	_, _ = as.bot.Answer(c, "")

	return err
}

func (as *AestheticSystem) handleConfirmWish(ctx tb.Context) error {
	c := ctx.Callback()
	parts := strings.Split(c.Data, ":")

	if len(parts) < 3 {
		return nil
	}

	tmdbID, _ := strconv.Atoi(parts[1])
	mediaType := parts[2]

	user := c.Sender
	binding, err := as.db.GetOrCreateBinding(user.ID)
	if err != nil {
		return err
	}

	if binding.MovieQuota <= 0 && binding.TvQuota <= 0 {
		_, _ = as.bot.Answer(c, "◌ 今日星火已耗尽")
		return nil
	}

	mediaTypeFull := "movie"
	if mediaType == "tv" {
		mediaTypeFull = "tv"
	}

	if err := as.db.ConsumeQuota(user.ID, mediaTypeFull); err != nil {
		_, _ = as.bot.Answer(c, "❌ 许愿失败")
		return err
	}

	info, err := as.getTMDBInfo(tmdbID, mediaType)
	if err != nil {
		_, _ = as.bot.Answer(c, "❌ 获取信息失败")
		return err
	}

	wish := &Wish{
		TgID:      user.ID,
		Title:     info.Title,
		Category:  mediaType,
		Energy:    1,
		Status:    WishStatusDormant,
		TmdbID:    tmdbID,
		MediaType: mediaType,
	}

	if err := as.db.CreateWish(wish); err != nil {
		_, _ = as.bot.Answer(c, "❌ 心愿记录失败")
		return err
	}

	msg := fmt.Sprintf(
		"✧ 愿望已记录 ✧\n\n──────\n\n%s\n\n──────\n\n✦ 用 /星火 为心愿注入能量 ✦",
		BuildPoeticWishCard(wish),
	)

	_, _ = as.bot.Answer(c, "✧")
	_, err = as.bot.Edit(c.Message, msg)

	return err
}

func (as *AestheticSystem) handleIgniteCallback(ctx tb.Context) error {
	c := ctx.Callback()
	parts := strings.Split(c.Data, ":")

	if len(parts) < 2 {
		return nil
	}

	wishID, _ := strconv.Atoi(parts[1])

	wish, err := as.db.GetWishByID(wishID)
	if err != nil {
		return err
	}

	if wish.TgID != c.Sender.ID {
		_, _ = as.bot.Answer(c, "◌ 这不是你的心愿")
		return nil
	}

	if wish.Status != WishStatusDormant && wish.Status != WishStatusGlow {
		_, _ = as.bot.Answer(c, "◌ 心愿状态异常")
		return nil
	}

	err = as.db.IgniteWish(wishID, c.Sender.ID)
	if err != nil {
		_, _ = as.bot.Answer(c, "❌ 点燃失败")
		return err
	}

	oldEnergy := wish.Energy
	wish.Energy++
	wish.Status = WishStatusGlow

	msg := fmt.Sprintf(
		"✦ 能量已注入 ✦\n\n──────\n\n%s\n\n%s → %s\n\n──────\n\n✦ 达到七点能量时自动点燃 ✦",
		BuildPoeticWishCard(wish),
		FormatEnergy(oldEnergy),
		FormatEnergy(wish.Energy),
	)

	_, err = as.bot.Edit(c.Message, msg)
	if err != nil {
		return err
	}

	_, _ = as.bot.Answer(c, "✦")

	if wish.Energy >= 7 {
		go as.attemptIgniteWish(wishID)
	}

	return nil
}

func (as *AestheticSystem) attemptIgniteWish(wishID int) {
	wish, err := as.db.GetWishByID(wishID)
	if err != nil {
		return
	}

	if wish.Energy < 7 {
		return
	}

	tmdbID := wish.TmdbID
	mediaType := wish.MediaType

	if tmdbID == 0 || mediaType == "" {
		return
	}

	if err := as.sendToJellyseerr(wish); err == nil {
		as.db.SetWishIgnited(wishID, tmdbID)
		log.Printf("[Aesthetic] Wish %d ignited for tg_id=%d", wishID, wish.TgID)
	}
}

func (as *AestheticSystem) handleRemoveWish(ctx tb.Context) error {
	c := ctx.Callback()
	parts := strings.Split(c.Data, ":")

	if len(parts) < 2 {
		return nil
	}

	wishID, _ := strconv.Atoi(parts[1])

	wish, err := as.db.GetWishByID(wishID)
	if err != nil {
		return err
	}

	if wish.TgID != c.Sender.ID {
		_, _ = as.bot.Answer(c, "◌ 这不是你的心愿")
		return nil
	}

	err = as.db.DeleteWish(wishID)
	if err != nil {
		_, _ = as.bot.Answer(c, "❌ 消除失败")
		return err
	}

	_, err = as.bot.Edit(c.Message, "✧ 心愿已消散 ✧\n\n──────\n\n它将化作星尘回归虚空")
	if err != nil {
		return err
	}

	_, _ = as.bot.Answer(c, "✧")

	return nil
}

func (as *AestheticSystem) handleCancelWish(ctx tb.Context) error {
	c := ctx.Callback()

	_, err := as.bot.Answer(c, "已取消")
	if err != nil {
		return err
	}

	_, err = as.bot.Edit(c.Message, "✧ 许愿已取消 ✧\n\n──────\n\n期待下次相遇")

	return err
}

func (as *AestheticSystem) handleListWishes(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	wishes, err := as.db.GetUserWishes(user.ID)
	if err != nil {
		return err
	}

	msg := BuildPoeticWishList(wishes)

	_, err = as.bot.Send(user, msg)
	return err
}

func (as *AestheticSystem) handleIgnite(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	wishes, err := as.db.GetUserWishes(user.ID)
	if err != nil {
		return err
	}

	if len(wishes) == 0 {
		_, _ = as.bot.Send(user, "◌ 心愿清单空空如也\n\n──────\n\n用 /许愿 添加期待")
		return nil
	}

	inline := [][]tb.InlineButton{}
	row := []tb.InlineButton{}

	for _, wish := range wishes {
		if wish.Status == WishStatusDormant || wish.Status == WishStatusGlow {
			energy := FormatEnergy(wish.Energy)
			btnText := fmt.Sprintf("✦ %s (%s)", wish.Title, energy)

			btn := tb.InlineButton{
				Text: btnText,
				Data: fmt.Sprintf("wish_ignite:%d", wish.ID),
			}
			row = append(row, btn)

			if len(row) >= 1 {
				inline = append(inline, row)
				row = []tb.InlineButton{}
			}
		}
	}

	if len(row) > 0 {
		inline = append(inline, row)
	}

	_, err = as.bot.Send(user, "✧ 选择要注入能量的心愿 ✧\n\n──────\n\n", &tb.SendOptions{
		ReplyMarkup: tb.InlineKeyboardMarkup{
			InlineKeyboard: inline,
		},
	})

	return err
}

func (as *AestheticSystem) handleReset(ctx tb.Context) error {
	user := ctx.Sender()
	if user == nil {
		return nil
	}

	err := as.db.RestoreQuota(user.ID)
	if err != nil {
		return err
	}

	binding, _ := as.db.GetOrCreateBinding(user.ID)
	header := BuildPoeticHeader(binding)

	_, err = as.bot.Send(user, fmt.Sprintf("%s\n\n──────\n\n星火已重置\n\n愿新的黎明带来好运", header))

	return err
}

func (as *AestheticSystem) StartPolling() {
	log.Printf("[Aesthetic] Starting polling for %s", as.bot.Token)

	for {
		err := as.bot.Poll()
		if err != nil {
			log.Printf("[Aesthetic] Poll error: %v", err)
			continue
		}
	}
}
