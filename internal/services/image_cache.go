package services

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xzb177/yimao/pkg/logger"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ImageCache 本地图片缓存服务
// 减少对 Emby 服务器的重复请求，降低带宽消耗
type ImageCache struct {
	cacheDir   string
	maxAge     time.Duration // 缓存过期时间
	maxSize    int64         // 最大缓存大小（字节），默认 500MB
	httpClient *http.Client
	mu         sync.RWMutex
	enabled    bool
}

const defaultMaxCacheSize = 500 * 1024 * 1024 // 500MB

// NewImageCache 创建图片缓存服务
func NewImageCache(dataDir string, maxAge time.Duration) *ImageCache {
	cacheDir := filepath.Join(dataDir, "image_cache")
	cache := &ImageCache{
		cacheDir:   cacheDir,
		maxAge:     maxAge,
		maxSize:    defaultMaxCacheSize,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		enabled:    true,
	}

	// 确保缓存目录存在
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		logger.Info("[ImageCache] Failed to create cache directory: %v", err)
		cache.enabled = false
	}

	if cache.enabled {
		logger.Info("[ImageCache] Initialized with cache dir: %s, max age: %v", cacheDir, maxAge)
	}

	return cache
}

// Get 获取缓存图片，如果不存在或过期则返回 nil
func (c *ImageCache) Get(imageURL string) []byte {
	if !c.enabled {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	cachePath := c.getCachePath(imageURL)
	if cachePath == "" {
		return nil
	}

	// 检查文件是否存在
	info, err := os.Stat(cachePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Info("[ImageCache] Error checking cache file: %v", err)
		}
		return nil
	}

	// 检查是否过期
	if time.Since(info.ModTime()) > c.maxAge {
		logger.Info("[ImageCache] Cache expired: %s", filepath.Base(cachePath))
		os.Remove(cachePath)
		return nil
	}

	// 读取缓存文件
	data, err := os.ReadFile(cachePath)
	if err != nil {
		logger.Info("[ImageCache] Error reading cache file: %v", err)
		return nil
	}

	logger.Info("[ImageCache] Cache hit: %s (%d bytes)", filepath.Base(cachePath), len(data))
	return data
}

// Set 保存图片到缓存
func (c *ImageCache) Set(imageURL string, data []byte) error {
	if !c.enabled {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cachePath := c.getCachePath(imageURL)
	if cachePath == "" {
		return fmt.Errorf("invalid cache path for URL: %s", imageURL)
	}

	// 写入缓存文件
	if err := atomicWriteFile(cachePath, data, 0644); err != nil {
		logger.Info("[ImageCache] Error writing cache file: %v", err)
		return err
	}

	logger.Info("[ImageCache] Cached: %s (%d bytes)", filepath.Base(cachePath), len(data))
	return nil
}

// DownloadOrFetch 下载图片，优先使用缓存
func (c *ImageCache) DownloadOrFetch(imageURL string, headers map[string]string) ([]byte, error) {
	// 先尝试从缓存获取
	if cached := c.Get(imageURL); cached != nil {
		return cached, nil
	}

	// 缓存未命中，下载图片
	data, err := c.download(imageURL, headers)
	if err != nil {
		return nil, err
	}

	// 保存到缓存（异步，不阻塞）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Info("[ImageCache] Panic in async cache save: %v", r)
			}
		}()
		if err := c.Set(imageURL, data); err != nil {
			logger.Info("[ImageCache] Failed to cache image: %v", err)
		}
	}()

	return data, nil
}

// maxImageDownloadBytes 是全项目统一的图片下载上限。依据：
//   - 审计第 12 条：TMDB original 背景图常见 2–6MB，旧的 1MB 上限会静默截断；
//   - Telegram Bot API 的 sendPhoto 上限就是 10MB，超过它无论如何发不出去；
//   - 旧的两条路径上限不一致（1MB / 5MB）且都是静默截断，还把截断图缓存下来。
//
// 超限时返回明确错误、不缓存坏图，由调用方决定降级（例如改走 URL 直发）。
const maxImageDownloadBytes = 10 << 20

// 审计 A5：图片回退下载的 SSRF 防护与重定向凭据剥离。
//
//   - validateOutboundImageURL 拒绝内网/环回/链路本地/组播等非公网地址（含 DNS 解析后）。
//   - guardRedirectClient 给每一次重定向做同样的校验，并在跨主机跳转时剥掉
//     X-Emby-Token 等敏感头（Go 默认只在跨域时剥 Authorization/Cookie，自义定头会原样带过去）。
//   - allowPrivateImageHosts 仅供测试打开（httptest 跑在 127.0.0.1），生产恒为 false。
var allowPrivateImageHosts = false

var imageSensitiveRedirectHeaders = []string{
	"X-Emby-Token", "X-Webhook-Token", "X-Api-Key",
	"Authorization", "Proxy-Authorization", "Cookie",
}

func isNonPublicIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// validateOutboundImageURL 校验图片下载目标：只允许 http/https 公网地址。
func validateOutboundImageURL(rawURL string) error {
	if allowPrivateImageHosts {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid image URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("image URL scheme not allowed: %s", u.Scheme)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isNonPublicIP(ip) {
			return fmt.Errorf("image URL host is not public: %s", host)
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("image URL host resolve failed: %w", err)
	}
	for _, a := range addrs {
		if isNonPublicIP(a.IP) {
			return fmt.Errorf("image URL host resolves to non-public IP: %s", host)
		}
	}
	return nil
}

func sameURLHost(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname()) && a.Port() == b.Port() && strings.EqualFold(a.Scheme, b.Scheme)
}

// guardRedirectClient 包装 client：每次重定向都做 SSRF 校验；跨主机跳转剥敏感头。
func guardRedirectClient(base *http.Client) *http.Client {
	c := *base
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if err := validateOutboundImageURL(req.URL.String()); err != nil {
			return err
		}
		if len(via) > 0 && !sameURLHost(via[len(via)-1].URL, req.URL) {
			for _, h := range imageSensitiveRedirectHeaders {
				req.Header.Del(h)
			}
		}
		return nil
	}
	return &c
}

// downloadImageWithLimit 下载图片。超过上限返回明确错误，绝不静默截断。
func downloadImageWithLimit(client *http.Client, imageURL string, headers map[string]string) ([]byte, error) {
	if err := validateOutboundImageURL(imageURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return nil, err
	}

	// 添加 User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	// 添加自定义 headers（例如 X-Emby-Token）
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// 审计 A5：重定向守卫（校验每一跳 + 跨主机剥敏感头）。
	resp, err := guardRedirectClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}
	if resp.ContentLength > maxImageDownloadBytes {
		return nil, fmt.Errorf("image exceeds %d bytes limit (content-length %d)", maxImageDownloadBytes, resp.ContentLength)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxImageDownloadBytes {
		return nil, fmt.Errorf("image exceeds %d bytes limit (%d bytes received)", maxImageDownloadBytes, len(data))
	}

	logger.Info("[ImageCache] Downloaded: %s (%d bytes)", imageURL, len(data))
	return data, nil
}

// download 下载图片
func (c *ImageCache) download(imageURL string, headers map[string]string) ([]byte, error) {
	return downloadImageWithLimit(c.httpClient, imageURL, headers)
}

// getCachePath 根据图片 URL 生成缓存文件路径
func (c *ImageCache) getCachePath(imageURL string) string {
	if imageURL == "" {
		return ""
	}

	// 移除 URL 中的查询参数（因为 api_key 等参数可能变化）
	baseURL := imageURL
	if idx := strings.Index(imageURL, "?"); idx > 0 {
		baseURL = imageURL[:idx]
	}

	// 使用 MD5 作为文件名，避免文件名过长或包含非法字符
	hash := md5.Sum([]byte(baseURL))
	hashStr := hex.EncodeToString(hash[:])

	// 根据扩展名确定文件类型
	ext := ".jpg"
	if strings.Contains(baseURL, ".png") {
		ext = ".png"
	} else if strings.Contains(baseURL, ".webp") {
		ext = ".webp"
	}

	return filepath.Join(c.cacheDir, hashStr+ext)
}

// Cleanup 清理过期的缓存文件，并在超过大小限制时删除最旧的文件
func (c *ImageCache) Cleanup() {
	if !c.enabled {
		return
	}

	logger.Info("[ImageCache] Starting cleanup...")

	type fileInfo struct {
		path    string
		size    int64
		modTime time.Time
	}

	files, err := os.ReadDir(c.cacheDir)
	if err != nil {
		logger.Warn("[ImageCache] Error reading cache dir: %v", err)
		return
	}

	var allFiles []fileInfo
	var totalSize int64
	cleaned := 0

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		path := filepath.Join(c.cacheDir, file.Name())
		info, err := file.Info()
		if err != nil {
			continue
		}

		// 删除过期文件
		if time.Since(info.ModTime()) > c.maxAge {
			if err := os.Remove(path); err == nil {
				cleaned++
			}
			continue
		}

		allFiles = append(allFiles, fileInfo{path: path, size: info.Size(), modTime: info.ModTime()})
		totalSize += info.Size()
	}

	// 超过大小限制时，按时间从旧到新删除
	if totalSize > c.maxSize && len(allFiles) > 1 {
		// 按修改时间排序（旧的在前）
		sort.Slice(allFiles, func(i, j int) bool { return allFiles[i].modTime.Before(allFiles[j].modTime) })
		for _, fi := range allFiles {
			if totalSize <= c.maxSize {
				break
			}
			if err := os.Remove(fi.path); err == nil {
				totalSize -= fi.size
				cleaned++
			}
		}
	}

	logger.Info("[ImageCache] Cleanup complete: removed %d files, remaining size: %.1fMB", cleaned, float64(totalSize)/(1024*1024))
}

// GetStats 获取缓存统计信息
func (c *ImageCache) GetStats() (count int, totalSize int64) {
	if !c.enabled {
		return 0, 0
	}

	files, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return 0, 0
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		// 只统计未过期的文件
		if time.Since(info.ModTime()) <= c.maxAge {
			count++
			totalSize += info.Size()
		}
	}

	return count, totalSize
}

// StartCleanupRoutine 启动定期清理任务
func (c *ImageCache) StartCleanupRoutine(interval time.Duration) {
	if !c.enabled {
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Info("[ImageCache] Panic recovered in cleanup routine: %v", r)
			}
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			c.Cleanup()
		}
	}()

	logger.Info("[ImageCache] Cleanup routine started (interval: %v)", interval)
}
