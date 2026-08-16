# 多阶段构建：编译 → 精简运行镜像
FROM golang:1.26-alpine AS build

WORKDIR /src

# 国内网络可显著加速依赖下载；可通过 --build-arg GOPROXY=... 覆盖
ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://goproxy.cn,direct}

# 先拷贝依赖清单，充分利用构建缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/bili-torrent .

# hdrprobe：从码流快速检测杜比视界/HDR 等视频类型（GitHub release 预编译二进制）。
# 下载失败或设置 --build-arg SKIP_HDRPROBE=1 时，仅生成占位文件，不影响构建。
ARG HDRPROBE_VERSION=v1.0.0
ARG SKIP_HDRPROBE=0
RUN if [ "$SKIP_HDRPROBE" != "1" ]; then \
      wget -q -O /tmp/hdrprobe.tar.gz \
        "https://github.com/matthane/hdrprobe/releases/download/${HDRPROBE_VERSION}/hdrprobe-${HDRPROBE_VERSION#v}-linux-x64-static.tar.gz" \
        && tar xzf /tmp/hdrprobe.tar.gz -C /out --strip-components=1 --wildcards 'hdrprobe-*-linux-x64-static' \
        && mv /out/hdrprobe-*-linux-x64-static /out/hdrprobe \
        && chmod +x /out/hdrprobe \
        || { echo 'hdrprobe 下载失败，构建继续（杜比视界码流检测不可用）'; touch /out/hdrprobe; }; \
    else \
      touch /out/hdrprobe; \
    fi

# 运行镜像（不含 ffmpeg；如需 ffprobe 音频探测可在此追加 ffmpeg）
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=build /out/bili-torrent /usr/local/bin/bili-torrent
COPY --from=build /out/hdrprobe /usr/local/bin/hdrprobe
RUN chmod +x /usr/local/bin/hdrprobe || true

ENV TZ=Asia/Shanghai \
    BILI_TORRENT_CONFIG=/data/config/config.yaml

# 只映射 /data；config/data/downloads/torrents 子文件夹由应用启动时自动创建，已存在则复用
VOLUME ["/data"]

EXPOSE 8080

ENTRYPOINT ["bili-torrent"]
