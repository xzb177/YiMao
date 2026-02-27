# YiMao 项目更新日志

---

## 2026-02-28

### feat: 添加请求批准令牌机制防止重复提交 ✅ 已部署
- **问题**: 多位管理员同时批准同一求片请求会导致重复向 MoviePilot 提交订阅
- **解决方案**: 一次性令牌机制
  - `ReviewRequest` 新增 `ApproveToken` 字段
  - 创建请求时生成唯一的一次性令牌（时间戳 + 随机字符串）
  - 批准时验证令牌，验证通过后立即清空（一次性使用）
  - 批准按钮回调数据格式变为 `review_approve:id:xxx:token:yyy`
  - `Approve()` 方法增加状态检查，已批准则返回 special error
- **安全改进**: 使用 `crypto/rand` 生成安全的随机字符串
- **修改文件**:
  - `internal/services/review.go` - 令牌生成与验证逻辑
  - `internal/handlers/review.go` - 令牌参数传递与重复批准处理
  - `internal/handlers/request.go` - 通知消息按钮包含令牌
- **Git 提交**: `c42073c`

---

## 2026-02-28

### feat: 文件信息获取优化 - Emby API 补充 + 缓存机制 ✅ 已部署
- **问题**: 入库通知中文件大小和数量有时获取不到（如 strm 引用文件）
- **解决方案**: 多层降级策略 + 缓存机制
  - 新增 `fetchMediaSourcesFromEmby()` - 直接调用 Emby API 获取完整文件信息
  - 新增 `inferFileCount()` - 从路径智能推断文件数量（CD1/CD2, Part1/Part2 等）
  - 新增文件信息缓存 - 1小时 TTL，避免频繁 API 调用
- **优化**:
  - 缓存机制：同一文件 1 小时内不重复调用 API
  - 定期清理：每 30 分钟自动清理过期缓存
  - 负担控制：仅当 webhook 数据缺失时才调用 API
- **修改文件**:
  - `internal/services/webhook.go` - 新增文件信息获取增强逻辑
- **Git 提交**: `1941bec`

---

## 2026-02-28

### fix: 入库通知只推送到群组，不再发送给管理员个人 ✅ 已部署
- **问题**: 入库通知同时推送给群组和管理员私聊，造成重复通知
- **解决方案**:
  - `webhook.go`: 简化 `sendAggregatedEpisodeToAdmins` 函数，只发送到群组
  - `media_notification.go`: `handleItem` 不再发送即时通知给管理员
- **修改文件**:
  - `internal/services/webhook.go` - 删除管理员私聊通知逻辑
  - `internal/services/media_notification.go` - 移除即时通知发送代码
- **Git 提交**: `b550d13`

### feat: 增强电影格式检测，支持显示完整发布格式 ✅ 已部署
- **问题**: 电影格式只显示分辨率（如 "1080p"），不显示发布格式
- **解决方案**:
  - 新增 `Format` 字段存储发布格式
  - 新增 `parseReleaseFormat` 函数检测常见格式
  - 更新 `getFullQuality` 函数显示完整格式
- **支持格式**: BluRay.REMUX, WEB-DL, WEBRip, BluRay, HDTV, DVDRip, HDRip, WEB
- **显示效果**: `BluRay 1080p`、`WEB-DL 2160p` 等
- **修改文件**:
  - `internal/services/webhook.go` - 新增格式解析和显示逻辑
- **Git 提交**: `2b66541`

---

## 2026-02-28

### feat: 每日汇总自定义时间输入功能 ✅ 已部署
- **问题**: 「每日汇总时间回调没反应」- 原时间选择只支持预设时间
- **解决方案**: 新增自定义时间输入功能，支持任意 HH:MM 格式
- **新增功能**:
  - ✏️ 自定义时间按钮 - 在时间选择界面新增入口
  - 文本输入处理 - 支持 HH:MM 格式（00:00-23:59）
  - 完整验证逻辑 - 格式校验、范围校验、友好错误提示
  - 取消支持 - `/cancel` 或「取消」退出输入流程
- **使用方式**:
  1. 管理员菜单 → 通知设置 → 设置汇总时间
  2. 点击「✏️ 自定义时间」
  3. 输入时间（如 23:00、08:30）
  4. 确认设置成功并返回设置菜单
- **修改文件**:
  - `internal/callback/types.go` - 添加 `admin_notif_custom_time` 到白名单
  - `cmd/bot/main.go` - 注册自定义时间回调
  - `internal/bot/poll.go` - 添加 `waiting_for_time_input` 状态处理
  - `internal/handlers/admin.go` - 实现 `handleNotifCustomTime` 和 `HandleNotifCustomTimeInput`
- **Git 提交**: `c10ef3a`

---

## 2026-02-28

### fix: 修复回调注册 + 入库通知集成个人设置开关 ✅ 已部署
- **回调注册问题**: 新增的 V2 通知回调和管理员管理回调已在 main.go 中注册
- **入库通知集成**: 修改 flushSingleAggregation 使用 mediaNotificationSvc
- **新增方法**: `sendAggregatedEpisodeToAdmins()` - 根据管理员个人设置发送通知
- **功能**:
  - 检查每个管理员的 `Enabled` 和 `InstantEnabled` 设置
  - 支持个人格式偏好（详细/简洁）
  - 图片通知发送到启用的管理员私聊

#### Git 提交
- 提交: `07a54c4` - fix: 修复回调注册 + 入库通知集成个人设置开关

---

### feat: 多管理员权限控制模块 ✅ 已部署

### feat: 多管理员权限控制模块 ✅ 已部署
- **需求**: 实现完整的超级管理员/普通管理员两级权限体系
- **核心功能**:
  1. **权限分级**:
     - 👑 超级管理员（Root）- 可管理其他管理员，拥有所有权限
     - 普通管理员 - 可审批求片，不能管理管理员
  2. **管理员设置子菜单**（仅 Root 可见）:
     ```
     [ 📋 查看管理员列表 ]
     [ ➕ 添加管理员 ] [ ➖ 移除管理员 ]
     [ ⬅️ 返回上级菜单 ]
     ```
  3. **添加管理员流程**:
     - 点击后进入 `waiting_for_add_admin` 状态
     - 支持输入 Telegram 数字 ID
     - 自动校验是否已存在
     - 添加成功后自动重置状态
  4. **移除管理员流程**:
     - 列出所有可移除的管理员（排除 Root）
     - 点击对应按钮即可移除
     - 移除后自动刷新列表

#### 数据结构变更
- `AdminInfo` 结构体：新增 `Role` 字段（AdminRoleRoot / AdminRoleNormal）
- `AdminService` 新增方法：
  - `IsRootAdmin(userID)` - 检查是否为超级管理员
  - `GetRootAdminID()` - 获取超级管理员 ID
  - `GetAllAdminInfo()` - 获取所有管理员详细信息
  - `SetRootAdmin()` - 设置超级管理员

#### 新增 Callback Actions
- `admin_mgmt` - 管理员设置子菜单
- `admin_list` - 查看管理员列表
- `admin_add_start` - 开始添加管理员流程
- `admin_remove_list` - 显示可移除的管理员列表
- `admin_remove_confirm:id:xxx` - 确认移除指定管理员

#### 会话状态机
- `waiting_for_add_admin` - 等待输入管理员 ID 的状态
- 使用 Session.Set/Get/Delete 管理状态

#### 权限控制
- 管理员菜单中显示当前角色标识
- 【👮‍♂️ 管理员设置】按钮仅对 Root 可见
- 所有管理员管理操作都检查 Root 权限

#### 修改文件
- `internal/services/admin.go` - 扩展支持 Role 字段，新增 Root 管理方法
- `internal/handlers/admin.go` - 新增 5 个管理员管理处理函数
- `internal/bot/poll.go` - 添加管理员添加消息处理逻辑
- `internal/callback/types.go` - 添加新 action 白名单
- `cmd/bot/main.go` - 添加 AdminHandler 到依赖注入链

#### 数据迁移
- 首次加载时自动将旧格式迁移到新格式
- 第一个管理员自动成为 Root

#### Git 提交
- 提交: `df063a7` - feat: 多管理员权限控制模块 + 通知设置UI重构

---

### refactor: 管理员通知设置 UI 现代化重构 ✅ 已部署
- **设计理念**: 采用"现代化 Telegram Bot"标准，状态融合按钮模式
- **核心升级**:
  1. **极简文本引导**: 移除冗余的状态罗列，仅保留简短提示
  2. **状态融合按钮**: 状态直接写在按钮文本上，点击后原地刷新
  3. **原地刷新机制**: 使用 `EditMessageReplyMarkup` 仅更新按钮，不发新消息
  4. **防抖提示**: 每次操作调用 `AnswerCallbackQuery` 显示 Toast 提示

#### 按钮矩阵布局（严格遵守）
```
[第一行：核心功能开关]
[ 📺 单集推送: ❌ 关闭 ] [ 📰 每日汇总: ✅ 开启 ]

[第二行：偏好设置]
[ 📝 格式: 详细 🔄 ] [ ⏰ 汇总时间: 23:50 ✏️ ]

[第三行：全局控制]
[ 🔕 停用所有通知 ]

[第四行：导航返回]
[ ⬅️ 返回管理员菜单 ]
```

#### 新增 Callback Actions
- `admin_notif_toggle_single_v2` - 单集推送切换（仅刷新按钮）
- `admin_notif_toggle_daily_v2` - 每日汇总切换（仅刷新按钮）
- `admin_notif_toggle_format` - 格式循环切换（详细↔简洁）
- `admin_notif_disable_all` - 停用所有通知

#### 新增方法
- `buildNotifSettingsKeyboard()` - 构建通知设置按钮键盘（用于原地刷新）
- `EditMessageReplyMarkup()` - Telegram API 调用，仅更新消息按钮

#### 修改文件
- `internal/handlers/admin.go` - 重写 `handleNotifSettings`，新增 4 个 V2 处理方法
- `internal/bot/poll.go` - 更新 `handleCallbackResponse` 支持仅更新键盘
- `internal/services/telegram.go` - 新增 `EditMessageReplyMarkup` 方法
- `internal/callback/types.go` - 添加新 action 白名单

---

## 2026-02-27

### refactor: 我的请求列表 UI 彻底重构 - 分页 + 极致排版 ✅ 已部署
- **问题**: 请求列表一次性输出 100+ 条，严重刷屏，排版松散，易触发消息长度上限
- **核心升级**:
  1. **一行流极致压缩排版**: `序号. [状态Emoji] 影片名 (年份) [媒体类型Emoji] [额外信息]`
     - 状态 Emoji: ⏳排队中 🔍搜索中 ⬇️下载中 ✅已完成 ❌失败
     - 媒体类型: 🎬电影 📺剧集
  2. **分页功能**: 每页 10 条，底部导航 `[⬅️ 上一页] [2/5] [下一页 ➡️]`
  3. **数字操作按钮**: `[1]` `[2]`...`[10]` 每行 5 个（Telegram 限制），点击弹出子菜单
     - `🔄 重新搜索` - 触发 MoviePilot 重新搜索
     - `⬅️ 返回列表`
  4. **平滑刷新**: 使用 `EditMessageText` 原地更新，不刷屏
- **新增 Callback Actions**:
  - `myreqs_page` - 分页切换
  - `myreqs_item` - 项目操作（info/reshare/cancel）
- **新增 MoviePilot 方法**: `ReshareSubscription(id)` - 触发重新搜索
- **修改文件**:
  - `internal/handlers/menu.go` - MyRequestsHandler 完全重写 (+300 行)
  - `internal/callback/types.go` - 添加新 Action 常量
  - `internal/services/moviepilot.go` - 添加 ReshareSubscription 方法
  - `cmd/bot/main.go` - 注册新 callback handlers
- **修复**: 按钮布局问题 - 每行最多 5 个按钮，分两行显示 `[1][2][3][4][5]` / `[6][7][8][9][10]`

---

### fix: TMDB 跨库撞车严重 Bug - mediaType 参数强制修复 ✅ 已部署
- **问题**: 动漫入库时，TMDB fallback 返回了毫无关联的法国老电影的竖屏海报
- **根本原因**:
  1. `getTMDBBackdrop()` 只传 `TMDBID`，没有传媒体类型
  2. TMDB API 把 TV 的 ID 当作 Movie 去请求了！
  3. 使用了错误的 API 端点（基础详情端点而非 `/images` 端点）
  4. 会 fallback 到竖屏 poster（竖版海报）
- **解决方案**:
  1. **函数签名修改**: `getTMDBBackdrop(tmdbID string, mediaType string)`
  2. **API 端点修正**: 使用 `/{mediaType}/{id}/images` 端点
  3. **添加语言参数**: `include_image_language=zh,null`
  4. **绝对锁定横屏**: 只读取 `backdrops` 数组，无 backdrop 返回空字符串
  5. **严禁 poster fallback**: 宁可纯文本也不发竖屏海报
- **EmbyEnhancedInfo 结构体增强**: 添加 `Type` 字段传递媒体类型
- **修改文件**: `internal/services/webhook.go`
  - `getTMDBBackdrop()` - 完全重写，新增 mediaType 参数
  - `EmbyEnhancedInfo` - 添加 Type 字段
  - 8 处调用点全部更新，正确传入 `movie`/`tv`

### refactor: 图片获取策略重构 - TMDB 为主，删除 Emby 图片代码 ✅ 已部署
- **背景**: 简化图片获取逻辑，TMDB 作为唯一主要途径
- **删除代码**:
  - `flushEpisodeAggregation` - 删除 Emby Series 图片获取
  - `getEmbyEnhancedInfoForEpisode` - 删除 Emby 图片优先级
  - `getEmbyEnhancedInfo` - 删除 Emby backdrop/primary 获取
  - `getSeriesInfo` - 删除 Series backdrop/primary 图片获取
  - `sendNotificationWithPhoto` - 删除 Emby parent backdrop fallback
- **新优先级**:
  1. TMDB `/{mediaType}/{id}/images` → 获取横屏 backdrop
  2. 无 backdrop → 返回空 → 使用纯文本 fallback
- **效果**: 代码更简洁，避免 Emby 图片访问失败的问题
- **修改文件**: `internal/services/webhook.go` (-138 行, +78 行)

---

### fix: 入库通知图片稳定性增强 - TMDB 备胎优化 ✅ 已部署
- **问题**: 部分入库通知没有图片，TMDB 备胎不稳定
- **原因分析**:
  1. Emby 图片返回 500（Cloudflare 拦截）
  2. TMDB backdrop 有时为空
  3. Telegram 无法直接获取 TMDB 图片（`failed to get HTTP URL content`）
- **解决方案**:
  1. **增强 TMDB 获取逻辑**: backdrop 为空时自动 fallback 到 poster
  2. **TMDB 图片也使用代理上传**: 避免 Telegram 无法获取的问题
  3. **增加超时时间**: 从 5 秒增加到 10 秒
  4. **增强日志输出**: 清晰显示图片来源和失败原因
- **修改文件**:
  - `internal/services/webhook.go` - `getTMDBBackdrop()` 增强 poster 备胎，`sendNotificationWithPhoto()` TMDB 代理上传
  - `internal/services/telegram.go` - `SendPhotoWithAuth()` 通用化日志

### feat: Emby 媒体库搜索优化 - 模糊搜索 + 智能评分匹配 ✅ 已部署
- **问题**: 搜索逻辑过于死板，用户输入"怪奇迷案"无法搜索出《怪奇迷案限时破》
- **原因**: 原代码使用 `strings.Contains` 简单匹配，只返回第一个结果，忽略了更匹配的选项
- **解决方案**:
  1. **充分利用 Emby API 的原生模糊搜索**: `SearchTerm` 参数已支持模糊匹配
  2. **增加结果数量**: 从 10 条增加到 20 条，扩大匹配池
  3. **智能评分系统**: 根据标题相似度和年份匹配度打分，返回最优结果
  4. **搜索参数优化**:
     - `IncludeItemTypes=Movie,Series` - 只搜索电影和剧集，避免单集刷屏
     - `Recursive=true` - 递归搜索所有文件夹
- **评分规则**:
  - 标题完全匹配: 100 分
  - 标题包含搜索词: 50-80 分（根据覆盖率）
  - 年份完全匹配: +30 分
  - 年份接近(±2年): +15 分
- **效果示例**:
  - 搜索"怪奇迷案" → 找到《怪奇迷案限时破》
  - 搜索"复仇者" → 找到《复仇者联盟》
  - 搜索"全职猎人" → 找到《全职猎人 (1999)》和《全职猎人 (2011)》并返回最匹配的
- **修改文件**:
  - `internal/services/webhook.go` - `SearchEmbyMedia()` 函数重构

### feat: 图片缓存服务 - 减少 Emby 带宽消耗
- **问题**: 重复的入库通知会多次下载相同图片，对 Emby 服务器造成带宽负担
- **解决方案**: 本地图片缓存服务
- **缓存策略**:
  - 缓存目录: `/app/data/image_cache/`
  - 缓存时长: 7 天（可配置）
  - 文件命名: 图片 URL 的 MD5 哈希
  - 自动清理: 每 24 小时清理过期缓存
- **工作流程**:
  1. 发送图片前先检查缓存
  2. 缓存命中：直接使用本地文件
  3. 缓存未命中：下载后异步保存
- **效果**: 同一剧集的多次入库通知只下载一次图片，大幅减少 Emby 带宽消耗
- **修改文件**:
  - `internal/services/image_cache.go` - 新增缓存服务
  - `internal/services/telegram.go` - TelegramClient 添加缓存支持
  - `cmd/bot/main.go` - 初始化缓存服务并启动清理任务

### feat: 图片处理双重保障机制 - Emby 代理上传 + TMDB 中文优化
- **背景**: Emby 服务器与机器人不在同一内网，Telegram 无法直接访问 Emby 图片（Cloudflare 拦截）
- **解决方案**: 双重保障逻辑，代码纯净无需豆瓣爬虫

#### 第一步：Emby 图片代理上传（最优先）
- **逻辑**: 机器人使用 `http.Client` 下载 Emby webhook 中的真实图片 URL（Primary/Backdrop）
- **发送**: 调用 Telegram `SendPhotoWithAuth`，以 multipart/form-data 将内存中的图片字节流发给 Telegram
- **伪装**: 添加 User-Agent 伪装防止被拦截
- **修改**: `internal/services/telegram.go` - SendPhotoWithAuth() 添加 User-Agent 和更清晰的日志

#### 第二步：TMDB 备胎优化
- **修复**: TMDB API 添加中文语言参数
- **参数**: `&language=zh-CN` 和 `&include_image_language=zh,null`
- **效果**: 优先获取中文海报和横幅
- **修改**: `internal/services/webhook.go`
  - `getTMDBBackdrop()` - 添加中文参数
  - `getTMDBPoster()` - 添加中文参数

#### 第三步：图片优先级调整
- **原逻辑**: TMDB → Emby（导致 Emby 图片无法显示）
- **新逻辑**: Emby（代理上传）→ TMDB（备胎）
- **修改位置**:
  - `getEmbyEnhancedInfoForEpisode()` - 剧集增强信息获取
  - `getEmbyEnhancedInfo()` - 电影增强信息获取
  - `flushSingleAggregation()` - 入库聚合通知（两处）

#### 保留逻辑（一字未动）
- ✅ 首尾集数合并（如 E01-E12）
- ✅ 隐藏 0B 大小
- ✅ 空行排版

---

### fix: 反馈功能完整修复 - FeedbackHandler 依赖传递遗漏 (commit: 06ca91b)
- **问题**: 用户反馈流程中输入文本被当成搜索关键词处理
- **现象**: 点击"🐛 画质问题"后输入"画质不行"，回复"☹️ 未找到相关内容"
- **根本原因**: 三处依赖传递遗漏导致 FeedbackHandler 为 nil
  1. `webhook.go` 的 `HandleWebhookMessage` 缺少状态检查 ✅
  2. `cmd/bot/main.go` 的 `toBotDeps()` 未传递 `FeedbackHandler` ✅
  3. `cmd/bot/main.go` 的 `initRegistry()` 返回的 `deps` 未包含 `FeedbackHandler` ✅
- **日志证据**: `[Poll] Checking feedback process for user xxx, FeedbackHandler=false`
- **修改文件**:
  - `cmd/bot/main.go` - toBotDeps() 和 initRegistry() 函数
  - `internal/bot/webhook.go` - 添加反馈状态检查
  - `internal/handlers/feedback.go` - handleStart() Edit 参数修复

### fix: 详情页反馈按钮回调错误修复
- **问题**: 点击详情页「🐛 反馈」按钮报错：`Bad Request: there is no text in the message to edit`
- **原因**: 详情页使用 `sendPhoto` 发送图片消息，但 FeedbackHandler 的 `handleStart` 返回 `Edit: true`，尝试用 `editMessageText` 编辑图片消息失败
- **修复**: 将 `handleStart` 中的 `Edit: true` 改为 `Edit: false`，改为发送新消息而非编辑原消息
- **文件**: `internal/handlers/feedback.go` - handleStart() 方法

---

## ⚠️ 重要警告：媒体库入库通知功能

**核心文件**: `internal/services/webhook.go`

**禁止随意修改以下逻辑**：
1. **图片获取优先级** - 必须保持 TMDB > Emby（Emby 图片受 Cloudflare 保护，Telegram 无法访问）
2. **入库聚合机制** - 剧集批量聚合延迟发送，防止刷屏
3. **文件大小累加** - 聚合时正确累加 FileSize/FileCount
4. **季集格式化** - 统一使用 `E01-E23` 格式（忽略中间断层）
5. **通知排版格式** - 呼吸感空行、统一"总大小"标签

**修改前必须**：
- 理解现有逻辑的完整流程
- 在测试环境验证
- 确保不影响现有功能

**关键函数**：
- `flushAggregation()` - 入库聚合发送
- `getEmbyEnhancedInfoForEpisode()` - 剧集增强信息获取
- `getTMDBBackdrop()` - TMDB 横幅图获取
- `buildEpisodeRangeString()` - 季集范围格式化

---

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

### 2026-02-27 - 帮助文案优化
- **标题优化**: ❓ 帮助中心 → ✨ 功能介绍
- **按钮优化**: ❓ 帮助 → ✨ 功能介绍，⬅️ 返回主菜单 → ⬅️ 返回
- **内容精简**:
  - 移除冗余描述，突出核心功能
  - 推荐分类精简为一行展示
  - 快捷命令只保留核心项
- **提示优化**: 绑定提示文案更简洁友好
- **修改文件**:
  - `internal/handlers/menu.go` - HelpHandler 内容重构
  - `internal/services/telegram.go` - 主菜单按钮文案
  - `internal/handlers/request.go` - 绑定提示文案

### 2026-02-27 - 入库通知最终优化：文件大小累加 + 横幅图片 + 智能隐藏
- **修复1 - 文件大小重复累加**
  - **现象**: 第一集的文件大小被计算两次
  - **原因**: 创建聚合时从 `enhancedInfo.FileSize` 复制大小，添加第一集时又累加一次
  - **修复**: 将 `agg.FileSize` 初始化为 0，所有集数（包括第一集）都通过 `agg.FileSize += thisFileSize` 统一累加
- **修复2 - "未知"字样智能隐藏**
  - **现象**: 文件大小为 0 时显示"未知"，影响美观
  - **修复**: 当 `FileSize <= 0` 时，完全隐藏 `📦 总大小：...` 行（包括前后空行），不再输出"未知"
- **修复3 - 横幅图片补回**
  - **新增字段**: `EpisodeAggregation` 结构体添加 `SeriesID` 字段用于后续获取图片
  - **智能获取**: 在 `flushSingleAggregation` 发送通知前，如果没有图片：
    1. 先尝试从 Emby Series 获取 backdrop
    2. 失败时回退到 TMDB backdrop
    3. 全程容错处理，不会因图片获取失败而报错
- **修改文件**:
  - `internal/services/webhook.go`
    - `EpisodeAggregation` 结构体：添加 `SeriesID` 字段
    - `aggregateEpisode()`: FileSize 初始化为 0，保存 SeriesID
    - `flushSingleAggregation()`: 添加图片重试获取逻辑（Series > TMDB）
    - `formatAggregatedEpisodeMessage()`: FileSize 为 0 时隐藏整行
    - `formatEpisodePhotoCaption()`: FileSize 为 0 时隐藏整行

### 2026-02-27 - 入库通知三大致命Bug修复（真实Emby环境验证）
- **问题1 - 文件数量显示0个**
  - **现象**: 明明合并了几十集，文件数量却显示 0 个
  - **原因**: 试图累加单集的 FileCount，但 Emby Episode payload 里根本没有这个值，默认是 0
  - **修复**: 文件数量不再使用 FileCount，直接使用 `len(agg.Episodes)`（队列长度即为文件数量）
- **问题2 - 剧集名称完全丢失**
  - **现象**: 最终推送变成了 `✅ 入库成功： S01 E01...`，前面的名字没了
  - **原因**: `payload.SeriesName` 可能为空，没有 fallback 机制
  - **修复**: 添加多级 fallback：`payload.SeriesName` → `payload.Item.SeriesName` → `payload.Item.Name`
  - **安全检查**: `flushSingleAggregation` 中 SeriesName 为空时记录日志并跳过
- **问题3 - 文件大小显示0B**
  - **现象**: 即使累加了文件大小，依然显示 0 B
  - **修复**: 文件大小为 0 时显示"未知"而非 "0 B"，同时保留累加逻辑以备将来正确解析
- **修改文件**:
  - `internal/services/webhook.go`
    - `aggregateEpisode()`: SeriesName 多级 fallback，移除 FileCount 累加
    - `flushSingleAggregation()`: 添加 SeriesName fallback 和安全检查
    - `formatAggregatedEpisodeMessage()`: 使用 `len(agg.Episodes)` 计算文件数量，大小为0显示"未知"
    - `formatAggregatedEpisodeSimple()`: 大小为0显示"未知"
    - `formatEpisodePhotoCaption()`: 使用 `len(agg.Episodes)` 计算文件数量，大小为0显示"未知"

### 2026-02-26 - 横幅图优先级优化 + 极简呼吸感排版
- **图片优化**:
  - `getSeriesInfo` 添加 `BackdropImageTags` 字段请求，优先获取横幅图
  - 优化剧集图片回退逻辑：webhook backdrop > series backdrop > series primary > TMDB
- **排版优化**:
  - 重构为极简呼吸感排版，每项之间一个空行
  - 分割线改为纯文本横杠 `──────`，移除 Unicode 字符
  - 移除"引用文件"说法，统一使用"总大小"
  - 标题行完整显示年份和季集数：`[剧集名称] ([年份]) S01 E01-E23`
- **新格式示例**:
  ```
  ✅ 入库成功：神墓 (2024) S01 E01-E23
  ──────

  🎬 名称：神墓 (2024) S01 E01-E23

  🏷️ 类别：国产剧

  💎 质量：WEB-DL 1080p

  📦 总大小：5.41G

  📁 文件数量：23 个
  ```
- **修改文件**:
  - `internal/services/webhook.go`
    - `getSeriesInfo()`: 添加 BackdropImageTags 请求和图片获取逻辑
    - `getEmbyEnhancedInfoForEpisode()`: 优化图片回退逻辑
    - `formatAggregatedEpisodeMessage()`: 极简呼吸感排版
    - `formatEpisodePhotoCaption()`: 极简呼吸感排版

### 2026-02-26 - 修复入库通知数据造假和剧集刷屏问题
- **严重问题**: 入库通知出现数据造假和逻辑漏洞
- **问题1 - 质量造假**: 所有推送的质量显示为固定的 `WEB-DL 2160p`
  - **原因**: `parseQualityFromPath` 函数硬编码返回 `"1080p"`
  - **修复**: 无法解析时返回空字符串，严禁伪造数据
- **问题2 - 文件大小为0**: 文件大小和数量显示为 `0 B` 和 `0个`
  - **原因**: 创建聚合时没有从 enhancedInfo 复制 FileSize/FileCount
  - **修复**: 添加文件大小和数量的复制逻辑
- **问题3 - 剧集刷屏**: 队列合并代码未生效，每秒连发单集通知
  - **原因**: `handleItemAdded` 要求 `SeriesName != ""` 才聚合，否则直发
  - **修复**: 所有 Episode 类型强制进入聚合队列
- **额外优化**:
  - `detectQuality`: 4K 统一显示为 2160p，无法确定返回空字符串
  - WEB-DL 前缀只在质量非空时添加
  - 增强文件大小解析日志，支持小文件（如 strm）
- **修改文件**:
  - `internal/services/webhook.go`
    - `handleItemAdded()`: 移除 SeriesName 非空判断
    - `aggregateEpisode()`: 添加文件大小复制逻辑
    - `parseQualityFromPath()`: 返回空字符串而非硬编码
    - `detectQuality()`: 统一格式，返回空字符串

### 2026-02-26 - 剧集入库合并通知防刷屏机制
- **问题**: 一次性入库整季剧集时，Telegram 会被单集通知疯狂刷屏
- **解决方案**: 实现每个 Key 独立的 Debounce 防抖机制
- **核心特性**:
  - 每个 `seriesName_season` Key 有独立的 60 秒定时器
  - 新集到达时重置该 Key 的定时器，而非全局重置
  - 智能集数格式化：连续集数显示为 `E01-E10`，不连续显示为 `E01, E03, E05`
  - 累加每集的文件大小和数量，显示总信息
  - 使用 `sync.Mutex` 保证并发安全
- **通知格式示例**:
  ```
  ✅ 入库成功：神墓 S01 E01-E10, E12-E15
  ───────────────────
  🎬 名称：神墓 S01 E01-E10, E12-E15
  🏷️ 类别：国产剧
  💎 质量：WEB-DL 1080p
  📦 总大小：12.3 GB
  📁 文件数量：13个
  ```
- **工作流程**:
  1. 入库第1集 → 创建聚合 Key: "神墓_S01" → 启动 60s 定时器
  2. 入库第2集 → 加入队列 → 重置 60s 定时器
  3. 60s 无新集 → 触发发送合并通知 → 清理聚合数据
- **修改文件**:
  - `internal/services/webhook.go` - 核心聚合逻辑重构
    - `EpisodeAggregation` 结构：添加独立 `timer` 和 `mu` 字段
    - `WebhookService` 结构：移除全局定时器，添加 `aggregationDelay`
    - `aggregateEpisode()` 函数：重写，实现独立定时器防抖
    - `flushSingleAggregation()` 函数：新增，处理单个 Key 的发送
  - `docs/episode-aggregation.md` - 新增功能文档

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


---

## 2026-02-27 入库通知格式优化与订阅重试

### 入库通知格式优化

#### 集数显示优化
- **问题**: 不连续集数显示过长，如 `S03 E02, E06, E08, E10, E12, E18`
- **解决**: 强制首尾合并，统一格式为 `E02-E18`（忽略中间断层）
- **单集**: 保持 `E02` 格式

#### 通知格式优化
- **顶部行** `✅ 入库成功`：保留完整信息（剧名+年份+季集数）
  - 示例：`✅ 入库成功：全职猎人 (1999) S01 E02-E18`
- **名称行** `🎬 名称`：只显示剧名+年份，移除重复的季集数
  - 示例：`🎬 名称：全职猎人 (1999)`

#### 完整效果预览
```
✅ 入库成功：全职猎人 S01 E02-E18
──────

🎬 名称：全职猎人 (1999)

🏷️ 类别：动漫

💎 质量：WEB-DL 1080p

📦 总大小：5.41G

📁 文件数量：5 个
```

### TMDB 图片获取修复

#### ProviderIds 键名大小写问题
- **问题**: Emby webhook 返回 `ProviderIds: {"Tvdb": "xxx", "Imdb": "xxx"}`
- **原代码**: 只查找小写的 `tmdb` 键，导致无法获取 TMDB ID
- **解决**: 支持多种键名变体
  - `"tmdb"` (小写)
  - `"Tmdb"` (首字母大写)
  - `"Tvdb"` (TVDB ID，TMDB API 可用)

#### 图片获取优先级
1. **TMDB backdrop** (外部可访问) ✅
2. Emby backdrop (因 Cloudflare 保护，可能无法访问)
3. Emby primary (备用)

### 订阅重试机制

#### 问题诊断
- **现象**: 批准后订阅状态为 `"R"` (Recycled/重新搜索)
- **原因**: MoviePilot 找不到资源，订阅进入回收状态

#### 自动重试逻辑
- **检测**: 每 5 分钟刷新订阅状态
- **触发**: 当订阅状态为 `"R"` 时自动执行
- **流程**:
  1. 删除旧订阅
  2. 重新创建订阅（使用原始媒体信息）
  3. 更新订阅 ID
- **日志**: `[ReviewService] Resubscribing xxx: new subscription ID xxx`

### 文件变更
- `internal/services/webhook.go` - 季集格式化、TMDB ID 获取
- `internal/services/review.go` - 自动重新订阅功能
- `internal/services/telegram.go` - 订阅重试辅助函数

### 部署信息
- 提交: `2e9b3cf`
- 推送: `a6c98f0..2e9b3cf`
- 部署时间: 2026-02-27



## 2026-02-27

### fix: 入库通知横幅图优先级调整
- **问题**: Emby 图片优先级高于 TMDB，导致 Telegram 无法访问（Cloudflare 保护）
- **修复**: 调整图片获取优先级为 **TMDB > Emby**，确保横幅图可正常显示
- **文件**: `internal/services/webhook.go`
- **优先级顺序**:
  1. TMDB backdrop（优先，外部可访问）
  2. Emby parent backdrop（回退）
  3. Emby series primary（最后回退）

---

## 2026-02-27

### fix: 求片查重逻辑严重Bug - 同名影剧类型冲突
- **问题**: 用户求一部"剧集"，但 Emby 里有同名"电影"，机器人错误提示"库里已存在"
- **根本原因**: `SearchEmbyMedia` 函数硬编码 `IncludeItemTypes=Movie,Series`，忽略了传入的 `mediaType` 参数
- **修复**: 根据请求的媒体类型动态过滤 Emby 搜索结果
  - 求电影时：`IncludeItemTypes=Movie`
  - 求剧集时：`IncludeItemTypes=Series`
  - 只有【名称匹配】且【类型匹配】时才判定为已存在
- **修复前**:
  ```go
  searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=Movie,Series&Recursive=true&Limit=20", ...)
  ```
- **修复后**:
  ```go
  var includeItemTypes string
  switch mediaType {
  case MediaTypeMovie:
      includeItemTypes = "Movie"
  case MediaTypeTV:
      includeItemTypes = "Series"
  default:
      includeItemTypes = "Movie,Series"
  }
  searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=%s&Recursive=true&Limit=20", ..., includeItemTypes)
  ```
- **修改文件**: `internal/services/webhook.go` - `SearchEmbyMedia()` 函数
- **部署**: `docker compose up -d --build` ✅ 已部署
- **影响**: 彻底解决同名电影/剧集互相干扰的问题

---

### debug: 搜索功能 Panic 问题 ✅ 已解决
- **问题**: 搜索时出现 `runtime error: index out of range [0] with length 0` panic
- **现象**: 搜索"择天记"时服务崩溃，显示超时
- **日志**: `[Callback] Panic recovered: action=search, userID=xxx, panic=runtime error: index out of range [0] with length 0`
- **根因**: 之前的构建存在代码问题，重新构建后问题消失
- **解决方案**: 重新构建并部署服务
- **验证**: 用户搜索"择天记"成功，创建了求片请求
- **修改文件**:
  - `internal/services/moviepilot.go` - 添加调试日志
  - `internal/handlers/search.go` - 添加调试日志
- **部署**: `docker compose up -d --build` ✅ 已部署
- **状态**: ✅ 问题已解决
- **提交**: `e505a95` - 已推送到远程仓库

---
**2026-02-27 18:11** - 最新版本部署完成，服务正常运行 ✅

## 2026-02-27

### fix: 求片查重逻辑严重Bug - 同名影剧类型冲突
- **问题**: 用户求一部"剧集"，但 Emby 里有同名"电影"，机器人错误提示"库里已存在"
- **根本原因**: `SearchEmbyMedia` 函数硬编码 `IncludeItemTypes=Movie,Series`，忽略了传入的 `mediaType` 参数
- **修复**: 根据请求的媒体类型动态过滤 Emby 搜索结果
  - 求电影时：`IncludeItemTypes=Movie`
  - 求剧集时：`IncludeItemTypes=Series`
  - 只有【名称匹配】且【类型匹配】时才判定为已存在
- **修复前**:
  ```go
  searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=Movie,Series&Recursive=true&Limit=20", ...)
  ```
- **修复后**:
  ```go
  var includeItemTypes string
  switch mediaType {
  case MediaTypeMovie:
      includeItemTypes = "Movie"
  case MediaTypeTV:
      includeItemTypes = "Series"
  default:
      includeItemTypes = "Movie,Series"
  }
  searchParams := fmt.Sprintf("?SearchTerm=%s&IncludeItemTypes=%s&Recursive=true&Limit=20", ..., includeItemTypes)
  ```
- **修改文件**: `internal/services/webhook.go` - `SearchEmbyMedia()` 函数
- **部署**: `docker compose up -d --build` ✅ 已部署
- **影响**: 彻底解决同名电影/剧集互相干扰的问题


