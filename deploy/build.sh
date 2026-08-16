#!/bin/bash
# bili-torrent 服务器部署脚本：构建运行时镜像并启动容器
PASS='qwer1213800000'
S() { echo "$PASS" | sudo -S -p '' "$@"; }

echo '=== 1/6 停止旧构建(如有) ==='
S pkill -f 'docker build' || true
S pkill -f 'apk add' || true
sleep 2

echo '=== 2/6 更新运行时 Dockerfile ==='
S cp /tmp/Dockerfile.runtime /opt/bili-torrent/runtime/Dockerfile

echo '=== 3/6 构建镜像 ==='
S docker build -t bili-torrent:latest /opt/bili-torrent/runtime
if [ $? -ne 0 ]; then echo '构建失败'; exit 1; fi

echo '=== 4/6 启动容器 ==='
cd /opt/bili-torrent || exit 1
S docker compose up -d
S docker ps --filter name=bili-torrent

echo '=== 5/6 验证 ==='
sleep 3
curl -s -o /dev/null -w '首页 HTTP %{http_code}\n' http://localhost:8080/ || echo '首页不可达'
echo '--- api/config ---'
curl -s http://localhost:8080/api/config || echo '(api 不可达)'
echo
echo '=== 6/6 容器日志(前 15 行) ==='
S docker logs --tail 15 bili-torrent 2>&1 || true
