package services

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"

	"github.com/xzb177/yimao/pkg/logger"
)

const (
	randomPasswordLen = 10
	randomCharset     = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789" // no confusing chars (0/O/l/1/I)
)

// GenerateRandomPassword generates a cryptographically secure random password.
func GenerateRandomPassword(length int) (string, error) {
	var sb strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(randomCharset))))
		if err != nil {
			return "", err
		}
		sb.WriteByte(randomCharset[n.Int64()])
	}
	return sb.String(), nil
}

// ResetUserPassword resets a MoviePilot user's password by directly updating the SQLite database.
// Returns the new random password on success.
// Strategy: copy DB → modify copy → replace original (avoids WAL lock conflict with MoviePilot).
func (c *MoviePilotClient) ResetUserPassword(dbPath, username string) (string, error) {
	if dbPath == "" {
		return "", fmt.Errorf("MoviePilot 数据库路径未配置 (MOVIEPILOT_DB_PATH)")
	}

	// Generate random password
	newPassword, err := GenerateRandomPassword(randomPasswordLen)
	if err != nil {
		return "", fmt.Errorf("生成随机密码失败: %w", err)
	}

	// Hash with bcrypt (same as MoviePilot uses: $2b$12$...)
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return "", fmt.Errorf("密码哈希失败: %w", err)
	}

	// Step 1: Copy DB to temp location to avoid WAL lock conflict
	tmpPath := dbPath + ".tmp"
	srcFile, err := os.Open(dbPath)
	if err != nil {
		return "", fmt.Errorf("打开数据库失败: %w", err)
	}

	dstFile, err := os.Create(tmpPath)
	if err != nil {
		srcFile.Close()
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		srcFile.Close()
		dstFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("复制数据库失败: %w", err)
	}
	srcFile.Close()
	dstFile.Close()

	// Step 2: Open the copy and update password
	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("打开临时数据库失败: %w", err)
	}

	var userID int64
	err = db.QueryRow("SELECT id FROM user WHERE name = ?", username).Scan(&userID)
	if err == sql.ErrNoRows {
		db.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("用户 %s 不存在于 MoviePilot", username)
	}
	if err != nil {
		db.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("查询用户失败: %w", err)
	}

	result, err := db.Exec("UPDATE user SET hashed_password = ? WHERE id = ?", string(hash), userID)
	if err != nil {
		db.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("更新密码失败: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		db.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("密码更新未生效")
	}

	// Checkpoint WAL into main DB before closing
	db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	db.Close()

	// Step 3: Replace original with modified copy
	if err := os.Rename(tmpPath, dbPath); err != nil {
		// Try copy instead of rename
		os.Remove(tmpPath)
		return "", fmt.Errorf("替换数据库失败: %w", err)
	}

	logger.Info("[PasswordReset] User %s (ID:%d) password reset successfully", username, userID)
	return newPassword, nil
}
