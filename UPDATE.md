# 更新指南

本文档指导如何安全地更新 Emby Telegram Bot。

## 更新前准备

### 1. 备份数据

在更新前，**务必备份**以下数据：

```bash
# 创建备份目录
BACKUP_DIR="./backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

# 备份所有数据文件
cp -r data/ "$BACKUP_DIR/" 2>/dev/null || true
cp preferences.json "$BACKUP_DIR/" 2>/dev/null || true
cp user_quotas.json "$BACKUP_DIR/" 2>/dev/null || true
cp user_mappings.json "$BACKUP_DIR/" 2>/dev/null || true
cp binding_requests.json "$BACKUP_DIR/" 2>/dev/null || true
cp review_requests.json "$BACKUP_DIR/" 2>/dev/null || true

echo "✅ 备份完成: $BACKUP_DIR"
```

### 2. 记录当前版本

```bash
# 查看当前提交版本
git log -1 --oneline

# 查看当前标签（如果有）
git describe --tags --always
```

### 3. 查看更新内容

```bash
# 查看最近的提交
git log origin/master --oneline -10

# 查看 CHANGELOG（如果有）
cat CHANGELOG.md | head -50
```

## 更新方法

### 方法一：使用管理脚本（推荐）

最简单的方式，自动完成所有步骤：

```bash
./manage.sh update
```

这个命令会：
1. 拉取最新代码 (`git pull`)
2. 重新构建镜像
3. 停止旧容器
4. 启动新容器

### 方法二：手动更新

如果需要更多控制，可以手动执行每个步骤：

```bash
# 1. 拉取最新代码
git pull origin master

# 2. 查看更新的文件
git diff HEAD@{1} --stat

# 3. 检查是否有配置文件变更
git diff HEAD@{1} .env.example docker-compose.yml

# 4. 重新构建并启动
docker compose build
docker compose up -d --force-recreate
```

### 方法三：指定版本更新

如果需要更新到特定版本：

```bash
# 1. 查看可用版本
git tag -l

# 2. 切换到指定版本
git checkout v1.2.3

# 3. 重新构建
docker compose build
docker compose up -d --force-recreate
```

## 更新后验证

### 1. 检查容器状态

```bash
# 查看容器是否正常运行
docker ps | grep emby-telegram-bot

# 应该看到 "healthy" 状态
```

### 2. 检查日志

```bash
# 查看启动日志
docker logs emby-telegram-bot --tail 50

# 应该看到类似输出：
# 🌐 Server listening on 0.0.0.0:8080
# 🔄 Starting Telegram updates polling...
```

### 3. 功能测试

在 Telegram 中测试以下功能：

| 功能 | 测试命令 | 预期结果 |
|------|---------|---------|
| 主菜单 | `/start` | 收到主菜单 |
| 搜索 | `/search` | 提示输入搜索关键词 |
| 绑定 | `/link` | 提示绑定账号 |
| 帮助 | `/help` | 收到帮助信息 |

### 4. 检查数据完整性

```bash
# 进入容器检查数据
./manage.sh shell
ls -la /app/data/
exit
```

## 回滚方法

如果更新后出现问题，可以回滚到之前的版本：

### 方法一：回滚到上一个提交

```bash
# 1. 查看提交历史
git log --oneline -10

# 2. 回滚到指定提交
git reset --hard <commit-hash>

# 3. 重新构建
docker compose build
docker compose up -d --force-recreate
```

### 方法二：使用备份恢复

如果数据出现问题：

```bash
# 1. 停止容器
./manage.sh stop

# 2. 恢复备份
cp -r backup-20260308-043558/* .

# 3. 重启容器
./manage.sh start
```

### 方法三：保存旧镜像

更新前可以先保存当前镜像：

```bash
# 更新前标记当前镜像
docker tag emby-telegram-bot-emby-telegram-bot:latest emby-telegram-bot-emby-telegram-bot:backup-$(date +%Y%m%d)

# 更新后如果需要回滚
docker compose down
# 修改 docker-compose.yml 中的镜像标签
# 然后重新启动
docker compose up -d
```

## 常见问题

### Q1: 更新后 Bot 无响应

```bash
# 1. 检查容器状态
./manage.sh status

# 2. 查看日志
./manage.sh logs-f

# 3. 检查环境变量是否需要更新
git diff HEAD@{1} .env.example
```

### Q2: 更新后数据丢失

```bash
# 1. 检查 data 目录是否挂载正确
docker inspect emby-telegram-bot | grep -A 10 Mounts

# 2. 从备份恢复数据
# （参考上面的回滚方法）
```

### Q3: 配置文件变更

如果 `.env.example` 有更新：

```bash
# 1. 比较新旧配置
diff .env .env.example

# 2. 手动合并新配置项到 .env
nano .env

# 3. 重启容器
docker compose restart
```

### Q4: 构建失败

```bash
# 1. 清理缓存重新构建
docker compose build --no-cache

# 2. 如果还是失败，检查 Go 版本
docker --version
# 应该使用 Docker 镜像中指定的 Go 版本
```

### Q5: 依赖服务变更

如果 MoviePilot、Emby 等依赖服务的配置有变更：

```bash
# 1. 检查 CHANGELOG 了解变更
cat CHANGELOG.md

# 2. 更新 .env 中的相关配置
nano .env

# 3. 重启容器
docker compose restart
```

## 自动更新（可选）

如果希望自动更新，可以设置 cron 任务：

```bash
# 编辑 crontab
crontab -e

# 添加每天凌晨 3 点检查更新（不自动安装）
0 3 * * * cd /path/to/YiMao && git fetch origin && git diff HEAD origin/master | grep -q "." && echo "New version available" | telegram-cli

# 或者直接更新（谨慎使用）
0 3 * * * cd /path/to/YiMao && ./manage.sh update >> /var/log/bot-update.log 2>&1
```

## 更新通知

建议关注以下渠道获取更新信息：

- GitHub Releases: https://github.com/xzb177/YiMao/releases
- GitHub Commits: https://github.com/xzb177/YiMao/commits/master
- CHANGELOG.md: 项目更新日志

## 更新检查清单

在执行更新前，请确认：

- [ ] 已备份所有数据文件
- [ ] 已记录当前版本号
- [ ] 已查看本次更新内容
- [ ] 确认网络连接正常
- [ ] 确认 Docker 服务正常运行
- [ ] 已通知用户可能的短暂中断

更新后检查：

- [ ] 容器状态为 healthy
- [ ] 日志无错误信息
- [ ] Bot 响应正常
- [ ] 数据完整性检查通过
- [ ] 功能测试通过

## 获取帮助

如果更新过程中遇到问题：

1. 查看 [故障排查](DOCKER.md#故障排查)
2. 查看日志寻找错误信息
3. 在 GitHub 提交 Issue
4. 回滚到之前版本
