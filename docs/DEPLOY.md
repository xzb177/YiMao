# YiMao 部署与运维

本文以仓库中的 `manage.sh`、`Dockerfile`、`docker-compose.yml` 和程序配置校验为准。所有命令均在 YiMao 项目目录执行。

## 1. 新机器前置条件

必需：

- 64 位 Linux 主机
- 可用的 Docker daemon；当前用户可执行 `docker info`
- `git`、`curl`、`sha256sum`
- 可访问 Telegram Bot API
- 已运行且从宿主机可访问的 MoviePilot

不需要在宿主机安装 Go。构建、`gofmt`、`go vet` 和 `go test ./...` 都在 Docker verify stage 中完成。

正式发布前运行完整门禁：

```bash
./scripts/preflight.sh --env --engine docker --lifecycle
```

`--lifecycle` 使用独立的 `yimao-lifecycle-*` 容器和 volume，验证备份、信号中断、故障恢复、回滚和卸载，不会操作生产 `yimao` 容器或 `yimao-data` volume。

`install`、`update` 和 `rollback` 都会在切换前确认现有 `/app/data` 正是受管的 `yimao-data` named volume；检测到旧 bind mount 时会停止操作，必须先按本文迁移步骤离线迁移。

YiMao 固定使用：

- `network=host`
- `restart=unless-stopped`
- named volume `yimao-data:/app/data`
- no Docker socket mount in the production `yimao` container
- Docker `HEALTHCHECK` 与 `GET /health`

因此 `MOVIEPILOT_URL`、`EMBY_URL` 必须是**宿主机可访问地址**。不要照搬 Compose service name，除非宿主机本身能解析该名称。

## 2. 获取项目

推荐克隆后本地执行，不要直接把远程脚本管道给 root shell：

```bash
git clone https://github.com/xzb177/YiMao.git /opt/YiMao
cd /opt/YiMao
./install.sh
```

首次运行会创建 `.env` 并退出。编辑配置后再次运行：

```bash
chmod 600 .env
./manage.sh install
```

`install` 的实际顺序：

1. 检查 Docker daemon、socket、`curl` 和 checksum 工具。
2. 校验 `.env` 权限与管理员配置。
3. 运行 Docker verify stage、应用配置校验及生产镜像构建。
4. 将 Git commit 写入 OCI revision label。
5. 创建 `yimao-data` named volume。
6. 事务切换容器并等待 Docker health 与 `/health` 同时成功。
7. 新容器失败时自动恢复原容器。
8. 配置了 `MINI_APP_URL` 时更新 Telegram 默认 Mini App 菜单。

## 3. 环境变量

### 必填

- `TELEGRAM_BOT_TOKEN`：从 `@BotFather` 创建 Bot 后获取。
- `ADMIN_USER_IDS`：Telegram 数字用户 ID，逗号分隔；第一个 ID 是 root admin。
- `MOVIEPILOT_URL`：MoviePilot 的宿主机可达 URL。
- `MOVIEPILOT_API_KEY`：MoviePilot API Key。
- `API_KEYS`：当 `ENABLE_API_AUTH=true` 时必填，格式是 JSON object，每个 key 至少 16 字符。

`/link` 只绑定或创建 MoviePilot 账号，**不会授予管理员权限**。

### 推荐

- `EMBY_URL` 与 `EMBY_API_KEY`：必须同时填写。用于媒体库检查和“真正可看”状态确认。
- `WEBHOOK_SECRET`：保护 Emby/MoviePilot 入站 Webhook。
- `TMDB_API_KEY`：影视元数据与海报搜索。

### OpenAI 兼容 AI

启用时同时配置：

```dotenv
AI_ENABLED=true
OPENAI_BASE_URL=https://provider.example/v1
OPENAI_API_KEY=[REDACTED]
OPENAI_MODEL=model-name
```

AI 不是求片主链路的前置条件。未启用时搜索、订阅、追踪、入库通知和 Mini App 仍可工作。

### Telegram Webhook 与 polling

- `WEBHOOK_URL` 为空：启动时删除旧 Telegram webhook，使用 long polling。
- `WEBHOOK_URL` 非空：必须同时设置 `TELEGRAM_WEBHOOK_SECRET`，URL 指向公开 HTTPS 的 `/webhook`。

## 4. Docker 启动顺序

建议外部依赖顺序：

1. MoviePilot 及其数据库、下载器依赖可用。
2. Emby 可用；若暂不接入，可将两个 Emby 变量都留空。
3. YiMao 构建并启动。
4. `/health` 检查 MoviePilot；配置 Emby 后也检查 Emby。
5. Telegram 命令菜单由程序启动时自动设置。
6. Mini App 菜单由 `./manage.sh telegram` 设置。

查看真实参数：

```bash
./manage.sh status
docker inspect yimao
./manage.sh doctor
```

`doctor` 会重跑代码/配置门禁，成本较高；日常只看状态可用 `status`。

## 5. 首次初始化

程序第一次启动会在 `/app/data` 自动创建/迁移所需 JSON 与 SQLite 数据。不要手工预创建旧 JSON 模板。

初始化后按顺序验收：

1. `./manage.sh status` 显示 `healthy`、revision 非 `unknown`、重启次数为 `0`。
2. `curl -fsS http://127.0.0.1:8080/health` 返回 `status=ok`。
3. 在 Telegram 私聊 Bot 发送 `/start`。
4. root admin 能看到管理员入口。
5. 新 MoviePilot 用户发送 `/link 用户名`；已有用户发送 `/link 用户名 密码`。
6. 搜索一部测试影片并确认候选、审核/订阅、进度查询正常。

### Mini App URL 与 revision

先通过 HTTPS 反向代理或 Tunnel 暴露：

```text
https://bot.example.com/miniapp
```

在 `.env` 设置：

```dotenv
MINI_APP_URL=https://bot.example.com/miniapp
```

这里必须填写不含凭据、query 参数或 fragment 的 HTTPS 基础 URL。影视详情入口需要的 `tmdb_id`、`type`、`season` 由应用校验后生成，不应手工写进环境变量。

然后执行：

```bash
./manage.sh telegram
```

脚本从运行镜像读取 OCI revision，并将 `?v=<revision>` 加到 Telegram 默认 Web App URL。每次升级成功后也会自动刷新。菜单文本和 URL 不包含 Token。

若需要 chat-specific 菜单，需使用 Telegram Bot API 单独指定 `chat_id`；默认脚本只设置全局菜单，避免覆盖已有私聊定制。

## 6. Emby 与 MoviePilot Webhook

YiMao 接收：

- `POST /webhook/moviepilot` 或 `/webhook/mp`
- `POST /webhook/emby`

设置 `WEBHOOK_SECRET` 后，调用方必须携带匹配的 token/signature。公网反向代理只需暴露必要路由；管理 API 不应直接裸露。

MoviePilot “下载完成”只表示资源已齐，YiMao 会显示等待入库；Emby webhook 确认媒体已索引后，才进入“真正可看”通知。

## 7. 备份

```bash
./manage.sh backup
```

备份流程会短暂停止 YiMao，使 SQLite 和 WAL 静止，再归档整个 `yimao-data` volume。备份包含：

- `data.tar.gz`
- `env.backup`，权限 `0600`
- `container.state`，记录镜像、revision、network 和 restart policy
- `SHA256SUMS`

脚本会立即执行 checksum 校验，并在完成后恢复服务及健康检查。默认目录为 `./backups/<timestamp>`，可通过 `YIMAO_BACKUP_DIR` 放到独立磁盘。

## 8. 升级与失败回滚

```bash
./manage.sh update
```

升级要求工作区干净且当前分支有 upstream。顺序为：

1. `git fetch` 与 `git merge --ff-only`。
2. 完整 verify/config/build，不影响当前容器。
3. 创建一致性数据备份。
4. 保留旧容器作为临时回滚点。
5. 启动新镜像并等待健康。
6. 健康失败自动恢复旧容器；成功才删除旧容器。
7. 配置了 Mini App 时更新 revision。

数据或环境需要恢复时：

```bash
./manage.sh rollback ./backups/20260808_120000
```

回滚前会验证 `SHA256SUMS`，恢复备份中的 `.env`、named volume 和旧镜像。旧镜像若已被删除，脚本会拒绝恢复，不会用未知镜像继续。

## 9. 卸载

默认只删除应用容器，保留数据：

```bash
./manage.sh uninstall
```

确认不再需要任何 YiMao 数据后，才显式删除 named volume：

```bash
./manage.sh uninstall --delete-data
```

该命令不删除 MoviePilot、Emby、下载器、rclone、媒体目录或云端媒体。

## 10. 从旧安装迁移

旧版可能使用 systemd、宿主目录 bind mount 或散落在项目根目录的 JSON/SQLite 文件。不要让新容器与旧进程同时写同一份数据，也不要在旧服务运行时只复制 SQLite 主文件。

迁移按以下顺序执行：

1. 从旧服务的实时配置或 `docker inspect` 确认真实数据目录，不能按历史文档猜路径。
2. 停止旧服务，确认没有进程继续写入 SQLite/WAL。
3. 归档完整数据目录和旧环境配置，生成并验证 SHA-256。
4. 将归档导入临时 named volume，在临时环境核对 JSON 可解析、所有 SQLite `PRAGMA integrity_check` 返回 `ok`。
5. 验证通过后才将临时 volume 切换为 `yimao-data`，执行 `./manage.sh install`。
6. Telegram、MoviePilot、Emby 和 Mini App 验收完成前保留旧实例及原始归档；失败时停止新实例并恢复旧服务。

这是一次性维护操作，不属于新用户安装路径。旧数据位置、UID 或 SQLite 布局无法唯一确认时应停止迁移，先查清真实运行状态。

`manage.sh backup` 会从当前容器实际挂载的 `/app/data` 导出数据，包括旧 bind mount。`install` 和 `update` 不会自动把 bind mount 切换为 named volume；检测到非标准挂载时会拒绝部署，必须先按上述离线流程迁移到 `yimao-data`，避免新容器启动空数据卷。

## 11. 故障排查

### 配置失败

```bash
chmod 600 .env
./manage.sh preflight
```

常见原因：占位凭据未替换、`API_KEYS` JSON 非法、管理员 ID 为空、Emby 只填了 URL 没填 Key。

### Bot 无响应

```bash
./manage.sh status
./manage.sh logs
```

检查 Telegram API 连通性、Token、polling/webhook 模式以及是否存在另一个实例使用同一 Bot Token。

### `/health` 为 503

读取 JSON 中的 `dependencies`。`moviepilot=unreachable` 或 `emby=unreachable` 表示 YiMao 已启动，但依赖地址从宿主网络不可达。

### `/resetpw` 失败

确认 Docker socket 已挂载、`MOVIEPILOT_CONTAINER` 与实际容器名一致、`MOVIEPILOT_DB_PATH` 是 MoviePilot 容器内数据库路径。不要手工编辑密码哈希。

### Mini App 打不开或仍是旧版

```bash
curl -fsS https://bot.example.com/miniapp >/dev/null
./manage.sh telegram
./manage.sh status
```

确认 HTTPS 证书有效、反向代理保留 `/miniapp` 与 `/api/miniapp/v1/`、运行 revision 与菜单 `v=` 一致。
