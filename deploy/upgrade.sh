#!/bin/bash
# bili-torrent 服务器升级脚本：更新二进制/hdrprobe → 迁移 data 子目录结构 → 重建镜像 → 重启容器
PASS='qwer1213800000'
S() { echo "$PASS" | sudo -S -p '' "$@"; }

echo '=== 1/6 更新文件 ==='
S cp /tmp/bili-torrent-linux-amd64 /opt/bili-torrent/runtime/bili-torrent-linux-amd64
S cp /tmp/hdrprobe-linux /opt/bili-torrent/runtime/hdrprobe-1.0.0-linux-x64-static
S cp /tmp/Dockerfile.runtime /opt/bili-torrent/runtime/Dockerfile
S cp /tmp/compose.yaml /opt/bili-torrent/compose.yaml
S chmod +x /opt/bili-torrent/runtime/bili-torrent-linux-amd64 /opt/bili-torrent/runtime/hdrprobe-1.0.0-linux-x64-static

echo '=== 2/6 数据目录迁移（旧 /opt/bili-torrent/data → 新位置）==='
S bash -c '
  set -e
  D=/vol3/1000/docker/bili-torrent/data
  mkdir -p "$D" "$D/data"
  # 将旧部署的数据目录（config/data/downloads/torrents）合并到新位置；已存在则不覆盖
  if [ -d /opt/bili-torrent/data ]; then
    for d in config data downloads torrents; do
      if [ -e "/opt/bili-torrent/data/$d" ]; then
        if [ -e "$D/$d" ]; then
          cp -rn "/opt/bili-torrent/data/$d/." "$D/$d/" 2>/dev/null || true
        else
          mv "/opt/bili-torrent/data/$d" "$D/$d"
        fi
      fi
    done
  fi
'
S cp /tmp/config.yaml /vol3/1000/docker/bili-torrent/data/config/config.yaml

echo '=== 2b/6 TLS 证书（如提供）==='
if [ -f /tmp/fullchain.crt ] && [ -f /tmp/fn.suay.cn.key ]; then
  S cp /tmp/fullchain.crt /vol3/1000/docker/bili-torrent/data/config/fullchain.crt
  S cp /tmp/fn.suay.cn.key /vol3/1000/docker/bili-torrent/data/config/fn.suay.cn.key
  S chmod 644 /vol3/1000/docker/bili-torrent/data/config/fullchain.crt
  S chmod 600 /vol3/1000/docker/bili-torrent/data/config/fn.suay.cn.key
  echo 'TLS 证书已更新'
else
  echo '未提供证书，跳过'
fi

echo '=== 3/6 重建镜像 ==='
S docker build -t bili-torrent:latest /opt/bili-torrent/runtime
if [ $? -ne 0 ]; then echo '构建失败'; exit 1; fi

echo '=== 4/6 重启容器 ==='
cd /opt/bili-torrent || exit 1
S docker compose up -d --force-recreate

echo '=== 5/6 验证 ==='
sleep 3
# 服务可能为 HTTPS（配置 tls_cert/tls_key）或纯 HTTP，两种都尝试
curl -sk -o /dev/null -w '首页(HTTPS) HTTP %{http_code}\n' https://localhost:8080/ 2>/dev/null \
  || curl -s -o /dev/null -w '首页(HTTP) HTTP %{http_code}\n' http://localhost:8080/
S docker ps --filter name=bili-torrent --format '{{.Names}} | {{.Status}}'
S docker exec bili-torrent sh -c 'hdrprobe --version || echo hdrprobe-missing'
S docker logs --tail 5 bili-torrent 2>&1

echo '=== 6/6 配置确认 ==='
curl -sk -o /dev/null https://localhost:8080/api/config 2>/dev/null \
  && curl -sk https://localhost:8080/api/config \
  || curl -s http://localhost:8080/api/config
echo
