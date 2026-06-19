package richmessage

import (
	"fmt"
	"strings"
)

// MediaInfo represents media information
type MediaInfo struct {
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Rating    float64  `json:"rating"`
	Genres    []string `json:"genres"`
	Overview  string   `json:"overview"`
	PosterURL string   `json:"poster_url"`
	TMDBID    int      `json:"tmdb_id"`
	MediaType string   `json:"media_type"`
}

// BuildMediaInfoCard builds a rich media info card
func BuildMediaInfoCard(info MediaInfo) RichMessage {
	builder := NewBuilder()

	// Heading (handle empty title)
	title := info.Title
	if title == "" {
		title = "未知影视"
	}
	builder.Heading(fmt.Sprintf("📺 《%s》", title), 2)

	// Info table
	headers := []string{"项目", "详情"}
	rows := [][]string{
		{"评分", fmt.Sprintf("⭐ %.1f", info.Rating)},
		{"年份", fmt.Sprintf("%d", info.Year)},
		{"类型", strings.Join(info.Genres, "/")},
	}
	builder.Table(headers, rows)

	// Overview in collapsible section (closed by default)
	if info.Overview != "" {
		builder.Details("📝 剧情简介（点击展开）", info.Overview, false)
	}

	return builder.Build()
}

// SubscriptionStatus represents subscription status
type SubscriptionStatus struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Progress int    `json:"progress"` // 0-100
}

// BuildWelcomeMessage builds the Rich Message welcome page for /start
func BuildWelcomeMessage(userName string, hasAI bool) RichMessage {
	builder := NewBuilder()

	// Heading with optional user name
	if userName != "" {
		builder.Heading(fmt.Sprintf("🌊 云海影视 · %s", userName), 2)
	} else {
		builder.Heading("🌊 云海影视", 2)
	}

	builder.BoldParagraph("你的私人选片师 · 随时为你找片")
	builder.Divider()

	// Features table
	headers := []string{"功能", "说明"}
	rows := [][]string{
		{"🔍 搜片名", "直接发片名，中英文都行"},
		{"🎭 心情推荐", "告诉我你想看什么类型的"},
		{"📊 我的求片", "查看请求和订阅进度"},
		{"🎲 随机盲盒", "不知道看什么？试试运气"},
		{"🔥 热门趋势", "本周大家都在看什么"},
	}
	if hasAI {
		rows = append(rows, []string{"🤖 AI 聊天", "用自然语言描述你的口味"})
	}
	builder.Table(headers, rows)
	builder.Divider()

	// Quick tips (collapsible)
	tips := "• 直接发送片名即可搜索\n• 支持中英文、电影、剧集\n• 点击结果可查看详情和求片\n• 去许愿池许愿，有源第一时间通知\n• 我的请求里查看所有求片进度"
	builder.Details("💡 快速上手", tips, false)

	return builder.Build()
}

// DailySummaryMovie represents a movie for the daily summary card
type DailySummaryMovie struct {
	Title string
	Year  int
}

// DailySummarySeries represents a series for the daily summary card
type DailySummarySeries struct {
	Title string
}

// WeeklyReportData holds data for the weekly report card
type WeeklyReportData struct {
	UserName        string
	WeekStart       string
	WeekEnd         string
	SearchCount     int
	RequestCount    int
	ApprovedCount   int
	MovieQuota      string // "∞" or number
	TVQuota         string // "∞" or number
	BehaviorTags    []string
	TopSearches     []string
	GenrePrefs      map[string]int
	Recommendations []string
}

// RecommendMovie holds data for a daily recommendation card
type RecommendMovie struct {
	Title string
	Year  int
}

// WishFoundData holds data for a wish found notification card
type WishFoundData struct {
	Title       string
	Year        int
	MediaType   string // "movie" / "tv"
	Season      int
	FoundDetail string
}

// InstantNotifyData holds data for an instant notification card
type InstantNotifyData struct {
	Title        string
	Year         int
	SeriesName   string
	SeasonNum    int
	EpisodeStart int
	EpisodeEnd   int
	EpisodeCount int
	MediaType    string // "movie" / "tv" / "anime"
	Quality      string
	Rating       float64
	Category     string
	FileSize     string
	FileCount    int
	Time         string // "15:04"
	ImageURL     string
}

// BuildWeeklyReportCard builds a Rich Message weekly report
func BuildWeeklyReportCard(data WeeklyReportData) RichMessage {
	builder := NewBuilder()

	builder.Heading("📊 观影周报", 2)
	builder.BoldParagraph(fmt.Sprintf("%s · %s - %s", data.UserName, data.WeekStart, data.WeekEnd))
	builder.Divider()

	// Stats table
	headers := []string{"指标", "数据"}
	rows := [][]string{
		{"🔍 搜索", fmt.Sprintf("%d 次", data.SearchCount)},
		{"📋 求片", fmt.Sprintf("%d 次", data.RequestCount)},
	}
	if data.ApprovedCount > 0 {
		rows = append(rows, []string{"✅ 通过", fmt.Sprintf("%d 个", data.ApprovedCount)})
	}
	rows = append(rows, []string{"💎 剩余配额", fmt.Sprintf("电影 %s / 剧集 %s", data.MovieQuota, data.TVQuota)})
	builder.Table(headers, rows)
	builder.Divider()

	// Behavior tags
	if len(data.BehaviorTags) > 0 {
		builder.Heading("🏷️ 本周标签", 3)
		for _, tag := range data.BehaviorTags {
			builder.Paragraph(fmt.Sprintf("• %s", tag))
		}
	}

	// Top searches
	if len(data.TopSearches) > 0 {
		builder.Heading("🔥 热搜关键词", 3)
		for _, s := range data.TopSearches {
			builder.Paragraph(fmt.Sprintf("• %s", s))
		}
	}

	// Genre preferences
	if len(data.GenrePrefs) > 0 {
		builder.Heading("🎭 类型偏好", 3)
		for genre, count := range data.GenrePrefs {
			builder.Paragraph(fmt.Sprintf("• %s：%d 次", genre, count))
		}
	}

	// AI recommendations
	if len(data.Recommendations) > 0 {
		builder.Heading("💡 专属建议", 3)
		for _, rec := range data.Recommendations {
			builder.Paragraph(fmt.Sprintf("• %s", rec))
		}
	}

	return builder.Build()
}

// BuildDailyRecommendCard builds a Rich Message daily recommendation
func BuildDailyRecommendCard(movies []RecommendMovie) RichMessage {
	builder := NewBuilder()

	builder.Heading("🎬 每日推荐", 2)
	builder.BoldParagraph("今日精选 · 云海影视")
	builder.Divider()

	for i, m := range movies {
		if m.Year > 1900 && m.Year < 2100 {
			builder.Paragraph(fmt.Sprintf("%d. %s (%d)", i+1, m.Title, m.Year))
		} else {
			builder.Paragraph(fmt.Sprintf("%d. %s", i+1, m.Title))
		}
	}

	return builder.Build()
}

// BuildWishFoundCard builds a Rich Message wish fulfilled notification
func BuildWishFoundCard(data WishFoundData) RichMessage {
	builder := NewBuilder()

	builder.Heading("🎉 许愿出源啦！", 2)

	title := data.Title
	if data.Year > 1900 && data.Year < 2100 {
		title += fmt.Sprintf(" (%d)", data.Year)
	}
	if data.MediaType == "tv" && data.Season > 0 {
		title += fmt.Sprintf(" 第%d季", data.Season)
	}
	builder.BoldParagraph(title)
	builder.Divider()

	if data.FoundDetail != "" {
		builder.Heading("📦 命中详情", 3)
		builder.Paragraph(data.FoundDetail)
	}

	builder.Italic("点下面按钮即可发起求片（需要你确认）")

	return builder.Build()
}

// BuildInstantNotifyCard builds a Rich Message instant notification for new media
func BuildInstantNotifyCard(data InstantNotifyData) RichMessage {
	builder := NewBuilder()

	// Emoji based on type
	emoji := "🎬"
	switch data.MediaType {
	case "anime":
		emoji = "🎨"
	case "tv":
		emoji = "📺"
	case "movie":
		emoji = "🎥"
	}

	// Build display title
	displayTitle := ""
	if data.SeriesName != "" {
		displayTitle = data.SeriesName
		if data.Year > 1900 && data.Year < 2100 {
			displayTitle += fmt.Sprintf(" (%d)", data.Year)
		}
		if data.SeasonNum > 0 {
			displayTitle += fmt.Sprintf(" S%02d", data.SeasonNum)
		}
		if data.EpisodeCount > 0 {
			if data.EpisodeStart > 0 && data.EpisodeEnd > 0 {
				displayTitle += fmt.Sprintf(" E%02d-E%02d", data.EpisodeStart, data.EpisodeEnd)
			} else if data.EpisodeCount == 1 {
				displayTitle += fmt.Sprintf(" E%02d", data.EpisodeStart)
			}
		}
	} else {
		displayTitle = data.Title
		if data.Year > 1900 && data.Year < 2100 {
			displayTitle += fmt.Sprintf(" (%d)", data.Year)
		}
	}

	builder.Heading(fmt.Sprintf("%s 新入库", emoji), 2)
	builder.BoldParagraph(displayTitle)
	builder.Divider()

	// Info table
	headers := []string{"项目", "详情"}
	rows := [][]string{}
	if data.Category != "" {
		rows = append(rows, []string{"🏷️ 类别", data.Category})
	}
	if data.Quality != "" {
		rows = append(rows, []string{"💎 质量", data.Quality})
	}
	if data.Rating > 0 {
		rows = append(rows, []string{"⭐ 评分", fmt.Sprintf("%.1f", data.Rating)})
	}
	if data.FileSize != "" {
		rows = append(rows, []string{"📦 大小", data.FileSize})
	}
	if data.FileCount > 0 {
		rows = append(rows, []string{"📁 文件", fmt.Sprintf("%d 个", data.FileCount)})
	}
	if len(rows) > 0 {
		builder.Table(headers, rows)
	}

	builder.Italic(fmt.Sprintf("🕒 %s", data.Time))

	return builder.Build()
}

// BuildDailySummaryCard builds a Rich Message daily summary for media notifications
func BuildDailySummaryCard(dateStr string, movies []DailySummaryMovie, series []DailySummarySeries, totalCount int) RichMessage {
	builder := NewBuilder()

	// Header
	builder.Heading("📥 今日入库", 2)
	builder.BoldParagraph(fmt.Sprintf("%s · 云海影视", dateStr))
	builder.Divider()

	// Stats table
	headers := []string{"类型", "数量"}
	rows := [][]string{}
	if len(movies) > 0 {
		rows = append(rows, []string{"🎥 新增电影", fmt.Sprintf("%d 部", len(movies))})
	}
	if len(series) > 0 {
		rows = append(rows, []string{"📺 剧集更新", fmt.Sprintf("%d 部", len(series))})
	}
	rows = append(rows, []string{"📊 合计", fmt.Sprintf("%d 部", totalCount)})
	builder.Table(headers, rows)
	builder.Divider()

	// Movies section
	if len(movies) > 0 {
		builder.Heading(fmt.Sprintf("🎥 新增电影（%d 部）", len(movies)), 3)
		for _, m := range movies {
			if m.Year > 1900 && m.Year < 2100 {
				builder.Paragraph(fmt.Sprintf("• %s (%d)", m.Title, m.Year))
			} else {
				builder.Paragraph(fmt.Sprintf("• %s", m.Title))
			}
		}
	}

	// Series section
	if len(series) > 0 {
		builder.Heading(fmt.Sprintf("📺 剧集更新（%d 部）", len(series)), 3)
		for _, s := range series {
			builder.Paragraph(fmt.Sprintf("• %s", s.Title))
		}
	}

	return builder.Build()
}

// BuildSubscriptionDashboard builds a subscription dashboard
func BuildSubscriptionDashboard(subs []SubscriptionStatus, todayAdded, weekDownload int) RichMessage {
	builder := NewBuilder()

	// Heading
	builder.Heading("📋 我的订阅状态", 2)

	// Handle empty subs
	if len(subs) == 0 {
		builder.Paragraph("暂无订阅")
	} else {
		// Subscription table
		headers := []string{"影视", "状态", "进度"}
		rows := make([][]string, len(subs))

		for i, sub := range subs {
			// Build progress bar
			progressBar := buildProgressBar(sub.Progress)
			rows[i] = []string{sub.Name, sub.Status, progressBar}
		}

		builder.Table(headers, rows)
	}

	// Summary
	builder.Paragraph(fmt.Sprintf("📊 今日新增：%d 部 | 本周下载：%d 部", todayAdded, weekDownload))

	return builder.Build()
}

// buildProgressBar builds a progress bar string
func buildProgressBar(progress int) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	filled := progress / 10
	empty := 10 - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return fmt.Sprintf("%s %d%%", bar, progress)
}
