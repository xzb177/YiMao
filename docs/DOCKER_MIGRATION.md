# Docker 迁移指南

本文档说明如何将 yimao 从 systemd 服务迁移到 Docker 容器。

## 迁移步骤

### 1. 准备工作

确保已安装 Docker 和 Docker Compose：

```bash
# 检查 Docker
docker --version

# 检查 Docker Compose
docker compose --version
# 或
docker compose version
```

### 2. 数据备份

在迁移前备份重要数据：

```bash
# 创建备份目录
mkdir -p backup-$(date +%Y%m%d)

# 备份数据文件
cp preferences.json backup-$(date +%Y%m%d)/
cp user_quotas.json backup-$(date +%Y%m%d)/
cp user_mapping.json backup-$(date +%Y%m%d)/
```

### 3. 使用自动部署脚本

```bash
# 运行部署脚本
./deploy-docker.sh
```

### 4. 手动部署（可选）

如果自动脚本不适用，可以手动执行：

```bash
# 构建镜像
docker compose build

# 启动容器
docker compose up -d

# 查看日志
docker compose logs -f
```

## 验证部署

### 检查容器状态

```bash
docker compose ps
```

### 查看日志

```bash
# 实时日志
docker compose logs -f

# 最近 100 行
docker compose logs --tail=100
```

### 测试 Webhook

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"Event": "system.notificationtest"}'
```

## 常用命令

```bash
# 启动
docker compose up -d

# 停止
docker compose stop

# 重启
docker compose restart

# 查看状态
docker compose ps

# 查看日志
docker compose logs -f

# 进入容器
docker compose exec yimao sh

# 更新代码后重新构建
docker compose up -d --build

# 完全清理
docker compose down
```

## 数据持久化

以下数据通过卷挂载持久化：

- `./data` - 应用数据目录
- `./preferences.json` - 用户偏好设置
- `./user_quotas.json` - 用户配额
- `./user_mapping.json` - 用户映射

## 网络配置

默认端口映射为 `8080:8080`。如果需要修改：

```yaml
ports:
  - "9000:8080"  # 将容器 8080 映射到主机 9000
```

如果需要访问同一主机上的其他服务（如 Emby/Jellyseerr 使用 localhost），可以使用 host 网络模式：

```yaml
# network_mode: "host"
ports: []  # host 模式下不需要端口映射
```

## 环境变量

所有环境变量通过 `.env` 文件配置。参考 `.env.example`：

```bash
cp .env.example .env
vi .env
```

## 回滚

如果需要回滚到 systemd 服务：

```bash
# 1. 停止 Docker 容器
docker compose down

# 2. 重新启用 systemd 服务
sudo systemctl enable yimao
sudo systemctl start yimao
```

## 故障排查

### 容器无法启动

```bash
# 查看详细日志
docker compose logs

# 检查环境变量
docker compose config
```

### 端口冲突

修改 `docker-compose.yml` 中的端口配置，或停止占用 8080 端口的服务。

### 数据丢失

检查卷挂载是否正确配置：
```bash
docker compose config | grep -A 10 volumes
```
