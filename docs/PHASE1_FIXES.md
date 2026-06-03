# Phase 1 修复记录（安全 + 正确性）

> 本阶段只做"修复 + 健壮性"，不引入新功能。每项改动后均通过
> `go build ./...`、`go vet ./...`、`go test ./...`、`go build -race ./cmd/bot`。

## 🔴 安全

### 1. Webhook 入口增加可选验签（防伪造 / 防重放基础）
- `internal/api/router.go`：`HandleWebhook` 增加 `verifyWebhookAuth`。
  当配置 `WEBHOOK_SECRET` 时，所有入站 webhook 必须携带：
  - `X-Webhook-Signature: sha256=<hmac>`（对 raw body 做 HMAC-SHA256，`hmac.Equal` 常数时间比较），或
  - `?token=` / `X-Webhook-Token` 共享密钥（`subtle.ConstantTimeCompare`）。
  - body 读取限制 1MB，校验后用 `io.NopCloser` 还原供下游再次读取。
- **未配置 secret 时跳过校验 → 完全向后兼容**，不影响现有部署。
- 新增配置：`WEBHOOK_SECRET`（config.go）。

### 2. 去除 Emby webhook body 全量日志
- `internal/api/router.go`：`[API] Emby webhook received ... Body: <整包>` →
  改为只记录 `Body length: N bytes`，避免 payload 落盘。

### 3. TLS 校验改为可配置，默认安全
- `internal/services/webhook_emby_api.go`：`InsecureSkipVerify: true`（硬编码）
  → `InsecureSkipVerify: s.embySkipTLSVerify`，默认 `false`（校验证书）。
- 新增配置：`EMBY_SKIP_TLS_VERIFY`（默认 false），仅在可信自签名场景显式开启。
- 串接链路：config → WebhookService 构造 → 使用点。

### 4. 死代码 handleDebug 敏感字段清除（纵深防御）
- `internal/api/router.go`：`handleDebug` 原本回显 `moviepilot_url / emby_url /
  data_dir / webhook_url / admin_count`。该 handler 实际未被路由（`SetupRoutes`
  从未调用），属死代码，但仍清除敏感字段，改为只回 `*_configured` 布尔，防止
  日后误接线泄露。

> 说明：审查初稿提到的「RateLimiter 定义未注册」「/api/admins 列出管理员」经
> 复核为**误报** —— middleware 包中并不存在 RateLimiter/AdminOnly；`/api/admins`
> 实际路由到 POST-only 的 HandleWebhook，GET 返回 405。故未做改动。

## 🟠 正确性（并发 data race，P0）

### 5. Scheduler 配置字段加锁 + Stop 防重复 close
- `internal/services/scheduler.go`：
  - 新增 `sync.RWMutex` 保护 `dailyHour / dailyMinute / enabled`
    （`run()` goroutine 与 `SetDailyTime/SetEnabled` 并发读写）。
  - `Stop()` 用 `sync.Once` 包裹 `close(stopCh)`，二次调用不再 panic。

### 6. 推荐引擎 userProfiles 结构体竞争修复
- `ai/recommendation_v2.go`：原代码在 `RUnlock()` 之后才读取
  `profile.Behavior/Context/...` 嵌套字段，与写端（RecordInteraction /
  UpdateMood / analyzeUserProfile）的并发写构成 data race。
  - 新增 `snapshotProfile(userID)`：在读锁内对 profile 做 JSON 深拷贝返回，
    读端全部改用快照，彻底消除共享可变状态。
  - `analyzeUserProfile`：读用快照，写回时重新在写锁内取真本 profile。

## 🟡 健壮性（P1）

### 7. JSON 状态文件原子写入
- 新增 `internal/services/atomic_write.go`：`atomicWriteFile`（同目录 temp +
  `Sync` + `Chmod` + `os.Rename` 原子替换）。
- 替换以下文件的 `os.WriteFile`：admin / image_cache / issue /
  media_notification / notification / preferences / quota / review /
  search_history / user_mapping / weekly_report。
- `internal/config/config.go`：`saveAdmins` 同样改原子写（独立 `atomicWrite`，
  避免 config→services 反向依赖）。
- 效果：进程被 kill / 崩溃时不会留下半截损坏的 JSON。

## 🧹 仓库卫生

### 8. 删除编译不过的死代码包
- 删除 `internal/presenters/`、`internal/common/`：二者无任何引用，且自身带
  编译错误（`results.Results undefined`、未使用的 `fmt` 等），导致
  `go build ./...` 整体失败。删除后全量构建通过。

### 9. 修正 go.mod 版本声明
- `go.mod`：`go 1.23` → `go 1.24.0`。原仓库依赖 `modernc.org/sqlite@v1.46.1`
  与 `golang.org/x/exp` 均要求 Go ≥ 1.24，但 go.mod 声明 1.23，导致用 1.23
  工具链直接构建失败。修正为 1.24.0 与实际依赖一致。

### 10. 修复 go vet 抓到的两个原有小 bug
- `internal/ui/cinema.go:152`：`fmt.Sprintf("...今日精选...", title)` 多传了
  无格式占位的 `title` 参数 → 改为静态字符串。
- `internal/handlers/search_history.go:305`：`s = s` 自赋值 → 移除。

## 验证
```
go build ./...        # OK
go vet ./...          # OK（无告警）
go test ./...         # OK
go build -race ./cmd/bot   # OK
```
