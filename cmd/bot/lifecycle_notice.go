package main

import (
	"fmt"

	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/types"
)

func sendRichLifecycleNotice(tc *services.TelegramClient, userID int64, title string, year int, mediaType, heading, tagline, body string) error {
	yearStr := ""
	if year > 0 {
		yearStr = fmt.Sprintf("%d", year)
	}
	kind := "\u7535\u5f71"
	if mediaType == "tv" {
		kind = "\u5267\u96c6"
	}
	card := richmessage.BuildPlaybillCard(richmessage.PlaybillCard{Title: title, Tagline: tagline, Body: body, Year: yearStr, Kind: kind, Next: "\u67e5\u770b\u8fdb\u5ea6", Refresh: "my_requests"})
	_, err := tc.SendStructuredRichMessage(userID, card.Input(), nil)
	return err
}

func sendRichSeasonNotice(tc *services.TelegramClient, userID int64, tmdbID int, title string, season services.TVSeason) error {
	body := fmt.Sprintf("\u300a%s\u300b\u51fa\u4e86\u7b2c %d \u5b63\u3002", title, season.SeasonNumber)
	if season.AirDate != "" {
		body += fmt.Sprintf(" \u5f00\u64ad\u65e5\u671f\uff1a%s\u3002", season.AirDate)
	}
	card := richmessage.BuildPage(richmessage.Page{Kicker: "SEASON RADAR", Heading: "\u8ffd\u66f4\u63d0\u9192", Tagline: "\u65b0\u5b63\u5df2\u4e0a\u7ebf", Body: body, Buttons: [][]types.TelegramRichMessageButton{{{Text: "\u7533\u8bf7\u672c\u5b63", Style: "primary", CallbackData: fmt.Sprintf("request:id:%d:type:tv:season:%d", tmdbID, season.SeasonNumber)}, {Text: "\u67e5\u770b\u8fdb\u5ea6", Style: "primary", CallbackData: "requests"}, {Text: "\u641c\u7d22\u6c42\u7247", Style: "success", CallbackData: "search:menu"}}}})
	_, err := tc.SendStructuredRichMessage(userID, card.Input(), nil)
	return err
}
