# AI 推荐功能

## 概述

AI 推荐功能为用户提供智能媒体推荐，基于热门和高分内容帮助用户发现优质影视内容。

## 功能特性

### 1. 私聊专用
- AI 推荐仅在私聊中可用
- 群聊中不显示 AI 推荐按钮

### 2. 推荐分类

| 分类 | 说明 | 关键词 |
|------|------|--------|
| 🔥 热门电影 | 当前热门电影 | 复仇者联盟、沙丘、奥本海默、流浪地球等 |
| 📺 热播剧集 | 当前热门剧集 | 随机热门关键词搜索 |
| ⭐ 高分佳作 | 高评分作品 | 肖申克的救赎、教父、星际穿越等 (7.0+) |
| 🆕 最新上线 | 最新流行内容 | 沙丘2、奥本海默、惊奇队长等 |
| 🎲 随机发现 | 随机类型推荐 | 科幻、动作、喜剧、动画等 |

### 3. 换一批功能
- 每次点击"换一批"随机选择不同关键词
- 确保每次推荐结果不同

### 4. 详情页展示

#### 信息展示
- 📅 上映年份
- ⭐ 评分
- 🏷️ 媒体类型（电影/剧集）
- 📺 季数信息（剧集）
- 📖 剧情简介
- 🆔 TMDB ID

#### 横幅图片
- 优先使用 MoviePilot API 获取完整媒体信息
- 优先显示 backdrop 背景图（高分辨率）
- fallback 到 poster 海报

#### 操作按钮
- **电影**: ✅ 立即求片 + 🐛 反馈
- **剧集**: ✅ 订阅全季 + 分季选择 + 更多...

## 技术实现

### 目录结构
```
ai/
├── recommendation_v2.go  # 推荐引擎 v2
├── recommend.go          # 推荐结果结构
├── trending.go          # 热门推荐管理
├── search.go            # AI 搜索
├── integration.go       # 集成接口
└── cache.go             # 缓存
```

### API 端点
- `POST /api/v1/media/{type}?tmdbid={id}&type_name={type}` - 获取媒体详情

### 回调格式
```
ai:trending    # 热门电影
ai:hot         # 热播剧集
ai:toprated   # 高分佳作
ai:new         # 最新上线
ai:random     # 随机发现
search:type:{type}  # 替代上述格式
detail:id:{id}:type:{type}  # 查看详情
```

## 更新日志

### 2024-02-24
- 移除 AI 聊天和 Q&A 学习功能
- 优化 AI 推荐详情页，添加横幅图片展示
- 详情页优先使用 MoviePilot API 获取完整媒体信息
- 修复 Go 1.24 语法问题
- 限制 AI 推荐仅在私聊中使用

### 2024-02-24 - 稳定性优化
- **错误处理增强**: poll 回调处理添加全面的错误日志
- **HTTP 超时配置**:
  - 连接超时: 10s
  - 空闲连接超时: 90s
  - Keep-alive: 30s
  - 连接池: 100 最大连接
- **输入验证**: 新增 `pkg/validation/sanitize.go` 防止恶意输入
  - SQL 注入检测
  - XSS 模式检测
  - 路径遍历检测
  - 输入长度限制
- **会话管理**: 改进日志记录，便于调试

### 2024-02-24 - Webhook通知增强
- **通知开关验证**: 全面测试并验证通知开关功能
  - 总体开关 (`Enabled`): 控制所有通知
  - 单集推送开关 (`InstantEnabled`): 控制立即入库通知
  - 每日汇总开关 (`DailySummaryEnabled`): 控制每日汇总发送
- **详细日志记录**: 添加通知操作日志
  - 保存操作成功/失败日志
  - 开关切换操作日志
  - 通知发送决策日志 (发送/跳过)
- **Webhook 测试**: 验证 Emby 入库通知正常工作

### 2024-02-25 - Webhook 配置完成
- **Nginx 反向代理**: 配置 Nginx 处理外部 webhook 请求
- **HTTP Webhook**: 配置 `http://154.40.33.156:8080/webhook/emby` 端点
- **Emby 集成**: 成功配置 Emby webhook 通知
- **自动检测**: 改进 webhook 类型自动检测逻辑
  - 支持 `NotificationType` 和 `Event` 字段
  - 添加请求体日志便于调试
- **可用 URL**:
  - HTTP: `http://154.40.33.156:8080/webhook/emby`
  - HTTPS: `https://emby.135505.auts/webhook/emby` (需 DNS 生效)

### 2024-02-25 - 搜索历史修复
- **问题**: 点击搜索按钮时没有显示历史搜索记录
- **原因**: `showSearchHistoryOrPrompt` 函数只显示提示信息，没有调用历史记录显示
- **修复**:
  - 修改 `internal/handlers/search.go:139-152`
  - 现在点击搜索按钮会显示最近 5 条搜索历史
  - 每条历史记录都有快捷搜索按钮
  - 添加"清空历史"按钮
  - 无历史记录时显示输入提示

### 2024-02-25 - 反馈历史整合
- **新功能**: 在主菜单添加「🐛 我的反馈」入口
- **修改文件**:
  - `internal/services/telegram.go` - 主菜单键盘添加按钮
  - `internal/handlers/callback.go` - 更新主菜单说明文字
  - `internal/handlers/feedback.go` - 添加反馈列表和详情查看
  - `internal/handlers/callback.go` - BackHandler 使用新键盘函数
  - `internal/bot/command.go` - SendStartMenu 使用新键盘函数
  - `cmd/bot/main.go` - 注册 `my_feedback` 回调
- **功能**:
  - 主菜单新增「🐛 我的反馈」按钮
  - 点击查看用户所有反馈历史
  - 显示反馈状态：🔵待处理、💬已回复、🔧处理中、✅已解决、🚫已关闭
  - 支持查看反馈详情和管理员回复
  - 支持从详情页返回列表
  - 布局调整为 2x3 网格（搜索/AI、请求/反馈、绑定/帮助）
- **测试结果**: ✅ 搜索历史和反馈历史功能均正常工作
