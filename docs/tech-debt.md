# 技术债与路线图

排查基线：`main` @ `8b4145b`。整改轮次：2026-09-06（P0 全清、P1 大部、P2 三项，见下方勾选）。修掉一项就在该项打勾并注明提交号。

## P0 — 安全与正确性

- [x] **口令哈希过弱** `backend/internal/auth/auth.go`（2026-09-06）
  已改为 bcrypt（`golang.org/x/crypto/bcrypt`，DefaultCost）；旧格式哈希登录成功时无感升级（`NeedsRehash`），新增 `auth_test.go` 覆盖新旧格式与限流。

- [x] **registry 客户端热替换数据竞争** `backend/internal/api/api.go`、`backend/internal/scanner/scanner.go`（2026-09-06）
  双方 `reg` 字段改为 `atomic.Pointer[registry.Client]`，设置页热替换不再与 handler/扫描 goroutine 竞争。

- [x] **DB 故障窗口可被接管管理员** `backend/internal/store/auth.go` + `backend/internal/api/api.go`（2026-09-06）
  `HasAdmin`/`GetAdmin` 改为返回错误；`/api/auth/setup`、`/api/auth/login` 在查询失败时返回 503，setup 窗口不再重新开放。

- [x] **登录限流缺陷** `backend/internal/auth/ratelimit.go`（2026-09-06）
  整改中发现并修复了更严重的隐藏 bug：`RecordFailure`/`Allow` 对零值 `until` 的 `now.After` 恒真，**导致每次失败都重建计数，锁定机制从未真正生效过**。另新增全局失败上限（100 次/窗口，兜底反代共享出口 IP 场景）与空闲条目清理（追踪 IP 上限 4096，防内存无限增长），补单测。

- [x] **内部错误串泄露给客户端** `backend/internal/api/api.go`（2026-09-06）
  全部 500 收敛到 `serverError`（固定文案 + `log.Printf` 带方法/路径），DB 故障走 `storeUnavailable`（503）。

## P1 — 质量与健壮性

- [x] **CI 无质量门禁** `.github/workflows/build.yml`（2026-09-06）
  新增 `test` job：`gofmt -l` 检查、`go vet`、`go test -race`、前端 `npm ci && npm run build`；`build` job `needs: test`，不过门禁不出镜像。

- [x] **删除镜像不清派生数据** `backend/internal/store/images.go`（2026-09-06）
  `DeleteImage` 现同步清理 `image_digest_notified`（去重基线）；通知行保留作事件历史（冗余字段可独立展示）。重新添加同名引用会以全新基线开始，不再被旧行为干扰。

- [x] **扫描错误不可见** `backend/internal/scanner/scanner.go`（2026-09-06）
  逐镜像错误聚合写入 `scans.error`（按 rune 截断防断字）并输出日志；`CreateScan` 失败不再静默。

- [x] **前端错误状态缺失**（2026-09-06）
  新增 `components/ErrorState.jsx`（错误 + 重试）；Dashboard / Images / Compare / Settings / Notifications 的列表加载全部接入三态，`catch {}` 静默清零。

- [x] **强制扫描轮询器泄漏** `frontend/src/pages/Notifications.jsx`（2026-09-06）
  轮询器登记到 `pollRef`，卸载时 `clearInterval`；重复触发强制扫描前先清旧轮询。

- [x] **gofmt 漂移**（2026-09-06）全库 `gofmt -w`，CI 门禁含 fmt 检查防复发。

- [ ] **镜像构建不可复现**：Dockerfile 用 `npm install`（应为 `npm ci`）且在构建期跑 `go mod tidy`；`GOPROXY` 钉死 goproxy.cn 对海外 CI 不友好。
- [ ] **`HasAdmin` 之外的查询吞错**：`Stats` 子项已补日志；其余影响 UI 的查询错误应至少留痕。
- [ ] **版本号硬编码** `backend/internal/api/api.go`、`internal/registry/registry.go`：应由 `-ldflags` 从 git tag 注入，否则 RULES #12 的 semver 体系与实际产物脱节。

## P2 — 可维护性

- [x] **`store/store.go` 拆分**（2026-09-06）：按域拆为 store/images/versions/seentags/notifications/scans/auth/settings 八个文件；行扫描统一走 `imageFromScan`，消灭 `ListImages` 的复制粘贴。
- [x] **钉钉推送三处复制**（2026-09-06）：抽为 `Scanner.notifyUpdate` / `notifyNewTag`，后续多通知渠道在 helper 上扩展。
- [x] **命名分裂 dockmon vs vigil**（2026-09-06）：Go module、包名、cookie（`vigil_token`，需重新登录一次）、UA、UI/钉钉文案、Dockerfile 二进制名、compose 服务名统一为 vigil。**数据卷名 `dockmon-data` 刻意保留**——改名会使已有部署的监控数据失联（compose 已加注释说明）。
- [ ] **`/api/scans` 硬编码 limit 20**：接入分页参数。
- [ ] **`testDingTalk` 是已登录用户的 SSRF 原语**：管理员-only 风险低，可加 URL scheme 白名单（仅 https）。
- [ ] **无 CSRF token**（仅 SameSite=Lax）与**通知渠道测试接口无额外限流**：随多渠道改造一并考虑。
- [ ] **前端残留 eslint-disable 注释**（Notifications/Images/Compare）：装 ESLint 或删注释，随 CI 门禁一起加。

## 后续开发方向建议

**短期（1-2 个迭代）——把地基打牢**
1. ~~CI 质量门禁 + P0 五项安全整改~~（已完成）；剩余：Dockerfile 复现性、版本号 ldflags 注入。
2. 结构化日志：标准库 `log/slog` 替换 `log`，与 RULES #3「有迹可查」对齐。
3. 通知渠道抽象：把 `internal/notification` 改成 `Channel` 接口（站内/钉钉先实现），为所有后续渠道铺路。

**中期——对用户最有感知的功能**
1. **多通知渠道**：接口化之后接 Telegram Bot、企业微信、Server酱、通用 webhook（含自定义模板），是同类工具（diun/wud）用户最常要的。
2. **邮件通知 + 通知摘要/静默时段**：避免夜间刷屏。
3. **Prometheus /metrics**：镜像数、有更新数、扫描耗时、注册表错误率；纳入 `/api` 之外的独立端点。监控工具自带 metrics 是高频诉求。
4. **监控项分组与标签**：镜像多了以后列表页需要按项目/主机分组过滤。
5. **API Token**：给自动化脚本一个非 Cookie 的 Bearer token 管理入口。

**长期——按需评估**
1. 前端迁移 TypeScript：页面量再翻一倍前值得做，迁移可增量（新文件 .tsx）。
2. 多用户 / 只读访客角色：当前单管理员模型够用，除非出现真实需求。
3. Watchtower 式自动更新：与「监控提醒」定位差异大，会显著放大 docker.sock 风险面，除非明确要做再评估。
4. 多 Docker 主机监控（TCP remote dockerd 已有雏形，主要补 UI 与鉴权模型）。

顺序原则：每一步都有测试与 CI 兜底后再进下一步；不做与当前改动无关的顺手优化（RULES #5）。
