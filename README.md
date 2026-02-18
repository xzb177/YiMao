# Emby Telegram Bot

Emby 媒体库入库 Telegram 通知服务，通过 Webhook 接收 Emby 事件并发送到 Telegram 群组。

## 功能特性

- 接收 Emby Webhook 事件通知
- 新媒体入库时自动发送 Telegram 通知
- 支持电影、剧集、季度等不同类型
- 消息格式美观，带 emoji 图标
- 支持 Markdown 格式

## 部署方式

### Docker Compose (推荐)

1. 复制环境变量配置文件：
```bash
cp .env.example .env
```

2. 编辑 `.env` 文件，填入你的 Telegram Bot 配置：
```bash
TELEGRAM_BOT_TOKEN=你的bot_token
TELEGRAM_CHAT_ID=你的群组id
PORT=8080
```

3. 启动服务：
```bash
docker-compose up -d
```

### 直接运行

1. 设置环境变量：
```bash
export TELEGRAM_BOT_TOKEN=你的bot_token
export TELEGRAM_CHAT_ID=你的群组id
```

2. 运行：
```bash
go run main.go
```

### 编译后运行

```bash
go build -o emby-telegram-bot
./emby-telegram-bot
```

## 获取 Telegram 配置

### 获取 Bot Token

1. 在 Telegram 中找到 [@BotFather](https://t.me/BotFather)
2. 发送 `/newbot` 创建新机器人
3. 按提示设置名称
4. 保存返回的 Token

### 获取 Chat ID

**对于群组：**
1. 将机器人拉入群组
2. 在群组发送一条消息
3. 访问：`https://api.telegram.org/bot<你的TOKEN>/getUpdates`
4. 找到 `chat.id` (通常是负数)

**对于频道：**
1. 将机器人设为频道管理员
2. 往频道发一条消息
3. 同样用 getUpdates 获取

## Emby Webhook 配置

1. 打开 Emby 控制台
2. 导航到 **设置** → **高级** → **Webhook**
3. 添加新 Webhook：
   - **URL**: `http://your-server-ip:8080/webhook`
   - **事件类型**: 勾选 `Item Added` 等
   - **请求类型**: `POST (JSON)`

## API 端点

- `/webhook` - Emby Webhook 接收端点
- `/health` - 健康检查端点

## 消息示例

```
📺 *新剧集入库*

📺 剧集: 第1集
📂 季度: 第一季
🔢 集数: E01
🎬 所属: 某部剧集

🕐 时间: 2026-02-16 12:00:00
```

```
🎥 *新电影入库*

🎬 电影: 某部电影
📅 年份: 2024

🕐 时间: 2026-02-16 12:00:00
```
