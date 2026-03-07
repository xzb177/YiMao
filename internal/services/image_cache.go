package services

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ImageCache 本地图片缓存服务
// 减少对 Emby 服务器的重复请求，降低带宽消耗
type ImageCache struct {
	cacheDir    string
	maxAge      time.Duration // 缓存过期时间
	httpClient  *http.Client
	mu          sync.RWMutex
	enabled     bool
}

// NewImageCache 创建图片缓存服务
func NewImageCache(dataDir string, maxAge time.Duration) *ImageCache {
	cacheDir := filepath.Join(dataDir, "image_cache")
	cache := &ImageCache{
		cacheDir:   cacheDir,
		maxAge:     maxAge,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		enabled:    true,
	}

	// 确保缓存目录存在
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Printf("[ImageCache] Failed to create cache directory: %v", err)
		cache.enabled = false
	}

	if cache.enabled {
		log.Printf("[ImageCache] Initialized with cache dir: %s, max age: %v", cacheDir, maxAge)
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
			log.Printf("[ImageCache] Error checking cache file: %v", err)
		}
		return nil
	}

	// 检查是否过期
	if time.Since(info.ModTime()) > c.maxAge {
		log.Printf("[ImageCache] Cache expired: %s", filepath.Base(cachePath))
		os.Remove(cachePath)
		return nil
	}

	// 读取缓存文件
	data, err := os.ReadFile(cachePath)
	if err != nil {
		log.Printf("[ImageCache] Error reading cache file: %v", err)
		return nil
	}

	log.Printf("[ImageCache] Cache hit: %s (%d bytes)", filepath.Base(cachePath), len(data))
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
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		log.Printf("[ImageCache] Error writing cache file: %v", err)
		return err
	}

	log.Printf("[ImageCache] Cached: %s (%d bytes)", filepath.Base(cachePath), len(data))
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
				log.Printf("[ImageCache] Panic in async cache save: %v", r)
			}
		}()
		if err := c.Set(imageURL, data); err != nil {
			log.Printf("[ImageCache] Failed to cache image: %v", err)
		}
	}()

	return data, nil
}

// download 下载图片
func (c *ImageCache) download(imageURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return nil, err
	}

	// 添加 User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	// 添加自定义 headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("[ImageCache] Downloaded: %s (%d bytes)", imageURL, len(data))
	return data, nil
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

// Cleanup 清理过期的缓存文件
func (c *ImageCache) Cleanup() {
	if !c.enabled {
		return
	}

	log.Printf("[ImageCache] Starting cleanup...")

	files, err := os.ReadDir(c.cacheDir)
	if err != nil {
		log.Printf("[ImageCache] Error reading cache dir: %v", err)
		return
	}

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
		}
	}

	log.Printf("[ImageCache] Cleanup complete: removed %d expired files", cleaned)
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
				log.Printf("[ImageCache] Panic recovered in cleanup routine: %v", r)
			}
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			c.Cleanup()
		}
	}()

	log.Printf("[ImageCache] Cleanup routine started (interval: %v)", interval)
}
