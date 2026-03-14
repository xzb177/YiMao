# YiMao · Telegram 影视求片助手

> 在 Telegram 内完成「搜索影片 → 发起请求 → 查看进度 → 接收通知」的闭环。

与 MoviePilot + Emby/Jellyfin 配合使用，无需切换应用。

**🔄 部署用户请注意**：更新前请务必查看 [📘 更新指南](UPDATE.md) 以避免数据丢失！

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Last Commit](https://img.shields.io/github/last-commit/xzb177/YiMao)](https://github.com/xzb177/YiMao/commits/master)
[![Stars](https://img.shields.io/github/stars/xzb177/YiMao?style=social)](https://github.com/xzb177/YiMao/stargazers)
[![Issues](https://img.shields.io/github/issues/xzb177/YiMao)](https://github.com/xzb177/YiMao/issues)
[![Forks](https://img.shields.io/github/forks/xzb177/YiMao)](https://github.com/xzb177/YiMao/network/members)

---

## ⚡ 3 分钟快速上手

1. 用 BotFather 创建机器人，拿到 `TELEGRAM_BOT_TOKEN`
2. 准备 MoviePilot 地址与 `MOVIEPILOT_API_KEY`
3. 配置 `.env` 并启动容器

```bash
curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/install.sh | bash
```

启动后在 Telegram 输入 `/start` 即可。

---

## 📋 核心能力

### 主链路功能

| 功能 | 说明 |
|------|------|
| 🔍 **搜索影片** | 支持中英文片名、模糊搜索 |
| 📬 **发起请求** | 命中后一键求片，自动走 MoviePilot 流程 |
| 📊 **查看进度** | 实时跟踪请求状态（排队/搜索/下载/完成） |
| 🔔 **接收通知** | 单集推送 + 每日汇总，可自定义时间 |

### 增强功能

| 功能 | 说明 |
|------|------|
| 📺 **剧集订阅** | 支持按季选择，避免整剧误下 |
| 🎬 **内容推荐** | 结合 TMDB 提供热门、高分、随机推荐 |
| 🤖 **AI 选片** | 按心情入口推荐内容（可选功能） |
| 📜 **搜索历史** | 记录搜索记录，支持快速再次搜索 |
| 🔗 **账号绑定** | 支持 MoviePilot 多用户映射，各看各的记录 |
| 🎨 **多套 UI** | 5 种视觉风格可自由切换 |

### 管理能力

| 功能 | 说明 |
|------|------|
| ✅ **审核流** | 管理员可审核请求，降低滥用风险 |
| 📊 **配额策略** | 按用户限制求片次数 |
| 🐞 **反馈闭环** | 用户提交反馈，管理员统一处理 |

---

## 🚀 一键部署

### 部署前检查清单

- [ ] 已从 @BotFather 获取 `TELEGRAM_BOT_TOKEN`
- [ ] MoviePilot 已安装并可访问
- [ ] 已从 MoviePilot 获取 API Key
- [ ] 确认 MoviePilot 地址（跨机器部署勿用 localhost）
- [ ] （可选）Emby/Jellyfin 地址和 API Key

### 快速安装

```bash
curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/install.sh | bash
```

或手动部署：

```bash
git clone https://github.com/xzb177/YiMao.git
cd YiMao
cp .env.example .env
# 编辑 .env 后启动
docker compose up -d
```

---

## 🛠 配置说明

### 必填环境变量

| 变量名 | 说明 | 获取方式 |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram 机器人 Token | [@BotFather](https://t.me/BotFather) |
| `MOVIEPILOT_URL` | MoviePilot 地址 | 例如 `http://192.168.1.100:4500` |
| `MOVIEPILOT_API_KEY` | MoviePilot API Key | MoviePilot 设置→安全设置 |

> ⚠️ **注意**：候选资源列表功能依赖 MoviePilot，请确保正确配置 MOVIEPILOT_URL 和 API_KEY

### 可选环境变量

| 变量名 | 说明 | 默认值 |
|---|---|---|
| `ADMIN_USER_IDS` | 管理员 Telegram ID（逗号分隔） | 首个绑定用户自动成为管理员 |

### 常用可选变量

| 变量名 | 说明 | 默认值 |
|---|---|---|
| `ADMIN_USER_IDS` | 管理员 Telegram ID | 首个绑定用户自动成为管理员 |
| `EMBY_URL` | Emby/Jellyfin 地址 | - |
| `EMBY_API_KEY` | Emby API Key | - |
| `TMDB_API_KEY` | TMDB API Key | 内置默认值 |
| `TELEGRAM_CHAT_ID` | 群组 Chat ID（通知用） | - |
| `ZHIPU_API_KEY` | 智谱 AI Key（推荐功能） | - |
| `CLAUDE_API_KEY` | Claude API Key（AI 功能） | - |
| `TZ` | 时区 | `Asia/Shanghai` |

---

## 📚 常用命令

### 用户侧

| 命令 | 作用 |
|---|---|
| `/start` | 打开主菜单 |
| `/search 关键词` | 搜索影视 |
| `/ai` | 打开推荐菜单 |
| `/requests` | 查看我的请求 |
| `/link 账号 密码` | 绑定 MoviePilot 账号 |

### 管理员侧（私聊机器人）

- 审核请求
- 配置通知策略
- 查看反馈
- 管理配额

---

## 🔄 维护与更新

> **⚠️ 重要：更新前请务必备份数据！** 详见 [📘 更新指南](UPDATE.md)

### 快速更新

```bash
# 一键更新（推荐）
./manage.sh update

# 查看运行日志
./manage.sh logs-f

# 重启服务
./manage.sh restart

# 停止服务
./manage.sh stop
```

### 更新指南

- [📘 UPDATE.md](UPDATE.md) - **完整更新指南**
  - 更新前准备与数据备份
  - 三种更新方法（脚本/手动/指定版本）
  - 更新后验证与回滚方案
  - 常见问题排查

### 必读：更新注意事项

1. **每次更新前务必备份数据**
   - `data/` 目录
   - `*.json` 配置文件（preferences.json, user_quotas.json 等）

2. **查看更新内容**
   ```bash
   git log HEAD@{1}..HEAD --oneline  # 查看本次更新了什么
   ```

3. **检查配置变更**
   ```bash
   git diff HEAD@{1} .env.example      # 查看是否有新增配置项
   ```

4. **更新后验证**
   - 容器状态是否 `healthy`
   - Bot 是否响应 `/start` 命令
   - 数据是否完整

---

## 🏗 架构概览

```
Telegram User
   │
   ▼
YiMao Bot (Handlers / Services / Session)
   ├── MoviePilot（请求处理、下载链路）
   ├── Emby / Jellyfin（库检查与媒体状态）
   └── TMDB（推荐与元数据）
```

### 代码分层

- `internal/handlers`：命令与回调入口
- `internal/services`：对外部系统能力封装
- `internal/session`：会话与流程状态
- `pkg/*`：通用类型、校验与工具

---

## 📘 项目文档

**部署与维护**
- [📘 UPDATE.md](UPDATE.md) - **更新指南（必读）**
- [DEPLOY.md](DEPLOY.md) - 部署说明
- [DOCKER.md](DOCKER.md) - Docker 管理

**功能说明**
- [COMMANDS.md](COMMANDS.md) - 命令与回调说明
- [FEATURES.md](FEATURES.md) - 功能特性

**开发文档**
- [ARCHITECTURE.md](ARCHITECTURE.md) - 架构设计
- [CHANGELOG.md](CHANGELOG.md) - 版本变更记录
- [CONTRIBUTING.md](CONTRIBUTING.md) - 贡献指南
- [SECURITY.md](SECURITY.md) - 安全策略

---

## 🧱 技术栈

- Go 1.24+
- Telegram Bot API
- Docker Compose
- JSON 文件存储（轻量部署友好）

---

## 🤝 相关项目

- [MoviePilot](https://github.com/jxxghp/MoviePilot)
- [Emby](https://emby.media/)
- [TMDB](https://www.themoviedb.org/)

---

## License

[MIT](LICENSE)
