# 反馈功能替换指南

## 概述

本文档说明如何用新的反馈功能替换旧的 JSON 存储版本。

**替换方式**：直接替换，移除旧代码，使用新的 SQLite 存储。

---

## 📋 替换步骤

### 步骤 1：备份数据

```bash
# 备份旧的 JSON 文件
cp data/feedback.json data/feedback.json.backup

# 检查备份
ls -lh data/feedback.json.backup
```

### 步骤 2：运行数据迁移

```bash
# 进入项目目录
cd /path/to/YiMao

# 运行迁移脚本
go run scripts/migrate_feedback.go

# 或指定数据目录
go run scripts/migrate_feedback.go /path/to/data
```

迁移示例输出：
```
========================================
反馈数据迁移工具
========================================
JSON 文件: ./data/feedback.json
数据库文件: ./data/feedback.db

📖 读取 JSON 文件...
🔍 解析 JSON 数据...
✓ 找到 42 个反馈

🗄️  初始化 SQLite 数据库...
✓ 数据库初始化成功

📦 开始迁移数据...

  已迁移 10 / 42
  已迁移 20 / 42
  已迁移 30 / 42
  已迁移 40 / 42

========================================
迁移完成
========================================
✓ 成功: 42
✗ 失败: 0
📊 总数: 42

📈 新数据库统计：
• 总反馈数: 42
• 待处理: 15
• 已解决: 20

✓ JSON 文件已备份到: ./data/feedback.json.backup.20260308_120000

✅ 迁移成功完成！

📝 下一步：
1. 检查迁移结果
2. 如果一切正常，可以删除备份文件
3. 更新配置文件，启用新的反馈功能
```

### 步骤 3：修改 main.go

#### 修改前（旧代码）
```go
import (
    "emby-telegram-bot/internal/services"
    "emby-telegram-bot/internal/handlers"
)

// 初始化旧的 IssueService
issueService := services.NewIssueService("./data")

// 创建旧的 FeedbackHandler
feedbackHandler := handlers.NewFeedbackHandler(
    sessionManager,
    telegram,
    issueService,
    adminService,
)
```

#### 修改后（新代码）
```go
import (
    "emby-telegram-bot/internal/services"
    "emby-telegram-bot/internal/handlers"
)

// 初始化新的 FeedbackDB
feedbackDB, err := services.NewFeedbackDB("./data")
if err != nil {
    log.Fatalf("Failed to init feedback DB: %v", err)
}

// 创建新的 FeedbackHandlerV2
feedbackHandler := handlers.NewFeedbackHandlerV2(
    sessionManager,
    telegram,
    adminService,
    feedbackDB,
    notificationService,
)
```

### 步骤 4：更新回调注册

#### 修改前（旧代码）
```go
registry.Register("feedback", feedbackHandler)
registry.Register("feedback_list", feedbackHandler)
registry.Register("feedback_detail", feedbackHandler)
registry.Register("feedback_reply", feedbackHandler)
```

#### 修改后（新代码）
```go
registry.Register("feedback", feedbackHandler)
registry.Register("feedback_type", feedbackHandler)
registry.Register("feedback_template", feedbackHandler)
registry.HandleText("feedback", feedbackHandler)
registry.Register("feedback_list", feedbackHandler)
registry.Register("my_feedback", feedbackHandler)
registry.Register("feedback_detail", feedbackHandler)
registry.Register("feedback_reply", feedbackHandler)
registry.Register("feedback_stats", feedbackHandler)
registry.Register("feedback_export_csv", feedbackHandler)
registry.Register("feedback_export_excel", feedbackHandler)
```

### 步骤 5：添加文本处理（可选）

```go
// 在主循环中添加文本处理
// 处理用户输入的描述
if feedbackV2, ok := feedbackHandler.(*handlers.FeedbackHandlerV2); ok {
    if feedbackV2.IsInFeedbackProcess(update.Message.From.ID) {
        err := feedbackV2.HandleFeedbackText(
            update.Message.From.ID,
            update.Message.Chat.ID,
            update.Message.Text,
        )
        if err != nil {
            log.Printf("Failed to handle feedback text: %v", err)
        }
        continue
    }
}
```

### 步骤 6：更新命令处理

```go
// 修改 /feedback 命令
cmdHandlers["/feedback"] = func(update tgbotapi.Update) {
    ctx := &callback.Context{
        UserID: update.Message.From.ID,
        ChatID: update.Message.Chat.ID,
        Callback: &callback.Callback{
            Action: "feedback",
            Params: map[string]string{},
        },
    }
    feedbackHandler.Handle(ctx)
}

// 添加 /my_feedback 命令
cmdHandlers["/myfeedback"] = func(update tgbotapi.Update) {
    ctx := &callback.Context{
        UserID: update.Message.From.ID,
        ChatID: update.Message.Chat.ID,
        Callback: &callback.Callback{
            Action: "feedback_list",
            Params: map[string]string{},
        },
    }
    feedbackHandler.Handle(ctx)
}
```

### 步骤 7：验证功能

```bash
# 启动 bot
./bot

# 测试功能
1. 点击「问题反馈」
2. 选择问题类型
3. 选择描述模板
4. 输入描述
5. 提交反馈
6. 查看我的反馈
7. 管理员查看统计
```

---

## 🔄 回滚方案

如果新功能有问题，可以回滚到旧版本：

### 方式一：恢复 JSON 数据

```bash
# 停止 bot

# 删除新数据库
rm data/feedback.db

# 恢复 JSON 数据
cp data/feedback.json.backup data/feedback.json

# 恢复旧代码（使用 git）
git checkout HEAD~1 internal/handlers/feedback.go
git checkout HEAD~1 cmd/bot/main.go

# 重启 bot
./bot
```

### 方式二：使用 git 回滚

```bash
# 查看提交历史
git log --oneline

# 回滚到上一个版本
git reset --hard <commit-id>

# 重新启动
./bot
```

---

## ✅ 验证清单

替换完成后，请验证以下功能：

- [ ] 反馈菜单正常显示
- [ ] 可以选择问题类型
- [ ] 可以选择描述模板
- [ ] 可以输入问题描述
- [ ] 可以查看我的反馈
- [ ] 可以查看反馈详情
- [ ] 管理员可以回复
- [ ] 管理员可以查看统计
- [ ] 旧数据已迁移
- [ ] 新反馈可以正常创建

---

## 📊 功能对比

| 功能 | 旧版本 | 新版本 |
|------|--------|--------|
| 存储方式 | JSON | SQLite |
| 问题描述 | 手动 | 模板/手动 |
| 图片上传 | ❌ | ✅ 1-3张 |
| 重复检测 | ❌ | ✅ |
| 统计分析 | ❌ | ✅ |
| 数据导出 | ❌ | ✅ CSV/Excel |
| 智能优先级 | ❌ | ✅ |
| 模板系统 | ❌ | ✅ 18个模板 |

---

## 🆘 常见问题

### Q1: 迁移失败怎么办？

**A**: 检查 JSON 文件格式是否正确，确保没有语法错误。

```bash
# 验证 JSON 格式
python3 -m json.tool data/feedback.json > /dev/null
```

### Q2: 新功能无法创建反馈？

**A**: 检查数据库文件权限和目录访问权限。

```bash
# 检查数据库文件
ls -lh data/feedback.db

# 检查目录权限
ls -ld data/
```

### Q3: 旧数据丢失？

**A**: 迁移前会自动备份 JSON 文件到 `data/feedback.json.backup.时间戳`。

```bash
# 查找备份文件
ls -lh data/feedback.json.backup.*
```

### Q4: 如何删除旧代码？

**A**: 确认新功能正常后，可以删除旧文件。

```bash
# 删除旧的 handler（不推荐，可以保留作为备份）
# rm internal/handlers/feedback.go

# 删除旧的 JSON 数据（已备份）
# rm data/feedback.json.backup.*
```

---

## 📝 注意事项

1. **备份数据**：迁移前务必备份 JSON 文件
2. **测试验证**：在生产环境部署前，先在测试环境验证
3. **逐步迁移**：如果有大量用户，建议分批通知
4. **日志监控**：迁移后密切监控日志，及时发现问题

---

## 🎉 完成

替换完成后，新反馈功能将提供：

- ✅ 更快的查询性能（SQLite）
- ✅ 更丰富的功能（模板、图片、统计、导出）
- ✅ 更智能的处理（重复检测、自动优先级）
- ✅ 更好的用户体验

---

**文档版本**: v1.0
**更新时间**: 2026-03-08
