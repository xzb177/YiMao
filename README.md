# YiMao 运维助手

> 基于 Golem 框架的 Emby Telegram Bot 服务管理助手

## 项目简介

YiMao 运维助手是一个智能化的服务管理工具，通过 Telegram Bot 对 Emby Telegram Bot 进行运维管理。支持状态查询、日志查看、服务重启、一键部署等功能。

## 快速开始

### 一键部署

```bash
git clone https://github.com/xzb177/YiMao.git
cd YiMao
./deploy.sh
```

### 配置环境变量

复制 `.env.example` 为 `.env` 并填入你的 Telegram Bot Token：

```bash
cp .env.example .env
nano .env
```

### 启动服务

```bash
npm start
```

## 脚本说明

| 脚本 | 功能 |
|------|------|
| `./deploy.sh` | 一键部署，自动安装依赖并配置环境 |
| `./update.sh` | 一键更新，拉取最新代码并重新部署 |
| `./yimao.sh` | 快捷运维脚本，自动切换到工作目录执行命令 |

## 常用运维命令

| 需求 | 命令 |
|------|------|
| 查看容器状态 | `docker ps \| grep emby` |
| 查看日志 | `docker logs emby-telegram-bot --tail 30` |
| 重启服务 | `docker restart emby-telegram-bot` |
| 更新部署 | `git pull && docker-compose up -d --build` |

## 项目结构

```
YiMao/
├── deploy.sh          # 一键部署脚本
├── update.sh          # 一键更新脚本
├── yimao.sh           # 快捷运维脚本
├── golem.yaml         # Golem 框架配置
├── package.json       # 项目依赖
├── .env.example       # 环境变量模板
└── skills/            # 技能目录
    ├── general/       # 通用助手
    ├── im-adapter/    # IM 适配器
    └── yimao-ops/     # YiMao 运维技能
```

## 安全提示

- 不要将 `.env` 文件提交到版本控制
- Telegram Bot Token 已通过环境变量配置，请妥善保管
- 建议定期更新依赖包以确保安全性

## 许可证

MIT License

## 作者

YiMao Team
