# Rich Message Package

This package provides Rich Message support for Telegram Bot API 10.1 in the YiMao project.

## Features

- **Media Info Card**: Display media information with tables and collapsible sections
- **Subscription Dashboard**: Show subscription status with progress bars
- **Custom Rich Messages**: Build custom rich messages with headings, tables, and details

## Usage

### Basic Usage

```go
package main

import (
    "github.com/xzb177/yimao/internal/richmessage"
)

func main() {
    // Create a sender
    sender := richmessage.NewRichMessageSender("BOT_TOKEN", 123456789)
    
    // Send media info card
    info := richmessage.MediaInfo{
        Title:    "流浪地球3",
        Year:     2026,
        Rating:   8.5,
        Genres:   []string{"科幻", "冒险"},
        Overview: "太阳即将毁灭...",
    }
    sender.SendMediaInfoCard(info)
}
```

### Custom Rich Message

```go
builder := richmessage.NewBuilder()
builder.Heading("自定义标题", 2)
builder.Paragraph("这是一段文本")
builder.Table([]string{"列1", "列2"}, [][]string{{"A", "B"}})
builder.Details("点击展开", "折叠内容", false)  // false = closed by default

msg := builder.Build()
richmessage.SendRichMessage("BOT_TOKEN", 123456789, msg)
```

## API Reference

### Builder Methods

- `Heading(text string, level int)` - Add a heading (level 1-6, clamped)
- `Paragraph(text string)` - Add a paragraph
- `BoldParagraph(text string)` - Add a bold paragraph
- `Table(headers []string, rows [][]string)` - Add a table (empty headers = no-op)
- `Details(summary string, content string, isOpen bool)` - Add a collapsible section
  - `isOpen = false` - Closed by default (user clicks to expand)
  - `isOpen = true` - Open by default (user clicks to collapse)
  - Note: summary and content are HTML-escaped to prevent XSS
- `Divider()` - Add a divider
- `Reset()` - Reset the builder for reuse
- `Build()` - Build the rich message
- `ToJSON()` - Build and convert to JSON

### Pre-built Cards

- `BuildMediaInfoCard(info MediaInfo)` - Build a media info card
- `BuildSubscriptionDashboard(subs []SubscriptionStatus, todayAdded, weekDownload int)` - Build a subscription dashboard

## Rich Message Syntax

### Markdown Format

```markdown
## Heading 2

| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |

<details><summary>Click to expand</summary>

Content here

</details>

<details open><summary>Expanded by default</summary>

Content here

</details>
```

### Supported Features

- **Headings**: `# H1`, `## H2`, `### H3`, etc.
- **Tables**: `| H1 | H2 |\n|---|---|\n| C1 | C2 |`
- **Collapsible Sections**: `<details><summary>Title</summary>Content</details>`
- **Bold**: `**text**`
- **Italic**: `*text*`
- **Strikethrough**: `~~text~~`
- **Code**: `` `code` ``
- **Links**: `[text](url)`
- **Lists**: `- item`, `1. item`
- **Task Lists**: `- [ ] task`, `- [x] completed`
- **Blockquotes**: `> quote`
- **Math**: `$formula$`, `$$formula$$`
- **Media**: `![caption](url)`

## Validation

The package includes the following validations:

- **Heading level**: Clamped to 1-6 range
- **Empty headers**: Table with empty headers is a no-op
- **Empty title**: Media info card with empty title shows "未知影视"
- **Empty subs**: Subscription dashboard with empty subs shows "暂无订阅"
- **HTML escaping**: Details summary and content are HTML-escaped
- **Send validation**: Bot token, chat ID, and markdown are validated before sending

## Testing

Run tests:
```bash
go test ./internal/richmessage/...
```

## Notes

- Rich Messages require Telegram Bot API 10.1 or later
- Tables are rendered with borders and striped rows
- Collapsible sections use `<details>` HTML tags
- Progress bars use Unicode block characters (█░)
- Maximum 32768 UTF-8 characters per message
- Maximum 500 blocks per message
