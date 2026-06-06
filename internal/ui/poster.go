package ui

import (
	"strings"
)

const (
	// FallbackPosterURL 默认占位海报（本地资源，不依赖外部 CDN）
	FallbackPosterURL = ""
)

// EnsureSafePosterURL 确保海报链接安全，防空值/死链/非法协议。
// 返回空字符串表示无海报（调用方应跳过 SendPhoto，用纯文本）。
func EnsureSafePosterURL(posterURL string) string {
	cleanURL := strings.TrimSpace(posterURL)

	if cleanURL == "" {
		return ""
	}

	// 必须是 http/https 开头
	if !strings.HasPrefix(cleanURL, "http://") && !strings.HasPrefix(cleanURL, "https://") {
		return ""
	}

	// 过滤 TMDB 脏数据
	lowerURL := strings.ToLower(cleanURL)
	if strings.Contains(lowerURL, "null") || strings.HasSuffix(cleanURL, "/") {
		return ""
	}

	return cleanURL
}
