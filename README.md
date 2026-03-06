# YiMao · Telegram 影视求片助手

> 让「找片、求片、追进度、收通知」都在 Telegram 内一次完成。

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

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

## ✨ 项目定位

YiMao 是一个面向 Emby / Jellyfin + MoviePilot 用户的 Telegram Bot。

它的核心目标很直接：

- 在聊天里就能搜片与求片
- 自动走 MoviePilot 流程
- 用户自己查看请求状态
- 管理员可控审核、通知与配额

不做复杂后台，不引入重依赖，用一个机器人把「影视请求链路」跑顺。

---

## 🚀 一键部署

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

## 🧩 核心能力

| 模块 | 说明 |
|------|------|
| 🔍 搜索与求片 | Telegram 内直接搜索，命中后可一键求片 |
| 📋 请求进度 | 用户可查看自己的请求状态（排队/搜索/下载/完成） |
| 🧠 精选推荐 | 结合 TMDB 提供热门、高分、随机内容推荐 |
| ✅ 审核流 | 支持管理员审核，降低滥用风险 |
| 🔗 账号绑定 | 支持 MoviePilot 多用户映射，各看各的记录 |
| 📬 通知系统 | 单集推送 + 每日汇总，支持时间配置 |
| 📊 配额策略 | 可按用户限制求片次数 |
| 📺 剧集订阅 | 支持按季选择，避免整剧误下 |
| 🐞 反馈闭环 | 用户提交反馈，管理员统一处理 |

---

## 🛠 配置说明

### 必填环境变量

| 变量名 | 说明 | 获取方式 |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram 机器人 Token | [@BotFather](https://t.me/BotFather) |
| `MOVIEPILOT_URL` | MoviePilot 地址 | 例如 `http://localhost:4500` |
| `MOVIEPILOT_API_KEY` | MoviePilot API Key | MoviePilot 后台 |
| `ADMINS` | 管理员 Telegram ID | [@userinfobot](https://t.me/userinfobot) |

### 常用可选变量

| 变量名 | 说明 | 默认值 |
|---|---|---|
| `EMBY_URL` | Emby 地址 | - |
| `EMBY_API_KEY` | Emby API Key | - |
| `TMDB_API_KEY` | TMDB Key（可替换） | 内置默认 |
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

```bash
# 推荐：一键更新
./update.sh

# 查看运行日志
docker logs -f emby-telegram-bot

# 重启服务
docker compose restart

# 停止服务
docker compose down

# 手动更新
git pull && docker compose up -d --build
```

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

- [DEPLOY.md](DEPLOY.md) 部署说明
- [COMMANDS.md](COMMANDS.md) 命令与回调说明
- [ARCHITECTURE.md](ARCHITECTURE.md) 架构设计
- [CHANGELOG.md](CHANGELOG.md) 版本变更记录
- [CONTRIBUTING.md](CONTRIBUTING.md) 贡献指南
- [SECURITY.md](SECURITY.md) 安全策略

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
