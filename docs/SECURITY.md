# Security Policy

感谢你为 YiMao 的安全性做出贡献。

## Supported Versions

当前默认维护分支：`master`

我们优先修复：

- 凭据泄漏风险（Token / API Key / 密码）
- 越权访问与权限绕过
- 输入验证与注入问题
- 回调与 Webhook 安全问题

## Reporting a Vulnerability

请不要在公开 issue 中直接披露漏洞细节。

建议通过以下方式私下报告：

- 提交最小复现步骤
- 影响范围与风险等级
- 修复建议（可选）

维护者会尽快确认并安排修复。

## Security Best Practices

- 不要将 `.env`、日志或数据库文件提交到仓库
- 生产环境务必替换默认密钥并收紧管理员 ID
- 定期轮换 Telegram / MoviePilot / TMDB 凭据
- 升级前先备份数据并保留回滚路径
