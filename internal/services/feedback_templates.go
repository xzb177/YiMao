package services

import (
	"encoding/json"
	"fmt"
)

// FeedbackTemplate 问题描述模板
type FeedbackTemplate struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Fields      []Field  `json:"fields"`
	Example     string   `json:"example"`
}

// Field 模板字段
type Field struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"` // "text", "select", "multiline"
	Placeholder string `json:"placeholder"`
	Required    bool   `json:"required"`
	Options     []string `json:"options,omitempty"`
}

// FeedbackTemplateService 模板服务
type FeedbackTemplateService struct {
	templates map[string][]FeedbackTemplate
}

// NewFeedbackTemplateService 创建模板服务
func NewFeedbackTemplateService() *FeedbackTemplateService {
	service := &FeedbackTemplateService{
		templates: make(map[string][]FeedbackTemplate),
	}

	service.initTemplates()

	return service
}

// initTemplates 初始化所有模板
func (fts *FeedbackTemplateService) initTemplates() {
	fts.templates["video_quality"] = []FeedbackTemplate{
		{
			ID:    "vq_blur",
			Type:  "video_quality",
			Title: "画面模糊/马赛克",
			Fields: []Field{
				{
					ID:          "blur_location",
					Label:       "模糊位置",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"整体模糊", "部分模糊", "特定场景"},
				},
				{
					ID:          "blur_time",
					Label:       "出现时间",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"0-5分钟", "5-15分钟", "15分钟以上", "全程"},
				},
				{
					ID:          "blur_severity",
					Label:       "严重程度",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"轻微", "一般", "严重"},
				},
				{
					ID:          "other_info",
					Label:       "其他信息",
					Type:        "multiline",
					Placeholder: "是否尝试其他画质？具体表现如何？",
					Required:    false,
				},
			},
			Example: "模糊位置：部分模糊\n出现时间：5-15分钟\n严重程度：一般\n其他信息：第10分钟左右画面出现马赛克，切换到4K画质后问题依旧",
		},
		{
			ID:    "vq_lag",
			Type:  "video_quality",
			Title: "画面卡顿/掉帧",
			Fields: []Field{
				{
					ID:          "lag_frequency",
					Label:       "卡顿频率",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"偶尔", "频繁", "持续"},
				},
				{
					ID:          "lag_duration",
					Label:       "卡顿时长",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"1-3秒", "3-5秒", "5秒以上"},
				},
				{
					ID:          "network_type",
					Label:       "网络类型",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"WiFi", "4G", "5G", "其他"},
				},
				{
					ID:          "device_type",
					Label:       "设备类型",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"Web", "iOS", "Android", "TV"},
				},
			},
			Example: "卡顿频率：频繁\n卡顿时长：3-5秒\n网络类型：WiFi\n设备类型：iOS",
		},
		{
			ID:    "vq_black",
			Type:  "video_quality",
			Title: "黑屏/画面丢失",
			Fields: []Field{
				{
					ID:          "black_time",
					Label:       "黑屏时间",
					Type:        "text",
					Placeholder: "例如：播放到第15分钟时",
					Required:    true,
				},
				{
					ID:          "black_duration",
					Label:       "黑屏持续时长",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"1秒以内", "1-3秒", "3-5秒", "5秒以上"},
				},
				{
					ID:          "audio_status",
					Label:       "黑屏时音频",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"正常", "断断续续", "无音频"},
				},
			},
			Example: "黑屏时间：第15分钟\n黑屏持续时长：3-5秒\n黑屏时音频：正常",
		},
	}

	fts.templates["audio_quality"] = []FeedbackTemplate{
		{
			ID:    "aq_no_sound",
			Type:  "audio_quality",
			Title: "无音频/静音",
			Fields: []Field{
				{
					ID:          "no_sound_time",
					Label:       "静音时间",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"全程无音频", "部分段落无音频", "偶尔静音"},
				},
				{
					ID:          "no_sound_location",
					Label:       "静音位置",
					Type:        "text",
					Placeholder: "例如：第10-15分钟",
					Required:    false,
				},
				{
					ID:          "tried_fixes",
					Label:       "已尝试的修复方法",
					Type:        "multiline",
					Placeholder: "例如：切换音轨、调整音量、重启播放器",
					Required:    false,
				},
			},
			Example: "静音时间：部分段落无音频\n静音位置：第10-15分钟\n已尝试的修复方法：切换音轨、调整音量",
		},
		{
			ID:    "aq_distortion",
			Type:  "audio_quality",
			Title: "音频失真/杂音",
			Fields: []Field{
				{
					ID:          "distortion_type",
					Label:       "杂音类型",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"滋滋声", "爆音", "断断续续", "其他"},
				},
				{
					ID:          "distortion_time",
					Label:       "出现时间",
					Type:        "text",
					Placeholder: "例如：第20分钟后",
					Required:    false,
				},
				{
					ID:          "sound_quality",
					Label:       "音质表现",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"完全无法听清", "部分听不清", "勉强能听", "基本正常但有杂音"},
				},
			},
			Example: "杂音类型：滋滋声\n出现时间：第20分钟后\n音质表现：勉强能听",
		},
	}

	fts.templates["subtitle"] = []FeedbackTemplate{
		{
			ID:    "sub_no_subtitle",
			Type:  "subtitle",
			Title: "无字幕",
			Fields: []Field{
				{
					ID:          "subtitle_language",
					Label:       "需要字幕语言",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"中文字幕", "英文字幕", "双语字幕", "其他"},
				},
				{
					ID:          "subtitle_source",
					Label:       "字幕来源",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"内置字幕", "外挂字幕", "自动字幕"},
				},
			},
			Example: "需要字幕语言：中文字幕\n字幕来源：内置字幕",
		},
		{
			ID:    "sub_error",
			Type:  "subtitle",
			Title: "字幕错误/乱码",
			Fields: []Field{
				{
					ID:          "error_type",
					Label:       "错误类型",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"乱码", "显示不完整", "错别字/翻译错误", "延迟/不同步", "其他"},
				},
				{
					ID:          "error_location",
					Label:       "出现位置",
					Type:        "text",
					Placeholder: "例如：第30分钟后字幕乱码",
					Required:    false,
				},
				{
					ID:          "error_examples",
					Label:       "具体例子",
					Type:        "multiline",
					Placeholder: "请提供具体的错误字幕内容",
					Required:    false,
				},
			},
			Example: "错误类型：延迟/不同步\n出现位置：第30分钟后\n具体例子：字幕比音频慢约3秒",
		},
		{
			ID:    "sub_no_sync",
			Type:  "subtitle",
			Title: "字幕不同步",
			Fields: []Field{
				{
					ID:          "sync_issue",
					Label:       "同步问题",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"字幕超前", "字幕滞后", "时快时慢"},
				},
				{
					ID:          "sync_duration",
					Label:       "偏差时长",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"1秒以内", "1-3秒", "3-5秒", "5秒以上"},
				},
			},
			Example: "同步问题：字幕滞后\n偏差时长：3-5秒",
		},
	}

	fts.templates["search"] = []FeedbackTemplate{
		{
			ID:    "search_not_found",
			Type:  "search",
			Title: "搜索不到影片",
			Fields: []Field{
				{
					ID:          "search_keywords",
					Label:       "搜索关键词",
					Type:        "text",
					Placeholder: "请输入搜索的内容",
					Required:    true,
				},
				{
					ID:          "search_language",
					Label:       "搜索语言",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"中文", "英文", "拼音", "其他"},
				},
				{
					ID:          "media_type",
					Label:       "媒体类型",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"电影", "电视剧", "综艺", "其他"},
				},
				{
					ID:          "release_year",
					Label:       "上映/播出年份（如果知道）",
					Type:        "text",
					Placeholder: "例如：2023",
					Required:    false,
				},
				{
					ID:          "additional_info",
					Label:       "其他信息",
					Type:        "multiline",
					Placeholder: "例如：导演、演员、出品方等",
					Required:    false,
				},
			},
			Example: "搜索关键词：流浪地球2\n搜索语言：中文\n媒体类型：电影\n上映年份（如果知道）：2023\n其他信息：导演郭帆",
		},
	}

	fts.templates["playback"] = []FeedbackTemplate{
		{
			ID:    "pb_not_play",
			Type:  "playback",
			Title: "无法播放",
			Fields: []Field{
				{
					ID:          "playback_error",
					Label:       "错误提示",
					Type:        "multiline",
					Placeholder: "请提供完整的错误提示信息",
					Required:    true,
				},
				{
					ID:          "media_info",
					Label:       "影片信息",
					Type:        "text",
					Placeholder: "影片名称、分辨率等",
					Required:    false,
				},
				{
					ID:          "tried_methods",
					Label:       "已尝试的方法",
					Type:        "multiline",
					Placeholder: "例如：刷新页面、切换画质、更换浏览器",
					Required:    false,
				},
			},
			Example: "错误提示：播放器初始化失败，错误代码 5003\n影片信息：复仇者联盟 1080P\n已尝试的方法：刷新页面、切换画质",
		},
		{
			ID:    "pb_stuck",
			Type:  "playback",
			Title: "播放卡住/加载失败",
			Fields: []Field{
				{
					ID:          "stuck_time",
					Label:       "卡住时间",
					Type:        "text",
					Placeholder: "例如：播放到第10分钟时",
					Required:    true,
				},
				{
					ID:          "stuck_duration",
					Label:       "等待时长",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"30秒以内", "30秒-1分钟", "1-3分钟", "3分钟以上"},
				},
				{
					ID:          "loading_status",
					Label:       "加载状态",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"一直在加载", "画面黑屏有转圈", "画面静止"},
				},
			},
			Example: "卡住时间：第10分钟\n等待时长：30秒-1分钟\n加载状态：画面黑屏有转圈",
		},
	}

	fts.templates["other"] = []FeedbackTemplate{
		{
			ID:    "other_general",
			Type:  "other",
			Title: "其他问题",
			Fields: []Field{
				{
					ID:          "issue_category",
					Label:       "问题分类",
					Type:        "select",
					Placeholder: "请选择",
					Required:    true,
					Options:     []string{"账号问题", "支付问题", "功能建议", "UI问题", "Bug反馈", "其他"},
				},
				{
					ID:          "issue_description",
					Label:       "问题描述",
					Type:        "multiline",
					Placeholder: "请详细描述您遇到的问题",
					Required:    true,
				},
				{
					ID:          "expected_behavior",
					Label:       "期望的行为",
					Type:        "multiline",
					Placeholder: "您期望应该是什么样的？",
					Required:    false,
				},
			},
			Example: "问题分类：功能建议\n问题描述：希望添加按评分排序功能\n期望的行为：在搜索结果页面可以按评分排序",
		},
	}
}

// GetTemplatesByType 根据问题类型获取模板列表
func (fts *FeedbackTemplateService) GetTemplatesByType(issueType string) []FeedbackTemplate {
	return fts.templates[issueType]
}

// GetTemplate 获取指定模板
func (fts *FeedbackTemplateService) GetTemplate(templateID string) (*FeedbackTemplate, bool) {
	for _, templates := range fts.templates {
		for _, template := range templates {
			if template.ID == templateID {
				return &template, true
			}
		}
	}
	return nil, false
}

// GetAllTemplates 获取所有模板
func (fts *FeedbackTemplateService) GetAllTemplates() map[string][]FeedbackTemplate {
	return fts.templates
}

// FormatTemplate 格式化模板为文本
func (fts *FeedbackTemplateService) FormatTemplate(template FeedbackTemplate, answers map[string]string) string {
	var result string

	result += fmt.Sprintf("【%s】%s\n", getIssueTypeLabel(template.Type), template.Title)
	result += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n"

	for _, field := range template.Fields {
		answer := answers[field.ID]
		if answer == "" {
			answer = "未填写"
		}
		result += fmt.Sprintf("• %s：%s\n", field.Label, answer)
	}

	return result
}

// getIssueTypeLabel 获取问题类型标签
func getIssueTypeLabel(issueType string) string {
	labels := map[string]string{
		"video_quality": "画质问题",
		"audio_quality": "音频问题",
		"subtitle":      "字幕问题",
		"search":        "搜索问题",
		"playback":      "播放问题",
		"other":         "其他问题",
	}

	if label, exists := labels[issueType]; exists {
		return label
	}

	return issueType
}

// GetAvailableIssueTypes 获取可用的问题类型
func (fts *FeedbackTemplateService) GetAvailableIssueTypes() []string {
	types := make([]string, 0, len(fts.templates))
	for issueType := range fts.templates {
		types = append(types, issueType)
	}
	return types
}
