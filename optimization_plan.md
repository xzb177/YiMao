# Emby Telegram Bot 深度优化方案

## 1. 智能去重与聚合 (优先级: 高)

### 1.1 短时间事件聚合
- **剧集批量入库**: 10分钟内同一剧集的多集入库，合并为一条通知
  - 示例: "《权力的游戏》第1-5集已入库"
- **电影批量入库**: 同一天内多部电影合并通知
- **去重窗口**: 可配置的聚合时间窗口 (默认10分钟)

### 1.2 防刷屏机制
- 单个用户短时间内多个请求合并显示
- 设置最大消息频率限制
- 相同媒体多次请求自动去重

### 1.3 实现方式
```go
// 聚合缓冲区
type AggregationBuffer struct {
    events map[string][]AggregatedEvent  // key: mediaID
    mutex  sync.RWMutex
    timer  *time.Timer
}

// 定时刷新聚合消息 (每5分钟)
// 超时自动发送聚合消息
```

---

## 2. Jellyseerr API 深度集成 (优先级: 高)

### 2.1 需要的环境变量
```bash
JELLYSEERR_URL=https://embyrequest.oceancloud.asia
JELLYSEERR_API_KEY=your_api_key_here  # 新增
```

### 2.2 内联操作增强
- ✅ 批准/拒绝请求 - 直接调用 API 操作
- 📊 查看请求状态 - 实时查询状态
- 🔍 搜索媒体 - 支持搜索并直接请求
- 📋 查看待处理列表 - 显示所有待审批请求

### 2.3 新增命令
```
/pending - 查看待处理请求
/search <关键词> - 搜索媒体
/request <tmdb_id> - 直接发起请求
/approve <request_id> - 批准请求
/decline <request_id> - 拒绝请求
```

### 2.4 实现方式
```go
// Jellyseerr API 客户端
type JellyseerrClient struct {
    baseURL string
    apiKey  string
}

// API 方法
func (c *JellyseerrClient) ApproveRequest(requestID int) error
func (c *JellyseerrClient) DeclineRequest(requestID int) error
func (c *JellyseerrClient) GetPendingRequests() []Request
func (c *JellyseerrClient) SearchMedia(query string) []Media
```

---

## 3. 智能推送策略 (优先级: 中)

### 3.1 用户订阅偏好
- 用户可选择关注的内容类型
  - 电影 / 剧集 / 分开关注
  - 仅特定类型 / 特定演员
  - 仅高评分内容

### 3.2 勿扰模式
- 设置免打扰时间段
- 紧急通知（视频/音频问题）不受限

### 3.3 关键词过滤
- 白名单/黑名单关键词
- 支持正则表达式
- 示例: 只通知包含 "4K" 的内容

### 3.4 实现方式
```go
// 用户偏好配置
type UserPreferences struct {
    UserID        string
    NotifyMovies  bool
    NotifySeries  bool
    QuietHours    QuietHoursConfig
    Keywords      KeywordFilter
}

type QuietHoursConfig struct {
    Enabled bool
    Start   string  // "22:00"
    End     string  // "08:00"
}
```

---

## 4. 数据分析仪表板 (优先级: 中)

### 4.1 统计维度
- 请求趋势: 每日/每周/每月请求量
- 热门媒体: Top 10 请求最多
- 用户活跃度: 谁请求最多
- 处理效率: 平均批准时间
- 媒体类型分布: 电影 vs 剧集

### 4.2 可视化输出
- Telegram 内嵌图表 (使用 Mermaid 或字符图)
- Web 仪表板 (可选)
- 定期统计报告

### 4.3 新增命令
```
/trends - 查看请求趋势
/top - 查看热门媒体
/activity - 查看用户活跃度
/report - 生成详细报告
```

### 4.4 实现方式
```go
// 统计数据结构
type Analytics struct {
    Requests      []RequestRecord
    Users         map[string]*UserStats
    Media         map[string]*MediaStats
}

// 字符图表生成
func GenerateChart(data []int) string
```

---

## 5. 其他增强功能

### 5.1 智能通知
- 媒体可用时自动 @请求者
- 季季完结通知
- 续订通知 (新季开播)

### 5.2 多语言支持
- 中文 / 英文 切换
- 根据用户设置自动切换

### 5.3 备份与恢复
- 管理员列表备份
- 用户偏好备份

---

## 实施优先级

### 第一阶段 (立即实施)
1. ✅ 智能去重与聚合
2. ✅ Jellyseerr API 基础集成 (批准/拒绝)

### 第二阶段 (后续)
3. 智能推送策略
4. 数据分析仪表板

### 第三阶段 (可选)
5. Web 仪表板
6. 多语言支持

---

## 配置示例 (.env 新增)

```bash
# Jellyseerr API
JELLYSEERR_URL=https://embyrequest.oceancloud.asia
JELLYSEERR_API_KEY=your_api_key_here

# 聚合设置
AGGREGATION_WINDOW=10m      # 聚合时间窗口
MAX_MESSAGES_PER_MINUTE=10  # 防刷屏限制

# 推送策略
ENABLE_SMART_FILTER=true
DEFAULT_NOTIFY_MOVIES=true
DEFAULT_NOTIFY_SERIES=true
QUIET_HOURS_ENABLED=false
QUIET_HOURS_START=22:00
QUIET_HOURS_END=08:00
```
