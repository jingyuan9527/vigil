# DockMon · Docker 镜像监控

[![Build Multi-Arch Image](https://github.com/jingyuan9527/vigil/actions/workflows/build.yml/badge.svg)](https://github.com/jingyuan9527/vigil/actions/workflows/build.yml)

一个参考 [diun](https://github.com/crazy-max/diun) 与 [wud (What's Up Docker)](https://github.com/fmartinou/whats-up-docker) 的 Docker 镜像监控工具：

- **镜像扫描**：通过 Docker Engine API 采集本地镜像（含摘要），或监控手动配置的镜像引用。
- **版本检测**：查询注册表（Docker Hub / 私有 registry）manifest 摘要，与本地摘要比对。
- **更新提醒**：远端摘要变化时生成通知，并记录版本时间线。
- **状态展示**：React + Tailwind 前端，视觉严格遵循 [stylekit bento-grid](https://www.stylekit.top/zh/styles/bento-grid) 风格。

整体以单个 Docker 容器部署，对外服务端口固定为 **54321**。

---

## 架构

```
┌──────────────────────────────────────────────────────────┐
│                       Docker 容器                          │
│                                                            │
│   React + Tailwind 静态资源  ──┐                           │
│                                │  54321 (单端口同源)        │
│   Go HTTP Server  ─────────────┤                           │
│     ├─ /api/*    REST 接口      │                           │
│     ├─ /*        前端 SPA        │                           │
│     ├─ scanner   采集+检测+通知   │                           │
│     ├─ registry  注册表摘要比对   │                           │
│     └─ store     SQLite 持久化   │                           │
└───────────────┬────────────────┘                           │
                │ 挂载 /var/run/docker.sock                  │
                ▼                                            │
        Docker 守护进程 / 注册表                               │
```

### 目录结构

```
.
├── Dockerfile              # 多阶段构建（node 前端 + golang 后端）
├── docker-compose.yml      # 一键部署（挂载 docker.sock + 数据卷）
├── backend/                # Go 后端
│   ├── go.mod
│   ├── cmd/server/main.go  # 入口：配置、扫描调度、HTTP 服务
│   └── internal/
│       ├── config/         # 环境变量配置
│       ├── models/         # 数据模型
│       ├── docker/         # Docker Engine API 客户端
│       ├── registry/       # 注册表客户端（manifest 摘要 + 鉴权）
│       ├── store/          # SQLite 存储与 CRUD
│       ├── scanner/        # 扫描编排（采集→检测→通知）
│       └── api/            # REST 路由 + 静态资源服务
└── frontend/               # React + Tailwind 前端
    ├── src/
    │   ├── api/client.js        # API 客户端
    │   ├── components/          # BentoCard / StatCard / StatusBadge / Layout
    │   ├── context/ThemeContext # 明暗主题
    │   └── pages/               # Dashboard / Images / Compare / Notifications
    ├── tailwind.config.js
    └── vite.config.js
```

---

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
docker compose up -d --build
# 访问 http://localhost:54321
```

容器会挂载宿主 `docker.sock`，自动监控本机所有带 tag 的镜像，并附带一组常用镜像的演示监控列表。

### 方式二：本地开发

```bash
# 后端
cd backend && go run ./cmd/server

# 前端（另开终端，Vite 代理 /api 到 54321）
cd frontend && npm install && npm run dev
# 访问 http://localhost:5173
```

### 方式三：使用 GitHub 预构建镜像（GHCR，免本地编译）

适合不想本地构建、或直接在 ARM 设备（树莓派等）上部署的场景。镜像由 GitHub Actions 自动构建并推送至 GHCR，已包含 `linux/amd64` 与 `linux/arm64` 双架构，无需本地 `go` / `node` 工具链。

使用预置的 compose 文件（仅拉取镜像，不构建）：

```bash
docker compose -f docker-compose.ghcr.yml up -d
# 访问 http://localhost:54321
```

或直接用 `docker run` 拉取运行：

```bash
docker run -d --name dockmon \
  -p 54321:54321 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v dockmon-data:/data \
  ghcr.io/jingyuan9527/vigil:latest
```

如需显式指定架构（通常 docker 会自动匹配宿主架构，无需手动指定）：

```bash
docker pull --platform linux/arm64 ghcr.io/jingyuan9527/vigil:latest
```

---

## 配置（环境变量）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `54321` | 对外服务端口 |
| `DB_PATH` | `/data/monitor.db` | SQLite 数据库路径 |
| `STATIC_DIR` | `./static` | 前端静态资源目录 |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker 守护进程地址（`unix://` / `tcp://`） |
| `SCAN_INTERVAL` | `3600` | 周期扫描间隔（秒） |
| `REGISTRY_INSECURE` | `false` | 是否允许 `http` 注册表 |
| `REGISTRY_MIRROR` | 空 | 注册表镜像地址（非空时所有 manifest/tag 请求改发往该主机，用于私有/加速镜像） |
| `WATCH` | 空 | 额外监控的镜像引用，逗号分隔（如 `nginx:latest,redis:7`） |
| `DISABLE_DEFAULT_WATCH` | `false` | 设为 `1` 关闭内置演示监控列表 |

> 上述 `SCAN_INTERVAL` / `REGISTRY_INSECURE` / `REGISTRY_MIRROR` / `DISABLE_DEFAULT_WATCH` 四项**也可在页面「设置」中直接修改**，保存后即时生效并持久化到数据库，重启后仍然保留；环境变量仅作为首次启动的初值。
>
> 即使没有 Docker 守护进程（如仅监控远端镜像），也可通过 `WATCH` / 页面「添加监控」来监控任意注册表镜像的版本变化。

---

## REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET | `/api/auth/check` | 是否需要初始化设置 + 当前是否已认证 |
| POST | `/api/auth/setup` | 首次部署设置管理员账号（成功后写入登录 cookie） |
| POST | `/api/auth/login` | 登录（成功后写入登录 cookie） |
| POST | `/api/auth/logout` | 登出（清除登录 cookie） |
| GET | `/api/stats` | 仪表盘统计 |
| GET | `/api/images?status=` | 镜像列表（可按状态过滤） |
| POST | `/api/images` | 新增手动监控 `{ "reference": "nginx:latest" }` |
| GET | `/api/images/:id` | 镜像详情（含版本时间线、可用 tag） |
| DELETE | `/api/images/:id` | 移除监控 |
| POST | `/api/scan?force=1` | 立即触发一次扫描；`force=1` 为强制扫描，对所有版本差异补发通知（按摘要去重） |
| GET | `/api/settings` | 获取当前运行时设置（扫描间隔、注册表、演示列表等） |
| PUT | `/api/settings` | 更新设置，持久化并即时生效（重启后仍保留） |
| GET | `/api/scans` | 扫描历史 |
| GET | `/api/notifications?unread=1` | 通知列表（支持 `cursor` 参数翻页加载更早通知） |
| POST | `/api/notifications/:id/read` | 标记单条已读 |
| POST | `/api/notifications/read-all` | 全部已读 |
| PUT | `/api/images/:id/mode` | 设置镜像检测模式覆写 `{ "mode": "auto" \| "digest-only" \| "pin-watch" }` |

> **认证与安全**：登录态由后端维护，JWT 通过 `httpOnly` cookie（`SameSite=Lax`）下发，
> 前端 JS 不接触令牌，降低 XSS 窃取风险；跨站请求不携带 cookie，天然抵御 CSRF。
> 登录/初始化接口按 IP 限流：连续 5 次失败锁定 15 分钟。
> **扫描并发**：同一时刻只允许一次扫描执行（定时器、手动扫描、添加镜像并发触发时，
> 后到者跳过），重复触发 `/api/scan` 返回 `"result": "scan already running"`。

---

## 检测模式与通知边界

每个镜像有两种检测模式，默认按 tag 自动识别（用户可在镜像列表手动覆写单个镜像）：

### Digest-Only（仅校验当前标签摘要）
- 默认分配给浮动标签（`latest` / `nightly` / `dev` / `canary` / `beta`）及其它非版本号 tag。
- 只查当前 tag 的远端摘要是否变化 → 变化即发「更新告警」。
- **不拉取仓库全部 tag 列表，不通知别的新版本。**

### Pin-Watch（锁定版本 + 监视新标签）
- 默认分配给数字版本号 tag（如 `8.4.5`、`v3.9`、`1.2.3-alpine`）。
- 仍对比当前 tag 摘要（用于状态与版本时间线），但**锁定 tag 被仓库覆盖时不发告警**。
- 额外巡检仓库完整 tag 列表，出现从未见过的版本 tag → 每个 tag 发一条「新版本发布通知」（每个 tag 仅一次，记入已见清单）。

### 通知类型（两类完全分开）
- `update`（强，系统 + 钉钉）：当前标签内容变更（Digest-Only 常规转移，或强制扫描补报）。
- `new-tag`（弱，系统 + 钉钉弱提醒）：仓库出现全新版本 tag（仅 Pin-Watch）。

### 防抖
- digest 变更：同一个摘要只通知一次（按 image + digest 去重）；标记已读仅 UI 效果。
- new-tag：每个 tag 仅通知一次；首扫建立已见标签基线，不刷屏。

### 忽略优先级最高
镜像被忽略后**跳过全部检测**（不校验摘要、不巡检标签），行数据冻结，也不产生任何通知。

### 强制扫描
更新通知页「全部重新扫描」触发强制扫描：对当前所有存在版本差异的镜像补发 `update` 通知（含 Pin-Watch 锁定 tag 被覆盖的情况），统一按 (image, digest) 去重，重复触发不会刷屏。

---

## 核心页面

- **仪表盘（Dashboard）**：bento 网格展示总量、最新/待更新/未读统计、状态分布、最近更新动态与扫描信息。
- **镜像列表（Images）**：全部被监控镜像卡片，有更新的排在前；支持状态筛选、搜索与分页；概览统计可点击直接过滤；每张卡片提供版本对比、忽略/恢复提醒与移除操作。
- **版本对比（Compare）**：左侧选择器支持搜索与状态过滤（有更新优先）；右侧展示本地 vs 远端摘要、可用标签与版本时间线，标签与时间线超长时默认折叠、可一键展开全部。
- **更新通知（Notifications）**：按优先级分组独立展示——「有新版本」（高优先级，同一 tag 摘要变化）与「可选更新」（低优先级，更高的独立版本 tag），各组可折叠、独立分页；支持未读过滤、单条/全部已读与一键「全部重新扫描」。
- **设置（Settings）**：在页面上调整扫描间隔、关闭内置演示监控列表、允许 http 注册表、配置注册表镜像主机，保存后即时生效并持久化。

---

## 预构建镜像（多架构）

项目通过 GitHub Actions 自动构建 **x86_64（amd64）** 与 **arm64（aarch64）** 双架构镜像并推送至 GitHub Container Registry（GHCR），无需本地编译即可在树莓派等 ARM 设备上直接运行。

| 镜像 | 支持架构 |
|------|----------|
| `ghcr.io/jingyuan9527/vigil:latest` | `linux/amd64`, `linux/arm64` |
| `ghcr.io/jingyuan9527/vigil:<tag>` | 同上述双架构 |

使用预构建镜像（推荐直接使用预置的 [`docker-compose.ghcr.yml`](docker-compose.ghcr.yml)，或按下方 `docker run` 命令）：

```bash
docker run -d --name dockmon \
  -p 54321:54321 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v dockmon-data:/data \
  ghcr.io/jingyuan9527/vigil:latest
```

如需指定架构拉取：

```bash
docker pull --platform linux/arm64 ghcr.io/jingyuan9527/vigil:latest
```

构建流程定义在 [`.github/workflows/build.yml`](.github/workflows/build.yml)：

- 监听 `main` 分支 push 与 `v*` 标签；Pull Request 仅做构建校验、不推送镜像。
- 通过 QEMU + Buildx 实现跨架构构建，`main` 分支推送 `latest` 标签，标签 push 自动生成对应语义化标签。
- 复用 GitHub Actions 缓存（`type=gha`）加速后续构建。

> 镜像默认推送至 GHCR（无需额外配置，`GITHUB_TOKEN` 自动授权）。如需推送至 Docker Hub，可在工作流中改用 `docker/login-action` 登录 `docker.io` 并调整 `IMAGE` 环境变量。

