# YiMao 运维手册

## 快速操作

### 运维脚本 (`ops.sh`)

```bash
cd /path/to/YiMao

# 查看状态
./scripts/ops.sh status

# 查看实时日志
./scripts/ops.sh logs

# 只看错误日志
./scripts/ops.sh logs-error

# 重启服务
./scripts/ops.sh restart

# 重新构建
./scripts/ops.sh rebuild

# 更新代码
./scripts/ops.sh update

# 清理图片缓存
./scripts/ops.sh clean

# 备份数据
./scripts/ops.sh backup

# 部署前验收（不启动、不重启）
./scripts/ops.sh preflight

# 健康检查
./scripts/ops.sh health
```

## 服务状态

不要依赖文档中的静态状态，始终以当前环境实测为准：

```bash
./scripts/ops.sh status
./scripts/ops.sh health
curl -fsS http://localhost:8080/health
```

如需持续监控，请优先由服务器自身的监控系统定时调用 `/health`。仓库保留 `monitor.sh` 作为兼容健康检查入口，但不会自动写入 crontab，也不会擅自重启服务。

## 常见问题

### 1. 容器未运行

```bash
./scripts/ops.sh status
./scripts/ops.sh start
```

### 2. API 连接失败

检查 `.env` 配置并重新执行只读验收：
```bash
./scripts/ops.sh preflight
curl -fsS http://localhost:8080/health
```

### 3. 图片缓存过大

```bash
./scripts/ops.sh clean
```

### 4. 更新代码

```bash
./scripts/ops.sh update
```

## 自动监控

仓库不自动安装定时任务。需要持续监控时，请在服务器的监控平台或系统定时器中调用：

```bash
curl -fsS http://localhost:8080/health
# 或使用兼容入口
./monitor.sh
```

`./monitor.sh --loop` 默认每 300 秒检查一次，可通过 `MONITOR_INTERVAL_SECONDS` 调整。告警投递与重启策略应由部署环境显式配置，避免仓库脚本擅自重启生产服务。

## 数据备份

### 手动备份

```bash
./scripts/ops.sh backup
```

备份位置: 项目根目录下的 `backup-YYYYMMDD_HHMMSS/`

### 恢复备份

```bash
# 停止服务
docker stop yimao

# 在项目根目录恢复数据与配置
cp -r backup-XXXXX/data/* ./data/
cp backup-XXXXX/.env ./

# 重启服务
docker start yimao
```

## Docker 命令

```bash
# 查看日志
docker logs -f yimao

# 进入容器
docker exec -it yimao sh

# 重启容器
docker restart yimao

# 查看资源
docker stats yimao
```

## 配置文件

| 文件 | 说明 |
|------|------|
| `.env` | 环境变量配置 |
| `docker-compose.yml` | Docker 编排配置 |
| `data/preferences.json` | 用户偏好设置 |
| `data/user_mappings.json` | 用户映射关系 |
| `data/user_quotas.json` | 用户配额 |

## 日志位置

| 日志类型 | 位置 |
|----------|------|
| 容器日志 | `docker logs yimao` |
| 健康接口 | `GET http://localhost:8080/health` |
| Docker 服务日志 | 通过 `journalctl -u docker` 查看 |

## 外部服务

MoviePilot、Emby/Jellyfin 与 Telegram Bot 的地址、账号均来自当前 `.env`。文档不记录生产地址或 Bot 标识，避免过期信息和环境泄露。

## 更新日志

- 2026-07-19: 增加部署前验收流程，移除过期的静态状态、地址与不存在的监控脚本
- 2026-03-06: 添加运维脚本
- 2026-03-05: 添加一键更新脚本
