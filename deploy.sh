#!/bin/bash
# YiMao 部署脚本 — 挂载 yimao-data 卷持久化数据
set -e
cd /opt/data/yimao-build
docker build -t yimao:latest .
docker stop yimao || true
docker rm yimao || true
docker run -d --name yimao --network host -v yimao-data:/app/data --env-file .env yimao:latest
sleep 2
docker logs yimao --tail 5
echo "✅ YiMao deployed with persistent volume yimao-data:/app/data"
