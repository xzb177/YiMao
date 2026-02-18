# Cloudflare Tunnel 配置方案

如果 Emby 服务器无法直接访问你的服务器，可以用 Cloudflare Tunnel：

## 安装 cloudflared

```bash
# 下载安装
wget https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
dpkg -i cloudflared-linux-amd64.deb
```

## 创建隧道

```bash
# 登录
cloudflared tunnel login

# 创建隧道
cloudflared tunnel create emby-webhook

# 配置文件
mkdir -p ~/.cloudflared
cat > ~/.cloudflared/config.yml << EOF
tunnel: <你的隧道ID>
credentials-file: /root/.cloudflared/<你的隧道ID>.json

ingress:
  - hostname: emby-webhook.your-domain.com
    service: http://localhost:8080
  - service: http_status:404
EOF

# 运行
cloudflared tunnel run emby-webhook
```

然后在 Emby 配置：`https://emby-webhook.your-domain.com/webhook`
