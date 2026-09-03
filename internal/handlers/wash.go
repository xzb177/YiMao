package handlers

import (
	"fmt"
	"strconv"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

// WashHandler creates media-bound wash work orders in the shared review queue.
// It deliberately does not depend on MoviePilot account binding or quota.
type WashHandler struct {
	reviews *services.ReviewService
	tmdb    *services.TMDBClient
	emby    *services.WebhookService
	notify  func(*services.ReviewRequest)
	sessMgr *session.Manager
}

func NewWashHandler(reviews *services.ReviewService, tmdb *services.TMDBClient, emby *services.WebhookService, notify func(*services.ReviewRequest), sessMgr *session.Manager) *WashHandler {
	return &WashHandler{reviews: reviews, tmdb: tmdb, emby: emby, notify: notify, sessMgr: sessMgr}
}

func (h *WashHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	idText := ctx.Callback.Params["id"]
	mediaType := ctx.Callback.Params["type"]
	if idText == "" {
		if h.sessMgr != nil {
			h.sessMgr.GetOrCreate(ctx.UserID).Set("media_search_intent", "wash")
		}
		card := richmessage.BuildWashPromptCard()
		return &callback.Response{Text: card.Markdown, RichMessage: card.Markdown, StructuredRichMessage: card.Input(), Edit: true, ParseMode: "none", Keyboard: &callback.Keyboard{RemoveKeyboard: true}}, nil
	}
	id, err := strconv.Atoi(idText)
	if err != nil || id <= 0 || (mediaType != "movie" && mediaType != "tv") {
		return &callback.Response{CallbackMsg: "媒体参数无效", ShowAlert: true}, nil
	}
	season, episode := 0, 0
	if mediaType == "tv" {
		season, _ = strconv.Atoi(ctx.Callback.Params["season"])
		episode, _ = strconv.Atoi(ctx.Callback.Params["episode"])
		if season <= 0 {
			return h.seasons(id)
		}
		if episode <= 0 {
			if ctx.Callback.Params["confirm"] == "1" {
				return &callback.Response{CallbackMsg: "仅限单集", ShowAlert: true}, nil
			}
			return h.episodes(id, season)
		}
	} else if ctx.Callback.Params["season"] != "" || ctx.Callback.Params["episode"] != "" {
		return &callback.Response{CallbackMsg: "范围无效", ShowAlert: true}, nil
	}
	if h.reviews == nil || h.tmdb == nil || h.emby == nil {
		return &callback.Response{CallbackMsg: "洗版服务未就绪", ShowAlert: true}, nil
	}
	kind := services.MediaTypeMovie
	if mediaType == "tv" {
		kind = services.MediaTypeTV
	}
	media, err := h.tmdb.GetMediaByType(id, mediaType)
	if err != nil || media == nil {
		return &callback.Response{CallbackMsg: "无法读取该媒体", ShowAlert: true}, nil
	}
	exists, err := h.emby.HasEmbyWashTarget(id, media.GetTitle(), media.GetYear(), kind, season, episode)
	if err != nil {
		return &callback.Response{Text: "♻️ 暂时无法核验媒体库，请稍后再试。洗版只接受当前已经存在的资源。", CallbackMsg: "媒体库核验失败", ShowAlert: true}, nil
	}
	if !exists {
		return &callback.Response{Text: "♻️ 媒体库里暂时没有这部影片或所选季度，因此不能申请洗版。\n\n洗版用于更换已有资源；如果想新增内容，请使用「🎬 求片」。", CallbackMsg: "媒体库中未找到", ShowAlert: true, Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "🎬 改为求片", CallbackData: fmt.Sprintf("request:id:%d:type:%s", id, mediaType)}}, {{Text: "🔍 重选影片", CallbackData: "wash"}, {Text: "🏠 主菜单", CallbackData: "start"}}}}}, nil
	}
	baseline, err := h.emby.CaptureEmbyWashBaseline(id, kind, season, episode)
	if err != nil {
		return &callback.Response{Text: "♻️ 无法记录当前版本基线，已安全停止提交。请确认 Emby 已扫描出该影片或季度的文件后重试。", CallbackMsg: "基线采集失败", ShowAlert: true}, nil
	}
	seasonText := ""
	if season > 0 {
		seasonText = fmt.Sprintf(" · 第%d季第%d集", season, episode)
	}
	if ctx.Callback.Params["confirm"] != "1" {
		icon := "🎬"
		if mediaType == "tv" {
			icon = "📺"
		}
		return &callback.Response{
			Text:     fmt.Sprintf("♻️ 确认洗版\n\n%s《%s》%s\n\n✅ 已确认媒体库中存在当前版本\n🛡️ 新版本确认可用前，当前版本不会被删除或覆盖\n📋 提交后由管理员审核处理", icon, media.GetTitle(), seasonText),
			Edit:     true,
			Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "✅ 提交洗版工单", CallbackData: fmt.Sprintf("wash:id:%d:type:%s:season:%d:episode:%d:confirm:1", id, mediaType, season, episode)}}, {{Text: "⬅️ 重选", CallbackData: "wash"}, {Text: "❌ 取消", CallbackData: "cancel"}}}},
		}, nil
	}
	review := &services.ReviewRequest{
		RequestID: fmt.Sprintf("wash_%d_%d_%d_%d_%d", ctx.UserID, id, season, episode, time.Now().UnixNano()), BusinessType: services.BusinessTypeWash,
		TelegramID: ctx.UserID, TelegramName: fmt.Sprintf("用户%d", ctx.UserID), TmdbID: id,
		MediaTitle: media.GetTitle(), MediaYear: media.GetYear(), MediaType: kind, Season: season, Episode: episode,
		PosterPath: media.PosterPath, Overview: media.Overview, QuotaCost: 0, RequestOrigin: "wash", WashBaseline: baseline,
	}
	existing, created, err := h.reviews.CreateRequestIfNoActiveSimilar(review)
	if err != nil {
		return nil, err
	}
	if !created {
		return &callback.Response{Text: fmt.Sprintf("♻️ 《%s》的洗版工单正在处理中", existing.MediaTitle), CallbackMsg: "请勿重复提交", ShowAlert: true}, nil
	}
	if h.notify != nil {
		h.notify(review)
	}
	if h.sessMgr != nil {
		h.sessMgr.GetOrCreate(ctx.UserID).Delete("media_search_intent")
	}
	return &callback.Response{
		Text:        fmt.Sprintf("✅ 已提交\n\n♻️ 《%s》%s\n📋 状态：等待管理员审核\n\n当前版本会保留，进度可在求片进度查看。", review.MediaTitle, seasonText),
		CallbackMsg: "已提交", Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "查看进度", CallbackData: "start_requests"}, {Text: "返回首页", CallbackData: "start"}}}},
	}, nil
}

func (h *WashHandler) seasons(id int) (*callback.Response, error) {
	if h.tmdb == nil {
		return &callback.Response{CallbackMsg: "季度服务未就绪", ShowAlert: true}, nil
	}
	detail, err := h.tmdb.GetTVDetailsWithSeasons(id)
	if err != nil || detail == nil {
		return &callback.Response{CallbackMsg: "无法读取季度", ShowAlert: true}, nil
	}
	kb := &callback.Keyboard{}
	for _, season := range detail.Seasons {
		if season.SeasonNumber > 0 && season.EpisodeCount > 0 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, []callback.Button{{Text: fmt.Sprintf("第%d季", season.SeasonNumber), CallbackData: fmt.Sprintf("wash:id:%d:type:tv:season:%d", id, season.SeasonNumber)}})
		}
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []callback.Button{{Text: "⬅️ 返回详情", CallbackData: fmt.Sprintf("detail:id:%d:type:tv", id)}})
	return &callback.Response{Text: fmt.Sprintf("♻️ 《%s》\n\n请选择季度，再选择一集。每张工单仅处理一集。", detail.Name), Edit: true, Keyboard: kb}, nil
}

func (h *WashHandler) episodes(id, wantedSeason int) (*callback.Response, error) {
	if h.tmdb == nil {
		return &callback.Response{CallbackMsg: "剧集服务未就绪", ShowAlert: true}, nil
	}
	detail, err := h.tmdb.GetTVDetailsWithSeasons(id)
	if err != nil || detail == nil {
		return &callback.Response{CallbackMsg: "无法读取剧集", ShowAlert: true}, nil
	}
	count := 0
	for _, season := range detail.Seasons {
		if season.SeasonNumber == wantedSeason {
			count = season.EpisodeCount
			break
		}
	}
	if count <= 0 {
		return &callback.Response{CallbackMsg: "该季暂无可选单集", ShowAlert: true}, nil
	}
	kb := &callback.Keyboard{}
	row := make([]callback.Button, 0, 4)
	for episode := 1; episode <= count; episode++ {
		row = append(row, callback.Button{Text: fmt.Sprintf("第%d集", episode), CallbackData: fmt.Sprintf("wash:id:%d:type:tv:season:%d:episode:%d", id, wantedSeason, episode)})
		if len(row) == 4 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, row)
			row = make([]callback.Button, 0, 4)
		}
	}
	if len(row) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []callback.Button{{Text: "⬅️ 重选季度", CallbackData: fmt.Sprintf("wash:id:%d:type:tv", id)}})
	return &callback.Response{Text: fmt.Sprintf("♻️ 《%s》第%d季\n\n请选择一集；不支持整剧、整季或多集洗版。", detail.Name, wantedSeason), Edit: true, Keyboard: kb}, nil
}
