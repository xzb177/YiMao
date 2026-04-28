# Emby Telegram Bot 功能说明

## 🎉 最新功能 (v2.0)

### 📊 数据分析系统

#### 命令列表

| 命令 | 说明 | 示例 |
|------|------|------|
| `/top` | 查看热门媒体排行 | `/top` |
| `/activity` | 查看用户活跃度排行 | `/activity` |
| `/trends` | 查看请求趋势 (7天) | `/trends` |

#### 功能特点

- 📈 **热门媒体**: 自动统计最受请求的内容
- 👥 **用户排行**: 谁请求数最多，一目了然
- 📊 **趋势图表**: ASCII 字符图展示每日请求量
- 💾 **数据持久化**: 自动保存到 `analytics.json`

---

### ⚙️ 用户偏好系统

#### 命令列表

| 命令 | 说明 |
|------|------|
| `/prefs` | 查看我的通知设置 |
| `/setprefs <选项> <值>` | 修改设置 |
| `/resetprefs` | 重置为默认设置 |

#### 支持的设置选项

| 选项 | 说明 | 值 |
|------|------|-----|
| `movies` | 电影通知 | on/off |
| `series` | 剧集通知 | on/off |
| `issues` | 问题报告 | on/off |
| `approved` | 批准通知 | on/off |
| `available` | 可用通知 | on/off |
| `quiet` | 勿扰模式 | on/off |
| `quietstart` | 勿扰开始时间 | HH:MM |
| `quietend` | 勿扰结束时间 | HH:MM |
| `whitelist` | 白名单关键词 | 任意文本 |
| `blacklist` | 黑名单关键词 | 任意文本 |

#### 使用示例

```
/setprefs movies on        # 开启电影通知
/setprefs quiet on         # 开启勿扰模式
/setprefs quietstart 22:00 # 设置勿扰开始时间
/setprefs whitelist 4K     # 只接收含"4K"的内容
/setprefs blacklist HDR     # 屏蔽含"HDR"的内容
```

---

### 🔧 Jellyseerr API 集成

| 命令 | 说明 | 示例 |
|------|------|------|
| `/pending` | 查看待处理请求 | `/pending` |
| `/approve <ID>` | 批准请求 | `/approve 123` |
| `/decline <ID>` | 拒绝请求 | `/decline 123` |
| `/search <关键词>` | 搜索媒体 | `/search 星球大战` |

---

### 🔔 智能通知

- **事件聚合**: 短时间内多个事件自动合并
- **勿扰模式**: 支持设置免打扰时间段
- **关键词过滤**: 白名单/黑名单机制
- **紧急通知**: 视频/音频问题不受勿扰限制

---

## 📝 完整命令列表

### 基础命令
- `/start` / `/help` - 显示帮助信息
- `/register` - 注册为管理员 (仅当系统无管理员时)
- `/unregister` - 取消管理员权限
- `/status` - 查看当前状态
- `/stats` - 查看今日统计数据
- `/admins` - 查看所有管理员

### 管理员命令
- `/addadmin <ID> [姓名]` - 添加管理员
- `/deladmin <ID>` - 删除管理员
- `/pending` - 查看待处理请求
- `/approve <ID>` - 批准请求
- `/decline <ID>` - 拒绝请求

### 搜索命令
- `/search <关键词>` - 搜索媒体

### 数据分析
- `/top` - 热门媒体排行
- `/activity` - 用户活跃度排行
- `/trends` - 请求趋势统计

### 偏好设置
- `/prefs` - 查看通知设置
- `/setprefs <选项> <值>` - 修改设置
- `/resetprefs` - 重置设置

---

## 🚀 部署

### 环境变量 (.env)

```bash
# 必需配置
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id

# 可选配置
PORT=8080
JELLYSEERR_URL=https://embyrequest.oceancloud.asia
JELLYSEERR_API_KEY=your_api_key  # 用于完整 API 功能
ADMINS=123456:张三,789012:李四
```

### 启动服务

```bash
cd /root/yimao
./start.sh
```

---

## 📁 文件说明

| 文件 | 说明 |
|------|------|
| `main.go` | 主程序 |
| `jellyseerr.go` | Jellyseerr API 客户端 |
| `analytics.go` | 数据分析系统 |
| `preferences.go` | 用户偏好管理 |
| `analytics.json` | 分析数据 (自动生成) |
| `preferences.json` | 用户偏好 (自动生成) |
| `start.sh` | 启动脚本 |
