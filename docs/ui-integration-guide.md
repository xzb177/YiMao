# YiMao UI 模块集成指南

## 概述

已为 YiMao 创建了完整的强视觉 UI 模块，包含 5 种风格：

- **暗黑霓虹风** (StyleNeon) - 主风格
- **文艺胶片风** (StyleFilm) - 推荐模块
- **波普艺术风** (StylePop) - 菜单系统
- **极简卡片风** (StyleCard) - 请求列表
- **沉浸电影风** (StyleCinema) - 详情页

## 文件结构

```
internal/ui/
├── ui.go         # 主模块和接口定义
├── neon.go       # 暗黑霓虹风格实现
├── film.go       # 文艺胶片风格实现
├── pop.go        # 波普艺术风格实现
├── card.go       # 极简卡片风格实现
├── cinema.go     # 沉浸电影风格实现
└── keyboard.go   # 按钮构建器
```

## 使用方法

### 1. 导入模块

```go
import "emby-telegram-bot/internal/ui"
```

### 2. 基本使用

#### 构建主菜单（波普艺术风格）

```go
// 直接使用默认风格（波普艺术）
message := ui.BuildMenu("YiMao · 你的私人选片师", "今天想看什么？")
keyboard := ui.NewKeyboardBuilder(ui.StylePop).BuildMenuKeyboard()
```

#### 构建搜索结果（暗黑霓虹风格）

```go
// 使用暗黑霓虹风格
results := []services.SearchResult{...}
message := ui.BuildSearchResults("复仇者联盟", results, 1, 5)
keyboard := ui.NewKeyboardBuilder(ui.StyleNeon).BuildSearchKeyboard(results, 1, 5)
```

#### 构建推荐内容（文艺胶片风格）

```go
// 使用文艺胶片风格，带伤感文案
results := []services.SearchResult{...}
message := ui.BuildRecommendation("今日推荐 · 治愈系电影", results, "happy")
keyboard := ui.NewKeyboardBuilder(ui.StyleFilm).BuildSearchKeyboard(results, 1, 1)
```

#### 构建媒体详情（暗黑霓虹风格）

```go
result := &services.SearchResult{...}
message := ui.BuildMediaDetail(result)
keyboard := ui.NewKeyboardBuilder(ui.StyleNeon).BuildDetailKeyboard(true, true)
```

#### 构建请求列表（极简卡片风格）

```go
requests := []services.SubscribeItem{...}
message := ui.BuildRequestList(requests, 1, 3, 28)
keyboard := ui.NewKeyboardBuilder(ui.StyleCard).BuildPaginationKeyboard(1, 3)
```

### 3. 高级使用

#### 创建自定义构建器

```go
// 创建特定风格的构建器
builder := ui.NewBuilder(ui.StyleNeon)
message := builder.BuildSearchResults(query, results, page, total)
```

#### 构建不同场景的消息

```go
// 主菜单 - 波普艺术
menuMsg := ui.BuildMenu("YiMao", "私人选片师")
menuKb := ui.NewKeyboardBuilder(ui.StylePop).BuildMenuKeyboard()

// 搜索结果 - 暗黑霓虹
searchMsg := ui.BuildSearchResults(query, results, page, total)
searchKb := ui.NewKeyboardBuilder(ui.StyleNeon).BuildSearchKeyboard(results, page, totalPages)

// 推荐内容 - 文艺胶片
recMsg := ui.BuildRecommendation("精选推荐", results, "happy")
recKb := ui.NewKeyboardBuilder(ui.StyleFilm).BuildSearchKeyboard(results, 1, 1)

// 请求列表 - 极简卡片
reqMsg := ui.BuildRequestList(requests, page, totalPages, total)
reqKb := ui.NewKeyboardBuilder(ui.StyleCard).BuildPaginationKeyboard(page, totalPages)
```

## 集成到现有代码

### 修改 SearchHandler

在 `internal/handlers/search.go` 中：

```go
// 导入 UI 模块
import "emby-telegram-bot/internal/ui"

// 修改 sendSearchResults 方法
func (h *SearchHandler) sendSearchResults(userID int64, chatID int64, query string, results *services.SearchResponse) {
    // 构建消息（使用暗黑霓虹风格）
    message := ui.BuildSearchResults(query, results.Results, 1, results.Total)

    // 构建键盘
    kb := ui.NewKeyboardBuilder(ui.StyleNeon).BuildSearchKeyboard(results.Results, 1, results.Total)

    // 发送消息
    h.telegram.SendMessage(chatID, message, "", kb)
}
```

### 修改 MenuHandler

在 `internal/handlers/menu.go` 中：

```go
// 导入 UI 模块
import "emby-telegram-bot/internal/ui"

// 修改主菜单
func (h *MenuHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
    message := ui.BuildMenu("YiMao · 你的私人选片师", "今天想看什么？")
    keyboard := ui.NewKeyboardBuilder(ui.StylePop).BuildMenuKeyboard()

    return &callback.Response{
        Text:     message,
        Keyboard: keyboard,
    }, nil
}
```

### 修改 AI 推荐模块

在 `ai/recommendation.go` 中：

```go
// 导入 UI 模块
import "emby-telegram-bot/internal/ui"

// 修改推荐展示
func (r *RecommendationService) ShowRecommendations(chatID int64, mood string, results []services.SearchResult) {
    title := r.getRecommendationTitle(mood)
    message := ui.BuildRecommendation(title, results, mood)
    keyboard := ui.NewKeyboardBuilder(ui.StyleFilm).BuildSearchKeyboard(results, 1, 1)

    r.telegram.SendMessage(chatID, message, "", keyboard)
}
```

## 样式切换

可以根据用户偏好或不同场景动态切换样式：

```go
// 根据用户偏好选择样式
style := getUserPreferredStyle(userID) // 从用户配置获取

// 构建消息
message := ui.NewBuilder(style).BuildSearchResults(query, results, page, total)
keyboard := ui.NewKeyboardBuilder(style).BuildSearchKeyboard(results, page, total)
```

## 推荐的组合方案

根据用户需求，建议使用以下组合：

| 场景 | 风格 | 说明 |
|------|------|------|
| 主菜单 | StylePop（波普艺术） | 趣味性强，年轻潮流 |
| 搜索结果 | StyleNeon（暗黑霓虹） | 强视觉冲击，信息清晰 |
| 媒体详情 | StyleNeon（暗黑霓虹） | 主风格保持一致 |
| 推荐内容 | StyleFilm（文艺胶片） | 伤感文案，情感共鸣 |
| 请求列表 | StyleCard（极简卡片） | 信息密集，高效浏览 |

## 注意事项

1. **消息长度限制**：Telegram 消息有 4096 字符限制，UI 模块已自动处理文本截断
2. **按钮数量限制**：每行最多 5 个按钮，键盘已遵循此限制
3. **Markdown 格式**：当前使用纯文本格式，如需 Markdown 需要调整消息构建逻辑
4. **图片支持**：可以结合 `SendPhoto` 方法发送海报图片，增强视觉效果

## 扩展建议

### 添加用户个性化设置

```go
// 保存用户的 UI 风格偏好
func saveUserPreference(userID int64, style ui.UIStyle) {
    // 存储到数据库或配置文件
}

// 获取用户偏好
func getUserStyle(userID int64) ui.UIStyle {
    // 从数据库读取用户偏好
    return ui.StyleNeon // 默认值
}
```

### 添加主题切换功能

```go
// 用户可以通过命令切换主题
func (h *MenuHandler) HandleStyleChange(ctx *callback.Context, style ui.UIStyle) (*callback.Response, error) {
    saveUserPreference(ctx.UserID, style)

    return &callback.Response{
        Text:        "✅ 主题已切换",
        CallbackMsg: "切换成功",
        ShowAlert:   true,
    }, nil
}
```

## 测试

```go
// 测试消息构建
func TestUIBuilders() {
    // 测试暗黑霓虹风格
    neonBuilder := ui.NewBuilder(ui.StyleNeon)
    neonMsg := neonBuilder.BuildMenu("测试标题", "测试副标题")

    // 测试文艺胶片风格
    filmBuilder := ui.NewBuilder(ui.StyleFilm)
    filmMsg := filmBuilder.BuildRecommendation("测试推荐", []services.SearchResult{}, "happy")

    // 测试波普艺术风格
    popBuilder := ui.NewBuilder(ui.StylePop)
    popMsg := popBuilder.BuildSearchResults("测试", []services.SearchResult{}, 1, 5)
}
```

## 后续优化

1. **添加动画效果**：使用 Telegram 的编辑消息功能实现消息动画
2. **支持更多主题**：添加季节性主题（如春节、圣诞节等）
3. **智能推荐样式**：根据用户行为自动推荐最合适的样式
4. **A/B 测试**：收集用户对不同样式的反馈数据

---

**文档版本**: v1.0
**更新时间**: 2026-03-08
