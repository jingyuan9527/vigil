package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"vigil/internal/store"
)

// HTTPHandler 返回 MCP Streamable HTTP 传输的无状态实现，挂载于 `/mcp`：
//   - 仅接受 POST（客户端→服务器消息），响应单一 JSON-RPC 帧（不提供 SSE 流，
//     本服务器的工具调用都是短请求，单帧响应即可满足主流客户端）；
//   - 鉴权使用页面「设置 → MCP 接入」生成的 API Key：`Authorization: Bearer <key>`
//     或 `X-API-Key: <key>`；库内仅存 SHA-256 哈希，明文只在创建时展示一次；
//   - 无会话状态（stateless）：每个请求独立处理，客户端无需携带会话 ID。

const maxBodyBytes = 4 << 20 // 单请求上限 4MB，与 stdio 帧上限一致

// extractAPIKey 从请求头提取 API Key（兼容两种常见形态）。
func extractAPIKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// HashAPIKey 计算 API Key 的 SHA-256 十六进制哈希。
// 生成（api 层）与校验（此处）共用同一实现，保证哈希口径唯一。
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// hashAPIKey 供包内调用的别名。
func hashAPIKey(key string) string { return HashAPIKey(key) }

// HTTPHandler 构造 /mcp 的 HTTP 处理器。
func HTTPHandler(st *store.Store) http.Handler {
	srv := NewServer(st)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 鉴权：Key 哈希必须命中已注册记录；未命中一律 401（不区分「不存在/已吊销」，
		// 避免向持有无效密钥的调用方透露记录状态）。
		key := extractAPIKey(r)
		if _, aerr := st.UseMCPKey(hashAPIKey(key)); key == "" || aerr != nil {
			writeRPCError(w, http.StatusUnauthorized, &RPCError{Code: errCodeParams, Message: "invalid API key"})
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			writeRPCError(w, http.StatusBadRequest, &RPCError{Code: errCodeParse, Message: errMsgParse})
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.JSONRPC != "2.0" {
			writeRPCError(w, http.StatusBadRequest, &RPCError{Code: errCodeParse, Message: errMsgParse})
			return
		}

		// 通知（无 id）：按规范返回 202 且无响应体
		if len(req.ID) == 0 || strings.EqualFold(string(req.ID), "null") {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		result, rpcErr := srv.Handle(req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// writeRPCError 输出一个 id=null 的 JSON-RPC 错误帧（用于请求无法关联 id 的失败：
// 鉴权失败、解析失败）。
func writeRPCError(w http.ResponseWriter, status int, rpcErr *RPCError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var id any // null
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id, "error": rpcErr,
	})
}
