# syntax=docker/dockerfile:1

# ---------- 阶段 1：构建前端 ----------
FROM node:20-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# ---------- 阶段 2：构建后端（静态二进制） ----------
FROM golang:1.25-alpine AS backend
# 使用国内模块代理，确保在 CN 网络下 go mod tidy / go build 可稳定拉取依赖
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY backend/ ./
# go mod tidy 会自动解析并锁定 modernc.org/sqlite 及其依赖与正确版本
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /out/dockmon ./cmd/server

# ---------- 阶段 3：运行镜像 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /out/dockmon /app/dockmon
COPY --from=frontend /app/frontend/dist /app/static

ENV PORT=54321 \
    STATIC_DIR=/app/static \
    DB_PATH=/data/monitor.db \
    DOCKER_HOST=unix:///var/run/docker.sock \
    REGISTRY_MIRROR=

EXPOSE 54321
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:54321/api/health || exit 1
ENTRYPOINT ["/app/dockmon"]
