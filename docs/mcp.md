# Vigil MCP 服务器

Vigil 内置一个 MCP（Model Context Protocol）服务器，让 AI 助手（Claude Desktop、CodeBuddy、OpenCode 等）可以直接回答"我哪些镜像有更新？"这类问题，并代为执行忽略、标记已读等轻量操作。提供 stdio 与 HTTP 两种接入形态。

## 设计边界

- **只读 + 标记**：只暴露查看与标记两类工具，不提供通知渠道配置、管理员认证、删除镜像等危险或低频管理操作（这些请使用 Web 界面或 REST API）。
- **零新增依赖**：协议实现为 MCP 规范的最小 JSON-RPC 2.0 子集（`initialize` / `ping` / `tools/list` / `tools/call`），不引入第三方 MCP SDK。
- **与 Web 服务共用数据**：直接读写 `DB_PATH` 指向的同一个 SQLite 库（WAL 模式支持多进程并发），Web 端与 MCP 端看到的数据实时一致。
- **双形态鉴权**：stdio 仅限本机进程调用，无需鉴权；HTTP `/mcp` 端点使用页面生成的 API Key（Bearer 头），随时可吊销。

## 两种接入形态

### HTTP（远程/容器部署，推荐）

Web 服务在同一端口挂载了无状态的 MCP Streamable HTTP 端点 `/mcp`。在页面「设置 → MCP 接入」生成 API Key 后，请求携带 `Authorization: Bearer <API Key>`（或 `X-API-Key` 头）即可访问：

```json
{
  "mcpServers": {
    "vigil": {
      "type": "http",
      "url": "http://your-server:54321/mcp",
      "headers": { "Authorization": "Bearer vigil_mcp_xxxxxxxx..." }
    }
  }
}
```

要点：

- Key 只在生成时完整展示一次，库内仅存 SHA-256 哈希；列表只保留前缀供辨认。
- 吊销立即生效（每次请求都实时校验）。
- 工具能力与 stdio 完全一致（同一份工具注册表）。

### stdio（本机/与 Vigil 同机部署）

```bash
# 数据库路径沿用 DB_PATH 环境变量（默认 /data/monitor.db）
vigil mcp
# 源码运行
DB_PATH=./monitor.db go run ./cmd/server mcp
```

stdio 形态面向本机进程调用，无需 API Key。

### Claude Desktop（stdio 示例，claude_desktop_config.json）

```json
{
  "mcpServers": {
    "vigil": {
      "command": "/usr/local/bin/vigil",
      "args": ["mcp"],
      "env": { "DB_PATH": "/data/monitor.db" }
    }
  }
}
```

### CodeBuddy / 通用（.mcp.json，容器内进程示例）

```json
{
  "mcpServers": {
    "vigil": {
      "command": "docker",
      "args": ["exec", "vigil", "/app/vigil", "mcp"],
      "env": { "DB_PATH": "/data/monitor.db" }
    }
  }
}
```

> 两种形态的数据都来自同一个 SQLite 库，Web 界面与 AI 助手看到的数据实时一致。

## 工具清单

| 工具 | 类型 | 入参 | 说明 |
|------|------|------|------|
| `list_images` | 只读 | `status?`（unknown / up-to-date / update-available / stale） | 镜像列表及更新状态 |
| `get_image` | 只读 | `id` | 镜像详情：摘要对比、检测模式、版本时间线（最近 50 条） |
| `list_notifications` | 只读 | `unread_only?`、`cursor?` | 更新通知列表，每页 100 条，结果含 `next_cursor` |
| `get_stats` | 只读 | 无 | 镜像状态分布、未读数、最近扫描信息 |
| `add_ignore` | 标记 | `id` 或 `reference` 二选一 | 忽略更新提醒（扫描继续但不通知） |
| `remove_ignore` | 标记 | `id` 或 `reference` 二选一 | 取消忽略 |
| `mark_notifications_read` | 标记 | `id?`（缺省标记全部未读） | 标记通知已读 |

工具执行失败（如镜像不存在、参数非法）按 MCP 规范返回 `isError: true` 的文本结果，便于模型理解并自我纠正；协议层错误（未知方法等）返回 JSON-RPC 错误帧。

## 实现

- `backend/internal/mcp/mcp.go`：stdio 帧循环、握手协商、协议分发
- `backend/internal/mcp/tools.go`：7 个工具定义与处理函数（基于 `internal/store`）
- `backend/internal/mcp/http.go`：Streamable HTTP 无状态传输 + API Key 鉴权
- `backend/internal/api/mcpkeys.go`：Key 生成/列表/吊销 REST 接口（密钥仅存 SHA-256 哈希）
- `backend/internal/store/keys.go`：`mcp_api_keys` 表 CRUD 与节流式 last_used_at
- `frontend/src/components/MCPKeyManager.jsx`：设置页密钥管理卡片
- 测试：`mcp_test.go`（stdio）、`http_test.go`（HTTP 鉴权与协议流）、`keys_test.go`（存储层）

协议版本协商：客户端请求 `2024-11-05` / `2025-03-26` / `2025-06-18` 之一则原样回显，否则回退到 `2025-06-18`。
