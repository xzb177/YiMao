# 云海求片助手 - Docker 部署说明

## 运行方式

推荐使用 Docker Compose 部署。每次更新前先执行只读验收，确认代码、配置、测试和构建通过后再启动或重建服务：

```bash
./scripts/preflight.sh --env
```

## 管理脚本

使用仓库内的 `scripts/ops.sh`：

```bash
./scripts/ops.sh preflight  # 部署前验收，不启动、不重启
./scripts/ops.sh start      # 启动容器
./scripts/ops.sh stop       # 停止容器
./scripts/ops.sh restart    # 重启容器
./scripts/ops.sh status     # 查看状态
./scripts/ops.sh logs       # 实时日志
./scripts/ops.sh rebuild    # 重新构建并启动
./scripts/ops.sh update     # 拉取代码、重新构建并启动
./scripts/ops.sh health     # 健康检查
./scripts/ops.sh backup     # 备份数据与配置
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
# 必需
TELEGRAM_BOT_TOKEN=你的_bot_token
MOVIEPILOT_URL=http://moviepilot:4500
MOVIEPILOT_API_KEY=你的_moviepilot_api_key

# HTTP API 鉴权默认开启；Key 至少 16 个字符
ENABLE_API_AUTH=true
API_KEYS={"replace-with-32-random-characters":"deployment"}

# 可选
ADMIN_USER_IDS=123456789,987654321
EMBY_URL=http://emby:8096
EMBY_API_KEY=你的_emby_api_key
TMDB_API_KEY=你的_tmdb_api_key
```

完整字段与说明以 [`.env.example`](../.env.example) 为准。

## 数据持久化

Compose 将宿主机的 `./data/` 挂载到容器 `/app/data`。用户映射、配额、反馈、搜索历史、许愿池等运行数据都应保存在该目录中；升级前使用 `./scripts/ops.sh backup` 备份 `data/` 与 `.env`。

## 更新代码后重新部署

```bash
# 方法 1：先验收，再使用运维脚本更新
./scripts/ops.sh preflight
./scripts/ops.sh update

# 方法 2：手动更新
./scripts/preflight.sh --env
git pull --ff-only
docker compose up -d --build
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
./scripts/ops.sh logs
```

进入容器调试：
```bash
docker exec -it yimao sh
```

健康检查与重启：
```bash
./scripts/ops.sh health
./scripts/ops.sh restart
```
