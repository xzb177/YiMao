# YiMao Staging 与真机验收

本流程用于 `v1.0.0-rc` 前的隔离验收。它不会读取生产 `.env`，不会使用生产数据目录，也不会操作 `yimao` 正式容器。Staging 主机需要 Docker 与 Docker Compose v2；所有 Go 测试和构建都强制在 Docker 的 Go 1.24 环境中执行。

## 隔离边界

| 资源 | Staging 值 | 生产保护 |
|------|------------|----------|
| 容器 | `yimao-staging` | 不操作 `yimao` |
| HTTP | `127.0.0.1:18080` | 不占用正式端口 |
| 数据 | `./staging-data/` | 不挂载 `./data/` |
| 配置 | `.env.staging` | 不读取/覆盖 `.env` |
| 报告 | `./staging-reports/` | 不记录 Token/API Key |
| Telegram | 独立测试 Bot | 与本地生产 Token 相同时拒绝运行 |
| MoviePilot | 独立测试实例 | 与本地生产 URL 相同时拒绝运行 |

`STAGING_CONFIRM_ISOLATED=true` 是显式确认开关；默认模板为 `false`。

## 准备配置

```bash
./scripts/staging.sh init
chmod 600 .env.staging
```

至少填写：

- `STAGING_CONFIRM_ISOLATED=true`
- `STAGING_EXPECTED_BOT_USERNAME`：测试 Bot 用户名，不含或包含 `@` 均可
- `TELEGRAM_BOT_TOKEN`：独立测试 Bot Token
- `MOVIEPILOT_URL`、`MOVIEPILOT_API_KEY`：隔离测试实例
- `API_KEYS`：JSON 对象，例如 `{"32-char-random-staging-key-value":"staging"}`
- `STAGING_SMOKE_CHAT_ID`：可选；填写后 Smoke 会静默发送一条测试消息并立即删除
- `STAGING_REQUIRE_CHAT=true`：如要求消息发送/删除必须通过

可选的 Emby、TMDB、AI 配置也必须指向测试服务。不要复制生产凭据。

## 启动与自动 Smoke

```bash
# 静态、配置、测试、构建与 Compose 验收
./scripts/staging.sh preflight

# 启动隔离容器并等待健康
./scripts/staging.sh up

# 自动只读检查；可选测试消息会立即删除
./scripts/staging.sh smoke

# 查看状态和日志
./scripts/staging.sh status
./scripts/staging.sh logs
```

Smoke 自动检查：

1. `/health` 返回 `ok`，MoviePilot 依赖健康；
2. `/debug` 缺少 API Key 时返回 401，正确 Key 返回 200；
3. Telegram `getMe` 与预期测试 Bot 用户名一致；
4. 测试 Bot 无 webhook，使用 polling；
5. 私聊命令菜单包含核心入口；
6. MoviePilot API Key 的只读请求成功；
7. 配置测试 Chat ID 时，测试消息静默发送并删除；
8. 容器最近日志没有 panic、fatal 或 Telegram `getUpdates` 冲突。

报告写入 `staging-reports/smoke-<UTC>.json`。报告仅包含检查名称、状态、耗时和脱敏详情。

## 72 小时预生产观察

```bash
SOAK_DURATION_SECONDS=259200 \
SOAK_INTERVAL_SECONDS=300 \
./scripts/staging.sh soak
```

任何一次 Smoke 失败都会立即以非零状态退出。长期运行建议交给服务器的 `systemd`、`tmux` 或监控平台；不要依赖 iOS 前台会话。

## 人工真机验收矩阵

### 私聊核心链路

- [ ] `/start` 首屏品牌、菜单层级和按钮顺序正确
- [ ] 中文、英文、Emoji、超长片名搜索均能返回或友好失败
- [ ] 电影详情、剧集季度、候选资源按钮可用
- [ ] 测试账号绑定、求片、重复求片、配额不足、撤回链路正确
- [ ] 「求片进度」与片单不泄露其他用户数据
- [ ] 问题反馈、快捷选项、图片、管理员回复完整可达
- [ ] AI 解说无剧透/剧透切换不串片；过期按钮有友好提示
- [ ] 每日挑战和电影冒险按钮不失效；旧版本消息按钮仍可兼容

### 群聊与隐私

- [ ] 群聊普通消息不展开私人搜索/求片数据
- [ ] 群命令结果仅点击者可见或明确引导私聊
- [ ] `/link`、片单、配额、画像等敏感内容不公开
- [ ] 群聊测试没有编辑或删除公共消息的副作用

### 故障与恢复

- [ ] 临时停止 MoviePilot 后，健康接口转为 degraded，用户收到友好错误
- [ ] 恢复 MoviePilot 后，健康接口自动恢复
- [ ] 重启 `yimao-staging` 后，绑定、反馈、许愿池等持久数据仍在
- [ ] Telegram 429/网络超时只重试允许重试的请求，不重复创建求片
- [ ] 超长消息、说明和按钮不会导致整条消息发送失败
- [ ] 日志中无 Bot Token、API Key、密码、完整带查询参数 URL

## 验收门槛

进入 `v1.0.0-rc.1` 必须同时满足：

- 自动 Smoke 0 失败；如 `STAGING_REQUIRE_CHAT=true`，不得有跳过项；
- 人工核心链路全部通过；
- 无 P0/P1 未解决问题；
- 连续 72 小时观察无重复请求、数据损坏或持续错误增长；
- 备份、恢复和回滚演练另行完成并留存证据。

## 停止 staging

```bash
./scripts/staging.sh down
```

该命令只停止 `yimao-staging`。`staging-data/` 和报告默认保留，便于复盘。
