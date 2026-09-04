# DockMon 代码审查报告（忽略功能配套核查）

审查基线：`main` @ `02dc875`，全量 `go build ./...`、`go vet ./...`、`go test ./...`、`frontend npm run build` 均已执行。
本次附带核查「忽略镜像」实现前后发现的既有缺陷，按 P0/P1/P2 分级，均已引用文件与行号。

## 结论速览

| 级别 | 数量 | 说明 |
|------|------|------|
| P0 | 0 | 无数据丢失 / 安全漏洞 / 主流程崩溃 |
| P1 | 1 | 仓库既有 API 单测必然失败（已随本次一并修复） |
| P2 | 1 | 非阻塞观察项（2 项已随治理处理：docker 残留清理、Compare 失效 id 空态） |

---

## P1（已修复）— API 集成测试自始无法通过

- **位置**：`backend/internal/api/api_test.go:50-56`
- **证据**：`TestAPIRouter` 用无认证的 `http.Post(srv.URL+"/api/images", ...)`，断言 `202`；
  但 `backend/internal/auth/auth.go:118-146` 的 `Middleware` 对除 `/api/health`、`/api/auth/*` 外的所有 `/api/*` 一律要求 `Bearer` token，未带则返回 `401`。
- **基线实测**（本次改动前）：`go test ./internal/api` 报 `api_test.go:56: POST /api/images status = 401, want 202` → **FAIL**。即仓库测试从未全绿过。
- **修复**：测试内先 `POST /api/auth/setup` 建立管理员获取 token，`get`/`POST`/`PUT` 均带 `Authorization: Bearer`；并顺带将镜像列表断言对象补上 `ignored` 字段、新增忽略开关往返断言。
- **验证**：`go test ./internal/api` 通过。

> 说明：`/api/health` 无鉴权是可接受的设计（健康检查常用于探活），故保留原中间件行为，只修正测试缺 token 的问题。

---

## P2（观察项）

> 注：原 P2「本机已删除 docker 镜像残留」与「Compare 失效 id 加载态」均已处理（见下两节），此处仅剩一项待办。

1. **镜像状态过滤的语义边界**
   - B 语义（忽略=仍扫描不提醒）下，被忽略镜像在「有更新」筛选里仍会出现且状态为 `update-available`，靠卡片内"已忽略"徽标区分。
   - 这是有意为之（见需求确认）；仅提示后续若用户反馈"忽略了怎么还橙色"，可提供"忽略项默认折叠/置灰"的视觉强化。

## 已处理的附带项：本机已删除 docker 镜像的库内残留

- **位置**：`backend/internal/scanner/scanner.go` 新增 `pruneRemovedDockerImages`；`backend/internal/store/store.go` 新增 `MarkDockerImagesMissing`。
- **行为**：每轮扫描结束时（Docker 可达时），将 `source='docker'` 且本机已不存在的行标记为 `stale`（缺失）；**manual / watch 来源与 ignored 项不受影响**；Docker 不可用或存活引用为空时不贸然清理。
- **单测**：`TestMarkDockerImagesMissing`（store）已补充，验证存活 docker / manual 不受影响、已删 docker 置 stale、空 liveRefs 安全无操作。

## 已处理的附带项：Compare 页失效 id 卡加载态

- **位置**：`frontend/src/pages/Compare.jsx` 详情区改为四态（未选 / 加载中 / 不存在 / 正常），避免无 id 或失效 id 时无限转圈。
- **行为**：无 id → "请从左侧选择"；加载中 → Spinner；加载完成取不到 detail → "镜像不存在或已被移除"；否则渲染详情。
- **验证**：`npm run build` 通过。

---

## 本次功能改动范围（供复核）

| 文件 | 内容 |
|------|------|
| `backend/internal/models/models.go` | `Image` 增加 `Ignored bool` |
| `backend/internal/store/store.go` | images 表加 `ignored` 列 + 旧库幂等 `ALTER`；`SetIgnored(id,bool)`；读/列表带出 ignored；`UpsertImage` **不写** ignored（防扫描覆盖） |
| `backend/internal/scanner/scanner.go` | `process` 在通知分支判断 `existing.Ignored`，忽略项仍扫描/更新状态但**不建通知、不发钉钉** |
| `backend/internal/api/api.go` | 新增 `PUT /api/images/{id}/ignored` |
| `backend/internal/store/store_test.go` | `TestIgnored`：set/get/list 往返 + upsert 不重置 |
| `backend/internal/scanner/scanner_integration_test.go` | `TestIgnoredSuppressesNotification`：忽略后远端变化 → 仍 update-available、不产生通知 |
| `backend/internal/api/api_test.go` | 修复缺 token + 增加忽略开关往返断言 |
| `frontend/src/api/client.js` | `setIgnored(id, ignored)` |
| `frontend/src/pages/Images.jsx` | "已忽略"徽标与说明、忽略/恢复按钮、筛选器加「已忽略」 |

验证结果：`go build` / `go vet` / `go test ./...` 全绿；`frontend npm run build` 成功。
