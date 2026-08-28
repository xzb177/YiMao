package richmessage

import (
	"strings"
	"testing"
)

func TestBuildWashReviewNotifyCardIsAdministratorSpecific(t *testing.T) {
	card := BuildWashReviewNotifyCard(ReviewNotifyData{Title: "银翼杀手", Year: 1982, MediaType: "电影", MediaIcon: "🎬", UserName: "影迷", UserID: 42, Overview: "希望替换为更好的版本"})
	for _, want := range []string{"新洗版工单", "银翼杀手", "影迷", "仅管理员可处理", "保留现有版本", "批准或拒绝"} {
		if !strings.Contains(card.Markdown, want) {
			t.Fatalf("wash review card missing %q:\n%s", want, card.Markdown)
		}
	}
	for _, forbidden := range []string{"新求片", "自动下载", "MoviePilot"} {
		if strings.Contains(card.Markdown, forbidden) {
			t.Fatalf("wash review card contains misleading text %q:\n%s", forbidden, card.Markdown)
		}
	}
}

func TestBuildWashApprovedCardExplainsPrivateWorkOrderProgress(t *testing.T) {
	card := BuildWashApprovedCard("银翼杀手", 1982, "🎬")
	for _, want := range []string{"洗版工单已批准", "银翼杀手", "管理员处理并验证", "查看进度", "完成后通知你"} {
		if !strings.Contains(card.Markdown, want) {
			t.Fatalf("wash approved card missing %q:\n%s", want, card.Markdown)
		}
	}
	for _, forbidden := range []string{"正在寻找资源", "自动下载", "求片"} {
		if strings.Contains(card.Markdown, forbidden) {
			t.Fatalf("wash approved card contains request copy %q:\n%s", forbidden, card.Markdown)
		}
	}
}
