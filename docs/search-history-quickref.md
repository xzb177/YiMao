# YiMao 搜索历史优化 - 快速参考

## 📁 新增文件总览

```
YiMao/
├── internal/
│   ├── services/
│   │   ├── search_history_db.go     # SQLite 数据库服务
│   │   └── search_history_cache.go # 内存缓存服务
│   ├── ui/
│   │   └── history_builder.go      # 搜索历史 UI 构建器
│   └── handlers/
│       └── search_history.go       # 搜索历史 Handler
├── docs/
│   ├── search-history-optimization.md      # 优化方案
│   ├── search-history-implementation.md    # 实施指南
│   └── search-history-summary.md         # 工作总结
└── examples/
    └── search_history_test.go    # 功能测试
```

---

## 🎯 功能清单

### ✅ 已实现功能

| 功能 | 说明 | 文件 |
|------|------|------|
| SQLite 数据库存储 | 替代 JSON 文件 | `search_history_db.go` |
| 内存缓存机制 | TTL 过期 + 自动清理 | `search_history_cache.go` |
| 搜索历史展示 | 暗黑霓虹风格 | `history_builder.go` |
| 搜索统计 | 总次数/本周/本月/最常搜索 | `search_history_db.go` |
| 热门搜索 | 跨用户热门搜索 TOP10 | `search_history_db.go` |
| 搜索趋势 | 增长最快的搜索（3/7/30天） | `search_history_db.go` |
| 搜索建议 | 基于历史记录的自动补全 | `search_history_db.go` |
| 按时间分组 | 今天/本周/本月/更早 | `search_history_db.go` |
| 删除单条历史 | 支持删除单个搜索记录 | `search_history_db.go` |
| 搜索标签 | 支持为搜索添加标签 | `search_history_db.go` |

---

## 🚀 快速开始

### 1. 初始化服务

```go
import (
    "emby-telegram-bot/internal/services"
)

// 初始化数据库
searchHistoryDB, err := services.NewSearchHistoryDB("./data")
if err != nil {
    log.Fatal(err)
}

// 初始化缓存（TTL 5分钟）
searchHistory := services.NewSearchHistoryCache(searchHistoryDB, 5*time.Minute)
```

### 2. 基本使用

```go
// 添加搜索
searchHistory.AddSearch(userID, "复仇者联盟")

// 获取历史记录
history, err := searchHistory.GetHistory(userID, 20)

// 获取统计信息
stats, err := searchHistory.GetStats(userID)

// 获取热门搜索
popular, err := searchHistory.GetPopularSearches(10)

// 获取搜索趋势
trends, err := searchHistory.GetSearchTrends(7)

// 获取搜索建议
suggestions, err := searchHistory.GetSuggestions(userID, "复")

// 删除单条记录
err := searchHistory.DeleteEntry(userID, 2)

// 清空历史记录
err := searchHistory.ClearHistory(userID)
```

---

## 🎨 UI 快速参考

### 主菜单回调

| 回调数据 | 功能 |
|----------|------|
| `search_history_menu` | 显示搜索历史主菜单 |
| `search_stats` | 显示统计信息 |
| `search_popular` | 显示热门搜索 |
| `search_trends` | 显示搜索趋势 |
| `search_manage` | 显示管理界面 |
| `search_delete:{index}` | 删除单条记录 |
| `search_clear_all` | 清空所有历史记录 |

### UI 构建器

```go
import (
    "emby-telegram-bot/internal/ui"
)

// 创建构建器（暗黑霓虹风）
builder := ui.NewHistoryBuilder(ui.StyleNeon)

// 构建搜索历史界面
message := builder.BuildHistoryUI(userID, stats, groupedHistory, popular, trends)

// 构建热门搜索界面
message := builder.BuildPopularSearchesUI(popular, false)

// 构建搜索趋势界面
message := builder.BuildTrendsUI(trends, 7)

// 构建管理界面
message := builder.BuildManageHistoryUI(history)
```

---

## 📊 数据库速查

### 表：search_history

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键（自增） |
| user_id | INTEGER | 用户 ID |
| query | TEXT | 搜索词 |
| timestamp | DATETIME | 搜索时间 |
| count | INTEGER | 搜索次数 |
| tags | TEXT | 标签（逗号分隔） |
| media_id | INTEGER | 关联的媒体 ID |
| media_type | TEXT | 媒体类型（movie/tv） |

### 索引

```sql
idx_user_id        -- 按用户查询
idx_timestamp      -- 按时间排序
idx_query         -- 按搜索词查询
idx_user_timestamp -- 用户+时间组合
```

---

## ⚙️ 配置选项

### 缓存 TTL

```go
// 5 分钟缓存（推荐）
searchHistory := services.NewSearchHistoryCache(searchHistoryDB, 5*time.Minute)

// 10 分钟缓存
searchHistory := services.NewSearchHistoryCache(searchHistoryDB, 10*time.Minute)

// 1 分钟缓存（实时性要求高）
searchHistory := services.NewSearchHistoryCache(searchHistoryDB, 1*time.Minute)
```

### 历史记录限制

默认每个用户保留 20 条记录，可在数据库中修改。

---

## 📚 文档导航

| 文档 | 用途 |
|------|------|
| [优化方案](docs/search-history-optimization.md) | 了解设计思路和优化方向 |
| [实施指南](docs/search-history-implementation.md) | 集成到项目的详细步骤 |
| [工作总结](docs/search-history-summary.md) | 功能实现总结和对比 |

---

## 🔌 集成检查清单

- [ ] 在 main.go 中初始化 `SearchHistoryDB`
- [ ] 初始化 `SearchHistoryCache`
- [ ] 初始化 `SearchHistoryHandler`
- [ ] 注册所有回调处理器
- [ ] 在主菜单添加"搜索历史"按钮
- [ ] 更新 `SearchHandler` 使用新的缓存服务
- [ ] 添加数据库备份策略
- [ ] 测试所有功能

---

## 🐛 故障排除

### 问题：数据库创建失败

**原因**：data 目录不存在

**解决**：
```bash
mkdir -p data
```

### 问题：缓存未生效

**原因**：TTL 设置过长或未正确初始化

**解决**：
```go
// 确保 TTL 设置正确
cache := services.NewSearchHistoryCache(db, 5*time.Minute)

// 验证缓存命中
stats, _ := cache.GetStats(userID)
```

### 问题：查询速度慢

**原因**：未建立索引

**解决**：
```sql
-- 重建索引
DROP INDEX IF EXISTS idx_user_id;
CREATE INDEX idx_user_id ON search_history(user_id);
```

---

## 📞 技术支持

如有问题，请查看：

1. **实施指南** - 详细的集成步骤
2. **API 参考** - 所有可用的方法和参数
3. **测试代码** - 参考示例实现

---

**快速参考版本**: v1.0
**更新时间**: 2026-03-08
