# YiMao - Emby Telegram Bot

一个用于 Emby/MoviePilot 的 Telegram 机器人，主要解决了求片和管理的问题。

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

## 功能

这个机器人是我用 Go 写的，主要用于家庭媒体库的管理：

* **搜索求片** - 用户可以直接在 Telegram 里搜片，机器人会调用 MoviePilot 的 API 帮你下载
* **媒体库通知** - 新片入库后会通知你，支持实时推送和每日汇总两种模式
* **库检查** - 求片前先检查 Emby 里有没有，避免重复下载
* **精选推荐** - 从 TMDB 获取热门和高分内容，帮你发现新片
* **审核机制** - 用户的求片请求需要管理员批准才会在 MoviePilot 里创建下载任务
* **账号绑定** - 支持 MoviePilot 多用户系统，每个人管理自己的账号
* **配额限制** - 可以设置用户的求片数量限制

---

## 为什么写这个

用 MoviePilot 有一段时间了，但是每次想下新片都要打开网页操作挺麻烦的。而且家里人多，大家各自求片容易下重复。所以做了个机器人挂在群里，大家直接搜就好了，也不会重复。

另外 MoviePilot 的 webhook 只能发通知，我就顺手加了入库通知功能，每天晚上汇总一次当天的新片，看看都下了什么。

---

## 快速开始

```bash
# 克隆
git clone https://github.com/xzb177/YiMao.git
cd YiMao

# 复制配置文件
cp .env.example .env
# 编辑 .env 填上你的 token 和地址

# 启动
docker compose up -d
```

---

## 配置说明

| 环境变量 | 说明 | 是否必须 |
|---------|------|---------|
| TELEGRAM_BOT_TOKEN | 从 @BotFather 获取 | 是 |
| MOVIEPILOT_URL | MoviePilot 地址 | 是 |
| MOVIEPILOT_API_KEY | MoviePilot 设置里的 API Key | 是 |
| EMBY_URL | Emby 地址 | 否，但建议配置 |
| EMBY_API_KEY | Emby API Key | 否 |
| TMDB_API_KEY | TMDB API Key | 否 |
| ADMINS | 管理员 Telegram ID，用逗号分隔 | 是 |

获取 Telegram ID 的方法：给 [@userinfobot](https://t.me/userinfobot) 发个消息就能看到。

---

## 命令列表

### 普通用户

| 命令 | 功能 |
|------|------|
| `/start` | 打开主菜单 |
| `/search 关键词` | 搜索影视作品 |
| `/link 账号 密码` | 绑定 MoviePilot 账号 |

### 管理员

私聊机器人会显示管理员菜单，里面可以：
* 审核用户的求片请求
* 设置媒体库通知模式（单集推送 / 每日汇总）
* 查看用户反馈

---

## 媒体库通知

这个功能可能是最实用的，我用了两种方式：

**单集推送** - 每有一部片子入库就发一条消息，包含片名、画质、评分这些信息。

**每日汇总** - 每天晚上固定时间发一条消息，汇总当天入库的所有内容，按动画/剧集/电影分类显示。

管理员可以在机器人私聊菜单里切换通知模式和设置汇总时间。

---

## 技术栈

* Go 1.24+
* Telegram Bot API
* SQLite（存用户数据和会话）
* Docker 部署

架构上就是标准的 handlers -> services 结构，没什么特别的。代码都在 `internal/` 目录下面，有兴趣的可以看看。

---

## 相关项目

* [MoviePilot](https://github.com/jxxghp/MoviePilot) - 自动刮削和下载工具
* [Emby](https://emby.media/) - 媒体服务器
* [TMDB](https://www.themoviedb.org/) - 影视数据库

---

## License

MIT
