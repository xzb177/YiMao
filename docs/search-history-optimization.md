# YiMao 搜索历史优化方案

> 基于现有功能的增强和改进建议

---

## 📊 当前功能分析

### 现有功能
```
✅ 存储用户搜索历史（最多20条）
✅ 显示最近5条搜索
✅ 点击历史记录快速搜索
✅ 清空历史功能
✅ 搜索频率统计（Count）
✅ 支持搜索建议（基于历史）
```

### 当前界面
```
🔍 搜索影片

📜 最近搜索：

1. 复仇者联盟
2. 盗梦空间
3. 绝命毒师
4. 沙丘
5. 奥本海默

[🔎 复仇者联盟] [🔎 盗梦空间]
[🔎 绝命毒师] [🔎 沙丘]
[🔎 奥本海默]
[🗑️ 清空历史]
[⬅️ 返回主菜单]
```

---

## 🎯 优化方向

### 方向一：UI 优化 ⭐⭐⭐⭐⭐
使用新设计的 UI 风格，提升视觉体验

### 方向二：功能增强 ⭐⭐⭐⭐⭐
添加热门搜索、搜索趋势等新功能

### 方向三：体验优化 ⭐⭐⭐⭐
改进交互流程，提升用户体验

### 方向四：性能优化 ⭐⭐⭐
使用数据库替代 JSON，提升性能

---

## 🎨 UI 优化方案

### 方案 A：暗黑霓虹风

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔮 搜索历史 · 你的观影足迹
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰

📊 统计数据
• 总搜索次数：28 次
• 本周搜索：12 次
• 最常搜索：复仇者联盟 (5次)

▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰

🔥 最近搜索

1. 🎬 复仇者联盟 [5次] [今天]
2. 🎬 盗梦空间 [3次] [昨天]
3. 📺 绝命毒师 [2次] [3天前]
4. 🎬 沙丘 [1次] [1周前]
5. 🎬 奥本海默 [1次] [1周前]

▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰

[🔍 快速搜索] [📊 查看全部]
[🗑️ 清空历史] [⚙️ 管理历史]
[⬅️ 返回]
```

### 方案 B：新中式风

```
┌───────────────────────────────────┐
│                                   │
│      ❀ 搜索历史 · 观影记录 ❀       │
│      ───────                    │
│      你的影片寻找之旅               │
│                                   │
└───────────────────────────────────┘

   ╭───────────────────────╮
   │  📊 搜索统计         │
   ╰───────────────────────╯

   ✿ 总计：28 次
   ✿ 本周：12 次
   ✿ 热门：复仇者联盟

   ╭───────────────────────╮
   │  ❀ 最近搜索          │
   ╰───────────────────────╯

   [1] 复仇者联盟
       今天 · 搜过 5 次

   [2] 盗梦空间
       昨天 · 搜过 3 次

   [3] 绝命毒师
       3天前 · 搜过 2 次

┌───────────────────────────────────┐
│  [🔍 搜索] [📊 全部]          │
│  [🗑️ 清空] [⬅️ 返回]          │
└───────────────────────────────────┘
```

### 方案 C：代码终端风

```
$> YiMao v2.0 - Search History
$> ─────────────────────────────────

$> USER_STATS:
$>   Total Searches: 28
$>   This Week: 12
$>   Most Frequent: 复仇者联盟 (5x)

$> ─────────────────────────────────

$> RECENT_SEARCHES (Top 5):

[1] movie - 复仇者联盟
    Count: 5x | Last: Today

[2] movie - 盗梦空间
    Count: 3x | Last: Yesterday

[3] tv - 绝命毒师
    Count: 2x | Last: 3 days ago

[4] movie - 沙丘
    Count: 1x | Last: 1 week ago

[5] movie - 奥本海默
    Count: 1x | Last: 1 week ago

$> ─────────────────────────────────
$> [Q]UICK_SEARCH [V]IEW_ALL [C]LEAR
$> ─────────────────────────────────
$> SELECTION [1-5] >
```

---

## 🔧 功能增强方案

### 1. 热门搜索（跨用户）

```go
// 显示所有用户最常搜索的内容
func (s *SearchHistoryService) GetPopularSearches(limit int) []PopularSearch {
    type SearchCount struct {
        Query string
        Count int
    }

    // 统计所有用户的搜索
    searchCounts := make(map[string]int)
    for _, entries := range s.history {
        for _, entry := range entries {
            searchCounts[entry.Query] += entry.Count
        }
    }

    // 排序并返回前 N 个
    var results []PopularSearch
    for query, count := range searchCounts {
        results = append(results, PopularSearch{
            Query: query,
            Count: count,
        })
    }

    sort.Slice(results, func(i, j int) bool {
        return results[i].Count > results[j].Count
    })

    if len(results) > limit {
        results = results[:limit]
    }

    return results
}
```

### 2. 搜索趋势（按时间）

```go
// 显示最近的热门搜索趋势
func (s *SearchHistoryService) GetSearchTrends(days int) []TrendItem {
    cutoff := time.Now().AddDate(0, 0, -days)

    type TrendItem struct {
        Query    string
        Count     int
        Yesterday int
    }

    trends := make(map[string]*TrendItem)

    // 统计最近几天的搜索
    for _, entries := range s.history {
        for _, entry := range entries {
            if entry.Timestamp.After(cutoff) {
                if _, exists := trends[entry.Query]; !exists {
                    trends[entry.Query] = &TrendItem{
                        Query: entry.Query,
                    }
                }
                trends[entry.Query].Count += entry.Count
            }
        }
    }

    // 计算趋势（对比昨天）
    yesterday := cutoff.AddDate(0, 0, -1)
    for _, entries := range s.history {
        for _, entry := range entries {
            if entry.Timestamp.After(yesterday) && entry.Timestamp.Before(cutoff) {
                if trend, exists := trends[entry.Query]; exists {
                    trend.Yesterday += entry.Count
                }
            }
        }
    }

    // 转换为列表并按增长排序
    var result []TrendItem
    for _, trend := range trends {
        result = append(result, *trend)
    }

    sort.Slice(result, func(i, j int) bool {
        growthI := result[i].Count - result[i].Yesterday
        growthJ := result[j].Count - result[j].Yesterday
        return growthI > growthJ
    })

    return result
}
```

### 3. 按时间分组显示

```go
// 获取按时间分组的历史
func (s *SearchHistoryService) GetHistoryGrouped(telegramID int64) map[string][]SearchEntry {
    entries := s.GetHistory(telegramID)
    now := time.Now()

    grouped := make(map[string][]SearchEntry)

    for _, entry := range entries {
        diff := now.Sub(entry.Timestamp)

        var group string
        if diff.Hours() < 24 {
            group = "今天"
        } else if diff.Hours() < 24*7 {
            group = "本周"
        } else if diff.Hours() < 24*30 {
            group = "本月"
        } else {
            group = "更早"
        }

        grouped[group] = append(grouped[group], entry)
    }

    return grouped
}
```

### 4. 删除单条历史

```go
// 删除指定的历史记录
func (s *SearchHistoryService) DeleteEntry(telegramID int64, index int) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    entries, exists := s.history[telegramID]
    if !exists || index < 0 || index >= len(entries) {
        return fmt.Errorf("invalid index")
    }

    // 删除指定索引
    s.history[telegramID] = append(entries[:index], entries[index+1:]...)

    // 异步保存
    go s.save()

    return nil
}
```

### 5. 搜索统计与可视化

```go
// 获取用户的搜索统计
func (s *SearchHistoryService) GetStats(telegramID int64) SearchStats {
    entries := s.GetHistory(telegramID)

    now := time.Now()
    stats := SearchStats{
        Total:   len(entries),
        Week:    0,
        Month:   0,
        Top5:    make([]string, 0, 5),
    }

    // 统计本周、本月
    for _, entry := range entries {
        diff := now.Sub(entry.Timestamp)

        if diff.Hours() < 24*7 {
            stats.Week++
        }
        if diff.Hours() < 24*30 {
            stats.Month++
        }
    }

    // 统计热门搜索（按搜索次数）
    searchCounts := make(map[string]int)
    for _, entry := range entries {
        searchCounts[entry.Query] += entry.Count
    }

    // 排序取前5
    type QueryCount struct {
        Query string
        Count int
    }

    var sorted []QueryCount
    for query, count := range searchCounts {
        sorted = append(sorted, QueryCount{query, count})
    }

    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].Count > sorted[j].Count
    })

    for i := 0; i < len(sorted) && i < 5; i++ {
        stats.Top5 = append(stats.Top5, sorted[i].Query)
    }

    return stats
}

type SearchStats struct {
    Total int
    Week  int
    Month int
    Top5  []string
}
```

---

## 💡 体验优化方案

### 1. 搜索建议（自动补全）

```go
// 当用户输入时，实时显示搜索建议
func (h *SearchHandler) HandleSearchInput(userID int64, chatID int64, input string) {
    suggestions := h.searchHistory.GetSuggestions(userID, input)

    if len(suggestions) > 0 {
        // 显示搜索建议
        msg := "💡 找到相关搜索：\n\n"
        for i, s := range suggestions {
            msg += fmt.Sprintf("%d. %s\n", i+1, s)
        }

        kb := services.NewKeyboardBuilder()
        for i, s := range suggestions {
            kb.AddButton(fmt.Sprintf("%d. %s", i+1, truncateString(s, 12)),
                fmt.Sprintf("search:query:%s", s))
            if (i+1)%2 == 0 {
                kb.NewRow()
            }
        }

        h.telegram.SendMessage(chatID, msg, "", kb.Build())
    }
}
```

### 2. 搜索标签

```go
// 为搜索添加标签（类型、心情等）
type SearchEntry struct {
    Query     string    `json:"query"`
    Timestamp time.Time `json:"timestamp"`
    Count     int       `json:"count"`
    Tags      []string  `json:"tags"` // 新增：电影、电视剧、科幻、动作等
    MediaID   int       `json:"media_id"` // 新增：关联的媒体ID
    MediaType string    `json:"media_type"` // 新增：movie/tv
}

// 按标签筛选历史
func (s *SearchHistoryService) GetHistoryByTag(telegramID int64, tag string) []SearchEntry {
    entries := s.GetHistory(telegramID)

    var filtered []SearchEntry
    for _, entry := range entries {
        for _, t := range entry.Tags {
            if t == tag {
                filtered = append(filtered, entry)
                break
            }
        }
    }

    return filtered
}
```

### 3. 编辑历史搜索

```go
// 允许用户编辑历史搜索词
func (h *SearchHandler) HandleEditHistory(ctx *callback.Context, index int) (*callback.Response, error) {
    entries := h.searchHistory.GetHistory(ctx.UserID)

    if index < 0 || index >= len(entries) {
        return &callback.Response{
            Text:        "❌ 无效的索引",
            CallbackMsg: "索引无效",
            ShowAlert:   true,
        }, nil
    }

    // 显示编辑界面
    msg := fmt.Sprintf("✏️ 编辑搜索词\n\n原搜索词：%s", entries[index].Query)
    msg += "\n\n请输入新的搜索词："

    // 使用会话存储编辑状态
    session := h.sessMgr.GetOrCreate(ctx.UserID)
    session.Set("editing_history_index", index)

    kb := services.NewKeyboardBuilder()
    kb.AddButton("❌ 取消", "start")

    return &callback.Response{
        Text:     msg,
        Edit:     true,
        Keyboard: convertKeyboard(kb.Build()),
    }, nil
}
```

---

## ⚡ 性能优化方案

### 1. 使用数据库存储

```go
// 使用 SQLite 替代 JSON
package services

import (
    "database/sql"
    _ "modernc.org/sqlite"
)

type SearchHistoryDB struct {
    db *sql.DB
}

func NewSearchHistoryDB(dbPath string) (*SearchHistoryDB, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, err
    }

    // 创建表
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS search_history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            query TEXT NOT NULL,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            count INTEGER DEFAULT 1,
            tags TEXT,
            media_id INTEGER,
            media_type TEXT
        );

        CREATE INDEX IF NOT EXISTS idx_user_id ON search_history(user_id);
        CREATE INDEX IF NOT EXISTS idx_timestamp ON search_history(timestamp);
        CREATE INDEX IF NOT EXISTS idx_query ON search_history(query);
    `)
    if err != nil {
        return nil, err
    }

    return &SearchHistoryDB{db: db}, nil
}

// 添加搜索
func (s *SearchHistoryDB) AddSearch(userID int64, query string) error {
    // 检查是否已存在
    var id int
    err := s.db.QueryRow(
        "SELECT id FROM search_history WHERE user_id = ? AND query = ? ORDER BY timestamp DESC LIMIT 1",
        userID, query,
    ).Scan(&id)

    if err == sql.ErrNoRows {
        // 新增
        _, err = s.db.Exec(
            "INSERT INTO search_history (user_id, query, timestamp, count) VALUES (?, ?, ?, 1)",
            userID, query, time.Now(),
        )
    } else if err == nil {
        // 更新
        _, err = s.db.Exec(
            "UPDATE search_history SET count = count + 1, timestamp = ? WHERE id = ?",
            time.Now(), id,
        )
    }

    return err
}

// 获取历史
func (s *SearchHistoryDB) GetHistory(userID int64, limit int) ([]SearchEntry, error) {
    rows, err := s.db.Query(
        "SELECT query, timestamp, count FROM search_history WHERE user_id = ? ORDER BY timestamp DESC LIMIT ?",
        userID, limit,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var entries []SearchEntry
    for rows.Next() {
        var entry SearchEntry
        if err := rows.Scan(&entry.Query, &entry.Timestamp, &entry.Count); err != nil {
            return nil, err
        }
        entries = append(entries, entry)
    }

    return entries, nil
}

// 获取热门搜索
func (s *SearchHistoryDB) GetPopularSearches(limit int) ([]PopularSearch, error) {
    rows, err := s.db.Query(`
        SELECT query, SUM(count) as total
        FROM search_history
        GROUP BY query
        ORDER BY total DESC
        LIMIT ?
    `, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var results []PopularSearch
    for rows.Next() {
        var result PopularSearch
        if err := rows.Scan(&result.Query, &result.Count); err != nil {
            return nil, err
        }
        results = append(results, result)
    }

    return results, nil
}
```

### 2. 添加缓存机制

```go
// 使用内存缓存减少数据库查询
type SearchHistoryCache struct {
    db       *SearchHistoryDB
    cache    map[int64][]SearchEntry
    cacheTTL time.Duration
    mu       sync.RWMutex
}

func (s *SearchHistoryCache) GetHistory(userID int64) []SearchEntry {
    s.mu.RLock()
    if cached, exists := s.cache[userID]; exists {
        s.mu.RUnlock()
        return cached
    }
    s.mu.RUnlock()

    // 从数据库加载
    entries, err := s.db.GetHistory(userID, 20)
    if err != nil {
        return nil
    }

    // 缓存结果
    s.mu.Lock()
    s.cache[userID] = entries
    s.mu.Unlock()

    // 定期清理缓存
    go func() {
        time.Sleep(s.cacheTTL)
        s.mu.Lock()
        delete(s.cache, userID)
        s.mu.Unlock()
    }()

    return entries
}
```

---

## 📊 实施优先级

| 优先级 | 优化项 | 复杂度 | 效果 |
|--------|---------|--------|------|
| P0 | UI 优化（暗黑霓虹风） | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| P0 | 搜索统计展示 | ⭐⭐ | ⭐⭐⭐⭐ |
| P1 | 按时间分组 | ⭐⭐ | ⭐⭐⭐ |
| P1 | 删除单条历史 | ⭐ | ⭐⭐⭐ |
| P2 | 热门搜索 | ⭐⭐⭐ | ⭐⭐⭐ |
| P2 | 搜索趋势 | ⭐⭐⭐ | ⭐⭐ |
| P2 | 搜索建议（自动补全） | ⭐⭐⭐ | ⭐⭐⭐ |
| P3 | 搜索标签 | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| P3 | 编辑历史搜索 | ⭐⭐ | ⭐⭐ |
| P3 | 数据库存储 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| P3 | 缓存机制 | ⭐⭐⭐ | ⭐⭐⭐ |

---

## 🚀 实施建议

### 阶段一：快速改进（1-2天）
```
1. UI 优化 - 使用暗黑霓虹风
2. 添加搜索统计展示
3. 按时间分组显示
4. 删除单条历史功能
```

### 阶段二：功能增强（3-5天）
```
1. 热门搜索功能
2. 搜索趋势展示
3. 搜索建议（自动补全）
4. 搜索标签系统
```

### 阶段三：性能优化（1周）
```
1. 数据库存储
2. 缓存机制
3. 批量操作优化
4. 索引优化
```

---

**文档版本**: v1.0
**更新时间**: 2026-03-08
