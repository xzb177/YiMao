# YiMao

YiMao 是面向 Telegram 的私人影视任务中心，把搜索、求片、审核、MoviePilot 下载/整理进度和 Emby 入库通知放在同一条链路中。用户可以使用 Bot 对话或 Mini App；AI 和许愿池是增强能力，不是部署主链路的前置条件。

## 工作方式

```text
Telegram Bot / Mini App
          |
          v
        YiMao
          |
          +-- MoviePilot：搜索、订阅、下载与整理状态
          +-- Emby：媒体库可见性与真正可播放状态
          +-- TMDB：元数据与海报
          +-- SQLite/JSON：绑定、审核、任务和用户偏好
```

普通求片的实际流程：

```text
搜索 -> 选择媒体/季度 -> 绑定与配额检查 -> 自动通过或管理员审核
     -> MoviePilot 订阅 -> 下载/整理 -> Emby 入库 -> Telegram 通知可以看
```

“MoviePilot 下载完成”和“Emby 已经可以看”是两个不同状态。YiMao 不会在外部系统失败时把错误伪装成空态或成功。

## 当前产品入口

- Bot 以搜索求片为默认路径，保留详情/季度、候选资源、求片进度、想看/拼车、许愿池、洗版、问题反馈和入库通知。
- Mini App 是 App-first 任务中心：首页先展示“今晚要看 / 卡住的事 / 正在替你办”，再进入找片、求片/洗版提交、任务时间线、想看、许愿和反馈。
- 游戏中心只保留电影情报站、盲盒、命运轮盘和观影画像；Roulette 的进入和再转一次回调都可用。
- 管理员负责求片/洗版审核、洗版认领与 MediaSource 安全核验、反馈处理和通知设置。洗版完成必须先进入明确确认，不会绕过旧版保留检查。

## 新用户一键部署

前置条件：Linux、Docker daemon、Git、curl，以及宿主机可访问的 MoviePilot。

```bash
git clone https://github.com/xzb177/YiMao.git /opt/YiMao
cd /opt/YiMao
./install.sh
```

首次执行会生成权限为 `0600` 的 `.env` 并退出。填写以下必需项：

- `TELEGRAM_BOT_TOKEN`
- `ADMIN_USER_IDS`，第一个 Telegram 数字 ID 是 root admin
- `MOVIEPILOT_URL`
- `MOVIEPILOT_API_KEY`
- `API_KEYS`，默认 API auth 开启时必填

然后执行：

```bash
chmod 600 .env
./manage.sh install
```

安装脚本会真实执行 Docker verify stage、生产镜像构建、Git revision 标记、named volume 创建、事务式容器切换和健康等待。已有容器升级失败时会自动恢复旧容器。

`/link` 用于绑定 MoviePilot 账号，不会自动授予管理员权限。

完整步骤、Webhook、Mini App、备份、回滚和排障见 [部署与运维](docs/DEPLOY.md)。

## 运行拓扑

默认生产参数：

- host network
- `restart=unless-stopped`
- named volume `yimao-data:/app/data`
- Docker socket `/var/run/docker.sock`
- `/health` 与 Docker `HEALTHCHECK`
- OCI label `org.opencontainers.image.revision=<git commit>`

由于使用 host network，`MOVIEPILOT_URL` 和 `EMBY_URL` 必须是宿主机可访问地址，不能默认使用 Compose service name。

## 常用运维

```bash
./manage.sh status               # 容器、镜像、revision、health、重启次数
./manage.sh logs                 # 日志
./manage.sh doctor               # 配置、代码、拓扑和依赖完整诊断
./manage.sh backup               # 一致性备份 named volume
./manage.sh update               # fast-forward、验证、备份、事务升级
./manage.sh rollback BACKUP_DIR  # checksum 验证后恢复环境、数据和旧镜像
./manage.sh telegram             # 设置默认 Mini App 菜单和 revision
./manage.sh uninstall            # 删除应用，默认保留数据卷
```

只有明确不再需要数据时才执行：

```bash
./manage.sh uninstall --delete-data
```

卸载 YiMao 不会删除 MoviePilot、Emby、下载器、rclone、媒体目录或云端媒体。

## HTTP 入口

- `GET /health`：服务与依赖健康检查
- `POST /webhook`：Telegram webhook 模式
- `POST /webhook/moviepilot`、`POST /webhook/mp`：MoviePilot 事件
- `POST /webhook/emby`：Emby 事件
- `GET /miniapp`：App-first Mini App shell
- `/api/miniapp/v1/*`：Telegram initData 鉴权后的搜索、详情、任务、求片/洗版和反馈 API。
- `/api/summary`、`/api/stats`、`/api/admins*`：受 API auth 或 localhost 限制的管理 API

设置 `WEBHOOK_SECRET` 后，MoviePilot/Emby webhook 调用方必须携带匹配的 token/signature。Mini App API 使用 Telegram `initData` HMAC 校验，不信任客户端直接传入的 user ID。

## 存储

运行数据位于 `/app/data`：

- SQLite：用户映射、搜索历史、许愿池、游戏/社交数据
- JSON：配额、偏好、审核工单、反馈和通知设置
- 内存：临时搜索分页和对话会话

SQLite 使用 WAL 的模块不能靠单独复制 `.db` 文件备份。`./manage.sh backup` 会短暂停止服务，归档整个 named volume，并生成和验证 `SHA256SUMS`。

## 开发与发布门禁

宿主机不需要 Go：

```bash
docker build --target verify -t yimao:verify .
docker build --build-arg REVISION="$(git rev-parse HEAD)" -t yimao:local .
```

verify stage 执行 `gofmt` 检查、`go vet ./...` 和 `go test ./...`。生产发布还应完成隔离 Bot/MoviePilot smoke、Mini App 移动端真机验收、备份、健康检查、依赖检查和重启次数检查。

- [项目流程与代码边界](docs/ARCHITECTURE.md)
- [部署与运维](docs/DEPLOY.md)
- [Staging 验收](docs/STAGING.md)
- [RC 验收模板](docs/RC_ACCEPTANCE_TEMPLATE.md)

## License

[MIT](LICENSE)
