# UI 优化实施指南

## 📋 概述

本文档说明如何使用新的 UI 构建器优化搜索结果页面和媒体详情页面。

---

## 📁 新增文件

### 1. 搜索结果构建器
**`internal/ui/search_results_builder.go`** (6,760 字节)

支持 4 种风格：
- ⚡ 暗黑霓虹风
- 🎞️ 文艺胶片风
- 🎨 波普艺术风
- 🎴 极简卡片风

### 2. 媒体详情构建器
**`internal/ui/media_detail_builder.go`** (8,151 字节)

支持 4 种风格：
- ⚡ 暗黑霓虹风
- 🎞️ 文艺胶片风
- 🎨 波普艺术风
- 🎴 极简卡片风（默认）

---

## 🔧 集成步骤

### 1. 优化搜索结果页面

#### 修改前（`internal/handlers/search.go`）

```go
func (h *SearchHandler) sendSearchResults(userID int64, chatID int64, query string, results *services.SearchResponse) {
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		// ... 构建按钮
	}
}
```

#### 修改后

```go
import (
	"emby-telegram-bot/internal/ui"
)

func (h *SearchHandler) sendSearchResults(userID int64, chatID int64, query string, results *services.SearchResponse) {
	// 使用新的搜索结果构建器（极简卡片风）
	builder := ui.NewSearchResultsBuilder(ui.StyleCard)

	// 构建消息
	text := builder.BuildSearchResultsMessage(query, results.Results, 1, len(results.Results))

	// 构建键盘
	kb := builder.BuildSearchKeyboard(results.Results, 1, 1)

	// 转换为 Telegram 格式
	telegramKeyboard := convertToTelegramKeyboard(kb)

	// 发送消息
	h.telegram.SendMessage(chatID, text, "", telegramKeyboard)
}

// 转换函数
func convertToTelegramKeyboard(kb *ui.SearchKeyboard) *types.TelegramInlineKeyboard {
	rows := make([][]types.TelegramInlineKeyboardButton, len(kb.Buttons))
	for i, row := range kb.Buttons {
		buttons := make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			buttons[j] = types.TelegramInlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
			}
		}
		rows[i] = buttons
	}
	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}
```

### 2. 优化媒体详情页面

#### 修改前（`internal/handlers/callback.go`）

```go
func (h *DetailHandler) buildDetailFromMediaInfo(info *services.MediaInfo, sess *session.Session, query string) *callback.Response {
	msg := services.NewMessageBuilder()

	// Title header
	msg.Bold(fmt.Sprintf("%s %s", typeIcon, info.Title)).Newline()
	msg.Newline()

	// Info section - Year, Rating, Type
	if info.Year > 0 {
		msg.Textf("📅 %d年  ", info.Year.Int())
	}
	if info.Rating > 0 {
		msg.Textf("⭐ %.1f分  ", info.Rating)
	}
	msg.Textf("🏷️ %s", typeLabel).Newline()
	// ... 更多代码
}
```

#### 修改后

```go
import (
	"emby-telegram-bot/internal/ui"
)

func (h *DetailHandler) buildDetailFromMediaInfo(info *services.MediaInfo, sess *session.Session, query string) *callback.Response {
	// 使用新的媒体详情构建器（极简卡片风）
	builder := ui.NewMediaDetailBuilder(ui.StyleCard)

	// 构建消息
	text := builder.BuildMediaDetailMessage(info)

	// 构建键盘
	hasSeasons := len(info.Seasons) > 0
	kb := builder.BuildMediaDetailKeyboard(info, hasSeasons, true)

	// 转换为 Telegram 格式
	telegramKeyboard := convertDetailKeyboard(kb)

	return &callback.Response{
		Text:     text,
		Edit:     false,
		Keyboard: telegramKeyboard,
	}
}

// 转换函数
func convertDetailKeyboard(kb *ui.DetailKeyboard) *types.TelegramInlineKeyboard {
	rows := make([][]types.TelegramInlineKeyboardButton, len(kb.Buttons))
	for i, row := range kb.Buttons {
		buttons := make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			buttons[j] = types.TelegramInlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
			}
		}
		rows[i] = buttons
	}
	return &types.TelegramInlineKeyboard{
		InlineKeyboard: rows,
	}
}
```

---

## 🎨 UI 效果对比

### 搜索结果页面

#### 修改前
```
🔍 搜索结果「复仇者联盟」

找到 6 条结果

1. 复仇者联盟 (2012) 🎬 电影 ⭐8.4
2. 复仇者联盟：终局之战 (2019) 🎬 电影 ⭐8.5
3. 复仇者联盟：无限战争 (2018) 🎬 电影 ⭐8.4
```

#### 修改后（极简卡片风）
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔮 搜索结果 · 复仇者联盟
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰

📊 找到 6 个结果

▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰

▸ 1. 复仇者联盟 (2012) 🎬 ⭐8.4
   超级英雄团队集结，共同对抗邪恶势力...

▸ 2. 复仇者联盟：终局之战 (2019) 🎬 ⭐8.5
   漫威十年布局的终极决战...

▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰

[1] [2] [3] [4]
[⬅️ 上一页] [PAGE 1/1] [下一页 ➡️]
[⬅️ 返回主菜单]
```

### 媒体详情页面

#### 修改前
```
🎬 复仇者联盟

📅 2012年  ⭐ 8.4分  🏷️ 电影

🎭 动作、科幻

📺 共 1 季
   • 第1季 (6集)

📖 剧情简介
超级英雄首次集结，共同对抗邪恶势力...

🆔 TMDB ID: 2995
```

#### 修改后（极简卡片风）
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🎬 复仇者联盟
   The Avengers
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📊 热度 85.5  ·  ⭐ 评分 8.4  ·  🎬 类型 电影

⏱️ 时长: 143 分钟

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📖 剧情简介

  超级英雄首次集结，共同对抗邪恶势力。当
  邪恶威胁降临，地球最强超级英雄首次联手！
  钢铁侠、雷神、美国队长、绿巨人、黑寡妇、
  鹰眼六位英雄首次集结，组成复仇者联盟，
  共同对抗洛基及其外星军队的入侵。

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🏷️ 动作 · 科幻 · 冒险

📅 发布: 2012-05-04

🆔 TMDB ID: 2995

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[✅ 立即求片] [📺 添加订阅]
[⬅️ 返回] [🏠 主菜单]
```

---

## 🔌 API 参考

### SearchResultsBuilder

```go
// 创建构建器
builder := ui.NewSearchResultsBuilder(ui.StyleCard)

// 构建消息
message := builder.BuildSearchResultsMessage(query, results, page, total)

// 构建键盘
keyboard := builder.BuildSearchKeyboard(results, page, totalPages)
```

### MediaDetailBuilder

```go
// 创建构建器
builder := ui.NewMediaDetailBuilder(ui.StyleCard)

// 构建消息
message := builder.BuildMediaDetailMessage(mediaInfo)

// 构建键盘
keyboard := builder.BuildMediaDetailKeyboard(mediaInfo, hasSeasons, hasRequests)
```

---

## ⚡ 性能考虑

### 字符限制
Telegram 消息限制为 4096 字符，已自动处理：
- 概要截断至 45 字符
- 多行文本自动格式化

### 键盘按钮
Telegram 限制：
- 每行最多 5 个按钮
- 回调数据最多 64 字节

已优化为：
- 每行 4 个按钮（数字选择）
- 回调数据精简格式

---

## 📝 注意事项

### 1. 类型转换

确保 `services.SearchResult` 和 `services.MediaInfo` 的类型字段正确：
- `Type` 应为 "movie" 或 "tv"
- 评分 `Rating` 为 float64
- 年份 `Year` 为 int

### 2. 图片处理

详情页可能发送海报图片：
```go
if info.Poster != "" {
    // 使用 SendPhoto 发送图片消息
    h.telegram.SendPhoto(chatID, info.Poster, text, keyboard)
} else {
    // 使用 SendMessage 发送文本消息
    h.telegram.SendMessage(chatID, text, "", keyboard)
}
```

### 3. 会话管理

确保保存搜索结果到会话：
```go
sess := h.sessMgr.GetOrCreate(userID)
searchItems := make([]session.SearchItem, len(results.Results))
// ... 填充 searchItems
sess.SetSearchResults(searchItems, 1, query)
```

---

## 🚀 测试

### 测试搜索结果
```bash
# 在 Telegram 中
/search 复仇者联盟
```

### 测试详情页
```bash
# 点击搜索结果中的任意一项
# 查看优化后的详情页
```

---

## 📚 相关文档

- [UI 优化方案](ui-optimization-plan.md)
- [UI 设计方案](ui-design-proposals.md)
- [UI 预览](ui-preview.html)

---

**文档版本**: v1.0
**更新时间**: 2026-03-08
