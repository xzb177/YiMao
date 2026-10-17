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

// buildResetPasswordScript builds the python script executed inside the
// MoviePilot container to reset a user's password. The script refuses to
// touch MoviePilot superuser accounts (is_superuser=1) and reports a distinct
// SUPERUSER_RESET_DENIED status so the caller can reject the reset.
func buildResetPasswordScript(dbPath, username, newPassword string) string {
	return fmt.Sprintf(`
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
# 超管账号禁止重置：is_superuser 列在老版本库里可能不存在，先探测。
c.execute("PRAGMA table_info('user')")
cols = [r[1] for r in c.fetchall()]
if 'is_superuser' in cols:
    c.execute('SELECT is_superuser FROM user WHERE name=?', (username,))
    srow = c.fetchone()
    if srow and srow[0]:
        print('SUPERUSER_RESET_DENIED')
        conn.close()
        exit(2)
hashed = bcrypt.hashpw(new_password.encode('utf-8'), bcrypt.gensalt()).decode()
c.execute('UPDATE user SET hashed_password=? WHERE name=?', (hashed, username))
conn.commit()
conn.close()
print('OK')
`, pythonStringLiteral(dbPath), pythonStringLiteral(username), pythonStringLiteral(newPassword))
}

// classifyResetPasswordResult maps the reset script's status line to an error.
// A nil return means the reset succeeded.
func classifyResetPasswordResult(result, username string) error {
	switch result {
	case "OK":
		return nil
	case "USER_NOT_FOUND":
		return fmt.Errorf("用户 %s 不存在于 MoviePilot", username)
	case "SUPERUSER_RESET_DENIED":
		return fmt.Errorf("拒绝重置：%s 是 MoviePilot 超管账号", username)
	default:
		return fmt.Errorf("密码重置失败: %s", result)
	}
}

// ResetUserPassword resets a MoviePilot user's password via docker exec.
// dbPath is the path inside the MoviePilot container (for example /config/user.db).
// The target container can be configured with MOVIEPILOT_CONTAINER.
// It always refuses MoviePilot superuser accounts (is_superuser=1).
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
		return "", fmt.Errorf("密码重置不可用：Docker CLI 未安装；请使用隔离的 resetpw 覆盖配置")
	}
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		return "", fmt.Errorf("密码重置不可用：生产请求机器人未授予 Docker socket；请使用隔离的 resetpw 覆盖配置")
	}

	script := buildResetPasswordScript(dbPath, username, newPassword)

	cmd := exec.Command("docker", "exec", containerName, "python3", "-c", script)
	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	if err != nil {
		return "", fmt.Errorf("docker exec 失败: %s (%w)", result, err)
	}
	if err := classifyResetPasswordResult(result, username); err != nil {
		return "", err
	}

	logger.Info("[ResetUserPassword] 密码重置成功: %s", username)
	return newPassword, nil
}
