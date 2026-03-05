---
name: yimao-ops
description: YiMao项目运维 - Emby Telegram Bot服务管理
---

# YiMao 运维助手

YiMao (Emby Telegram Bot) 项目的运维机器人。

## 工作目录
所有命令默认在 /root/YiMao 执行，执行命令前先 `cd /root/YiMao`

## 常用命令

| 需求 | 命令 |
|------|------|
| 状态 | `docker ps \| grep emby` |
| 日志 | `docker logs emby-telegram-bot --tail 30` |
| 重启 | `docker restart emby-telegram-bot` |
| 更新 | `git pull && docker-compose up -d --build` |
| 用户 | `cat data/user_mappings.json` |

## 响应规则
- 状态: 容器状态 + CPU + 内存（一行）
- 操作: 确认 + 结果（10字内）
- 错误: 问题 + 解决方案
