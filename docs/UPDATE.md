# 更新指南

> 💡 **新用户提示**：如果你是第一次使用本 Bot，请先查看 [README.md](README.md) 的快速上手部分，完成初始部署后再使用本文档进行更新。

---

## 📝 更新日志

### 2026-03-08 Bot 交互优化与通知聚合改进

**1. 主菜单键盘精简**
- 主菜单从 6 个入口精简到 3 个核心动作
- 当时的核心入口：🔍 搜影片、🎬 看推荐、📋 我的请求 + ⚙️ 设置（现已统一为“搜索求片 / 求片进度”）
- 情绪选片、不纠结等功能下沉到"看推荐"的二级页面
- 文案风格从"功能说明书"改为"真人引导"

**2. 每日汇总通知聚合优化**
- 剧集按"作品+季"真正聚合，避免重复显示
- 同一剧集多条记录合并为一条，显示为"更新至 EP12"或"EP01-EP12"
- 电影标题多层兜底解析，从文件名清洗提取
- 统计口径改为"作品数"而非"记录数"
- 通知格式从树形结构改为简洁的平铺列表

**修改的文件：**
- `internal/services/telegram.go` - 主菜单键盘精简
- `internal/services/media_notification.go` - 汇总聚合逻辑重写
- `internal/services/title_resolver.go` - 新增标题解析组件
- `internal/ui/card.go` - 页面文案优化
- `internal/ui/keyboard.go` - 键盘布局调整
- `internal/handlers/callback.go` - 新增设置页、帮助页处理
- `internal/handlers/menu.go` - 帮助页改版
- `internal/handlers/link.go` - 绑定页文案简化
- `cmd/bot/main.go` - 回调注册补充

**通知效果对比：**

之前：
```
📅 2026-03-08 总入库目录
├─ 📺 剧集库 (42部)
│   ├─ 偶然的田园日记 第1季 EP01-EP12
│   ├─ 偶然的田园日记 第1季 EP02-EP11
...
总计：45 部
```

之后：
```
📅 2026-03-08 入库汇总

📺 剧集更新（18 部）
• 偶然的田园日记 第1季：EP01-EP12
• 以下犯上 第1季：更新至 EP26
...

🎥 新增电影（3 部）
• Oppenheimer (2023)
• 沙丘2 (2024)

📌 今日总览
合计：21 部作品
```

---

### 2026-03-08 反馈功能完善

**新增功能：**

#### 管理员反馈面板
- 📊 反馈管理主面板 - 显示统计数据、类型分布、平均解决时间
- 📋 反馈列表 - 分页显示所有反馈，支持状态筛选
- 💬 快捷回复模板 - 预设常用回复，提高处理效率
- 🔧 优先级调整 - 支持设置反馈为高/低优先级

#### 用户交互增强
- 💬 用户追问功能 - 收到管理员回复后可继续对话
- ⭐ 满意度评分 - 问题解决后可进行 1-5 星评价
- 🚫 用户主动关闭 - 用户可关闭自己的反馈

**修改的文件：**
- `internal/callback/types.go` - 新增 18 个回调类型
- `internal/services/issue.go` - 新增统计、筛选、评分、模板功能
- `internal/handlers/admin.go` - 新增反馈面板处理函数（约 400 行）
- `internal/handlers/feedback.go` - 新增追问、关闭、评分处理（约 250 行）
- `internal/bot/poll.go` - 新增追问消息拦截检测
- `internal/bot/webhook.go` - 新增追问消息拦截检测

**使用方式：**
1. 管理员：`/start` → `🔧 管理员菜单` → `📊 反馈管理`
2. 用户：`/start` → `🐛 我的反馈` → 选择反馈查看详情

---

## 📋 快速更新（推荐）

对于大多数用户，使用一键更新脚本即可：

```bash
./update.sh
```

这个脚本会自动完成：
1. ✅ 备份所有数据
2. ✅ 拉取最新代码
3. ✅ 显示更新内容
4. ✅ 重新构建并启动
5. ✅ 检查容器状态

**更新命令对比**：

| 命令 | 说明 | 推荐场景 |
|------|------|---------|
| `./update.sh` | 一键更新脚本 | **日常更新（推荐）** |
| `./manage.sh update` | 管理脚本更新 | 习惯使用 manage.sh |
| 手动更新 | 逐步执行 | 需要更多控制 |

---

## 🆕 新用户首次更新

如果你刚刚完成首次部署，想获取最新代码：

```bash
# 1. 进入项目目录
cd YiMao

# 2. 拉取最新代码
git pull origin master

# 3. 重新构建（如有代码更新）
docker compose build

# 4. 重启容器
docker compose up -d --force-recreate

# 5. 验证运行状态
docker ps | grep yimao
```

**首次更新提示**：
- 📂 数据会保留在 Docker 卷中，更新不会影响
- 🔐 如果 `.env.example` 有更新，记得检查对比
- 📱 更新后在 Telegram 发送 `/start` 测试

---

## 更新前准备

### 1. 备份数据

更新脚本会自动备份，你也可以手动备份：

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

---

## 更新方法

### 方法一：使用 update.sh 脚本（最简单）

```bash
./update.sh
```

**输出示例**：
```
======================================
   YiMao Bot 更新脚本
======================================

▶️  检查运行环境...
✅ 运行环境检查通过

▶️  创建数据备份...
✅ 已备份 data/ 目录
✅ 已备份 .env 配置
ℹ️  备份位置: ./backup-20260308-120000

▶️  拉取最新代码...
✅ 代码已更新
ℹ️  从 abc1234 更新到 def5678

▶️  本次更新内容:
  • fix(search-ui): improve search history UI
  • chore: remove unused image files

▶️  构建 Docker 镜像...
✅ 镜像构建成功

▶️  重启容器...
✅ 容器已启动

======================================
   更新完成！
======================================
```

### 方法二：使用 manage.sh

```bash
./manage.sh update
```

这个命令会：
1. 拉取最新代码 (`git pull`)
2. 重新构建镜像
3. 停止旧容器
4. 启动新容器

### 方法三：手动更新

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

### 方法四：指定版本更新

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

---

## 更新后验证

### 1. 检查容器状态

```bash
# 查看容器是否正常运行
docker ps | grep yimao

# 应该看到 "healthy" 状态
```

### 2. 检查日志

```bash
# 查看启动日志
docker logs yimao --tail 50

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

---

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
docker tag yimao-yimao:latest yimao-yimao:backup-$(date +%Y%m%d)

# 更新后如果需要回滚
docker compose down
# 修改 docker-compose.yml 中的镜像标签
# 然后重新启动
docker compose up -d
```

---

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
docker inspect yimao | grep -A 10 Mounts

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

---

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

---

## 获取帮助

如果更新过程中遇到问题：

1. 查看 [故障排查](DOCKER.md#故障排查)
2. 查看日志寻找错误信息
3. 在 GitHub 提交 Issue
4. 回滚到之前版本
