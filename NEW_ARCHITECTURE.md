# Emby Telegram Bot - 企业级架构重构

## 概述

本项目已完成企业级架构重构，采用 Go 语言最佳实践，解决了原有代码库的架构问题。

## 原有问题

1. **main.go 过大** (6,876行) - 违反单一职责原则
2. **双路由系统竞争** - Legacy vs BotModule 路由混乱
3. **回调格式不统一** - 多种格式并存
4. **全局状态滥用** - 难以测试和维护
5. **职责混杂** - HTTP、Telegram、业务逻辑混在一起

## 新架构

### 目录结构

```
emby-telegram-bot/
├── cmd/
│   └── server/
│       └── main.go           # 应用入口点
├── internal/
│   ├── callback/             # 回调系统
│   │   └── types.go          # 统一回调格式、解析器、注册器
│   ├── config/               # 配置管理
│   │   └── config.go
│   ├── handlers/             # 处理器
│   │   ├── callback.go       # StartHandler, DetailHandler, BackHandler
│   │   ├── request.go        # RequestHandler, SearchHandler
│   │   └── menu.go           # MyRequestsHandler, LinkHandler, HelpHandler, AIHandler
│   ├── middleware/           # 中间件
│   │   └── callback.go       # Logger, Recovery, Validator, RateLimiter
│   ├── services/             # 服务层
│   │   ├── jellyseerr.go     # Jellyseerr 客户端
│   │   └── telegram.go       # Telegram 客户端
│   └── session/              # 会话管理
│       └── manager.go        # Session Manager
├── pkg/
│   ├── errors/               # 错误处理
│   │   └── errors.go         # 统一错误类型
│   └── types/                # 共享类型
│       └── telegram.go       # Telegram 类型定义
└── .env.example              # 环境变量示例
```

### 核心组件

#### 1. 回调系统 (internal/callback/)

统一的回调格式和处理器注册机制：

```go
// 回调格式: action:param1:value1:param2:value2
"detail:id:123:type:movie"
"request:id:456:type:tv:season:1"

// 回调解析器
parser := callback.NewParser()
cb, _ := parser.Parse("detail:id:123:type:movie")
// cb.Action = "detail"
// cb.Params = {"id": "123", "type": "movie"}

// 回调注册器
registry := callback.NewRegistry()
registry.Use(middleware.Logger)
registry.RegisterFunc(callback.ActionDetail, myHandler)
```

#### 2. 配置管理 (internal/config/)

基于环境变量的配置加载：

```go
cfg, err := config.Load()
// 自动从环境变量和 .env 文件加载配置
```

#### 3. 服务层 (internal/services/)

- **JellyseerrClient**: Jellyseerr API 客户端
- **TelegramClient**: Telegram Bot API 客户端
- **MessageBuilder**: 消息构建工具
- **KeyboardBuilder**: 键盘构建工具

#### 4. 中间件 (internal/middleware/)

- **Logger**: 记录回调处理日志
- **Recovery**: 恢复 panic
- **Validator**: 验证回调数据
- **RateLimiter**: 速率限制

#### 5. 错误处理 (pkg/errors/)

统一的错误类型和错误码：

```go
// 错误构造
return errors.InvalidInput("media ID is required")
return errors.JellyseerrErr("failed to fetch", err)

// 错误检查
if errors.Is(err, errors.ErrCodeNotFound) {
    // 处理未找到错误
}
```

### 部署

#### 环境变量

```bash
# Telegram
TELEGRAM_BOT_TOKEN=your_token
TELEGRAM_CHAT_ID=

# Jellyseerr
JELLYSEERR_URL=http://jellyseerr:5055
JELLYSEERR_API_KEY=your_api_key

# Server
PORT=8080
HOST=0.0.0.0
```

#### 构建

```bash
go build -o emby-telegram-bot ./cmd/server/main.go
```

#### Docker

```bash
docker build -f Dockerfile.new -t emby-telegram-bot:new .
```

### 回调格式规范

所有回调遵循统一格式：`action:key1:value1:key2:value2`

| Action | 格式 | 说明 |
|--------|------|------|
| start | `start` | 打开开始菜单 |
| search | `search` | 显示搜索提示 |
| detail | `detail:id:123:type:movie` | 显示媒体详情 |
| request | `request:id:123:type:movie` | 创建媒体请求 |
| back | `back` | 返回上一页 |
| cancel | `cancel` | 取消当前操作 |
| ai | `ai:type:trending` | AI 推荐 |

### 下一步

- [ ] 完善搜索功能
- [ ] 实现分页导航
- [ ] 添加单元测试
- [ ] 集成现有 AI 推荐系统
- [ ] 迁移用户绑定功能
- [ ] 添加管理员功能
