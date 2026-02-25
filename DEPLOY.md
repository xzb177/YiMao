# 快速部署指南

## 前置要求

- Docker 和 Docker Compose 已安装
- 已安装 MoviePilot（推荐）
- 已安装 Emby/Jellyfin（可选）

## 部署步骤

### 1. 获取代码

```bash
git clone https://github.com/xzb177/YiMao.git
cd YiMao
```

### 2. 配置环境变量

```bash
cp .env.example .env
nano .env  # 或使用你喜欢的编辑器
```

### 3. 填写必需配置

在 `.env` 文件中配置以下项：

| 配置项 | 说明 | 示例 |
|--------|------|------|
| `TELEGRAM_BOT_TOKEN` | Bot Token | `123456:ABC-DEF...` |
| `MOVIEPILOT_URL` | MoviePilot 地址 | `http://192.168.1.100:4500` |
| `MOVIEPILOT_API_KEY` | MoviePilot API Key | `abc123xyz` |
| `ADMINS` | 管理员 ID | `123456789` |

### 4. 启动服务

```bash
docker compose up -d
```

### 5. 查看日志

```bash
docker logs -f emby-telegram-bot
```

看到 `🌐 Server listening on 0.0.0.0:8080` 表示启动成功。

### 6. 在 Telegram 测试

给你的 Bot 发送 `/start` 命令，应该收到主菜单回复。

## 获取配置信息

### Telegram Bot Token

1. 在 Telegram 搜索 [@BotFather](https://t.me/BotFather)
2. 发送 `/newbot` 创建新机器人
3. 按提示设置名称和用户名
4. 获取到的 Token 即为 `TELEGRAM_BOT_TOKEN`

### Telegram 用户 ID

1. 在 Telegram 搜索 [@userinfobot](https://t.me/userinfobot)
2. 发送任意消息
3. 返回的 `Id` 即为你的 Telegram ID

### MoviePilot API Key

1. 登录 MoviePilot 网页
2. 进入 `设置` -> `API安全`
3. 创建或复制 API Key

### Emby API Key

1. 登录 Emby 网页
2. 进入 `设置` -> `密钥` -> `+`
3. 新建密钥并复制

## 故障排查

### Bot 无响应

```bash
# 检查服务状态
docker ps

# 查看日志
docker logs emby-telegram-bot
```

常见原因：
- `TELEGRAM_BOT_TOKEN` 填写错误
- 网络无法访问 Telegram API
- MoviePilot 地址配置错误

### 求片失败

检查 MoviePilot 配置：
- `MOVIEPILOT_URL` 是否正确（包含端口）
- `MOVIEPILOT_API_KEY` 是否有效
- MoviePilot 服务是否正常运行

### 入库通知不推送

检查 Emby 配置：
- `EMBY_URL` 和 `EMBY_API_KEY` 是否配置
- Emby Webhook 是否正确指向 Bot 地址
- MoviePilot 是否配置了通知 Webhook

## 升级更新

```bash
git pull
docker compose up -d --build
```

## 卸载

```bash
docker compose down
rm -rf data
```
