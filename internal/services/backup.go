package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// DataBackupService 定时备份 data/ 目录下的关键 JSON 文件到 data/backups/。
// 每次 bot 启动时执行一次，之后每 24 小时自动备份。
// 保留最近 7 天的备份，自动清理过期文件。
type DataBackupService struct {
	dataDir  string
	interval time.Duration
	maxAge   time.Duration
}

// NewDataBackupService 创建备份服务。
// dataDir: 数据目录（如 /app/data）
// interval: 备份间隔（如 24h）
// maxAge: 保留天数（如 7*24h = 7 天）
func NewDataBackupService(dataDir string, interval, maxAge time.Duration) *DataBackupService {
	return &DataBackupService{
		dataDir:  dataDir,
		interval: interval,
		maxAge:   maxAge,
	}
}

// Start 启动定时备份（非阻塞），立即执行一次。
func (s *DataBackupService) Start() {
	// 立即执行一次
	s.doBackup()

	// 启动定时循环
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for range ticker.C {
			s.doBackup()
		}
	}()
}

// doBackup 执行一次备份。
func (s *DataBackupService) doBackup() {
	backupDir := filepath.Join(s.dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		logger.Info("[Backup] 创建备份目录失败: %v", err)
		return
	}

	// 需要备份的文件列表
	files := []string{
		"user_mappings.json",
		"user_quotas.json",
		"review_requests.json",
		"admins.json",
		"preferences.json",
		"feedback.json",
		"carpool.json",
		"binding_requests.json",
	}

	timestamp := time.Now().Format("20060102_150405")
	backedUp := 0

	for _, filename := range files {
		src := filepath.Join(s.dataDir, filename)
		data, err := os.ReadFile(src)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Info("[Backup] 读取 %s 失败: %v", filename, err)
			}
			continue
		}

		dst := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", filename, timestamp))
		if err := os.WriteFile(dst, data, 0644); err != nil {
			logger.Info("[Backup] 写入 %s 失败: %v", dst, err)
			continue
		}
		backedUp++
	}

	if backedUp > 0 {
		logger.Info("[Backup] 备份完成: %d 个文件, 时间戳 %s", backedUp, timestamp)
	}

	// 清理过期备份
	s.cleanOldBackups(backupDir)
}

// cleanOldBackups 清理超过 maxAge 的备份文件。
func (s *DataBackupService) cleanOldBackups(backupDir string) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-s.maxAge)
	removed := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(backupDir, entry.Name()))
			removed++
		}
	}

	if removed > 0 {
		logger.Info("[Backup] 清理过期备份: %d 个文件", removed)
	}
}

// ListBackups 列出当前所有备份（供管理员查看）。
func (s *DataBackupService) ListBackups() []string {
	backupDir := filepath.Join(s.dataDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil
	}

	var result []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".bak") {
			result = append(result, entry.Name())
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(result)))
	return result
}

// RestoreBackup 从备份恢复指定文件。
// backupName: 备份文件名（如 "user_mappings.json.20260605_120000.bak"）
func (s *DataBackupService) RestoreBackup(backupName string) error {
	src := filepath.Join(s.dataDir, "backups", backupName)
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取备份失败: %w", err)
	}

	// 从备份文件名提取原始文件名：user_mappings.json.20260605_120000.bak → user_mappings.json
	parts := strings.SplitN(backupName, ".", 2)
	if len(parts) == 0 {
		return fmt.Errorf("无效的备份文件名")
	}
	// 找到第一个 ".数字" 之前的部分
	originalName := backupName
	if idx := strings.Index(backupName, ".20"); idx > 0 {
		originalName = backupName[:idx]
	}

	dst := filepath.Join(s.dataDir, originalName)
	return os.WriteFile(dst, data, 0644)
}
