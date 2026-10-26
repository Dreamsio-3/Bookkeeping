# syntax=docker/dockerfile:1

# ---- 构建阶段 ----
FROM golang:1.24-alpine AS builder

WORKDIR /src

# 先只拷贝依赖清单，源码变化时可以复用依赖缓存层。
COPY go.mod go.sum ./
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0 \
    GOTOOLCHAIN=local
RUN go mod download

COPY . .

# -s -w 去掉调试信息，减小体积；CGO 关闭后是静态二进制，可直接跑在 scratch/alpine 上。
RUN go build -trimpath -ldflags="-s -w" -o /out/bookkeeping . \
    && mkdir -p /out/data

# ---- 运行阶段 ----
FROM alpine:3.20

# ca-certificates 供将来外部调用使用；tini 负责正确转发信号，保证 SQLite 干净落盘。
RUN apk add --no-cache ca-certificates tini \
    && addgroup -g 10001 -S app \
    && adduser -u 10001 -S -G app app \
    && mkdir -p /data \
    && chown -R app:app /data

COPY --from=builder /out/bookkeeping /usr/local/bin/bookkeeping

ENV DATA_DIR=/data \
    DB_PATH=/data/bookkeeping.db \
    BACKUP_DIR=/data/backups \
    BACKUP_KEEP_DAYS=7 \
    PORT=8080 \
    TZ=Asia/Shanghai

# 数据目录必须挂载卷，容器重建后数据仍在。
VOLUME ["/data"]

USER app
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/api/health" >/dev/null || exit 1

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/bookkeeping"]
