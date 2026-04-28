# Emby Telegram Bot - Docker 部署说明

## ⚠️ 重要提示

**本机器人必须在 Docker 容器中运行，不要直接运行二进制文件。**

所有后续更新和管理都应使用 Docker 方式。

## 管理脚本

使用 `manage.sh` 脚本管理容器：

```bash
./manage.sh start      # 启动容器
./manage.sh stop       # 停止容器
./manage.sh restart    # 重启容器
./manage.sh status     # 查看状态
./manage.sh logs       # 查看日志
./manage.sh logs-f     # 实时查看日志
./manage.sh rebuild    # 重新构建并启动
./manage.sh update     # 拉取代码、重新构建并启动
./manage.sh shell      # 进入容器 shell
./manage.sh clean      # 清理无用容器和镜像
```

## Docker Compose 命令

```bash
docker compose up -d          # 启动容器
docker compose down           # 停止容器
docker compose restart        # 重启容器
docker compose logs -f        # 查看日志
docker compose build --no-cache  # 重新构建
```

## 环境变量配置

编辑 `.env` 文件配置环境变量：

```bash
# Telegram Bot 配置
TELEGRAM_BOT_TOKEN=你的bot_token
TELEGRAM_CHAT_ID=群组ID

# Jellyseerr 配置
JELLYSEERR_URL=https://your-jellyseerr-url
JELLYSEERR_API_KEY=你的api_key

# AI 配置
ZHIPU_API_KEY=智谱API密钥
CLAUDE_API_KEY=Claude API密钥

# 管理员配置
ADMINS=用户ID:姓名

# 其他配置...
```

## 数据持久化

以下目录和文件会被持久化：

- `./data/` - 数据目录
- `./preferences.json` - 用户偏好
- `./user_quotas.json` - 用户配额
- `./user_mappings.json` - 用户映射
- `./binding_requests.json` - 绑定请求

## 更新代码后重新部署

```bash
# 方法1：使用管理脚本
./manage.sh update

# 方法2：手动操作
git pull
./manage.sh rebuild
```

## 容器信息

- 容器名称: `yimao`
- 镜像名称: `yimao-yimao`
- 端口: `8080`
- 重启策略: `unless-stopped`
- 健康检查: 每30秒检查一次

## 故障排查

查看容器日志：
```bash
./manage.sh logs-f
```

进入容器调试：
```bash
./manage.sh shell
```

重启容器：
```bash
./manage.sh restart
```
