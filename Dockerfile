# syntax=docker/dockerfile:1

# 构建阶段全部固定在构建机架构（$BUILDPLATFORM）上原生执行：
# 前端产物与 CPU 架构无关，Go 又可用 GOARCH 交叉编译，只有最终运行镜像才需要按目标架构拉取。
# 这样可避免 npm / go 在 arm64 下走 QEMU 模拟——这是多架构构建最大的耗时来源。

# ---------- 阶段 1：构建前端 ----------
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
WORKDIR /app/frontend
# 只拷依赖清单：改源码不会令依赖安装层失效
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ ./
RUN npm run build

# ---------- 阶段 2：构建后端（静态二进制） ----------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETARCH
# 使用国内模块代理，确保在 CN 网络下 go mod download / go build 可稳定拉取依赖
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
# 只拷依赖清单：改源码不会令依赖下载层失效
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
# GOARCH 交叉编译出目标架构二进制，无需 QEMU；构建缓存挂载让重复构建免于全量重编
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /out/vigil ./cmd/server

# ---------- 阶段 3：运行镜像 ----------
FROM alpine:3.20
# 仅本阶段按目标架构执行（arm64 走 QEMU），只装两个小包，代价可忽略
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /out/vigil /app/vigil
# 历史镜像（≤v1.2.0）的二进制为 /app/dockmon；原地升级的容器 entrypoint 已固化在
# 容器配置中，保留旧路径符号链接使 1Panel/watchtower 等换镜像不换配置的工具无感升级
RUN ln -s vigil /app/dockmon
COPY --from=frontend /app/frontend/dist /app/static

ENV PORT=54321 \
    STATIC_DIR=/app/static \
    DB_PATH=/data/monitor.db \
    DOCKER_HOST=unix:///var/run/docker.sock \
    REGISTRY_MIRROR= \
    DINGTALK_WEBHOOK=

EXPOSE 54321
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:54321/api/health || exit 1
ENTRYPOINT ["/app/vigil"]
