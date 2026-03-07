# 反馈功能整合指南

## 📋 概述

本文档说明如何将新的反馈功能与现有反馈功能整合，使用适配器模式实现平滑过渡。

---

## 🎯 整合方案

### 核心思路

使用 **适配器模式** 连接新旧功能，通过配置开关动态选择使用哪种存储方式。

### 架构设计

```
┌─────────────────────────────────────┐
│   FeedbackHandler (统一接口）       │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│   FeedbackAdapter (适配器）         │
├─────────────────────────────────────┤
│  • useEnhanced (配置开关）         │
│  • issueService (旧：JSON）        │
│  • feedbackDB (新：SQLite）        │
│  • templateService (模板）          │
│  • similarityChecker (相似度）     │
└─────────────────────────────────────┘
         │                  │
    useEnhanced        !useEnhanced
         │                  │
         ▼                  ▼
┌─────────────┐    ┌─────────────┐
│  FeedbackDB │    │ IssueService│
│  (SQLite）  │    │   (JSON）   │
└─────────────┘    └─────────────┘
```

---

## 🚀 快速开始

### 1. 修改 main.go

```go
import (
    "emby-telegram-bot/internal/services"
    "emby-telegram-bot/internal/handlers"
)

// 初始化旧的 IssueService
issueService := services.NewIssueService("./data")

// 初始化新的 FeedbackDB
feedbackDB, err := services.NewFeedbackDB("./data")
if err != nil {
    log.Fatalf("Failed to initialize feedback DB: %v", err)
}

// 创建适配器
feedbackAdapter := services.NewFeedbackAdapter(
    issueService,
    feedbackDB,
    true, // useEnhanced - 从配置读取
)

// 如果需要，可以从环境变量读取
useEnhanced := os.Getenv("FEEDBACK_ENHANCED") == "true"
feedbackAdapter.SetUseEnhanced(useEnhanced)
```

### 2. 修改 FeedbackHandler

```go
type FeedbackHandler struct {
    sessMgr          *session.SessionManager
    telegram         *services.TelegramClient
    adminService     *services.AdminService
    feedbackAdapter  *services.FeedbackAdapter
}

func NewFeedbackHandler(
    sessMgr *session.SessionManager,
    telegram *services.TelegramClient,
    adminService *services.AdminService,
    feedbackAdapter *services.FeedbackAdapter,
) *FeedbackHandler {
    return &FeedbackHandler{
        sessMgr:         sessMgr,
        telegram:        telegram,
        adminService:    adminService,
        feedbackAdapter: feedbackAdapter,
    }
}
```

### 3. 添加环境变量

在 `.env` 文件中添加：

```env
# 反馈功能增强模式（true/false）
FEEDBACK_ENHANCED=true
```

---

## 📊 功能对比

| 配置 | 存储方式 | 模板 | 图片 | 重复检测 | 统计 | 导出 |
|------|----------|------|------|----------|------|------|
| `FEEDBACK_ENHANCED=false` | JSON | ✗ | ✗ | ✗ | ✗ | ✗ |
| `FEEDBACK_ENHANCED=true` | SQLite | ✓ | ✓ | ✓ | ✓ | ✓ |

---

## 🎨 整合效果

### 增强模式启用时

- 支持描述模板
- 支持图片上传
- 支持重复检测
- 支持统计分析
- 支持数据导出

### 增强模式禁用时

- 保持原有功能
- 使用 JSON 存储
- 兼容旧数据

---

**文档版本**: v1.0
**更新时间**: 2026-03-08