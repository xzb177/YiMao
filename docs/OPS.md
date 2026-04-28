# YiMao 运维手册

## 快速操作

### 运维脚本 (`ops.sh`)

```bash
cd /root/YiMao

# 查看状态
./ops.sh status

# 查看实时日志
./ops.sh logs

# 只看错误日志
./ops.sh logs-error

# 重启服务
./ops.sh restart

# 重新构建
./ops.sh rebuild

# 更新代码
./ops.sh update

# 清理图片缓存
./ops.sh clean

# 备份数据
./ops.sh backup

# 健康检查
./ops.sh health

# 测试 API 连接
./ops.sh test
```

### 监控脚本 (`monitor.sh`)

```bash
# 一次性检查
./monitor.sh

# 持续监控（每5分钟）
./monitor.sh --loop
```

## 服务状态

### 当前状态（2026-03-06）

| 项目 | 状态 |
|------|------|
| 容器 | ✅ 运行中 (healthy) |
| CPU | 0.03% |
| 内存 | 0.17% (13.84MB) |
| 磁盘 | 4% (95MB data) |
| MoviePilot | ✅ 连接正常 |
| Emby | ✅ 连接正常 |
| Webhook | ✅ 活跃 |

### 用户统计

- 注册用户: 9 人
- 有配额用户: 4 人

## 常见问题

### 1. 容器未运行

```bash
./ops.sh status
./ops.sh start
```

### 2. API 连接失败

检查 `.env` 配置：
```bash
./ops.sh test
```

### 3. 图片缓存过大

```bash
./ops.sh clean
```

### 4. 更新代码

```bash
./ops.sh update
```

## 定时任务

监控任务已添加到 crontab（每5分钟运行一次）：

```bash
# 查看定时任务
crontab -l

# 查看监控日志
tail -f /var/log/yimao_monitor.log
```

## 数据备份

### 手动备份

```bash
./ops.sh backup
```

备份位置: `/root/YiMao/backup_YYYYMMDD_HHMMSS/`

### 恢复备份

```bash
# 停止服务
docker stop yimao

# 恢复数据
cp -r backup_XXXXX/data/* /root/YiMao/data/
cp backup_XXXXX/.env /root/YiMao/

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
| 监控日志 | `/var/log/yimao_monitor.log` |
| 运维日志 | 通过 `journalctl -u docker` 查看 |

## 外部服务

- **MoviePilot**: http://167.17.76.115:4500
- **Emby**: https://emby.oceancloud.asia
- **Telegram Bot**: @...8809

## 更新日志

- 2026-03-06: 添加运维脚本和监控
- 2026-03-05: 添加一键更新脚本
