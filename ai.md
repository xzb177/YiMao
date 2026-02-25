# 精选推荐功能

## 概述

精选推荐功能为用户提供智能媒体推荐，基于热门和高分内容帮助用户发现优质影视内容。

## 功能特性

### 1. 私聊专用
- 精选推荐仅在私聊中可用
- 群聊中不显示推荐按钮

### 2. 推荐分类

| 分类 | 说明 |
|------|------|
| 🔥 本周热门 | 当前热门电影，大家都在看的好片 |
| 📺 热门剧集 | 热播剧集，追剧必看热门番 |
| ⭐ 必看神作 | 高评分经典，高分佳作不容错过 |
| 🆕 最新上映 | 最新上映内容，刚上线的新鲜内容 |
| 🎲 随机探索 | 随机类型推荐，发现未知的精彩 |

### 3. 换一批功能
- 每次点击"换一批"随机选择不同关键词
- 确保每次推荐结果不同

### 4. 详情页展示

#### 信息展示
- 📅 上映年份
- ⭐ 评分
- 🏷️ 媒体类型（电影/剧集）
- 🎭 类型标签
- 📺 季数信息（剧集）
- 📖 剧情简介
- 🆔 TMDB ID

#### 横幅图片
- 优先显示 backdrop 背景图（高分辨率）
- fallback 到 poster 海报

#### 操作按钮
- **电影**: ✅ 立即求片 + 📎 加入片单 + 🐛 反馈
- **剧集**: ✅ 订阅全季 + 📎 加入片单 + 分季选择 + 更多...

### 5. 数据源

#### TMDB API（主要数据源）
- `GET /movie/popular` - 获取流行电影
- `GET /tv/popular` - 获取流行剧集
- `GET /movie/top_rated` - 获取高分电影
- `GET /movie/now_playing` - 获取正在上映的电影
- 语言参数: `zh-CN` 中文支持
- 获取多页数据并随机打乱，确保每次"换一批"都有不同结果

## 技术实现

### 推荐策略
- 直接使用 TMDB API 获取推荐数据
- 获取多页数据（page 1-3）并随机打乱
- 每次点击"换一批"都会返回不同的推荐结果
- 不再验证 MoviePilot 资源可用性，扩大推荐范围

### 导航修复
- 详情页返回列表按钮处理：检测到从详情页返回时，删除图片消息并重新发送推荐列表
- 因为图片消息无法用 editMessageText 编辑，必须删除重发

### 回调格式
```
search:type:{category}  # 推荐类型
detail:id:{id}:type:{type}  # 查看详情
watchlist  # 查看片单
watchlist_add:{tmdbID}  # 加入片单
```

推荐类型:
- `trending` - 本周热门
- `hot` - 热门剧集
- `toprated` - 必看神作
- `new` - 最新上映
- `random` - 随机探索

## 更新日志

### 2024-02-25 - 个人片单与请求优先级
- **新增功能**: 个人片单系统
  - 用户可收藏感兴趣的影片到个人片单
  - 支持创建命名收藏夹分类管理
  - 片单数据持久化存储
  - 详情页新增"加入片单"按钮
  - 主菜单新增"我的片单"入口
- **新增功能**: 请求优先级系统
  - 求片时可选择优先级：低、普通、较高、紧急
  - 管理员审核队列按优先级排序
  - 紧急请求优先处理
- **修改文件**:
  - `internal/services/watchlist.go` - 片单服务
  - `internal/handlers/watchlist.go` - 片单处理器
  - `internal/services/review.go` - 优先级字段和排序
  - `internal/handlers/request.go` - 优先级选择流程
  - `cmd/bot/main.go` - 片单服务初始化和回调注册

### 2024-02-25 - 推荐算法优化与导航修复
- **问题1**: 推荐结果每次都一样，"换一批"按钮没有效果
- **原因**: TMDB API 单页结果固定，没有随机性
- **修复**: 获取多页数据（1-3页）并随机打乱，确保每次刷新都有不同结果
- **问题2**: 详情页返回列表按钮无响应
- **原因**: 详情页使用 sendPhoto 发送图片消息，返回时尝试编辑图片消息会失败
- **修复**: 添加 DeleteMessage 响应字段，从详情页返回时删除图片消息并重新发送列表
- **问题3**: DeleteMessage API 返回值解析错误
- **原因**: deleteMessage API 返回 bool 结果，但代码尝试解析为 Message 对象
- **修复**: 改用 makeSimpleRequest 方法处理返回 bool 的 API
- **数据源调整**: 改用 TMDB 直接数据，不再验证 MoviePilot 资源可用性
- **修改文件**:
  - `internal/handlers/search.go` - 多页获取+随机打乱，导航检测
  - `internal/bot/poll.go` - 处理 DeleteMessage 响应
  - `internal/callback/types.go` - 添加 DeleteMessage 字段
  - `internal/services/telegram.go` - 修复 DeleteMessage API 调用

### 2024-02-25 - 推荐去重修复
- **问题**: 精选推荐中出现同一媒体多个版本（如 4K、1080P）的重复情况
- **原因**: 使用标题进行去重，而同一媒体的不同资源版本标题可能略有不同
- **修复**: 改用 TMDB ID 进行去重，确保同一媒体只推荐一次
- **修改文件**:
  - `internal/handlers/search.go` - 所有推荐函数统一使用 ID 去重

### 2024-02-25 - 精选推荐优化
- **文案优化**: 将"AI 推荐"改为"精选推荐"，更符合产品定位
- **分类优化**:
  - 热门电影 → 本周热门（副标题：大家都在看的好片）
  - 热播剧集 → 热门剧集（副标题：追剧必看热门番）
  - 高分佳作 → 必看神作（副标题：高分经典，不容错过）
  - 最新上线 → 最新上映（副标题：刚上线的新鲜内容）
  - 随机发现 → 随机探索（副标题：发现未知的精彩）
- **空状态优化**: 更友好的提示文案
- **混合模式**: 使用 TMDB + MoviePilot 验证的混合推荐策略
- **修改文件**:
  - `internal/handlers/search.go` - 推荐算法优化，文案优化
  - `internal/handlers/callback.go` - 文案更新
  - `internal/services/moviepilot.go` - URL 编码修复，空数据验证
  - `cmd/bot/main.go` - 命令菜单文案更新
  - `internal/services/telegram.go` - 按钮文案更新

### 2024-02-25 - 搜索历史修复
- **问题**: 点击搜索按钮时没有显示历史搜索记录
- **修复**: 点击搜索按钮会显示最近 5 条搜索历史，每条历史记录都有快捷搜索按钮

### 2024-02-25 - 反馈历史整合
- **新功能**: 在主菜单添加「🐛 我的反馈」入口
- **功能**: 查看用户所有反馈历史，显示反馈状态，支持查看反馈详情和管理员回复

### 2024-02-25 - 清空搜索历史修复
- **问题**: 点击"清空历史"按钮后没有实际清空搜索历史
- **修复**:
  - 将回调数据格式从 `search:clear_history` 改为 `search:clear_history:1`
  - 修改 `ClearHistory` 函数释放锁后再调用 `saveAsync` 避免死锁

### 2024-02-25 - 系统稳定性优化
- **错误处理增强**: poll 回调处理添加全面的错误日志
- **HTTP 超时配置**:
  - 连接超时: 10s
  - 空闲连接超时: 90s
  - Keep-alive: 30s
  - 连接池: 100 最大连接
- **输入验证**: 新增 `pkg/validation/sanitize.go` 防止恶意输入
- **会话管理**: 改进日志记录，便于调试

### 2024-02-25 - 入库通知优化与片单功能简化
- **功能移除**: 删除请求优先级功能，简化求片流程
- **片单优化**:
  - 添加分页支持，每页显示 5 个项目
  - 简化添加流程，直接添加无需确认
  - 优化按钮布局和交互体验
- **剧集入库聚合**:
  - 10秒内同剧集多集合并显示（如 E01-E05）
  - 聚合消息格式化输出
- **入库通知格式优化**:
  - 使用横屏 backdrop 图片（1920x1080）
  - 从 TMDB 获取公开可访问的图片
  - 图片下载后通过 multipart 上传到 Telegram
  - 匹配参考频道 https://t.me/longemby_notify 样式
- **推送策略调整**:
  - 入库通知仅推送到群组（chatID < -100）
  - 不再推送给管理员私聊
  - 移除 mediaNotificationSvc 管理员推送
- **质量检测**: 添加从文件路径解析质量信息功能
- **日志优化**: 简化入库日志输出
- **修改文件**:
  - `internal/handlers/request.go` - 移除优先级选择流程
  - `internal/handlers/watchlist.go` - 分页支持和简化添加
  - `internal/services/webhook.go` - 剧集聚合、横屏图片、推送限制
  - `internal/services/telegram.go` - 图片下载上传
  - `internal/services/review.go` - 移除优先级排序
  - `cmd/bot/main.go` - 回调注册更新

### 2025-02-25 - 剧集季数选择功能
- **新增功能**: TV 剧集详情页显示季数选择按钮
  - 从 TMDB API 获取完整季数和集数信息
  - 显示季数总览（如"共 11 季 · 134 集"）
  - 每个季独立按钮（S1, S2, ...）
  - 支持订阅全季或单独订阅某一季
- **数据获取策略**:
  - 优先从 MoviePilot 获取媒体信息
  - MoviePilot 无数据时 fallback 到 TMDB API
  - 确保 TV 剧集始终能显示季数信息
- **按钮布局优化**:
  - 第一行: `[订阅全季] [加入片单] [返回]`
  - 季数按钮: 每行 3 个，简洁布局
  - 超过 6 季显示 "查看全部 X 季" 按钮
  - 移除冗余的图标，按钮文字更简洁
- **技术实现**:
  - 新增 `extractSeasons()` 辅助函数处理多种季数数据格式
  - 新增 `buildDetailFromTMDBTV()` 函数从 TMDB 获取完整 TV 信息
  - 修复 JSON 季数字段解析问题（对象 vs 数组）
- **修改文件**:
  - `internal/handlers/callback.go` - 季数显示和布局优化
  - `internal/services/moviepilot.go` - 季数字段类型改为 interface{}
  - `internal/services/search.go` - 使用 SeasonInfo 字段

### 2025-02-25 - 死锁问题修复
- **问题1**: 服务启动时卡在 "Creating basic clients and services..."
- **原因**: `UserMappingService.load()` 持有锁时调用 `save()`，导致死锁
- **修复**: 改用 `saveLocked()` 方法，因为锁已被持有
- **问题2**: `/link` 命令无响应，卡在 "Authentication successful"
- **原因**: `AddMapping()` 持有锁时调用 `scheduleSave()`，再次尝试获取锁
- **修复**: 将异步保存逻辑内联到 `AddMapping()` 中
- **问题3**: 未绑定用户点击求片按钮，没有显示绑定提示按钮
- **修复**: 添加 `Keyboard` 字段到 Response，确保绑定按钮显示
- **修改文件**:
  - `internal/services/user_mapping.go` - 死锁修复
  - `internal/handlers/request.go` - 绑定提示按钮修复
