# YiMao · Telegram 影视求片助手

<div align="center">

**在 Telegram 内完成「搜索影片 → 发起请求 → 查看进度 → 接收通知」的闭环**

与 MoviePilot + Emby/Jellyfin 配合使用，无需切换应用

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![Docker Pull](https://img.shields.io/badge/docker-xzb177%2Fyimao-blue?logo=docker)](https://hub.docker.com/r/xzb177/yimao)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Stars](https://img.shields.io/github/stars/xzb177/YiMao?style=social)](https://github.com/xzb177/YiMao/stargazers)

[功能特性](#-核心功能) · [快速开始](#-快速开始) · [部署指南](#-部署指南) · [命令说明](#-命令说明)

</div>

---

## ⚠️ 更新提示

**更新前请务必查看 [更新指南](docs/UPDATE.md) 以避免数据丢失！**

---

## ⚡ 快速开始

### 方式一：Docker 一键部署（推荐）

```bash
# 1. 拉取镜像并创建配置文件
mkdir -p yimao && cd yimao
curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/docker-compose.yml -o docker-compose.yml
curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/.env.example -o .env

# 2. 编辑 .env 文件，填入必填配置
nano .env  # 或使用其他编辑器

# 3. 启动服务
docker compose up -d

# 4. 查看日志
docker compose logs -f
```

### 方式二：安装脚本部署

```bash
curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/install.sh | bash
```

### 方式三：源码编译

```bash
git clone https://github.com/xzb177/YiMao.git
cd YiMao
cp .env.example .env
# 编辑 .env 后
docker compose up -d
```

启动后在 Telegram 输入 `/start` 即可。

---

## 核心功能

### 主流程

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
| 🎬 **智能推荐** | 结合 TMDB 提供热门、高分、随机推荐 |
| 🤖 **AI 选片** | 按心情入口推荐内容（需配置 API Key） |
| 📜 **搜索历史** | 记录搜索记录，支持快速再次搜索 |
| 🔗 **账号绑定** | 支持 MoviePilot 多用户映射，各看各的记录 |
| 🎨 **多套 UI** | 5 种视觉风格可自由切换 |
| 🌐 **候选列表** | 直观查看所有站点候选资源，支持分页 |

### 管理能力

| 功能 | 说明 |
|------|------|
| ✅ **审核流** | 管理员可审核请求，降低滥用风险 |
| 📊 **配额策略** | 按用户限制求片次数 |
| 🐞 **反馈闭环** | 用户提交反馈，管理员统一处理 |
| 🔄 **自动回收** | 自动回收长期未下载的订阅 |

---

## 部署指南

### 部署前检查

- [ ] 已从 @BotFather 获取 `TELEGRAM_BOT_TOKEN`
- [ ] MoviePilot 已安装并可访问
- [ ] 已从 MoviePilot 获取 API Key
- [ ] 确认 MoviePilot 地址（跨机器部署勿用 localhost）
- [ ] （可选）Emby/Jellyfin 地址和 API Key

### 必填环境变量

| 变量名 | 说明 | 获取方式 |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram 机器人 Token | [@BotFather](https://t.me/BotFather) |
| `MOVIEPILOT_URL` | MoviePilot 地址 | 如 `http://192.168.1.100:4500` |
| `MOVIEPILOT_API_KEY` | MoviePilot API Key | MoviePilot 设置→安全设置 |

### 可选环境变量

| 变量名 | 说明 | 默认值 |
|---|---|---|
| `ADMIN_USER_IDS` | 管理员 Telegram ID（逗号分隔） | 首个绑定用户自动成为管理员 |
| `EMBY_URL` | Emby/Jellyfin 地址 | - |
| `EMBY_API_KEY` | Emby API Key | - |
| `TMDB_API_KEY` | TMDB API Key | 内置默认值 |
| `TELEGRAM_CHAT_ID` | 群组 Chat ID（通知用） | - |
| `ZHIPU_API_KEY` | 智谱 AI Key（推荐功能） | - |
| `CLAUDE_API_KEY` | Claude API Key（AI 功能） | - |
| `TZ` | 时区 | `Asia/Shanghai` |
| `PORT` | 健康检查端口 | `8080` |

---

## 命令说明

### 用户命令

| 命令 | 作用 |
|---|---|
| `/start` | 打开主菜单 |
| `/search 关键词` | 搜索影视 |
| `/ai` | 打开推荐菜单 |
| `/requests` | 查看我的请求 |
| `/link 账号 密码` | 绑定 MoviePilot 账号 |

### 管理员命令（私聊机器人）

- 审核请求
- 配置通知策略
- 查看反馈
- 管理配额

---

## 维护与更新

> **重要：更新前请务必备份数据！** 详见 [更新指南](docs/UPDATE.md)

### 快速更新

```bash
# 使用管理脚本（推荐）
./manage.sh update

# 查看运行日志
./manage.sh logs-f

# 重启服务
./manage.sh restart
```

### 手动更新

```bash
# 拉取最新代码
git pull

# 重新构建镜像
docker compose build

# 重启服务
docker compose up -d
```

### 更新注意事项

1. **每次更新前务必备份数据**
   - `data/` 目录
   - `*.json` 配置文件

2. **查看更新内容**
   ```bash
   git log HEAD@{1}..HEAD --oneline
   ```

3. **检查配置变更**
   ```bash
   git diff HEAD@{1} .env.example
   ```

---

## 架构概览

```
┌─────────────┐
│ Telegram User │
└──────┬──────┘
       ▼
┌─────────────────────────────────────┐
│         YiMao Bot                    │
│  (Handlers / Services / Session)     │
└──┬────────────────────────────────┬──┘
   │                                │
   ▼                                ▼
┌─────────┐                    ┌─────────┐
│MoviePilot│              Emby/Jellyfin│
│ 请求处理  │                 库检查     │
│ 下载链路  │                 媒体状态   │
└─────────┘                    └─────────┘
```

### 代码结构

- `internal/handlers` — 命令与回调入口
- `internal/services` — 对外部系统能力封装
- `internal/session` — 会话与流程状态
- `internal/ui` — UI 构建系统
- `ai/` — AI 推荐模块
- `pkg/*` — 通用类型、校验与工具

---

## 项目文档

**部署与维护**
- [UPDATE.md](docs/UPDATE.md) — 更新指南（必读）
- [DEPLOY.md](docs/DEPLOY.md) — 部署说明
- [DOCKER.md](docs/DOCKER.md) — Docker 管理

**功能说明**
- [COMMANDS.md](docs/COMMANDS.md) — 命令与回调说明
- [FEATURES.md](docs/FEATURES.md) — 功能特性

**开发文档**
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) — 架构设计
- [CHANGELOG.md](CHANGELOG.md) — 版本变更记录
- [CONTRIBUTING.md](docs/CONTRIBUTING.md) — 贡献指南

---

## 技术栈

- **语言**: Go 1.23+
- **框架**: Telegram Bot API
- **存储**: JSON 文件（轻量部署）
- **容器**: Docker + Docker Compose

---

## 相关项目

- [MoviePilot](https://github.com/jxxghp/MoviePilot) — 自动化媒体整理工具
- [Emby](https://emby.media/) — 媒体服务器
- [TMDB](https://www.themoviedb.org/) — 电影数据库

---

## License

[MIT](LICENSE)

---

<div align="center">

**[功能特性](#-核心功能) · [快速开始](#-快速开始) · [部署指南](#-部署指南)**

Made with ❤️ by [xzb177](https://github.com/xzb177)

</div>

---

## 🌟 支持本项目

如果觉得本项目对你有帮助，欢迎支持：

[![VSLLM](https://img.shields.io/badge/Powered%20by-VSLLM-blue?logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZD0iTTEyIDJMMyA3djEwbDkgNSA5LTVIN0wxMiAyeiIgZmlsbD0iI2ZmZiIvPjwvc3ZnPg==)](https://vsllm.com)

> 🔗 [维云模型开放平台](https://vsllm.com) - 稳定极速的大模型API聚合平台

