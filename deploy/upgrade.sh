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

echo '=== 2/6 迁移为 data 子目录结构 ==='
S bash -c '
  set -e
  D=/opt/bili-torrent/data
  mkdir -p "$D" "$D/data"
  # 首次迁移标记：旧的顶层 config/downloads/torrents 存在才做路径重写（data 总是存在，不参与判断）
  FIRST=0
  for d in config downloads torrents; do
    [ -e "/opt/bili-torrent/$d" ] && FIRST=1
  done
  # 旧的 config/downloads/torrents → data/{config,downloads,torrents}
  for d in config downloads torrents; do
    if [ -e "/opt/bili-torrent/$d" ] && [ ! -e "$D/$d" ]; then
      mv "/opt/bili-torrent/$d" "$D/$d"
    fi
  done
  # 顶层 data 中的旧内容（db.json 等）并入 $D/data（排除已建立的子目录）
  for f in "$D"/*; do
    b=$(basename "$f")
    case "$b" in config|data|downloads|torrents) continue;; esac
    mv "$f" "$D/data/"
  done
  # db.json 中的旧绝对路径 /downloads、/torrents → /data/downloads、/data/torrents（防重复替换）
  if [ "$FIRST" = 1 ] && [ -f "$D/data/db.json" ]; then
    sed -i "s|/downloads/|@@DLOAD@@/|g; s|/torrents/|@@TORR@@/|g; s|@@DLOAD@@/|/data/downloads/|g; s|@@TORR@@/|/data/torrents/|g" "$D/data/db.json"
  fi
'
S cp /tmp/config.yaml /opt/bili-torrent/data/config/config.yaml

echo '=== 3/6 重建镜像 ==='
S docker build -t bili-torrent:latest /opt/bili-torrent/runtime
if [ $? -ne 0 ]; then echo '构建失败'; exit 1; fi

echo '=== 4/6 重启容器 ==='
cd /opt/bili-torrent || exit 1
S docker compose up -d --force-recreate

echo '=== 5/6 验证 ==='
sleep 3
curl -s -o /dev/null -w '首页 HTTP %{http_code}\n' http://localhost:8080/
S docker ps --filter name=bili-torrent --format '{{.Names}} | {{.Status}}'
S docker exec bili-torrent sh -c 'hdrprobe --version || echo hdrprobe-missing'
S docker logs --tail 5 bili-torrent 2>&1

echo '=== 6/6 配置确认 ==='
curl -s http://localhost:8080/api/config
echo
