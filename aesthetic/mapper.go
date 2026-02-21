package aesthetic

import (
	"fmt"
	"strings"
)

type Realm struct {
	ID    int
	Name  string
	Icon  string
	Color string
}

var Realms = []Realm{
	{RealmInit, "初识", "✧", "#9E9E9E"},
	{RealmFamiliar, "熟稔", "✦", "#4CAF50"},
	{RealmProfound, "深厚", "✪", "#2196F3"},
	{RealmLegendary, "传奇", "★", "#FFD700"},
}

type QuotaLevel struct {
	Name  string
	Icon  string
	Color string
}

var QuotaLevels = []QuotaLevel{
	{QuotaNameScant, "微薄", "◐", "#BDBDBD"},
	{QuotaNameAbund, "丰沛", "◆", "#4CAF50"},
	{QuotaNameExcell, "卓越", "◈", "#2196F3"},
	{QuotaNameApex, "巅峰", "◉", "#FFD700"},
}

func GetRealm(realmID int) Realm {
	if realmID >= 0 && realmID < len(Realms) {
		return Realms[realmID]
	}
	return Realms[0]
}

func GetQuotaLevel(points int) QuotaLevel {
	switch {
	case points >= 100:
		return QuotaLevels[3]
	case points >= 50:
		return QuotaLevels[2]
	case points >= 20:
		return QuotaLevels[1]
	default:
		return QuotaLevels[0]
	}
}

func FormatQuotaStars(used, limit int) string {
	var builder strings.Builder

	for i := 0; i < limit; i++ {
		if i < used {
			builder.WriteString("◈")
		} else {
			builder.WriteString("◇")
		}
		if i < limit-1 {
			builder.WriteString(" ")
		}
	}

	return builder.String()
}

func FormatEnergy(energy int) string {
	switch {
	case energy >= 10:
		return "✦✦✦"
	case energy >= 7:
		return "✦✦·"
	case energy >= 4:
		return "✦··"
	case energy >= 1:
		return "✦···"
	default:
		return "····"
	}
}

func FormatStatus(status string) string {
	switch status {
	case WishStatusDormant:
		return "沉眠"
	case WishStatusGlow:
		return "微光"
	case WishStatusIgnited:
		return "点燃"
	case WishStatusFaded:
		return "消散"
	default:
		return "未知"
	}
}

func FormatMediaType(mediaType string) string {
	switch mediaType {
	case "movie":
		return "光影"
	case "tv":
		return "剧集"
	default:
		return "作品"
	}
}

func FormatWishStatusIcon(status string) string {
	switch status {
	case WishStatusDormant:
		return "◌"
	case WishStatusGlow:
		return "✧"
	case WishStatusIgnited:
		return "✦"
	case WishStatusFaded:
		return "·"
	default:
		return "?"
	}
}

func BuildPoeticHeader(binding *Binding) string {
	realm := GetRealm(binding.Realm)
	quota := GetQuotaLevel(binding.Points)

	movieStars := FormatQuotaStars(2-binding.MovieQuota, 2)
	tvStars := FormatQuotaStars(2-binding.TvQuota, 2)

	return fmt.Sprintf(
		"%s %s\n\n%s  %s\n\n光影 %s\n剧集 %s",
		realm.Icon, realm.Name,
		quota.Icon, quota.Name,
		movieStars,
		tvStars,
	)
}

func BuildPoeticWishCard(wish *Wish) string {
	energy := FormatEnergy(wish.Energy)
	status := FormatStatus(wish.Status)
	statusIcon := FormatWishStatusIcon(wish.Status)

	return fmt.Sprintf(
		"%s %s\n\n%s %s",
		statusIcon, wish.Title,
		energy, status,
	)
}

func BuildPoeticWishList(wishes []Wish) string {
	if len(wishes) == 0 {
		return "· 心愿清单空空如也 ·\n\n──────\n\n用 /许愿 诉说你的期待"
	}

	var builder strings.Builder
	builder.WriteString("✧ 心愿清单 ✧\n\n──────\n\n")

	for _, wish := range wishes {
		energy := FormatEnergy(wish.Energy)
		status := FormatStatus(wish.Status)
		statusIcon := FormatWishStatusIcon(wish.Status)

		builder.WriteString(fmt.Sprintf("%s %s\n%s %s\n\n", statusIcon, wish.Title, energy, status))
	}

	builder.WriteString("──────\n\n· 用 /许愿 添加期待 ·")

	return builder.String()
}

func BuildPoeticDetail(title string, overview string, year int, rating float64) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("— %s —\n\n", title))

	if year > 0 {
		builder.WriteString(fmt.Sprintf("年份 %d · ", year))
	}

	if rating > 0 {
		stars := "★"
		if rating >= 6 {
			stars = "★★"
		}
		if rating >= 8 {
			stars = "★★★"
		}
		builder.WriteString(fmt.Sprintf("%s", stars))
	}

	builder.WriteString("\n\n")

	if overview != "" {
		if len(overview) > 80 {
			overview = overview[:77] + "..."
		}
		builder.WriteString(overview)
		builder.WriteString("\n\n")
	}

	return builder.String()
}

func BuildPoeticActionPanel(title string, canRequest bool, quotaMsg string) string {
	var builder strings.Builder

	builder.WriteString("──────\n\n")

	if canRequest {
		builder.WriteString(fmt.Sprintf("✦ 愿望可达成\n\n%s", quotaMsg))
	} else {
		builder.WriteString(fmt.Sprintf("◌ 暂时无法许愿\n\n%s", quotaMsg))
	}

	return builder.String()
}
