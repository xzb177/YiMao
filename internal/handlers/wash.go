package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

// washRequestNonce 生成不含用户 ID 的洗版工单随机后缀（审计 A1：request_id 脱敏）。
func washRequestNonce() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return fmt.Sprintf("%d_%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// WashHandler creates media-bound wash work orders in the shared review queue.
type WashHandler struct {
	reviews *services.ReviewService
	tmdb    *services.TMDBClient
	emby    *services.WebhookService
	notify  func(*services.ReviewRequest)
	sessMgr *session.Manager
	// 审计 A1：洗版写入接口必须验绑定（开小号刷审核队列）。
	userMapping services.UserMappingStore
}

// SetUserMapping 注入用户绑定关系（审计 A1）。
func (h *WashHandler) SetUserMapping(m services.UserMappingStore) {
	h.userMapping = m
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
	season := 0
	if mediaType == "tv" {
		season, _ = strconv.Atoi(ctx.Callback.Params["season"])
		if season <= 0 {
			return h.seasons(id)
		}
	}
	if h.reviews == nil || h.tmdb == nil || h.emby == nil {
		return &callback.Response{CallbackMsg: "洗版服务未就绪", ShowAlert: true}, nil
	}
	// 审计 A1：未绑定用户不许提交洗版工单。
	if h.userMapping != nil {
		if _, bound := h.userMapping.GetMoviePilotUserID(ctx.UserID); !bound {
			return &callback.Response{Text: "🔗 请先使用 /link 绑定账号，再申请洗版", CallbackMsg: "请先绑定账号", ShowAlert: true}, nil
		}
	}
	kind := services.MediaTypeMovie
	if mediaType == "tv" {
		kind = services.MediaTypeTV
	}
	media, err := h.tmdb.GetMediaByType(id, mediaType)
	if err != nil || media == nil {
		return &callback.Response{CallbackMsg: "无法读取该媒体", ShowAlert: true}, nil
	}
	exists, err := h.emby.HasEmbyWashTarget(id, media.GetTitle(), media.GetYear(), kind, season)
	if err != nil {
		return &callback.Response{Text: "♻️ 暂时无法核验媒体库，请稍后再试。洗版只接受当前已经存在的资源。", CallbackMsg: "媒体库核验失败", ShowAlert: true}, nil
	}
	if !exists {
		return &callback.Response{Text: "♻️ 媒体库里暂时没有这部影片或所选季度，因此不能申请洗版。\n\n洗版用于更换已有资源；如果想新增内容，请使用「🎬 求片」。", CallbackMsg: "媒体库中未找到", ShowAlert: true, Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "🎬 改为求片", CallbackData: fmt.Sprintf("request:id:%d:type:%s", id, mediaType)}}, {{Text: "🔍 重选影片", CallbackData: "wash"}, {Text: "🏠 主菜单", CallbackData: "start"}}}}}, nil
	}
	baseline, err := h.emby.CaptureEmbyWashBaseline(id, kind, season)
	if err != nil {
		return &callback.Response{Text: "♻️ 无法记录当前版本基线，已安全停止提交。请确认 Emby 已扫描出该影片或季度的文件后重试。", CallbackMsg: "基线采集失败", ShowAlert: true}, nil
	}
	seasonText := ""
	if season > 0 {
		seasonText = fmt.Sprintf(" · 第%d季", season)
	}
	if ctx.Callback.Params["confirm"] != "1" {
		icon := "🎬"
		if mediaType == "tv" {
			icon = "📺"
		}
		return &callback.Response{
			Text:     fmt.Sprintf("♻️ 确认洗版\n\n%s《%s》%s\n\n✅ 已确认媒体库中存在当前版本\n🛡️ 新版本确认可用前，当前版本不会被删除或覆盖\n📋 提交后由管理员审核处理", icon, media.GetTitle(), seasonText),
			Edit:     true,
			Keyboard: &callback.Keyboard{InlineKeyboard: [][]callback.Button{{{Text: "✅ 提交洗版工单", CallbackData: fmt.Sprintf("wash:id:%d:type:%s:season:%d:confirm:1", id, mediaType, season)}}, {{Text: "⬅️ 重选", CallbackData: "wash"}, {Text: "❌ 取消", CallbackData: "cancel"}}}},
		}, nil
	}
	// 审计 A1：request_id 不再嵌入原始 TG ID（它会出现在回调、管理界面与日志里）。
	review := &services.ReviewRequest{
		RequestID: fmt.Sprintf("wash_%d_%d_%s", id, season, washRequestNonce()), BusinessType: services.BusinessTypeWash,
		TelegramID: ctx.UserID, TelegramName: fmt.Sprintf("用户%d", ctx.UserID), TmdbID: id,
		MediaTitle: media.GetTitle(), MediaYear: media.GetYear(), MediaType: kind, Season: season,
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
		if season.SeasonNumber <= 0 {
			continue
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []callback.Button{{Text: fmt.Sprintf("第%d季", season.SeasonNumber), CallbackData: fmt.Sprintf("wash:id:%d:type:tv:season:%d", id, season.SeasonNumber)}})
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []callback.Button{{Text: "⬅️ 返回详情", CallbackData: fmt.Sprintf("detail:id:%d:type:tv", id)}})
	return &callback.Response{Text: fmt.Sprintf("♻️ 《%s》\n\n请选择要洗版的季度。每张工单只处理一个季度，其他季度不受影响。", detail.Name), Edit: true, Keyboard: kb}, nil
}
