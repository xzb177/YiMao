package richmessage

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/png"
	"strings"
	"testing"
)

func TestCinemaWelcomeMatchesMockupA(t *testing.T) {
	card := BuildWelcomeCard("", WelcomeOptions{})
	raw, _ := json.Marshal(card.Input())
	body := string(raw)
	md := card.Markdown
	for _, want := range []string{copyKickerCinema, copyWelcomeH1, copyWelcomeTag, copyWelcomeBody, copyWelcomeStat, "搜索求片", "查看进度", "帮助说明", "更多功能"} {
		if !strings.Contains(md, want) && !strings.Contains(body, want) {
			t.Fatalf("welcome missing %q md=%q body=%s", want, md, body)
		}
	}
	if strings.Contains(md, "云海求片助手") || strings.Contains(md, "首次加载") || strings.Contains(body, "web_app") {
		t.Fatalf("legacy welcome: %q %s", md, body)
	}
	if strings.Contains(body, "状态") {
		t.Fatalf("welcome must not wrap status in 状态 table: %s", body)
	}
	if !strings.Contains(body, `"type":"photo"`) || !strings.Contains(body, "attach://welcome_hero") {
		t.Fatalf("welcome missing photo: %s", body)
	}
	if len(WelcomeHeroPNG()) == 0 {
		t.Fatal("welcome hero png empty")
	}
	if strings.Count(body, `"style":"success"`) != 1 {
		t.Fatalf("success count %s", body)
	}
	if strings.Count(body, `"callback_data":"search:menu"`) != 1 {
		t.Fatalf("search:menu %s", body)
	}
}

func TestCinemaSearchHelpMoreCopy(t *testing.T) {
	search := BuildSearchPromptCard().Markdown
	for _, want := range []string{copySearchH1, copySearchTag, copySearchBody, "搜索求片", "查看进度", "返回首页"} {
		if !strings.Contains(search, want) {
			t.Fatalf("search missing %q in %q", want, search)
		}
	}
	help := BuildHelpCard().Markdown
	for _, want := range []string{copyHelpH1, copyHelpTag, copyHelpBody} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q in %q", want, help)
		}
	}
	more := BuildWelcomeMoreCard(WelcomeOptions{IsAdmin: true, MiniAppURL: "https://x"}).Markdown
	for _, want := range []string{copyMoreH1, copyMoreTag, copyMoreBody, "系统设置", "问题反馈", "观影画像", "返回首页"} {
		if !strings.Contains(more, want) {
			t.Fatalf("more missing %q in %q", want, more)
		}
	}
	// 申请洗版 / 进入许愿 / 查看进度 are on the welcome grid now and must not be
	// duplicated inside the secondary drawer.
	for _, promoted := range []string{"申请洗版", "进入许愿", "查看进度"} {
		if strings.Contains(more, promoted) {
			t.Fatalf("more duplicates promoted entry %q in %q", promoted, more)
		}
	}
}

func TestPlaybillProgressMatchesMockupB(t *testing.T) {
	card := BuildPlaybillCard(PlaybillCard{Title: "醉玲珑", Tagline: copyPlaybillTag, Body: copyPlaybillBody, Year: "2017", Kind: "剧集", Next: "入库确认", Refresh: "requests"})
	md := card.Markdown
	for _, want := range []string{copyKickerNow, "醉玲珑", copyPlaybillTag, copyPlaybillBody, "年份", "2017", "类型", "剧集", "下一步", "入库确认", "返回首页", "刷新状态"} {
		if !strings.Contains(md, want) {
			t.Fatalf("playbill missing %q in %q", want, md)
		}
	}
	if strings.Contains(md, "查看进度") {
		t.Fatalf("single title must not use 求片进度 as H1: %q", md)
	}
}

func TestWelcomeHeroPNGDecodes(t *testing.T) {
	data := WelcomeHeroPNG()
	if len(data) < 24 {
		t.Fatalf("hero too small: %d", len(data))
	}
	if data[0] != 0x89 || string(data[1:4]) != "PNG" {
		t.Fatalf("not a PNG signature: %x", data[:8])
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.Decode: %v", err)
	}
	if format != "png" {
		t.Fatalf("format=%s", format)
	}
	b := img.Bounds()
	if b.Dx() < 640 || b.Dy() < 360 {
		t.Fatalf("dims=%dx%d", b.Dx(), b.Dy())
	}
	t.Logf("image.Decode ok format=%s size=%dx%d bytes=%d", format, b.Dx(), b.Dy(), len(data))
}

func TestWelcomeTypeScaleAndLexicon(t *testing.T) {
	card := BuildWelcomeCard("", WelcomeOptions{})
	raw, _ := json.Marshal(card.Input())
	body := string(raw)
	if !strings.Contains(body, `"size":1`) || !strings.Contains(body, `"size":4`) {
		t.Fatalf("heading/kicker sizes missing: %s", body)
	}
	if !strings.Contains(body, `"type":"italic"`) {
		t.Fatalf("status must be italic, not a 状态 table: %s", body)
	}
	if strings.Count(body, `"style":"success"`) != 1 {
		t.Fatalf("only 搜索求片 is success: %s", body)
	}
	for _, leak := range []string{"立即求片", "云海求片助手", "🔍", "🎬", "🏠"} {
		if strings.Contains(body, leak) {
			t.Fatalf("lexicon leak %q", leak)
		}
	}
}
