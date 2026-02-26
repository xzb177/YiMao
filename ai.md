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
- **电影**: ✅ 立即求片 + 🐛 反馈 + ⬅️ 返回
- **剧集**: ✅ 订阅全季 + 🐛 反馈 + ⬅️ 返回列表 + 分季选择

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

### 2024-02-25 - 个人片单与请求优先级（已废弃）
- ~~**新增功能**: 个人片单系统~~（已于 2025-02-25 删除）
- ~~**新增功能**: 请求优先级系统~~（已于 2025-02-25 简化移除）
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
  - 第一行: `[订阅全季] [反馈]`
  - 第二行: `[返回] [全部 X 季]`（如有多季）
  - 季数按钮: 每行 3 个，简洁布局
  - 超过 6 季显示 "查看全部 X 季" 按钮
  - 移除冗余的图标，按钮文字更简洁
  - （注：片单功能已于后续版本删除）
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

### 2025-02-25 - 通知格式切换与横幅图片修复
- **新增功能**: 通知格式切换
  - 环境变量 `NOTIFICATION_FORMAT` 支持 `simple` 或 `detailed`
  - `simple`: 简洁格式，仅显示核心信息（标题、质量、大小、时间）
  - `detailed`: 详细格式，显示完整媒体信息（名称、类别、质量、文件信息）
- **横幅图片修复**:
  - 修复 Emby 横幅图片无法正确获取的问题
  - 之前代码从 `ImageTags["Backdrop"]` 读取，但 Emby API 返回的是 `BackdropImageTags` 数组
  - 现在正确从 `BackdropImageTags[0]` 读取横幅标签
  - 图片获取优先级：TMDB 横幅 → Emby 横幅 → Emby 主图
- **调试优化**: 添加详细的调试日志，便于排查图片获取问题
- **修改文件**:
  - `internal/config/config.go` - 添加 NotificationFormat 配置字段
  - `internal/services/webhook.go` - 横幅图片修复、格式切换支持、调试日志
  - `cmd/bot/main.go` - 传递 NotificationFormat 配置
  - `.env.example` - 添加 NOTIFICATION_FORMAT 说明

### 2025-02-25 - 管理员权限检查修复
- **问题**: `review_approve` 和 `review_reject` 回调没有管理员权限检查
- **影响**: 任何用户都可以批准或拒绝求片请求（严重安全问题）
- **原因**: `ReviewHandler.handleApprove` 和 `handleReject` 缺少权限验证
- **修复**: 在两个方法中添加 `adminService.IsAdmin()` 检查
- **修改文件**:
  - `internal/handlers/review.go` - 添加管理员权限检查到批准/拒绝方法

### 2025-02-25 - MoviePilot 无资源时显示警告
- **问题**: MoviePilot 没有资源时，详情页仍然显示"立即求片"，用户不知道可能无资源
- **影响**: 用户求片后才发现可能没有资源，体验不好
- **原因**: `GetMediaInfo` 失败后，详情页 fallback 到 TMDB 或基本视图，没有显示资源不可用警告
- **修复**:
  - 在 `buildDetailFromSearch` 中检测 MoviePilot "not found" 错误
  - 当 MoviePilot 无资源时，详情页显示 `⚠️ 资源库暂无` 警告
  - 求片按钮文字改为 `🔄 尝试求片`（而非 `✅ 立即求片`）
- **修改文件**:
  - `internal/handlers/callback.go` - 添加资源可用性检测和警告显示

### 2025-02-25 - 删除片单功能
- **功能移除**: 完全删除个人片单功能
  - 移除用户可收藏影片到个人片单的功能
  - 移除命名收藏夹分类管理功能
  - 简化 UI，减少功能复杂度
- **删除文件**:
  - `internal/handlers/watchlist.go` - 片单处理器
  - `internal/services/watchlist.go` - 片单服务
  - `data/watchlists.json` - 片单数据文件
- **修改文件**:
  - `internal/callback/types.go` - 移除 watchlist 相关 action 白名单
  - `internal/handlers/callback.go` - 移除详情页「加入片单」按钮
  - `internal/services/telegram.go` - 移除主菜单「我的片单」按钮
  - `cmd/bot/main.go` - 移除 WatchlistService 初始化和注册
  - `install.sh` - 移除 watchlists.json 初始化

### 2025-02-25 - 全面代码审查与修复
- **代码审查**: 对项目进行全面代码审查，检查安全性、并发、资源管理等方面
- **发现并修复的严重问题**:
  - `QuotaService.getOrCreateQuotaUnsafe` 中的死锁风险：持有锁时调用 `save()`
  - `QuotaService.SyncFromJellyseerr` 同样的死锁风险
  - `request.go` 中重复的 TMDB ID 解析代码
- **已修复的中等问题**:
  - Callback action 白名单验证：添加 `validActions` 白名单，拒绝未注册的 action
  - Goroutine 错误处理：`notifyAdmins` 添加错误日志记录
- **审查发现的其他问题（已记录待后续修复）**:
  - 日志级别不一致（轻微）
  - 魔法数字未提取为常量（轻微）
- **修改文件**:
  - `internal/services/quota.go` - 修复死锁风险
  - `internal/handlers/request.go` - 删除重复代码
  - `internal/callback/types.go` - 添加 action 白名单验证
  - `internal/handlers/feedback.go` - 添加错误处理

### 2025-02-25 - 剧集详情页按钮布局优化
- **优化目标**: 提升用户体验，使常用按钮更易访问
- **按钮布局重构**:
  - **第一行（主要操作）**: `[✅ 订阅全季] [🐛 反馈]`
  - **第二行（导航）**: `[⬅️ 返回列表] [📺 全部 N 季]`（如有多季）
  - **后续行**: 季数按钮（每行 3 个）
- **改进点**:
  - 反馈按钮从底部移至顶部，更易访问
  - 按钮按功能分组：操作、导航、选择
  - 所有 TV 详情页布局统一（TMDB、MoviePilot、搜索结果）
  - 季数列表页新增反馈按钮
- **修改文件**:
  - `internal/handlers/callback.go` - 所有详情页按钮布局重构
    - `buildDetailFromTMDBTV()` - TMDB TV 详情页
    - `buildSimpleTVDetail()` - 简化 TV 详情页
    - `buildDetailFromMediaInfo()` - MoviePilot TV 详情页
    - `buildBasicDetailFromSearch()` - 搜索结果详情页
    - `HandleSeasons()` - 季数列表页

### 2025-02-25 - 入库通知格式优化（STRM 文件处理）
- **问题**: 使用 `.strm` 引用文件时，入库通知在显示质量后就停止，看起来像被截断
- **原因**:
  - `.strm` 文件的 MediaSources 中的 Size 只有几百字节（引用文件本身的大小）
  - 当 FileSize 小于 1MB 时没有实际意义，但不显示会让通知看起来不完整
- **修复**:
  - 忽略小于 1MB 的文件大小（可能是 .strm 文件本身的大小）
  - 当没有有效文件大小时，质量行后面只添加一个换行符，保持格式一致性
  - 添加调试日志输出 FileSize 和 FileCount 值
- **影响**:
  - `.strm` 文件的入库通知现在在质量行后正确结束，不会显示无意义的几百字节
  - 如果有实际媒体文件大小信息（>1MB），仍然会正常显示
- **修改文件**:
  - `internal/services/webhook.go` - formatEmbyNotificationEnhanced, formatPhotoCaption, formatEpisodePhotoCaption

### 2025-02-25 - 文档一致性修正
- **问题**: ai.md 文档前面描述的功能仍包含已删除的片单功能
- **修复**:
  - 更新操作按钮描述，移除"📎 加入片单"按钮
  - 将片单功能相关更新日志标记为"已废弃"
- **修改文件**:
  - `ai.md` - 文档一致性修正

### 2025-02-25 - 每日汇总通知时间调整与入库记录功能
- **调整**: 将每日汇总通知时间从 12:59 改为 23:50（晚间推送更合适）
- **新增功能**: 入库媒体自动添加到每日汇总列表
  - 实时入库时自动记录到汇总列表
  - 剧集聚合入库时自动记录到汇总列表
  - 支持检测媒体类型（电影/剧集/动画）
- **修改文件**:
  - `internal/services/media_notification.go` - 默认汇总时间改为 23:50
  - `internal/services/webhook.go` - 新增 addMediaItemToSummary() 和 addAggregatedEpisodeToSummary() 方法

### 2025-02-26 - 搜索详情页修复与入库通知图片修复
- **问题1**: 点击搜索结果后详情页不出来
  - **原因**: `handleSelect` 方法硬编码了 `type:movie`，导致剧集也被当作电影处理
  - **修复**: 从 callback 的 `Params["type"]` 中获取正确的 media type
- **问题2**: 入库通知 TMDB 图片无法显示
  - **原因**: Emby 返回的 ProviderIds 中 TMDB key 是小写 `tmdb`，但代码检查的是首字母大写 `Tmdb`
  - **修复**: 将 `ProviderIds["Tmdb"]` 改为 `ProviderIds["tmdb"]`
- **问题3**: 发送图片时 UTF-8 编码错误
  - **原因**: multipart form 处理中文字符时编码问题
  - **修复**: `SendPhoto` 方法优先使用 URL 方式发送图片，避免 multipart 编码问题
- **修改文件**:
  - `internal/handlers/search.go` - handleSelect() 方法修复
  - `internal/services/webhook.go` - TMDB ID 提取修复（3处）
  - `internal/services/telegram.go` - SendPhoto() 方法优化

### 2025-02-26 - 入库通知图片和类别显示修复
- **问题1**: 剧集入库通知只显示"剧集"而非详细类别（如"日漫"、"国产剧"）
  - **原因**: Emby webhook 中单集 (Episode) 的 Genres 为空，代码使用 Episode ID 查询 API 获取不到类别信息
  - **修复**: 新增 `getEmbyEnhancedInfoForEpisode` 函数，额外查询 Series 获取 Genres
- **问题2**: 剧集入库通知图片不显示
  - **原因**: 代码使用 Episode ID 查询图片，但单集没有 backdrop 图片
  - **修复**: 使用 webhook payload 中的 `ParentBackdropItemId` 和 `ParentBackdropImageTags` 构建图片 URL
- **技术实现**:
  - 扩展 `EmbyItem` 结构体，添加 SeriesId、ParentBackdropItemId、ParentBackdropImageTags、SeriesPrimaryImageTag 等字段
  - 添加 `TMDBID` 字段到 `EmbyEnhancedInfo` 用于图片查找
  - 新增 `getSeriesInfo` 函数专门查询 Series 信息
  - 新增 `getEmbyEnhancedInfoForEpisode` 函数处理 Episode 类型的增强信息获取
  - 修改 `aggregateEpisode` 函数使用新的 episode-aware 函数
- **修改文件**:
  - `internal/services/webhook.go` - 结构体扩展、新增函数、聚合逻辑修复

### 2025-02-26 - 入库通知引用文件显示优化
- **问题**: 入库通知在质量行后就停止，没有显示文件大小和数量信息
  - **原因**: 当文件小于 1MB（如 .strm 引用文件）时，代码直接跳过显示，导致通知看起来不完整
- **修复**: 即使是引用文件也显示文件信息，使用不同标识区分
  - 大于 1MB：显示 `📦 总大小：X.XXG`
  - 小于 1MB：显示 `📋 引用文件：XXXB`（标识这是引用文件）
- **调试增强**: 添加调试日志帮助排查 enhanced 信息获取问题
  - `formatPhotoCaption` 开始时输出 Quality、FileSize、FileCount
  - `getEmbyEnhancedInfoForEpisode` 输出 episode 信息和错误状态
- **修改文件**:
  - `internal/services/webhook.go` - formatPhotoCaption() 和 formatEpisodePhotoCaption() 函数优化
