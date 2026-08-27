package richmessage

import (
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

func BuildWelcomeCard(userName string, opt WelcomeOptions) RichMessage {
	_ = opt
	b := newBlockBuilder()
	b.photo("attach://welcome_hero")
	applyPage(b, welcomePage(userName))
	card := b.card()
	card.Media = welcomeHeroMedia()
	return card.Rich()
}

func BuildWelcomeMoreCard(opt WelcomeOptions) RichMessage {
	return BuildPage(morePage(opt)).Rich()
}

func welcomePage(userName string) Page {
	_ = userName
	return Page{
		Heading: "云海求片",
		Tagline: "想看的，交给云海",
		Body:    "直接发片名，或点搜索。提交后可在进度里查到。",
		Facts:   [][]string{{"状态", "在线 · 可求片"}},
		Buttons: [][]types.TelegramRichMessageButton{
			pair("搜索求片", "search:menu", types.ButtonStyleSuccess, "求片进度", "requests", types.ButtonStylePrimary),
			pair("帮助", "help", types.ButtonStylePrimary, "更多", "start_more", types.ButtonStylePrimary),
		},
	}
}

func morePage(opt WelcomeOptions) Page {
	rows := [][]types.TelegramRichMessageButton{
		pair("洗版", "wash", types.ButtonStyleSuccess, "游戏中心", "game_menu", types.ButtonStylePrimary),
		pair("许愿池", "start_wish", types.ButtonStylePrimary, "大家最近在求", "request_heat", types.ButtonStylePrimary),
		pair("设置", "start_settings", types.ButtonStylePrimary, "遇到问题", "issue", types.ButtonStylePrimary),
		pair("我的进度", "start_requests", types.ButtonStylePrimary, "返回", "start", types.ButtonStylePrimary),
	}
	if opt.IsAdmin {
		rows = append(rows, full("管理", "admin_menu", types.ButtonStylePrimary))
	}
	if u := strings.TrimSpace(opt.MiniAppURL); u != "" {
		rows = append(rows, []types.TelegramRichMessageButton{richWebAppButton("打开云海小程序", u, "")})
	}
	return Page{Heading: "更多", Tagline: "洗版、许愿、设置都在这", Body: "不影响正常求片。点返回回到主页。", Buttons: rows}
}

func BuildSearchPromptCard() RichMessage {
	return BuildPage(Page{
		Heading: "搜索求片",
		Tagline: "发中文或英文片名",
		Body:    "点结果再提交。提交后可在进度里查到。",
		Buttons: [][]types.TelegramRichMessageButton{
			pair("历史记录", "search_history_menu", types.ButtonStylePrimary, "主菜单", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildSettingsCard(bound bool) RichMessage {
	status := "账号还没绑定。绑定后才能把求片进度和下载对上。"
	if bound {
		status = "账号已绑定。通知、周报和进度都跟这个号走。"
	}
	return BuildPage(Page{
		Heading: "设置",
		Tagline: "绑定、通知、周报",
		Body:    status,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("通知设置", "notify_settings", types.ButtonStylePrimary, "绑定账号", "start_link", types.ButtonStylePrimary),
			pair("重置密码", "resetpw", types.ButtonStylePrimary, "我的反馈", "my_feedback", types.ButtonStylePrimary),
			pair("观影周报", "weekly_report", types.ButtonStylePrimary, "帮助", "help", types.ButtonStylePrimary),
			full("主菜单", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildHelpCard() RichMessage {
	return BuildPage(Page{
		Heading: "帮助",
		Tagline: "求片、绑定、通知都能在这里看懂",
		Body:    "选一个问题。看完后点返回，或直接去搜索。",
		Buttons: [][]types.TelegramRichMessageButton{
			pair("怎么求片", "help_topic:topic:search", types.ButtonStylePrimary, "怎么绑定", "help_topic:topic:link", types.ButtonStylePrimary),
			pair("请求失败", "help_topic:topic:failed", types.ButtonStylePrimary, "没收到通知", "help_topic:topic:notify", types.ButtonStylePrimary),
			pair("其他问题", "help_topic:topic:other", types.ButtonStylePrimary, "主菜单", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}

func BuildHelpTopicCard(topic string) RichMessage {
	heading, tagline, body := "其他问题", "管理员会看你的反馈", "点遇到问题写下片名和情况。处理完会在这里回你。"
	switch topic {
	case "search":
		heading, tagline, body = "怎么求片", "点搜索或直接发片名", "选中结果后点立即求片。提交后可在进度里看到审核和下载。"
	case "link":
		heading, tagline, body = "怎么绑定", "把云海账号和这个 Telegram 对上", "点绑定账号，按提示输入用户名和密码。没有账号会自动创建。也可以发 /link 用户名 密码。"
	case "failed":
		heading, tagline, body = "请求失败", "多半是暂时没源", "到进度里点刷新再等一轮。还没有的话可以许愿，出源后会通知你。"
	case "notify":
		heading, tagline, body = "没收到通知", "先和我私聊一句", "群里点过按钮但没私聊过的话，我发不出信。绑定也要用同一个 Telegram 号。"
	}
	return BuildPage(Page{
		Heading: heading,
		Tagline: tagline,
		Body:    body,
		Buttons: [][]types.TelegramRichMessageButton{
			pair("返回帮助", "help", types.ButtonStylePrimary, "搜索求片", "search:menu", types.ButtonStyleSuccess),
		},
	}).Rich()
}

func BuildProgressEmptyCard(needBind bool) RichMessage {
	body := "还没有的话，去搜索求一片。"
	rows := [][]types.TelegramRichMessageButton{
		pair("搜索求片", "search:menu", types.ButtonStyleSuccess, "主菜单", "start", types.ButtonStylePrimary),
	}
	if needBind {
		body = "求片进度要绑定账号后才能和下载对上。问题和洗版工单不用绑定也能看。"
		rows = [][]types.TelegramRichMessageButton{
			pair("搜索求片", "search:menu", types.ButtonStyleSuccess, "绑定账号", "link", types.ButtonStylePrimary),
			full("主菜单", "start", types.ButtonStylePrimary),
		}
	}
	return BuildPage(Page{
		Heading: "求片进度",
		Tagline: "提交过的片在这里",
		Body:    body,
		Buttons: rows,
	}).Rich()
}

func BuildWashPromptCard() RichMessage {
	return BuildPage(Page{
		Heading: "洗版",
		Tagline: "只换库里已经有的片",
		Body:    "把片名发给我。新版本确认可用前，当前版本会继续留着。",
		Buttons: [][]types.TelegramRichMessageButton{
			pair("我的进度", "start_requests", types.ButtonStylePrimary, "取消", "cancel", types.ButtonStyleDanger),
			full("主菜单", "start", types.ButtonStylePrimary),
		},
	}).Rich()
}
