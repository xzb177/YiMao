# YiMao RC 验收报告

## 基本信息

- 候选版本/Commit：
- 验收日期：
- 验收人员：
- Staging Bot 用户名：
- Staging 环境标识：
- GitHub Actions Run：
- 自动 Smoke 报告：

> 不得填写 Bot Token、API Key、密码、生产 URL 或用户隐私数据。

## 自动门禁

| 检查 | 结果 | 证据/备注 |
|------|------|-----------|
| GitHub Actions | ⬜ | |
| Staging preflight | ⬜ | |
| App health / dependency health | ⬜ | |
| API auth deny/allow | ⬜ | |
| Telegram identity / polling / commands | ⬜ | |
| MoviePilot authenticated read | ⬜ | |
| Telegram send/delete | ⬜ | |
| 72h soak | ⬜ | |

## 人工真机矩阵

| 场景 | 结果 | 证据/备注 |
|------|------|-----------|
| 私聊 `/start` 和主菜单 | ⬜ | |
| 搜索电影/剧集/季度 | ⬜ | |
| 求片、重复、配额、撤回 | ⬜ | |
| 求片进度与隐私 | ⬜ | |
| 反馈、图片、管理员回复 | ⬜ | |
| AI 解说切换与过期按钮 | ⬜ | |
| 每日挑战与电影冒险 | ⬜ | |
| 群聊隐私与 ephemeral | ⬜ | |
| 旧消息 callback 兼容 | ⬜ | |
| 超长中文/Emoji/特殊字符 | ⬜ | |
| MoviePilot 故障与恢复 | ⬜ | |
| 容器重启与数据持久化 | ⬜ | |

## 缺陷记录

| ID | 严重度 | 场景 | 复现步骤 | 状态 | 修复提交 |
|----|--------|------|----------|------|----------|
| | | | | | |

## 发布结论

- [ ] 通过，可进入下一 RC / v1.1
- [ ] 有条件通过，需完成上表事项
- [ ] 不通过

结论说明：

签字/确认：
