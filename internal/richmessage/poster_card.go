package richmessage

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

type posterCardData struct {
	PosterURL  string
	Title      string
	Year       int
	Kind       string
	Status     string
	Rating     float64
	Runtime    int
	Seasons    int
	Episodes   int
	Overview   string
	Footer     string
	SeasonText string
	Pairs      [][]string
	Buttons    []types.TelegramRichMessageButton
}

func buildPosterCard(data posterCardData) Card {
	b := newBlockBuilder()
	title := strings.TrimSpace(data.Title)
	if title == "" {
		title = "未知影视"
	}
	b.photo(data.PosterURL)
	b.heading(title, 3)
	if data.Status != "" {
		b.bold(data.Status)
	}
	pairs := make([][]string, 0, 8)
	if data.Kind != "" {
		pairs = append(pairs, []string{"类型", data.Kind})
	}
	if data.Year > 0 {
		pairs = append(pairs, []string{"年份", fmt.Sprintf("%d", data.Year)})
	}
	if data.SeasonText != "" {
		pairs = append(pairs, []string{"季度", data.SeasonText})
	}
	if data.Rating > 0 {
		pairs = append(pairs, []string{"评分", fmt.Sprintf("%.1f", data.Rating)})
	}
	if data.Runtime > 0 {
		pairs = append(pairs, []string{"时长", formatRuntime(data.Runtime)})
	}
	if data.Seasons > 0 {
		seasons := fmt.Sprintf("%d 季", data.Seasons)
		if data.Episodes > 0 {
			seasons += fmt.Sprintf(" · %d 集", data.Episodes)
		}
		pairs = append(pairs, []string{"季集", seasons})
	}
	pairs = append(pairs, data.Pairs...)
	b.compactTable(pairs)
	if strings.TrimSpace(data.Overview) != "" {
		b.expandable("剧情", data.Overview)
	}
	if strings.TrimSpace(data.Footer) != "" {
		b.paragraph(data.Footer)
	}
	if len(data.Buttons) > 0 {
		b.buttonRow(data.Buttons...)
	}
	return b.card()
}

func formatRuntime(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	h, m := minutes/60, minutes%60
	if h > 0 {
		return fmt.Sprintf("%d小时%d分", h, m)
	}
	return fmt.Sprintf("%d分钟", m)
}

func mediaKind(mediaType string) string {
	switch mediaType {
	case "tv", "剧集":
		return "剧集"
	default:
		return "电影"
	}
}

// BuildRequesterReceiptCard is the user-visible request receipt.
func BuildRequesterReceiptCard(title string, year int, mediaType string, season int, status, footer string, posterURL string) Card {
	seasonText := ""
	if mediaType == "tv" || mediaType == "剧集" {
		if season > 0 {
			seasonText = fmt.Sprintf("第 %d 季", season)
		}
	}
	return buildPosterCard(posterCardData{
		PosterURL:  posterURL,
		Title:      title,
		Year:       year,
		Kind:       mediaKind(mediaType),
		Status:     status,
		SeasonText: seasonText,
		Footer:     footer,
		Buttons: []types.TelegramRichMessageButton{
			richButton("查看进度", "requests", "primary", false),
			richButton("返回首页", "start", "", false),
		},
	})
}
