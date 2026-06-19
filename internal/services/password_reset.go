package services

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
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

func pythonStringLiteral(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ResetUserPassword resets a MoviePilot user's password via docker exec.
// dbPath is the path inside the MoviePilot container (for example /config/user.db).
// The target container can be configured with MOVIEPILOT_CONTAINER.
func (c *MoviePilotClient) ResetUserPassword(dbPath, username string) (string, error) {
	newPassword, err := GenerateRandomPassword(randomPasswordLen)
	if err != nil {
		return "", fmt.Errorf("生成随机密码失败: %w", err)
	}

	containerName := strings.TrimSpace(os.Getenv("MOVIEPILOT_CONTAINER"))
	if containerName == "" {
		containerName = "moviepilot-v2"
	}
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = strings.TrimSpace(os.Getenv("MOVIEPILOT_DB_PATH"))
	}
	if dbPath == "" {
		return "", fmt.Errorf("MoviePilot 用户数据库路径未配置，请设置 MOVIEPILOT_DB_PATH（容器内路径，如 /config/user.db）")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker CLI 不可用，无法执行密码重置；Docker 部署请安装 docker-cli 并挂载 /var/run/docker.sock")
	}

	script := fmt.Sprintf(`
import sqlite3, bcrypt

db_path = %s
username = %s
new_password = %s

conn = sqlite3.connect(db_path)
c = conn.cursor()
c.execute('SELECT id FROM user WHERE name=?', (username,))
row = c.fetchone()
if not row:
    print('USER_NOT_FOUND')
    conn.close()
    exit(1)
hashed = bcrypt.hashpw(new_password.encode('utf-8'), bcrypt.gensalt()).decode()
c.execute('UPDATE user SET hashed_password=? WHERE name=?', (hashed, username))
conn.commit()
conn.close()
print('OK')
`, pythonStringLiteral(dbPath), pythonStringLiteral(username), pythonStringLiteral(newPassword))

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
