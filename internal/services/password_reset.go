package services

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os/exec"
	"strings"

	"github.com/xzb177/yimao/pkg/logger"
)

const (
	randomPasswordLen = 10
	randomCharset     = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
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

// ResetUserPassword resets a MoviePilot user's password via docker exec.
// This uses MoviePilot's own Python/SQLite engine, avoiding WAL corruption.
func (c *MoviePilotClient) ResetUserPassword(dbPath, username string) (string, error) {
	newPassword, err := GenerateRandomPassword(randomPasswordLen)
	if err != nil {
		return "", fmt.Errorf("生成随机密码失败: %w", err)
	}

	containerName := "moviepilot-v2"
	
	// Use docker exec to update password via MoviePilot's own Python
	script := fmt.Sprintf(`
import sqlite3, bcrypt
conn = sqlite3.connect('/config/user.db')
c = conn.cursor()
c.execute('SELECT id FROM user WHERE name=?', ('%s',))
row = c.fetchone()
if not row:
    print('USER_NOT_FOUND')
    exit(1)
hashed = bcrypt.hashpw(b'%s', bcrypt.gensalt()).decode()
c.execute('UPDATE user SET hashed_password=? WHERE name=?', (hashed, '%s'))
conn.commit()
print('OK')
conn.close()
`, username, newPassword, username)

	cmd := exec.Command("docker", "exec", containerName, "python3", "-c", script)
	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	if err != nil {
		return "", fmt.Errorf("docker exec 失败: %s (%w)", result, err)
	}
	if result == "USER_NOT_FOUND" {
		return "", fmt.Errorf("用户 %s 不存在于 MoviePilot", username)
	}
	if result != "OK" {
		return "", fmt.Errorf("密码重置失败: %s", result)
	}

	logger.Info("[ResetUserPassword] 密码重置成功: %s", username)
	return newPassword, nil
}
