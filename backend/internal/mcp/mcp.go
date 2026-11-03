// Package mcp 实现面向 AI 助手（MCP 客户端）的本地 stdio 服务器。
//
// 设计原则：
//   - 只暴露只读与标记类工具（查看镜像/通知/统计、忽略标记、已读标记），
//     不提供渠道配置、认证、删除等危险或低频管理操作；
//   - 协议为 MCP 规范的最小 JSON-RPC 2.0 子集（initialize / ping /
//     tools/list / tools/call），走 stdio 换行分隔帧，零第三方依赖；
//   - 与 Web 服务共用同一个 SQLite 库（WAL 模式支持多进程并发），
//     因此 stdio 形态天然是本机单用户场景，无需再做 HTTP 鉴权。
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"

	"vigil/internal/store"
)

// serverInfo 是 MCP initialize 响应中的服务器标识。
// 与 api.health 保持一致的版本口径。
const serverName = "vigil"
const serverVersion = "1.0.0"

// supportedVersions 是本服务器可回应的 MCP 协议版本。
// 客户端请求其中之一则原样回显，否则回退到最新已知版本。
var supportedVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

const fallbackVersion = "2025-06-18"

// RPCError 是 JSON-RPC 2.0 协议层错误（区别于工具执行错误，
// 后者按 MCP 规范放入 tools/call 结果的 isError 字段）。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	errCodeParse     = -32700
	errCodeMethod    = -32601
	errCodeParams    = -32602
	errCodeInternal  = -32603
	errMsgParse      = "parse error"
	errMsgMethod     = "method not found"
	errMsgParams     = "invalid params"
	errMsgInternal   = "internal error"
	errMsgStoreFault = "数据库暂不可用，请稍后再试"
)

// incoming 是一行 JSON-RPC 请求/通知的骨架。
type incoming struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// RunStdio 在 r（stdin）上逐行读取 JSON-RPC 消息，处理后将响应写入 w（stdout）。
// stdout 只承载协议帧：任何日志一律走 stderr（log 默认输出即 stderr）。
// 客户端关闭 stdin（EOF）后正常返回。
func RunStdio(r io.Reader, w io.Writer, st *store.Store) error {
	srv := NewServer(st)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 单帧上限 4MB，防超大输入撑爆内存
	bw := bufio.NewWriter(w)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		// 无法关联 id 的帧（坏行/通知）不响应：通知按规范本就无响应，
		// 坏行无 id 可回，写回只会产生对端无法关联的噪音。
		var req incoming
		if err := json.Unmarshal(line, &req); err != nil || req.JSONRPC != "2.0" || len(req.ID) == 0 || bytes.Equal(req.ID, []byte("null")) {
			continue
		}
		result, rpcErr := srv.Handle(req.Method, req.Params)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		b, err := json.Marshal(resp)
		if err != nil {
			// result 序列化失败属于服务器缺陷，降级为协议内部错误帧
			b, _ = json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"error": RPCError{Code: errCodeInternal, Message: errMsgInternal},
			})
		}
		if _, err := bw.Write(b); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Handle 分发一个已确认带 id 的 JSON-RPC 请求，返回结果或协议错误。
// stdio 与 HTTP 两种传输共用此入口。
func (s *Server) Handle(method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "initialize":
		return s.initialize(params)
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolDescriptors()}, nil
	case "tools/call":
		return s.callTool(params)
	default:
		return nil, &RPCError{Code: errCodeMethod, Message: errMsgMethod}
	}
}

// initialize 完成 MCP 握手：协商协议版本并声明工具能力。
func (s *Server) initialize(params json.RawMessage) (any, *RPCError) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: errCodeParams, Message: errMsgParams}
		}
	}
	version := fallbackVersion
	if supportedVersions[p.ProtocolVersion] {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		"instructions": "Vigil 是一个 Docker 镜像更新监控工具。可通过工具查看受监控镜像及其更新状态、" +
			"更新通知与统计概览，并可执行两类标记操作：忽略/取消忽略某个镜像的更新提醒、" +
			"把通知标记为已读。所有数据与 Web 界面共用同一数据库，实时一致。",
	}, nil
}

// callTool 处理 tools/call。按 MCP 规范，工具执行失败（业务错误）不作为
// 协议错误返回，而是以 isError=true 的文本结果回给客户端，便于模型理解并自纠。
func (s *Server) callTool(params json.RawMessage) (any, *RPCError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &RPCError{Code: errCodeParams, Message: errMsgParams}
	}
	tool := s.tool(p.Name)
	if tool == nil {
		return nil, &RPCError{Code: errCodeMethod, Message: "unknown tool: " + p.Name}
	}
	var args map[string]any
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, &RPCError{Code: errCodeParams, Message: errMsgParams}
		}
	}
	out, err := tool.Handler(args)
	if err != nil {
		log.Printf("mcp tool %s: %v", p.Name, err)
		return errorResult(err.Error()), nil
	}
	b, merr := json.Marshal(out)
	if merr != nil {
		return errorResult(errMsgInternal), nil
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(b)}},
	}, nil
}

// errorResult 构造 isError=true 的文本结果（工具执行失败形态）。
func errorResult(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}
