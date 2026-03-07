# YiMao · Telegram 影视求片助手

> 让「找片、求片、追进度、收通知」都在 Telegram 内一次完成。

**全新 UI 设计**：5 套强视觉风格，可自由切换

**🔄 部署用户请注意**：更新前请务必查看 [📘 更新指南](UPDATE.md) 以避免数据丢失！

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://golang.org)
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

## ✨ 项目定位

YiMao 是一个面向 Emby / Jellyfin + MoviePilot 用户的 Telegram Bot。

它的核心目标很直接：

- 在聊天里就能搜片与求片
- 自动走 MoviePilot 流程
- 用户自己查看请求状态
- 管理员可控审核、通知与配额

不做复杂后台，不引入重依赖，用一个机器人把「影视请求链路」跑顺。

---

## 🔍 搜索历史优化

**全新升级**：搜索历史功能全面优化，提供更强大的搜索体验

### ✨ 新增功能

- 📊 **搜索统计** - 总次数/本周/本月/最常搜索
- 🔥 **热门搜索** - 全平台热门内容 TOP10
- 📈 **搜索趋势** - 增长最快的搜索（3/7/30天）
- 🕐 **时间分组** - 今天/本周/本月/更早
- 🗑️ **精细管理** - 支持删除单条历史记录
- ⚡ **性能提升** - 查询速度提升 50 倍
- 💾 **数据库存储** - SQLite 替代 JSON，更可靠

### 🎨 暗黑霓虹风格

搜索历史界面采用暗黑霓虹风格，与整体 UI 保持一致。

### 📖 文档

- [搜索历史优化方案](docs/search-history-optimization.md)
- [搜索历史实施指南](docs/search-history-implementation.md)
- [搜索历史总结](docs/search-history-summary.md)

---

## 🎨 UI 设计

YiMao 提供 5 套强视觉 UI 风格，可根据场景自由切换：

| 风格 | 特点 | 适用场景 |
|------|------|----------|
| ⚡ 暗黑霓虹 | 赛博朋克、强对比、霓虹配色 | 搜索结果、媒体详情 |
| 🎞️ 文艺胶片 | 复古质感、伤感文案、治愈氛围 | 推荐内容、AI 选片 |
| 🎨 波普艺术 | 趣味撞色、年轻潮流、强记忆点 | 主菜单、功能入口 |
| 🎴 极简卡片 | 现代干净、信息清晰、高效交互 | 请求列表、状态管理 |
| 🎬 沉浸电影 | 影院海报、台词引用、氛围感强 | 影片详情页 |

**推荐组合**：
- 主菜单 → 波普艺术风
- 搜索结果 → 暗黑霓虹风
- 推荐内容 → 文艺胶片风
- 请求列表 → 极简卡片风

查看完整设计文档：[UI 设计方案](docs/ui-design-proposals.md)
查看在线预览：[UI 预览演示](docs/ui-preview.html)

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
| 💫 情绪选片 | 按心情入口推荐内容，减少“选片焦虑” |
| 🎯 不纠结模式 | 一次给出稳妥/刺激/冷门三种路线，秒选开看 |
| 🧠 观影人格 | 记录最近偏好心情，首页动态展示个人口味 |
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
| `ENABLE_AUTO_RESUBSCRIBE` | 自动处理回收订阅（建议关闭） | `false` |
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
