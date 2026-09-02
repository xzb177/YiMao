package richmessage

import (
	"fmt"
	"strings"

	"github.com/xzb177/yimao/pkg/types"
)

// MediaInfo represents media information
type MediaInfo struct {
	Title         string   `json:"title"`
	Year          int      `json:"year"`
	Rating        float64  `json:"rating"`
	Genres        []string `json:"genres"`
	Overview      string   `json:"overview"`
	PosterURL     string   `json:"poster_url"`
	TMDBID        int      `json:"tmdb_id"`
	MediaType     string   `json:"media_type"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Runtime       int      `json:"runtime,omitempty"` // minutes
	VoteCount     int      `json:"vote_count,omitempty"`
	SeasonCount   int      `json:"season_count,omitempty"`
	EpisodeCount  int      `json:"episode_count,omitempty"`
	Status        string   `json:"status,omitempty"`
}

// BuildMediaInfoCard builds a poster-led 10.3 media card.
func BuildMediaInfoCard(info MediaInfo) RichMessage {
	kind := mediaKind(info.MediaType)
	status := strings.TrimSpace(info.Status)
	pairs := [][]string{}
	if len(info.Genres) > 0 {
		pairs = append(pairs, []string{"题材", strings.Join(info.Genres, "/")})
	}
	return buildPosterCard(posterCardData{
		PosterURL: info.PosterURL,
		Title:     info.Title,
		Year:      info.Year,
		Kind:      kind,
		Status:    status,
		Rating:    info.Rating,
		Runtime:   info.Runtime,
		Seasons:   info.SeasonCount,
		Episodes:  info.EpisodeCount,
		Overview:  info.Overview,
		Pairs:     pairs,
	}).Rich()
}

// SubscriptionStatus represents subscription status
type SubscriptionStatus struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Progress int    `json:"progress"` // 0-100
}

// WelcomeOptions extra rows fused into the 10.3 welcome card.
type WelcomeOptions struct {
	IsAdmin    bool
	MiniAppURL string
}

// BuildWelcomeMessage builds the Rich Message welcome page for /start.
func BuildWelcomeMessage(userName string) RichMessage {
	return BuildWelcomeCard(userName, WelcomeOptions{})
}

func welcomeTitle(userName string) string {
	_ = userName
	return "云海求片"
}

func StatusActionButtons(refreshCallback string) []types.TelegramRichMessageButton {
	return []types.TelegramRichMessageButton{
		richButton("返回首页", "start", types.ButtonStylePrimary, false),
		richButton("刷新状态", refreshCallback, types.ButtonStylePrimary, false),
		richButton("搜索求片", "search:menu", types.ButtonStyleSuccess, false),
	}
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

// TransferDailySummarySeries is one title/season from MoviePilot transfer history.
type TransferDailySummarySeries struct {
	Title     string
	WorkTitle string // title without season, used for distinct-work statistics
	Files     int
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

	// Curated recommendations
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
	builder.BoldParagraph("今日精选 · 云海")
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
	builder.BoldParagraph(fmt.Sprintf("%s · 云海", dateStr))
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

// BuildTransferDailySummaryCard builds the authoritative natural-day summary
// from MoviePilot successful transfer history.
func BuildTransferDailySummaryCard(dateStr string, movies, series []TransferDailySummarySeries, fileCount int, firstAt, lastAt string) RichMessage {
	builder := NewBuilder()
	seriesWorks := make(map[string]struct{}, len(series))
	for _, item := range series {
		workTitle := item.WorkTitle
		if workTitle == "" {
			workTitle = item.Title
		}
		seriesWorks[workTitle] = struct{}{}
	}
	seriesLabel := fmt.Sprintf("%d 部", len(seriesWorks))
	if len(series) > len(seriesWorks) {
		seriesLabel = fmt.Sprintf("%d 部 / %d 季", len(seriesWorks), len(series))
	}
	builder.Heading("📥 入库日报", 2)
	builder.BoldParagraph(fmt.Sprintf("%s · 云海影视", dateStr))
	builder.Divider()
	rows := make([][]string, 0, 4)
	if len(movies) > 0 {
		rows = append(rows, []string{"🎬 电影", fmt.Sprintf("%d 部", len(movies))})
	}
	if len(series) > 0 {
		rows = append(rows, []string{"📺 剧集", seriesLabel})
	}
	rows = append(rows,
		[]string{"📁 媒体文件", fmt.Sprintf("%d 个", fileCount)},
		[]string{"🕒 入库时间", fmt.Sprintf("%s–%s", firstAt, lastAt)},
	)
	builder.Table([]string{"统计", "结果"}, rows)
	builder.Divider()
	if len(movies) > 0 {
		builder.Heading(fmt.Sprintf("🎬 新增电影（%d 部）", len(movies)), 3)
		for _, item := range movies {
			builder.Paragraph(fmt.Sprintf("• %s（%d 个文件）", item.Title, item.Files))
		}
	}
	if len(series) > 0 {
		builder.Heading(fmt.Sprintf("📺 剧集更新（%s）", seriesLabel), 3)
	}
	for _, item := range series {
		builder.Paragraph(fmt.Sprintf("• %s（%d 个文件）", item.Title, item.Files))
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

// DashboardData holds data for the admin dashboard card
type DashboardData struct {
	UserCount     int
	RequestCount  int
	ReqUserCount  int
	PendingCount  int
	ApprovedCount int
	RejectedCount int
	FBTotal       int
	FBOpen        int
	FBProcessing  int
	FBFixed       int
	FBClosed      int
	AdminCount    int
}

// BuildDashboardCard builds a compact Rich Message admin dashboard
func BuildDashboardCard(data DashboardData) RichMessage {
	builder := NewBuilder()

	builder.Heading("📈 数据概览", 3)
	builder.BoldParagraph("云海 · 管理看板")

	builder.Table(
		[]string{"模块", "数据"},
		[][]string{
			{"👥 用户", fmt.Sprintf("注册 %d · 求片 %d", data.UserCount, data.ReqUserCount)},
			{"📥 求片", fmt.Sprintf("总计 %d · 待审 %d", data.RequestCount, data.PendingCount)},
			{"✅ 通过", fmt.Sprintf("已通过 %d · 已拒绝 %d", data.ApprovedCount, data.RejectedCount)},
			{"💬 反馈", fmt.Sprintf("总计 %d · 待处理 %d · 处理中 %d", data.FBTotal, data.FBOpen, data.FBProcessing)},
			{"🛡️ 管理", fmt.Sprintf("管理员 %d 位", data.AdminCount)},
		},
	)

	if data.PendingCount > 0 || data.FBOpen > 0 || data.FBProcessing > 0 {
		builder.Italic(fmt.Sprintf("⚠️ 待处理：求片 %d · 反馈 %d", data.PendingCount, data.FBOpen+data.FBProcessing))
	} else {
		builder.Italic("✅ 当前无待处理事项")
	}

	return builder.Build()
}

// RequestCardItem is a compact row for the user request progress card.
type RequestCardItem struct {
	Index  int
	Title  string
	State  string
	Type   string
	Year   string
	Season int
	Date   string
}

// RequestCardData holds data for the user request progress card.
type RequestCardData struct {
	Total      int
	Page       int
	TotalPages int
	Running    int
	Done       int
	Problem    int
	Items      []RequestCardItem
}

// BuildRequestProgressCard builds a compact Rich Message request progress card.
func BuildRequestProgressCard(data RequestCardData) RichMessage {
	if len(data.Items) == 1 {
		item := data.Items[0]
		return BuildPlaybillCard(PlaybillCard{
			Title:   item.Title,
			Tagline: requestStateText(item.State),
			Body:    copyPlaybillBody,
			Year:    item.Year,
			Kind:    playbillKind(item.Type),
			Next:    playbillNext(item.State),
			Refresh: "requests",
		}).Rich()
	}
	pairs := make([][]string, 0, len(data.Items)+1)
	for _, row := range buildRequestCardRows(data.Items) {
		if len(row) >= 2 {
			pairs = append(pairs, []string{row[0], row[1]})
		}
	}
	return BuildPage(Page{
		Heading: copyProgressH1,
		Tagline: copyProgressTag,
		Facts:   pairs,
		Buttons: [][]types.TelegramRichMessageButton{
			trio("返回首页", "start", types.ButtonStylePrimary, "刷新状态", "requests", types.ButtonStylePrimary, "搜索求片", "search:menu", types.ButtonStyleSuccess),
		},
	}).Rich()
}

func buildRequestCardRows(items []RequestCardItem) [][]string {
	if len(items) == 0 {
		return [][]string{{"—", "暂无记录"}}
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		title := item.Title
		if item.Year != "" && item.Year != "0" {
			title = fmt.Sprintf("%s (%s)", title, item.Year)
		}
		if item.Season > 0 {
			title = fmt.Sprintf("%s S%d", title, item.Season)
		}
		if item.Index > 0 {
			title = fmt.Sprintf("%d. %s", item.Index, title)
		}
		rows = append(rows, []string{requestStateText(item.State), title})
	}
	return rows
}

func requestStateText(state string) string {
	switch state {
	case "WISH":
		return StatusSubmitted
	case "REVIEWING":
		return StatusPending
	case "STUCK":
		return StatusSyncing
	case "WORK_ORDER":
		return "处理中"
	case "ISSUE_OPEN":
		return "待处理"
	case "P":
		return StatusSubmitted
	case "R", "S":
		return StatusSearching
	case "D":
		return StatusDownload
	case "C":
		return StatusPlayable
	case "F":
		return "失败"
	case "X":
		return "已取消"
	default:
		return state
	}
}

// AdminTodoData holds data for admin action center.
type AdminTodoData struct {
	PendingRequests int
	StuckRequests   int
	OpenFeedback    int
	ProcessingFB    int
	FailedRequests  int
}

// BuildAdminTodoCard builds a compact admin todo center.
func BuildAdminTodoCard(data AdminTodoData) RichMessage {
	builder := NewBuilder()
	builder.Heading("✅ 待办中心", 3)
	builder.BoldParagraph("优先处理异常和待审核")
	builder.Table([]string{"事项", "数量"}, [][]string{
		{"📝 待审核求片", fmt.Sprintf("%d", data.PendingRequests)},
		{"⚠️ 同步异常", fmt.Sprintf("%d", data.StuckRequests)},
		{"💬 待处理反馈", fmt.Sprintf("%d", data.OpenFeedback)},
		{"🔧 处理中反馈", fmt.Sprintf("%d", data.ProcessingFB)},
		{"❌ 失败/取消求片", fmt.Sprintf("%d", data.FailedRequests)},
	})
	if data.PendingRequests+data.StuckRequests+data.OpenFeedback+data.ProcessingFB+data.FailedRequests == 0 {
		builder.Italic("✅ 当前没有待处理事项")
	} else {
		builder.Italic("点下方按钮进入对应处理页面")
	}
	return builder.Build()
}

// RequestStatsCardData holds data for request success statistics.
type RequestStatsCardData struct {
	Total            int
	UniqueUsers      int
	Approved         int
	Rejected         int
	Cancelled        int
	Completed        int
	Failed           int
	AverageDoneHours int
}

// BuildRequestStatsCard builds request success/funnel card.
func BuildRequestStatsCard(data RequestStatsCardData) RichMessage {
	builder := NewBuilder()
	builder.Heading("📊 求片统计", 3)
	approveRate := 0
	completeRate := 0
	if data.Total > 0 {
		approveRate = data.Approved * 100 / data.Total
		completeRate = data.Completed * 100 / data.Total
	}
	builder.Table([]string{"指标", "数据"}, [][]string{
		{"总求片", fmt.Sprintf("%d 次", data.Total)},
		{"求片用户", fmt.Sprintf("%d 人", data.UniqueUsers)},
		{"通过率", fmt.Sprintf("%d%% · 通过 %d", approveRate, data.Approved)},
		{"入库率", fmt.Sprintf("%d%% · 完成 %d", completeRate, data.Completed)},
		{"异常", fmt.Sprintf("拒绝 %d · 取消 %d · 失败 %d", data.Rejected, data.Cancelled, data.Failed)},
	})
	if data.AverageDoneHours > 0 {
		builder.Italic(fmt.Sprintf("平均完成耗时：约 %d 小时", data.AverageDoneHours))
	}
	return builder.Build()
}

// ============================================================
// Review Notification Cards
// ============================================================

// ReviewNotifyData holds data for admin new-request notification.
type ReviewNotifyData struct {
	Title      string
	Year       int
	MediaType  string // "电影" or "剧集"
	MediaIcon  string // "🎬" or "📺"
	Season     int    // 0 = not set
	SeasonText string // "全季", "第X季", or ""
	Overview   string
	UserName   string
	UserID     int64
	EmbyExists bool
	EmbyHours  int
	EmbyMins   int
}

// BuildReviewNotifyCard builds admin notification card for new review request.
func BuildReviewNotifyCard(data ReviewNotifyData) RichMessage {
	season := data.SeasonText
	if season == "" && data.Season > 0 {
		season = fmt.Sprintf("第 %d 季", data.Season)
	}
	pairs := [][]string{{"申请人", fmt.Sprintf("%s (%d)", data.UserName, data.UserID)}}
	if data.EmbyExists {
		pairs = append(pairs, []string{"媒体库", StatusInLibrary})
	}
	return buildPosterCard(posterCardData{
		Title:      data.Title,
		Year:       data.Year,
		Kind:       data.MediaType,
		Status:     StatusPending,
		SeasonText: season,
		Overview:   data.Overview,
		Pairs:      pairs,
	}).Rich()
}

// ReviewResultData holds data for user approval/rejection notification.
type ReviewResultData struct {
	Title     string
	Year      int
	MediaIcon string
	Status    string // "approved", "rejected", "stuck", "blocked"
	Reason    string // blocked reason or rejection reason
	SubID     int    // subscription ID (for approved)
}

// GroupApprovedData holds data for group chat approval notification.
type GroupApprovedData struct {
	Title      string
	Year       int
	MediaType  string // "电影" or "剧集"
	MediaIcon  string // "🎬" or "📺"
	SeasonText string // "全季", "第X季", or ""
	Requester  string // requester display name
	TMDBID     int
	ApprovedBy string // retained for compatibility; public cards do not expose it
}

// ReviewApprovedData holds data for a requester's approval notification.
type ReviewApprovedData struct {
	Title      string
	Year       int
	MediaType  string // "电影" or "剧集"
	MediaIcon  string // "🎬" or "📺"
	SeasonText string // "全季", "第X季", or ""
}

func approvedMediaTitle(title string, year int, mediaIcon string, seasonText string) string {
	titleText := fmt.Sprintf("%s 《%s》", mediaIcon, title)
	if year > 0 {
		titleText += fmt.Sprintf(" (%d)", year)
	}
	if seasonText != "" {
		titleText += " · " + seasonText
	}
	return titleText
}

// BuildGroupApprovedCard builds a compact public highlight when a request is approved.
func BuildGroupApprovedCard(data GroupApprovedData) RichMessage {
	b := newBlockBuilder()
	b.heading(data.Title, 3)
	b.bold(StatusApproved)
	pairs := [][]string{{"类型", data.MediaType}, {"进度", StatusSearching}}
	if data.Year > 0 {
		pairs = append([][]string{{"年份", fmt.Sprintf("%d", data.Year)}}, pairs...)
	}
	if data.SeasonText != "" {
		pairs = append(pairs, []string{"季度", data.SeasonText})
	}
	if data.Requester != "" {
		pairs = append(pairs, []string{"求片人", data.Requester})
	}
	b.compactTable(pairs)
	b.paragraph("入库后通知求片人")
	return b.card().Rich()
}

// WashStatusData describes a dedicated wash workflow card. Public cards omit
// requester information by construction.
type WashStatusData struct {
	Title     string
	Year      int
	MediaIcon string
	Season    int
	Status    string
	Requester string
	Public    bool
}

// BuildWashStatusCard builds wash-specific approval/completion messaging.
func BuildWashStatusCard(data WashStatusData) RichMessage {
	builder := NewBuilder()
	statusText := "已批准"
	heading := "✅ 洗版工单已批准"
	progress := "管理员处理并验证新版本；旧版须继续保留。"
	if data.Status == "completed" {
		statusText = "已完成"
		heading = "✅ 洗版验证完成"
		progress = "已确认新增不同版本，且创建工单时的旧版仍在媒体库中。"
	}
	builder.Heading(heading, 3)
	icon := data.MediaIcon
	if icon == "" {
		icon = "♻️"
	}
	seasonText := ""
	if data.Season > 0 {
		seasonText = fmt.Sprintf("第%d季", data.Season)
	}
	builder.BoldParagraph(approvedMediaTitle(data.Title, data.Year, icon, seasonText))
	builder.Table([]string{"项目", "状态"}, [][]string{{"洗版状态", statusText}})
	builder.Paragraph(progress)
	if !data.Public && data.Requester != "" {
		builder.Italic(fmt.Sprintf("申请人：%s", data.Requester))
	}
	if !data.Public && data.Status != "completed" {
		builder.Italic("可在「查看进度」跟进，完成后通知你")
	}
	return builder.Build()
}

// BuildWashReviewNotifyCard builds the administrator-only creation card.
func BuildWashReviewNotifyCard(data ReviewNotifyData) RichMessage {
	builder := NewBuilder()
	builder.Heading("♻️ 新洗版工单", 3)
	builder.BoldParagraph(approvedMediaTitle(data.Title, data.Year, data.MediaIcon, data.SeasonText))
	builder.Table([]string{"信息", "详情"}, [][]string{{"类型", data.MediaType}, {"申请人", fmt.Sprintf("%s (%d)", data.UserName, data.UserID)}})
	builder.Paragraph("仅管理员可处理；批准或拒绝前请核对目标。处理期间必须保留现有版本。")
	return builder.Build()
}

// BuildWashApprovedCard preserves the older call shape for private approvals.
func BuildWashApprovedCard(title string, year int, mediaIcon string) RichMessage {
	return BuildWashStatusCard(WashStatusData{Title: title, Year: year, MediaIcon: mediaIcon, Status: "approved"})
}

// BuildReviewApprovedCard builds the requester approval notification.
func BuildReviewApprovedCard(data ReviewApprovedData) RichMessage {
	return buildPosterCard(posterCardData{
		Title:      data.Title,
		Year:       data.Year,
		Kind:       data.MediaType,
		Status:     StatusApproved,
		SeasonText: data.SeasonText,
		Footer:     "匹配资源后自动下载，完成后可播放。",
		Buttons: []types.TelegramRichMessageButton{
			richButton("查看进度", "requests", types.ButtonStylePrimary, false),
			richButton("返回首页", "start", types.ButtonStylePrimary, false),
			richButton("搜索求片", "search:menu", types.ButtonStyleSuccess, false),
		},
	}).Rich()
}

// BuildReviewRejectedCard builds user notification for rejected request.
func BuildReviewRejectedCard(title string, year int, mediaIcon string) RichMessage {
	_ = mediaIcon
	return buildPosterCard(posterCardData{
		Title:  title,
		Year:   year,
		Status: StatusRejected,
		Footer: "配额已退还，可换片名再试。",
		Buttons: []types.TelegramRichMessageButton{
			richButton("搜索求片", "search:menu", types.ButtonStyleSuccess, false),
			richButton("查看进度", "requests", types.ButtonStylePrimary, false),
			richButton("返回首页", "start", types.ButtonStylePrimary, false),
		},
	}).Rich()
}

// BuildReviewStuckCard builds user notification for stuck (sync failed) request.
func BuildReviewStuckCard(title string, year int, mediaIcon string) RichMessage {
	_ = mediaIcon
	return buildPosterCard(posterCardData{
		Title:  title,
		Year:   year,
		Status: StatusApproved,
		Pairs:  [][]string{{"进度", StatusSyncing}},
		Footer: "系统会自动重试，可在查看进度跟进。",
		Buttons: []types.TelegramRichMessageButton{
			richButton("查看进度", "requests", types.ButtonStylePrimary, false),
			richButton("刷新状态", "requests", types.ButtonStylePrimary, false),
			richButton("返回首页", "start", types.ButtonStylePrimary, false),
		},
	}).Rich()
}

// BuildReviewBlockedCard builds user notification when the title is already in library.
func BuildReviewBlockedCard(title string, reason string, detail string) RichMessage {
	footer := strings.TrimSpace(reason)
	if detail != "" {
		if footer != "" {
			footer += " "
		}
		footer += detail
	}
	return buildPosterCard(posterCardData{
		Title:  title,
		Status: StatusInLibrary,
		Footer: footer,
		Buttons: []types.TelegramRichMessageButton{
			richButton("搜索求片", "search:menu", types.ButtonStyleSuccess, false),
			richButton("查看进度", "requests", types.ButtonStylePrimary, false),
			richButton("返回首页", "start", types.ButtonStylePrimary, false),
		},
	}).Rich()
}

// PendingReviewItem is a single row in the pending reviews list.
type PendingReviewItem struct {
	Index int
	Title string
	Year  int
	User  string
	Time  string
}

// BuildPendingReviewsCard builds admin pending reviews list card.
func BuildPendingReviewsCard(items []PendingReviewItem) RichMessage {
	builder := NewBuilder()
	builder.Heading("📋 待审核求片", 3)
	builder.BoldParagraph(fmt.Sprintf("共 %d 条待审核", len(items)))

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		titleText := item.Title
		if item.Year > 0 {
			titleText += fmt.Sprintf(" (%d)", item.Year)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", item.Index),
			titleText,
			item.User,
			item.Time,
		})
	}
	builder.Table([]string{"#", "标题", "用户", "时间"}, rows)
	return builder.Build()
}

// MyReviewItem is a single row in the user's review list.
type MyReviewItem struct {
	Title    string
	Year     int
	Status   string // "pending", "approved", "rejected"
	SubState string
	Time     string
}

// BuildMyReviewsCard builds user's review requests list card.
func BuildMyReviewsCard(items []MyReviewItem) RichMessage {
	builder := NewBuilder()
	builder.Heading("📊 求片进度", 3)
	builder.BoldParagraph(fmt.Sprintf("共 %d 条记录", len(items)))

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		statusIcon := "⏳"
		statusText := "待审核"
		switch item.Status {
		case "approved":
			statusIcon = "✅"
			statusText = "已通过"
			if item.SubState != "" && item.SubState != "N" {
				statusText += " · " + item.SubState
			}
		case "rejected":
			statusIcon = "❌"
			statusText = "已拒绝"
		}
		titleText := item.Title
		if item.Year > 0 {
			titleText += fmt.Sprintf(" (%d)", item.Year)
		}
		rows = append(rows, []string{
			statusIcon + " " + statusText,
			titleText,
			item.Time,
		})
	}
	builder.Table([]string{"状态", "标题", "时间"}, rows)
	return builder.Build()
}

// ==================== 观影画像 ====================

// PortraitCardData 观影画像卡片数据（从 services.PortraitResult 转换）
type PortraitCardData struct {
	UserName    string
	TotalItems  int
	GenreBar    []GenreBarData
	TopGenres   string
	AvgRating   float64
	TasteLevel  string
	TasteDesc   string
	RhythmType  string
	RhythmDesc  string
	PsychTraits []PsychTraitData
	Surprises   []string
	BlindSpots  []string
}

// GenreBarData 类型条形图
type GenreBarData struct {
	Genre string
	Pct   string
	Bar   string
}

// PsychTraitData 心理特质
type PsychTraitData struct {
	Genre string
	Trait string
	Desc  string
}

// BuildPortraitCard builds the soul portrait Rich Message card.
func BuildPortraitCard(data PortraitCardData) RichMessage {
	builder := NewBuilder()
	builder.Heading("🧠 观影画像", 3)

	nameText := "匿名用户"
	if data.UserName != "" {
		nameText = data.UserName
	}
	builder.BoldParagraph(fmt.Sprintf("👤 %s · %d 部作品", nameText, data.TotalItems))

	// 核心指标
	builder.Divider()
	topGenreLine := fmt.Sprintf("🎭 %s", data.TopGenres)
	if data.AvgRating >= 0 {
		topGenreLine += fmt.Sprintf("　⭐ %.1f", data.AvgRating)
	} else {
		topGenreLine += "　⭐ 暂无评分"
	}
	builder.BoldParagraph(topGenreLine)
	builder.Paragraph(fmt.Sprintf("%s — %s", data.TasteLevel, data.TasteDesc))
	builder.Paragraph(fmt.Sprintf("%s — %s", data.RhythmType, data.RhythmDesc))

	// 类型偏好
	builder.Divider()
	builder.BoldParagraph("📊 类型偏好")
	for _, bar := range data.GenreBar {
		builder.Paragraph(fmt.Sprintf("%s %s %s%%", bar.Bar, bar.Genre, bar.Pct))
	}

	// 心理特质
	if len(data.PsychTraits) > 0 {
		builder.Divider()
		builder.BoldParagraph("🔮 心理特质")
		for _, pt := range data.PsychTraits {
			builder.Paragraph(fmt.Sprintf("• %s → %s: %s", pt.Genre, pt.Trait, pt.Desc))
		}
	}

	// 反直觉发现
	if len(data.Surprises) > 0 {
		builder.Divider()
		builder.BoldParagraph("💡 反直觉发现")
		for _, s := range data.Surprises {
			builder.Paragraph(fmt.Sprintf("• %s", s))
		}
	}

	// 盲区
	if len(data.BlindSpots) > 0 {
		builder.Divider()
		builder.BoldParagraph("🧭 待探索类型")
		builder.Paragraph(strings.Join(data.BlindSpots, " · "))
		builder.Italic("如果感兴趣，可以从这些类型里挑一部试试。")
	}

	return builder.Build()
}
