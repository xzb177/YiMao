package richmessage

import (
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

const (
	copyKickerCinema = "YUNHAI · CINEMA"
	copyKickerNow    = "NOW PLAYING"
	copyWelcomeH1    = "云海求片"
	copyWelcomeTag   = "想看的，交给云海"
	copyWelcomeBody  = "直接发片名，或点搜索。提交后可在进度里查到。"
	copyWelcomeStat  = "在线 · 可求片"
	copySearchH1     = "搜索求片"
	copySearchTag    = "发中文或英文片名"
	copySearchBody   = "点结果看详情，确认后再提交。已在库的会直接告诉你。"
	copyHelpH1       = "帮助说明"
	copyHelpTag      = "三步就能求到"
	copyHelpBody     = "发片名 → 选结果 → 等审核。通过后会开始下载，可看时再通知你。"
	copyMoreH1       = "更多功能"
	copyMoreTag      = "不常用的入口"
	copyMoreBody     = "洗版、许愿、设置都在这，不影响正常求片。"
	copyProgressH1   = "求片进度"
	copyProgressTag  = "提交过的片在这里"
	copyPlaybillTag  = "资源已齐，等待入库"
	copyPlaybillBody = "Emby 确认可看后会再通知你。"
)

func BuildWelcomeCard(userName string, opt WelcomeOptions) RichMessage {
	_ = userName
	_ = opt
	b := newBlockBuilder()
	b.photo("attach://welcome_hero")
	applyPage(b, welcomePage())
	card := b.card()
	card.Media = welcomeHeroMedia()
	return card.Rich()
}

func welcomePage() Page {
	return Page{
		Kicker:  copyKickerCinema,
		Heading: copyWelcomeH1,
		Tagline: copyWelcomeTag,
		Body:    copyWelcomeBody,
		Status:  copyWelcomeStat,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("搜索求片", "search:menu", types.ButtonStyleSuccess, "查看进度", "requests", types.ButtonStylePrimary),
			pair("帮助说明", "help", types.ButtonStylePrimary, "更多功能", "start_more", types.ButtonStylePrimary),
		},
	}
}

func BuildWelcomeMoreCard(opt WelcomeOptions) RichMessage {
	_ = opt
	return BuildPage(Page{
		Heading: copyMoreH1,
		Tagline: copyMoreTag,
		Body:    copyMoreBody,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("申请洗版", "wash", types.ButtonStylePrimary, "进入许愿", "start_wish", types.ButtonStylePrimary),
			pair("系统设置", "start_settings", types.ButtonStylePrimary, "问题反馈", "issue", types.ButtonStylePrimary),
			pair("查看进度", "requests", types.ButtonStylePrimary, "游戏中心", "game_menu", types.ButtonStylePrimary),
			full("返回首页", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildSearchPromptCard() RichMessage {
	return BuildPage(Page{
		Heading: copySearchH1,
		Tagline: copySearchTag,
		Body:    copySearchBody,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("搜索求片", "search:menu", types.ButtonStyleSuccess, "返回首页", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildHelpCard() RichMessage {
	return BuildPage(Page{
		Heading: copyHelpH1,
		Tagline: copyHelpTag,
		Body:    copyHelpBody,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("搜索求片", "search:menu", types.ButtonStyleSuccess, "返回首页", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildSettingsCard(bound bool) RichMessage {
	status := "账号还没绑定。绑定后才能把求片进度和下载对上。"
	if bound {
		status = "账号已绑定。通知、周报和进度都跟这个号走。"
	}
	return BuildPage(Page{
		Heading: "系统设置",
		Tagline: "绑定、通知、周报",
		Body:    status,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("通知设置", "notify_settings", types.ButtonStylePrimary, "绑定账号", "start_link", types.ButtonStylePrimary),
			pair("重置密码", "resetpw", types.ButtonStylePrimary, "我的反馈", "my_feedback", types.ButtonStylePrimary),
			pair("观影周报", "weekly_report", types.ButtonStylePrimary, "返回首页", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildHelpTopicCard(topic string) RichMessage {
	_ = topic
	return BuildHelpCard()
}

func BuildProgressEmptyCard(needBind bool) RichMessage {
	_ = needBind
	return BuildPage(Page{
		Heading: copyProgressH1,
		Tagline: copyProgressTag,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("返回首页", "start", types.ButtonStylePrimary, "刷新状态", "requests", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildWashPromptCard() RichMessage {
	return BuildPage(Page{
		Heading: "申请洗版",
		Tagline: "只换库里已经有的片",
		Body:    "把片名发给我。新版本确认可用前，当前版本会继续留着。",
		Buttons: [][]types.TelegramRichMessageButton{
			pair("查看进度", "requests", types.ButtonStylePrimary, "取消操作", "cancel", types.ButtonStyleDanger),
			full("返回首页", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

// PlaybillCard is B 节目单 bubble#progress.
type PlaybillCard struct {
	Title   string
	Tagline string
	Body    string
	Year    string
	Kind    string
	Next    string
	Refresh string
}

func BuildPlaybillCard(p PlaybillCard) Card {
	title := strings.TrimSpace(p.Title)
	tag := strings.TrimSpace(p.Tagline)
	if tag == "" {
		tag = copyPlaybillTag
	}
	body := strings.TrimSpace(p.Body)
	if body == "" {
		body = copyPlaybillBody
	}
	refresh := strings.TrimSpace(p.Refresh)
	if refresh == "" {
		refresh = "requests"
	}
	facts := [][]string{}
	if y := strings.TrimSpace(p.Year); y != "" && y != "0" {
		facts = append(facts, []string{"年份", y})
	}
	if k := strings.TrimSpace(p.Kind); k != "" {
		facts = append(facts, []string{"类型", k})
	}
	if n := strings.TrimSpace(p.Next); n != "" {
		facts = append(facts, []string{"下一步", n})
	}
	return BuildPage(Page{
		Kicker:  copyKickerNow,
		Heading: title,
		Tagline: tag,
		Body:    body,
		Facts:   facts,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("返回首页", "start", types.ButtonStylePrimary, "刷新状态", refresh, types.ButtonStylePrimary),
		},
	})
}

func playbillKind(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "tv", "电视剧", "剧集":
		return "剧集"
	case "movie", "电影":
		return "电影"
	default:
		if strings.TrimSpace(mediaType) == "" {
			return "影片"
		}
		return mediaType
	}
}

func playbillNext(state string) string {
	switch state {
	case "C":
		return "可播放"
	case "D":
		return "下载"
	case "REVIEWING", "P":
		return "审核"
	case "R", "S":
		return "找源"
	default:
		return "入库确认"
	}
}

func BuildStatusNoticeCard(title, status, sentence string, buttons ...types.TelegramRichMessageButton) Card {
	_ = buttons
	return BuildPlaybillCard(PlaybillCard{
		Title:   title,
		Tagline: status,
		Body:    sentence,
		Next:    "入库确认",
		Refresh: "requests",
	})
}
