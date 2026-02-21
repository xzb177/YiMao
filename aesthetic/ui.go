package aesthetic

import (
	"fmt"
	"strings"
)

type PoeticUI struct {
	TelegramID int64
	Username   string
}

type PoeticState struct {
	Binding    *Binding
	Wishes     []Wish
	Category   string
}

func NewPoeticUI(tgID int64, username string) *PoeticUI {
	return &PoeticUI{
		TelegramID: tgID,
		Username:   username,
	}
}

func (p *PoeticUI) RenderProfile(binding *Binding) string {
	realm := GetRealm(binding.Realm)
	quota := GetQuotaLevel(binding.Points)

	movieStars := FormatQuotaStars(binding.MovieQuota, 2)
	tvStars := FormatQuotaStars(binding.TvQuota, 2)

	builder := &strings.Builder{}

	builder.WriteString(fmt.Sprintf("%s %s\n\n", realm.Icon, realm.Name))
	builder.WriteString(fmt.Sprintf("%s %s\n\n", quota.Icon, quota.Name))

	builder.WriteString("──────\n\n")
	builder.WriteString(fmt.Sprintf("光影 %s\n\n", movieStars))
	builder.WriteString(fmt.Sprintf("剧集 %s\n\n", tvStars))

	builder.WriteString("──────\n\n")
	builder.WriteString(fmt.Sprintf("累积 %d 点 · 境界 %s", binding.Points, realm.Name))

	return builder.String()
}

func (p *PoeticUI) RenderWishCard(wish *Wish) string {
	energy := FormatEnergy(wish.Energy)
	status := FormatStatus(wish.Status)
	statusIcon := FormatWishStatusIcon(wish.Status)

	builder := &strings.Builder{}

	builder.WriteString(fmt.Sprintf("%s %s\n\n", statusIcon, wish.Title))
	builder.WriteString(fmt.Sprintf("%s %s\n\n", energy, status))

	if wish.Status == WishStatusIgnited {
		builder.WriteString("──────\n\n")
		builder.WriteString("✦ 已点燃 · 星空已回应")
	} else if wish.Energy >= 7 {
		builder.WriteString("──────\n\n")
		builder.WriteString("✦ 能量充沛 · 即将点燃")
	} else {
		builder.WriteString("──────\n\n")
		builder.WriteString(fmt.Sprintf("还需 %d 点能量点燃", 7-wish.Energy))
	}

	return builder.String()
}

func (p *PoeticUI) RenderWishList(wishes []Wish) string {
	if len(wishes) == 0 {
		return "· 心愿清单空空如也 ·\n\n──────\n\n用 /许愿 诉说你的期待"
	}

	builder := &strings.Builder{}

	builder.WriteString("✧ 心愿清单 ✧\n\n")
	builder.WriteString("──────\n\n")

	for _, wish := range wishes {
		energy := FormatEnergy(wish.Energy)
		status := FormatStatus(wish.Status)
		statusIcon := FormatWishStatusIcon(wish.Status)

		builder.WriteString(fmt.Sprintf("%s %s\n", statusIcon, wish.Title))
		builder.WriteString(fmt.Sprintf("%s %s\n\n", energy, status))
	}

	builder.WriteString("──────\n\n")
	builder.WriteString("· 用 /星火 为心愿注入能量 ·")

	return builder.String()
}

func (p *PoeticUI) RenderDetail(title string, overview string, year int, rating float64, posterPath string) string {
	builder := &strings.Builder{}

	builder.WriteString(fmt.Sprintf("— %s —\n\n", title))

	if year > 0 {
		builder.WriteString(fmt.Sprintf("年份 %d · ", year))
	}

	if rating > 0 {
		stars := ""
		if rating >= 6 {
			stars = "★"
		}
		if rating >= 7 {
			stars = "★★"
		}
		if rating >= 8 {
			stars = "★★★"
		}
		builder.WriteString(fmt.Sprintf("%s", stars))
	}

	builder.WriteString("\n\n")

	if overview != "" {
		if len(overview) > 100 {
			overview = overview[:97] + "..."
		}
		builder.WriteString(overview)
		builder.WriteString("\n\n")
	}

	builder.WriteString("──────\n\n")

	return builder.String()
}

func (p *PoeticUI) RenderActionPanel(title string, canRequest bool, binding *Binding, tmdbID int, mediaType string) string {
	builder := &strings.Builder{}

	builder.WriteString("──────\n\n")

	if canRequest {
		remaining := binding.MovieQuota + binding.TvQuota
		builder.WriteString(fmt.Sprintf("✦ 愿望可达成\n\n今日还可许愿 %d 次", remaining))
	} else {
		builder.WriteString("◌ 暂时无法许愿\n\n今日星火已耗尽\n\n明日黎明时重置")
	}

	return builder.String()
}

func (p *PoeticUI) RenderIgnitePanel(wishes []Wish) string {
	if len(wishes) == 0 {
		return "◌ 心愿清单空空如也\n\n──────\n\n用 /许愿 添加期待"
	}

	builder := &strings.Builder{}

	builder.WriteString("✧ 选择要注入能量的心愿 ✧\n\n")
	builder.WriteString("──────\n\n")

	for _, wish := range wishes {
		if wish.Status == WishStatusDormant || wish.Status == WishStatusGlow {
			energy := FormatEnergy(wish.Energy)
			builder.WriteString(fmt.Sprintf("✦ %s (%s)\n", wish.Title, energy))
		}
	}

	builder.WriteString("\n──────\n\n")
	builder.WriteString("· 每次注入增加一点能量 ·\n")
	builder.WriteString("· 达到七点能量自动点燃 ·")

	return builder.String()
}

func (p *PoeticUI) RenderIgniteSuccess(wish *Wish) string {
	oldEnergy := wish.Energy - 1

	builder := &strings.Builder{}

	builder.WriteString("✦ 能量已注入 ✦\n\n")
	builder.WriteString("──────\n\n")
	builder.WriteString(p.RenderWishCard(wish))
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("%s → %s\n\n", FormatEnergy(oldEnergy), FormatEnergy(wish.Energy)))

	if wish.Energy >= 7 {
		builder.WriteString("──────\n\n")
		builder.WriteString("✦ 能量充沛 · 即将点燃")
	} else {
		builder.WriteString(fmt.Sprintf("──────\n\n"))
		builder.WriteString(fmt.Sprintf("还需 %d 点能量点燃", 7-wish.Energy))
	}

	return builder.String()
}

func (p *PoeticUI) RenderWishCreated(wish *Wish) string {
	builder := &strings.Builder{}

	builder.WriteString("✧ 愿望已记录 ✧\n\n")
	builder.WriteString("──────\n\n")
	builder.WriteString(p.RenderWishCard(wish))
	builder.WriteString("\n\n")
	builder.WriteString("──────\n\n")
	builder.WriteString("✦ 用 /星火 为心愿注入能量 ✦\n\n")
	builder.WriteString("心火点燃时，星空会回应")

	return builder.String()
}

func (p *PoeticUI) RenderWishRemoved(title string) string {
	return fmt.Sprintf(
		"✧ 心愿已消散 ✧\n\n──────\n\n· %s ·\n\n它将化作星尘回归虚空",
		title,
	)
}

func (p *PoeticUI) RenderQuotaExhausted() string {
	return "◌ 今日星火已耗尽\n\n──────\n\n明日黎明时重置"
}

func (p *PoeticUI) RenderSearchResults(results []SearchResult, page, total int) string {
	builder := &strings.Builder{}

	builder.WriteString("— 搜索结果 —\n\n")
	builder.WriteString("──────\n\n")

	for i, item := range results {
		num := i + 1 + page*10
		emoji := "🎬"
		if item.MediaType == "tv" {
			emoji = "📺"
		}

		builder.WriteString(fmt.Sprintf("%s %d. %s", emoji, num, item.Title))

		if item.Year > 0 {
			builder.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}

		builder.WriteString("\n")
	}

	builder.WriteString("\n──────\n\n")
	builder.WriteString("· 回复数字查看详情 ·")

	return builder.String()
}

func (p *PoeticUI) RenderMakeWishIntro(canRequest bool, binding *Binding) string {
	builder := &strings.Builder{}

	builder.WriteString("✧ 许愿仪式 ✧\n\n")
	builder.WriteString("──────\n\n")

	if canRequest {
		remaining := binding.MovieQuota + binding.TvQuota
		builder.WriteString(fmt.Sprintf("✦ 今日还可许愿 %d 次", remaining))
	} else {
		builder.WriteString("◌ 今日星火已耗尽\n\n明日黎明时重置")
	}

	builder.WriteString("\n\n──────\n\n")
	builder.WriteString("请发送你想看的影视名称\n\n· 或直接粘贴 Jellyfin 链接 ·")

	return builder.String()
}

func (p *PoeticUI) RenderLinkParsed(title string, tmdbID int, mediaType string) string {
	builder := &strings.Builder{}

	builder.WriteString("✧ 链接已解析 ✧\n\n")
	builder.WriteString("──────\n\n")
	builder.WriteString(fmt.Sprintf("· %s ·\n", title))
	builder.WriteString(fmt.Sprintf("TMDB ID: %d\n\n", tmdbID))
	builder.WriteString("──────\n\n")
	builder.WriteString("✦ 确认许愿？")

	return builder.String()
}

type SearchResult struct {
	Title     string
	Year      int
	MediaType string
	Overview  string
	PosterPath string
	TmdbID    int
}

func FormatQuotaProgress(used, total int) string {
	var builder strings.Builder

	for i := 0; i < total; i++ {
		if i < used {
			builder.WriteString("◈")
		} else {
			builder.WriteString("◇")
		}
	}

	return builder.String()
}

func FormatRealmProgress(realm int) string {
	realmInfo := GetRealm(realm)
	progress := ""

	switch realm {
	case RealmInit:
		progress = "···"
	case RealmFamiliar:
		progress = "✦··"
	case RealmProfound:
		progress = "✦✦·"
	case RealmLegendary:
		progress = "✦✦✦"
	}

	return fmt.Sprintf("%s %s", realmInfo.Icon, progress)
}

func FormatPointsLevel(points int) string {
	level := GetQuotaLevel(points)
	return fmt.Sprintf("%s %s", level.Icon, level.Name)
}
