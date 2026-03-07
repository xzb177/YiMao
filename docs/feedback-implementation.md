# 反馈功能实施文档

## 📋 概述

本文档说明如何使用优化后的反馈功能。

---

## 🎯 实现的功能

### 阶段一：快速改进

1. **问题描述模板** ✅
   - 6 种问题类型
   - 每种类型 3-4 个模板
   - 动态字段验证

2. **问题截图上传** ✅
   - 支持上传 1-3 张截图
   - 图片 URL 存储
   - 缩略图生成

3. **改进反馈通知** ✅
   - 用户收到回复时通知
   - 管理员通知优化
   - 状态变更通知

### 阶段二：功能增强

4. **数据库存储** ✅
   - SQLite 替代 JSON
   - 支持复杂查询
   - 索引优化

5. **问题分类和标签** ✅
   - 标签系统
   - 自动标签推荐
   - 多维度分类

6. **重复反馈检测** ✅
   - 文本相似度算法
   - 相似反馈提示
   - 智能合并

### 阶段三：高级功能

7. **反馈统计** ✅
   - 问题分布统计
   - 趋势分析
   - 平均解决时间

8. **智能优先级调整** ✅
   - 自动优先级建议
   - 规则引擎
   - 手动调整

9. **导出功能** ✅
   - CSV 导出
   - Excel 导出
   - 数据分析

---

## 📁 新增文件

| 文件 | 大小 | 说明 |
|------|------|------|
| `internal/services/feedback_db.go` | 19,850 字节 | 反馈数据库服务 |
| `internal/services/feedback_templates.go` | 14,659 字节 | 反馈模板服务 |
| `internal/services/feedback_similar.go` | 7,065 字节 | 相似度检测服务 |
| `internal/handlers/feedback_enhanced.go` | 13,486 字节 | 增强版反馈处理器 |

---

## 🔌 集成方式

### 1. 在 main.go 中初始化服务

```go
import (
    "emby-telegram-bot/internal/services"
    "emby-telegram-bot/internal/handlers"
)

// 初始化反馈数据库
feedbackDB, err := services.NewFeedbackDB("./data")

// 初始化模板服务
templateService := services.NewFeedbackTemplateService()

// 初始化相似度检测
similarityChecker := services.NewSimilarityChecker(0.5)

// 创建反馈处理器
feedbackHandler := handlers.NewFeedbackHandler(
    sessionManager,
    telegramService,
    feedbackDB,
    notificationService,
)

// 注册回调
registry.Register("feedback_menu", feedbackHandler)
registry.Register("feedback_type", feedbackHandler)
registry.Register("feedback_template", feedbackHandler)
registry.Register("feedback_submit", feedbackHandler)
registry.Register("feedback_list", feedbackHandler)
registry.Register("feedback_detail", feedbackHandler)
registry.Register("feedback_reply", feedbackHandler)
registry.Register("feedback_stats", feedbackHandler)
```

### 2. 在主菜单添加按钮

```go
kb.AddButton("🐛 问题反馈", "feedback_menu")
```

---

## 📊 数据库结构

### feedbacks 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键 |
| user_id | INTEGER | 用户 ID |
| user_name | TEXT | 用户名 |
| title | TEXT | 问题标题 |
| description | TEXT | 问题描述 |
| issue_type | TEXT | 问题类型 |
| priority | TEXT | 优先级 |
| status | TEXT | 状态 |
| media_id | TEXT | 关联媒体 ID |
| media_title | TEXT | 关联媒体标题 |
| tmdb_id | INTEGER | TMDB ID |
| template_used | TEXT | 使用的模板 ID |
| created_at | INTEGER | 创建时间 |
| updated_at | INTEGER | 更新时间 |

### feedback_replies 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键 |
| feedback_id | INTEGER | 反馈 ID |
| author_id | INTEGER | 作者 ID |
| author_name | TEXT | 作者名 |
| content | TEXT | 内容 |
| created_at | INTEGER | 创建时间 |

### feedback_tags 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键 |
| feedback_id | INTEGER | 反馈 ID |
| tag | TEXT | 标签 |

### feedback_images 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键 |
| feedback_id | INTEGER | 反馈 ID |
| image_url | TEXT | 图片 URL |
| created_at | INTEGER | 创建时间 |

---

## 🎨 使用流程

### 用户端

1. 点击「问题反馈」按钮
2. 选择问题类型（画质/音频/字幕/搜索/播放/其他）
3. 选择问题描述模板（或手动描述）
4. 填写字段信息
5. 上传截图（可选）
6. 提交反馈
7. 等待处理通知

### 管理员端

1. 查看待处理反馈列表
2. 查看反馈详情
3. 回复反馈
4. 更新状态
5. 查看统计数据

---

## 📈 统计功能

### 问题分布

```
问题类型分布
• 画质问题：35%
• 音频问题：20%
• 字幕问题：15%
• 搜索问题：10%
• 播放问题：15%
• 其他问题：5%
```

### 解决时间

```
平均解决时间
• 画质问题：3.2h
• 音频问题：2.1h
• 字幕问题：1.5h
• 搜索问题：4.5h
• 播放问题：3.8h
• 其他问题：2.3h
```

### 趋势分析

```
最近 7 天趋势
• 新增：15 个
• 已解决：12 个
• 待处理：8 个
• 已关闭：7 个
```

---

## 🔧 配置选项

### 相似度阈值

```go
// 0.5 表示 50% 相似度
similarityChecker := services.NewSimilarityChecker(0.5)
```

### 模板管理

```go
// 获取所有模板
templates := templateService.GetAllTemplates()

// 获取特定类型的模板
templates := templateService.GetTemplatesByType("video_quality")

// 获取单个模板
template, exists := templateService.GetTemplate("video_quality_1")
```

---

## 📤 导出功能

### CSV 导出

```go
feedbacks, _ := feedbackDB.GetAllFeedbacks()
csvData, _ := feedbackDB.ExportToCSV(feedbacks)
```

### Excel 导出

```go
feedbacks, _ := feedbackDB.GetAllFeedbacks()
excelData, _ := feedbackDB.ExportToExcel(feedbacks)
```

---

## ⚡ 性能优化

1. **数据库索引**
   - user_id
   - issue_type
   - status
   - created_at

2. **缓存机制**
   - 热门模板缓存
   - 反馈列表缓存

3. **批量操作**
   - 批量查询
   - 批量更新

---

## 🔒 安全性

1. **输入验证**
   - 字段长度限制
   - 必填字段检查
   - 格式验证

2. **SQL 注入防护**
   - 参数化查询
   - 预编译语句

3. **XSS 防护**
   - HTML 转义
   - Markdown 过滤

---

## 📝 注意事项

1. **数据库迁移**
   - 首次使用自动创建表
   - 如有旧数据需手动导入

2. **图片存储**
   - 建议使用对象存储
   - 支持本地存储

3. **通知服务**
   - 需要配置通知服务
   - 可自定义通知模板

---

## 🚀 下一步

- [ ] 添加单元测试
- [ ] 集成到主程序
- [ ] 性能测试
- [ ] 用户测试

---

**文档版本**: v1.0
**更新时间**: 2026-03-08
