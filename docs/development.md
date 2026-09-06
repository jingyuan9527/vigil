# Vigil 开发规范

本文档是 Vigil（DockMon）的规范化开发文档，约定项目结构、编码风格、测试与提交流程。个人铁律见根目录 [RULES.md](../RULES.md)，前端视觉规范以 [ui-design-spec.md](ui-design-spec.md) 为唯一事实源。三者冲突时以 RULES.md 为准。

## 1. 项目概览

Docker 镜像更新监控工具：采集本机镜像（或手动添加监控项）→ 请求注册表比对摘要/标签 → 差异落库并推送通知（站内 + 钉钉）。

```
┌────────────────────────── 单容器 · 单端口 54321 ──────────────────────────┐
│  frontend (React 18 + Vite + Tailwind)  ──构建产物──▶  backend static 目录 │
│  backend (Go 1.25 标准库)                                                 │
│    cmd/server/main.go  入口：装配 config/store/docker/scanner/api          │
│    internal/api        http.ServeMux 路由 + JWT 鉴权中间件                  │
│    internal/scanner    核心引擎：任务收集 → 并发检测(6) → 通知/去重          │
│    internal/registry   OCI 注册表客户端（Bearer token、digest、tags）       │
│    internal/docker     Docker Engine API 客户端（unix socket / tcp，只读） │
│    internal/store      SQLite（modernc.org/sqlite，无 CGO）+ 裸 SQL，按域拆分  │
│    internal/auth       bcrypt 口令、手写 HS256 JWT、httpOnly Cookie、登录限流  │
│    internal/config     环境变量 + LiveSettings 运行时热配置                 │
│    internal/version    语义化版本比较                                      │
└──────────────────────────────────────────────────────────────────────────┘
```

关键设计约束：

- **零重依赖**：后端唯一第三方依赖是 `modernc.org/sqlite`。HTTP、JWT、注册表协议全部手写，新增依赖必须先论证。
- **单二进制**：前端构建产物嵌入 `STATIC_DIR`，由 Go 进程托管；SPA 未命中路由回退 `index.html`。
- **数据不出本机**：SQLite 单文件（`/data/monitor.db`），WAL 模式，`SetMaxOpenConns(1)`。

## 2. 目录结构约定

```
backend/
  cmd/server/            唯一入口，只做装配，不写业务逻辑
  internal/<pkg>/        按关注点分包，包名单词小写
    *_test.go            与被测代码同包（白盒测试）
    *_integration_test.go 跨包协作的集成测试
frontend/
  src/
    api/client.js        唯一的 fetch 封装与响应格式化，页面不得直接 fetch
    components/          PascalCase 通用组件
    pages/               PascalCase 路由页面
    context/             AuthContext / ThemeContext
docs/                    开发文档与设计规范
```

约束：

- 业务逻辑禁止写进 `cmd/`；`main.go` 只做初始化、装配与优雅退出。
- 跨包只依赖 `internal/` 下的明确导出，禁止绕过分包直接摸底层类型。
- 前端页面内不新增独立样式体系，必须复用既有 Tailwind token 与组件（RULES #4）。

store 包按域拆分，新增存储逻辑先找对应文件，没有就开新文件而不是回填 store.go：

```
backend/internal/store/
  store.go          连接、迁移（幂等 DDL）、通用工具（时间/NULL 处理）
  images.go         镜像 CRUD、忽略/模式覆写、stale 标记
  versions.go       版本时间线
  seentags.go       pin-watch 已见标签基线
  notifications.go  通知 CRUD、去重基线、自动已读、保留策略
  scans.go          扫描记录与 Stats
  auth.go           管理员账号
  settings.go       运行时设置与 JWT secret
```

## 3. 后端规范（Go）

### 3.1 风格与注释

- `gofmt` + `go vet` 必须干净（提交前执行 `make fmt vet`）；禁止手写对齐漂移。
- 注释一律中文，只写「为什么」，不写「是什么」（RULES #2）；导出函数必须有 godoc。
- 单文件超过 500 行应考虑拆分（现状例外：`store/store.go`，见 tech-debt.md）。

### 3.2 错误处理

- handler 返回 `{"error": "<中文用户可读信息>"}`；**禁止**把内部 `err.Error()` 直接透给客户端——500/503 一律走 `serverError` / `storeUnavailable` helper（固定文案 + 日志带请求上下文）。
- 次要路径的 `_ =` 忽略必须在旁边注释说明为何可忽略；影响用户可见结果的错误不允许静默吞掉。
- 扫描/后台任务的错误必须至少落日志，能落库的（如 `scans.error`）落库。

### 3.3 日志

- 使用标准库 `log`，关键路径（启动、扫描开始/结束、注册表交互失败、通知发送）必须有带上下文的日志；异常带必要入参（RULES #3）。

### 3.4 数据库

- 全部裸 SQL + `?` 占位符，禁止字符串拼接 SQL；`where` 子句只允许由固定字面量组装。
- 迁移走 `CREATE TABLE IF NOT EXISTS` + `addColumnIfMissing` 幂等模式；新增列必须兼容旧库平滑升级，不写破坏性迁移。
- 行扫描统一走 `imageFromScan`（`*sql.Row` 与 `*sql.Rows` 共用一份列映射），禁止在单行/列表两处复制粘贴行映射逻辑。

### 3.5 并发

- 共享可变状态必须由互斥锁保护（参考 `config.LiveSettings`）；注册表客户端等可热替换对象禁止裸赋值。
- 后台扫描单飞靠 CAS（`CreateScan` 原子占位）实现，不要引入新的全局定时器直接调 `Run`。
- 每个镜像的处理超时 25s、并发 6（信号量），改动这些参数需同步测试。

### 3.6 测试

- `go test ./...` 必须全绿；新增功能需带测试：
  - 纯逻辑（mode 解析、版本比较、状态计算）→ 单元测试；
  - 涉及 store / scanner 协作 → `*_integration_test.go`，用临时目录建真实 SQLite；
  - HTTP 层 → `httptest` 起完整路由，先走 `/api/auth/setup` 拿 token 再调受保护接口。
- `internal/auth`（口令哈希、限流）、`internal/scanner`（检测模式、强制扫描语义）是回归重灾区，改动必须连带补测试。

## 4. 前端规范（React）

- React 18 + JavaScript (JSX) + Tailwind 3.4 + react-router 6；状态用 Context，不引入全局状态库。
- 视觉与交互严格遵守 [ui-design-spec.md](ui-design-spec.md)（Bento 卡片、间距、动效、移动端底部标签栏）。
- 所有请求经 `src/api/client.js`；页面必须同时处理 loading / error / empty 三态——错误态用 `components/ErrorState.jsx`（带重试入口），禁止 `catch {}` 后不留任何用户可见反馈。
- 轮询：页面级轮询在 `useEffect` cleanup 中必须清除 interval/timeout；能复用 Layout 顶栏扫描状态轮询的不要另起。
- 新增页面复用 `Layout` + `BentoCard` 等既有组件，适配移动端（RULES #4）。

## 5. API 约定

- 路径前缀 `/api`，REST-ish：资源 + 子资源动词（如 `POST /api/images/{id}/ignored`）。
- `/api/health` 与 `/api/auth/*` 免鉴权，其余 `/api/*` 一律经 `auth.Middleware`。
- 响应统一 JSON；列表接口带 `total` 支持分页（新增列表接口照此办理）。
- 版本号统一从 `internal/api` 的单一常量/ldflags 注入读取，禁止在多处硬编码。

## 6. 安全基线

- 口令必须经慢哈希（bcrypt/argon2id）存储，禁止单轮 SHA-256；旧格式哈希在登录成功时无感升级（见 `auth.NeedsRehash`）。
- 秘钥一律来自环境变量；JWT secret 入库仅为免重启续用，属已知权衡，不得再新增明文落库的敏感信息。
- 登录限流按 IP（`RemoteAddr`）；反代部署下的行为与限制见 tech-debt.md。
- 镜像默认挂载 `docker.sock`（宿主机 root 等价权限），客户端只发起 GET（`/_ping`、`/images/json`），任何时候不得给该客户端增加写操作。
- 提交前隐私检查按 RULES #9 执行。

## 7. Git 与版本

- 提交信息：`feat:` / `fix:` / `docs:` / `chore:` / `ci:` / `perf:` / `refactor:` 前缀 + 中文主题，一次提交一个独立功能点（RULES #7）。
- 版本号语义化、由提交自动推导：`feat:` → MINOR，`fix:`/`perf:` → PATCH，`BREAKING CHANGE` → MAJOR（打 tag 前征得确认）；仅 docs/chore/test/refactor 不打 tag。详见 RULES #12。
- 已推送的 tag 不可删除或移动（对应 GHCR 镜像标签永久保留供回滚）。

## 8. CI / 构建

- 唯一工作流 `.github/workflows/build.yml`：`test` job 为质量门禁（`gofmt` 检查、`go vet`、`go test -race`、前端 `npm ci && npm run build`），通过后才运行 `build` job 产出镜像——buildx 多架构（amd64/arm64），`v*` tag 产 `x.y.z`/`x.y`/`x`/`latest`，main 分支产 `edge`。
- Dockerfile 三阶段（node 构建 → go 构建 → alpine 运行）；注意 `npm install` 与 `go mod tidy` 在镜像构建中执行，属已知非确定性来源（整改项见 tech-debt.md）。

## 9. 文档一致性

代码行为变更时必须同步：README、`docs/ui-design-spec.md`（涉及 UI）、`docs/development.md`（涉及规范/流程）、包内注释（RULES #8）。`docs/code-review.md` 是历史审查记录，不随代码更新，允许与现状过时，但顶部不得混入无关内容。

## 10. 本地开发速查

```bash
# 后端（:54321）
cd backend && go run ./cmd/server
# 前端（:5173，/api 代理到 54321）
cd frontend && npm install && npm run dev
# 质量检查
cd backend && gofmt -l . && go vet ./... && go test ./...
cd frontend && npm run build
# 全栈容器
docker compose up -d --build
```
