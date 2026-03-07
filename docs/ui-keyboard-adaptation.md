# YiMao UI 键盘操作适配指南

> 将新 UI 风格的键盘操作适配到 YiMao 现有的回调系统

---

## 📋 项目回调系统概述

### 核心 Actions

```go
// 搜索相关
ActionSearch  = "search"   // 搜索操作
ActionDetail  = "detail"   // 详情页

// 请求相关
ActionRequest = "request"  // 求片/订阅
ActionPage    = "page"     // 分页导航
ActionBack    = "back"     // 返回上一步

// 功能相关
ActionStart   = "start"    // 主菜单
ActionAI      = "ai"       // AI 推荐
ActionMood    = "mood"     // 心情选择
ActionRandom  = "random"   // 随机推荐
ActionHot     = "hot"      // 热门内容
ActionNew     = "new"      // 最新内容

// 我的请求
ActionMyReqsPage = "myreqs_page" // 请求列表分页
ActionMyReqsItem = "myreqs_item" // 请求项目操作
```

### 回调数据格式

```go
// 标准格式: action:param1:value1:param2:value2
search:id:123:type:movie

// JSON 格式: {"action":"search","params":{"id":"123","type":"movie"}}
```

---

## 🎮 方案六：像素艺术风 - 键盘适配

### 主菜单键盘

```go
func (b *PixelBuilder) BuildMenuKeyboard() *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "🎮 搜索",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionSearch),
                },
                {
                    Text:         "🎲 随机",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRandom),
                },
            },
            {
                {
                    Text:         "💾 热门",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionHot),
                },
                {
                    Text:         "📋 我的请求",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
            {
                {
                    Text:         "👾 AI 推荐",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionAI),
                },
                {
                    Text:         "⚙️ 设置",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests), // 待实现
                },
            },
        },
    }
}
```

### 搜索结果键盘

```go
func (b *PixelBuilder) BuildSearchKeyboard(items []interface{}, page, totalPages int) *callback.Keyboard {
    var rows [][]callback.Button

    // 数字按钮（每行 4 个，像素风格）
    const buttonsPerRow = 4
    displayCount := len(items)
    if displayCount > 8 {
        displayCount = 8
    }

    for i := 0; i < displayCount; i++ {
        if i%buttonsPerRow == 0 {
            rows = append(rows, []callback.Button{})
        }

        // 获取项目 ID 和类型
        id, mediaType := getItemIDAndType(items[i])
        rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
            Text:         fmt.Sprintf("[%d]", i+1),
            CallbackData: callback.BuildSearchCallback(fmt.Sprintf("%d", id), mediaType),
        })
    }

    // 分页行（像素风格）
    paginationRow := []callback.Button{}
    if page > 1 {
        paginationRow = append(paginationRow, callback.Button{
            Text:         "◀ PREV",
            CallbackData: callback.BuildPageCallback(page-1, "search"),
        })
    }
    paginationRow = append(paginationRow, callback.Button{
        Text:         fmt.Sprintf("PAGE %d/%d", page, totalPages),
        CallbackData: callback.BuildSimpleCallback(callback.ActionStart), // 仅显示，无操作
    })
    if page < totalPages {
        paginationRow = append(paginationRow, callback.Button{
            Text:         "NEXT ▶",
            CallbackData: callback.BuildPageCallback(page+1, "search"),
        })
    }
    rows = append(rows, paginationRow)

    // 返回按钮
    rows = append(rows, []callback.Button{
        {
            Text:         "🎮 MENU",
            CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
        },
    })

    return &callback.Keyboard{InlineKeyboard: rows}
}
```

### 媒体详情键盘

```go
func (b *PixelBuilder) BuildDetailKeyboard(mediaID, mediaType string, season int) *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "💾 求片",
                    CallbackData: callback.BuildRequestCallback(mediaID, mediaType, 0),
                },
                {
                    Text:         "📺 订阅",
                    CallbackData: callback.BuildRequestCallback(mediaID, mediaType, 1),
                },
            },
            {
                {
                    Text:         "◀ BACK",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionBack),
                },
                {
                    Text:         "🎮 MENU",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
                },
            },
        },
    }
}
```

---

## 🌴 方案七：蒸汽波风 - 键盘适配

### 主菜单键盘

```go
func (b *VaporwaveBuilder) BuildMenuKeyboard() *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "🌴 RETRO",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionHot),
                },
                {
                    Text:         "🌺 AESTHETIC",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionAI),
                },
                {
                    Text:         "💀 VAPOR",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionMood),
                },
            },
            {
                {
                    Text:         "🏝️ MENU",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
                },
            },
            {
                {
                    Text:         "💾 SAVE",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
        },
    }
}
```

### 推荐键盘

```go
func (b *VaporwaveBuilder) BuildRecommendationKeyboard(items []interface{}, mood string) *callback.Keyboard {
    var rows [][]callback.Button

    // Wave 选择按钮
    rows = append(rows, []callback.Button{
        {
            Text:         "✦ WAVE 1",
            CallbackData: callback.BuildCallback(callback.ActionMood, map[string]string{"type": "happy"}),
        },
        {
            Text:         "✧ WAVE 2",
            CallbackData: callback.BuildCallback(callback.ActionMood, map[string]string{"type": "relax"}),
        },
        {
            Text:         "✦ WAVE 3",
            CallbackData: callback.BuildCallback(callback.ActionMood, map[string]string{"type": "calm"}),
        },
    })

    // 项目选择按钮（每行 3 个）
    const buttonsPerRow = 3
    displayCount := len(items)
    if displayCount > 6 {
        displayCount = 6
    }

    for i := 0; i < displayCount; i++ {
        if i%buttonsPerRow == 0 {
            rows = append(rows, []callback.Button{})
        }

        id, mediaType := getItemIDAndType(items[i])
        rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
            Text:         fmt.Sprintf("🌺 %d", i+1),
            CallbackData: callback.BuildDetailCallback(fmt.Sprintf("%d", id), mediaType),
        })
    }

    // 操作按钮
    rows = append(rows, []callback.Button{
        {
            Text:         "🔄 NEW WAVE",
            CallbackData: callback.BuildSimpleCallback(callback.ActionRandom),
        },
        {
            Text:         "🏝️ MENU",
            CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
        },
    })

    return &callback.Keyboard{InlineKeyboard: rows}
}
```

---

## 🏮 方案八：新中式风 - 键盘适配

### 主菜单键盘

```go
func (b *ChineseBuilder) BuildMenuKeyboard() *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "🎬 搜索影片",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionSearch),
                },
                {
                    Text:         "📺 热门剧集",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionHot),
                },
            },
            {
                {
                    Text:         "🏮 智能推荐",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionAI),
                },
                {
                    Text:         "📋 我的请求",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
            {
                {
                    Text:         "🎭 观影人格",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionMood),
                },
                {
                    Text:         "⚙️ 个人设置",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
        },
    }
}
```

### 详情页键盘

```go
func (b *ChineseBuilder) BuildDetailKeyboard(mediaID, mediaType string, season int) *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "❀ 求片",
                    CallbackData: callback.BuildRequestCallback(mediaID, mediaType, 0),
                },
                {
                    Text:         "❀ 收藏",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionStart), // 待实现收藏
                },
            },
            {
                {
                    Text:         "◀ 返回",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionBack),
                },
                {
                    Text:         "❀ 主菜单",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
                },
            },
        },
    }
}
```

---

## 💻 方案九：代码终端风 - 键盘适配

### 主菜单键盘

```go
func (b *TerminalBuilder) BuildMenuKeyboard() *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "[🔍] SEARCH",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionSearch),
                },
                {
                    Text:         "[📋] REQUESTS",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
            {
                {
                    Text:         "[🎮] STATS",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests), // 待实现统计
                },
                {
                    Text:         "[⚙️] CONFIG",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
        },
    }
}
```

### 搜索结果键盘

```go
func (b *TerminalBuilder) BuildSearchKeyboard(items []interface{}, page, totalPages int) *callback.Keyboard {
    var rows [][]callback.Button

    // 项目选择按钮
    displayCount := len(items)
    if displayCount > 8 {
        displayCount = 8
    }

    for i := 0; i < displayCount; i++ {
        id, mediaType := getItemIDAndType(items[i])
        rows = append(rows, []callback.Button{
            {
                Text:         fmt.Sprintf("[%d]", i+1),
                CallbackData: callback.BuildDetailCallback(fmt.Sprintf("%d", id), mediaType),
            },
        })
    }

    // 分页按钮
    paginationRow := []callback.Button{}
    if page > 1 {
        paginationRow = append(paginationRow, callback.Button{
            Text:         "[P] PREV",
            CallbackData: callback.BuildPageCallback(page-1, "search"),
        })
    }
    paginationRow = append(paginationRow, callback.Button{
        Text:         fmt.Sprintf("[PG %d/%d]", page, totalPages),
        CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
    })
    if page < totalPages {
        paginationRow = append(paginationRow, callback.Button{
            Text:         "[N] NEXT",
            CallbackData: callback.BuildPageCallback(page+1, "search"),
        })
    }
    rows = append(rows, paginationRow)

    return &callback.Keyboard{InlineKeyboard: rows}
}
```

---

## 🎨 方案十：街头涂鸦风 - 键盘适配

### 主菜单键盘

```go
func (b *GraffitiBuilder) BuildMenuKeyboard() *callback.Keyboard {
    return &callback.Keyboard{
        InlineKeyboard: [][]callback.Button{
            {
                {
                    Text:         "🔥 SEARCH",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionSearch),
                },
                {
                    Text:         "⚡ RANDOM",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRandom),
                },
                {
                    Text:         "🎯 TREND",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionHot),
                },
            },
            {
                {
                    Text:         "📋 MY LIST",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
                {
                    Text:         "🎭 MOOD",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionMood),
                },
                {
                    Text:         "⚙️ SET",
                    CallbackData: callback.BuildSimpleCallback(callback.ActionRequests),
                },
            },
        },
    }
}
```

### 推荐键盘

```go
func (b *GraffitiBuilder) BuildRecommendationKeyboard(items []interface{}) *callback.Keyboard {
    var rows [][]callback.Button

    // 项目选择按钮（每行 2 个，涂鸦风格）
    const buttonsPerRow = 2
    displayCount := len(items)
    if displayCount > 6 {
        displayCount = 6
    }

    for i := 0; i < displayCount; i++ {
        if i%buttonsPerRow == 0 {
            rows = append(rows, []callback.Button{})
        }

        id, mediaType := getItemIDAndType(items[i])
        rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
            Text:         fmt.Sprintf("💥 %d", i+1),
            CallbackData: callback.BuildDetailCallback(fmt.Sprintf("%d", id), mediaType),
        })
    }

    // 操作按钮
    rows = append(rows, []callback.Button{
        {
            Text:         "🔥 HOT",
            CallbackData: callback.BuildSimpleCallback(callback.ActionHot),
        },
        {
            Text:         "🎲 RANDOM",
            CallbackData: callback.BuildSimpleCallback(callback.ActionRandom),
        },
    })

    return &callback.Keyboard{InlineKeyboard: rows}
}
```

---

## 🔧 通用适配函数

### 获取项目 ID 和类型

```go
import (
    "emby-telegram-bot/internal/services"
)

func getItemIDAndType(item interface{}) (int, string) {
    switch v := item.(type) {
    case *services.SearchResult:
        return v.ID, v.Type
    case services.SearchResult:
        return v.ID, v.Type
    case session.SearchItem:
        id, _ := strconv.Atoi(v.ID)
        return id, v.Type
    default:
        return 0, "movie"
    }
}
```

### 构建通用分页键盘

```go
func buildPaginationKeyboard(page, totalPages int, source string) []callback.Button {
    row := []callback.Button{}

    if page > 1 {
        row = append(row, callback.Button{
            Text:         "⬅️ 上一页",
            CallbackData: callback.BuildPageCallback(page-1, source),
        })
    }

    row = append(row, callback.Button{
        Text:         fmt.Sprintf("%d/%d", page, totalPages),
        CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
    })

    if page < totalPages {
        row = append(row, callback.Button{
            Text:         "下一页 ➡️",
            CallbackData: callback.BuildPageCallback(page+1, source),
        })
    }

    return row
}
```

### 构建我的请求键盘

```go
func buildMyRequestsKeyboard(requests []interface{}, page, totalPages int) *callback.Keyboard {
    var rows [][]callback.Button

    // 请求项目按钮（每行 5 个）
    const buttonsPerRow = 5
    displayCount := len(requests)
    if displayCount > 10 {
        displayCount = 10
    }

    for i := 0; i < displayCount; i++ {
        if i%buttonsPerRow == 0 {
            rows = append(rows, []callback.Button{})
        }

        req := requests[i].(*services.SubscribeItem)
        rows[len(rows)-1] = append(rows[len(rows)-1], callback.Button{
            Text:         fmt.Sprintf("[%d]", i+1),
            CallbackData: callback.BuildCallback(callback.ActionMyReqsItem, map[string]string{
                "action": "info",
                "id":     fmt.Sprintf("%d", req.ID),
                "page":   fmt.Sprintf("%d", page),
            }),
        })
    }

    // 分页按钮
    rows = append(rows, buildPaginationKeyboard(page, totalPages, "myreqs"))

    // 刷新和返回按钮
    rows = append(rows, []callback.Button{
        {
            Text:         "🔄 刷新",
            CallbackData: callback.BuildPageCallback(page, "myreqs"),
        },
        {
            Text:         "⬅️ 返回",
            CallbackData: callback.BuildSimpleCallback(callback.ActionStart),
        },
    })

    return &callback.Keyboard{InlineKeyboard: rows}
}
```

---

## 📊 键盘适配对比表

| 方案 | 按钮风格 | 按钮数量/行 | 特殊处理 |
|------|---------|-------------|---------|
| 像素艺术 | `◀ PREV`, `NEXT ▶` | 4 个 | 像素图标 |
| 蒸汽波 | `✦ WAVE 1` | 2-3 个 | 浪漫符号 |
| 新中式 | `❀ 求片`, `◀ 返回` | 2 个 | 中式符号 |
| 代码终端 | `[P] PREV`, `[N] NEXT` | 1-2 个 | 方括号风格 |
| 街头涂鸦 | `💥 1`, `🔥 HOT` | 3 个 | 爆炸符号 |

---

## 🚀 集成步骤

### 1. 创建新 UI 构建器

```go
// internal/ui/pixel.go
type PixelBuilder struct{}

func (b *PixelBuilder) BuildMenuKeyboard() *callback.Keyboard {
    // 实现像素艺术风主菜单键盘
}

func (b *PixelBuilder) BuildSearchKeyboard(items []interface{}, page, totalPages int) *callback.Keyboard {
    // 实现像素艺术风搜索键盘
}
```

### 2. 在 handlers 中使用

```go
// internal/handlers/menu.go
import "emby-telegram-bot/internal/ui"

type MenuHandler struct {
    uiStyle ui.UIStyle
}

func (h *MenuHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
    builder := ui.NewBuilder(h.uiStyle)
    message := builder.BuildMenu("YiMao", "私人选片师")
    keyboard := builder.BuildMenuKeyboard()

    return &callback.Response{
        Text:     message,
        Keyboard: convertKeyboard(keyboard),
    }, nil
}
```

### 3. 支持用户自定义风格

```go
// 获取用户偏好的 UI 风格
func getUserUIStyle(userID int64) ui.UIStyle {
    // 从数据库或配置文件读取用户偏好
    return ui.StylePop // 默认值
}

// 在处理器中使用
style := getUserUIStyle(ctx.UserID)
builder := ui.NewBuilder(style)
keyboard := builder.BuildMenuKeyboard()
```

---

## ⚠️ 注意事项

1. **Callback Data 长度限制**：Telegram 限制为 64 字节，使用 JSON 格式可能超出
2. **按钮数量限制**：每行最多 5 个按钮
3. **Action 白名单**：所有 actions 必须在 `validActions` 中注册
4. **参数格式**：使用 `action:param1:value1:param2:value2` 格式以节省空间
5. **错误处理**：确保所有回调都有对应的 handler 处理

---

**文档版本**: v1.0
**更新时间**: 2026-03-08
