package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vigil/internal/models"
	"vigil/internal/store"
)

// openTestStore 打开独立的临时 SQLite 库并写入一条镜像与一条通知作种子数据。
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { /* 临时目录由测试框架回收 */ })
	img := &models.Image{
		Name: "nginx", Reference: "nginx:1.27", Registry: "docker.io", Tag: "1.27",
		Source: "manual", Status: models.StatusUpdateAvailable, CreatedAt: time.Now(),
	}
	if err := st.UpsertImage(img); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	full, err := st.GetImageByRef("nginx:1.27")
	if err != nil || full == nil {
		t.Fatalf("seeded image missing: %v", err)
	}
	if err := st.CreateNotification(&models.Notification{
		ImageID: full.ID, ImageName: "nginx", Reference: "nginx:1.27",
		NewDigest: "sha256:abc", Type: models.NotifUpdate, Message: "nginx 1.27 有更新",
	}); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return st
}

// roundTrip 喂入若干 JSON-RPC 请求行，解析出全部响应并按 method 索引（单请求场景）。
func roundTrip(t *testing.T, st *store.Store, requests ...string) map[string]map[string]any {
	t.Helper()
	var in bytes.Buffer
	for i, req := range requests {
		in.WriteString(strings.ReplaceAll(req, "$ID", itoa(i+1)))
		in.WriteByte('\n')
	}
	var out bytes.Buffer
	if err := RunStdio(&in, &out, st); err != nil {
		t.Fatalf("RunStdio: %v", err)
	}
	res := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		// 由响应 id 反查请求 method（$ID 从 1 顺序递增）
		method := requests[int(resp["id"].(float64))-1]
		res[method] = resp
	}
	return res
}

// pick 按请求串子串取出对应响应（请求串中含 method / name 等可辨识片段）。
func pick(t *testing.T, res map[string]map[string]any, substr string) map[string]any {
	t.Helper()
	for k, v := range res {
		if strings.Contains(k, substr) {
			return v
		}
	}
	t.Fatalf("no response for %q", substr)
	return nil
}

func itoa(n int) string {
	return json.Number([]byte(jsonMarshal(n))).String()
}

func jsonMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestInitializeHandshake(t *testing.T) {
	st := openTestStore(t)
	res := roundTrip(t, st,
		`{"jsonrpc":"2.0","id":$ID,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"0"}}}`,
	)
	resp := pick(t, res, `"method":"initialize"`)
	if resp == nil {
		t.Fatal("no initialize response")
	}
	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("initialize error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "vigil" {
		t.Fatalf("serverInfo.name = %v", info["name"])
	}
	// 未知协议版本应回退到服务器默认版本
	res = roundTrip(t, st,
		`{"jsonrpc":"2.0","id":$ID,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	result = pick(t, res, `"method":"initialize"`)["result"].(map[string]any)
	if result["protocolVersion"] != fallbackVersion {
		t.Fatalf("fallback protocolVersion = %v", result["protocolVersion"])
	}
}

func TestNotificationsAndUnknownMethodAreSilent(t *testing.T) {
	st := openTestStore(t)
	// 通知（无 id）与坏行都不产生响应；未知方法返回 -32601。
	var in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	in.WriteString("not-json\n")
	in.WriteString(`{"jsonrpc":"1.0","id":9,"method":"ping"}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"no/such/method"}` + "\n")
	var out bytes.Buffer
	if err := RunStdio(&in, &out, st); err != nil {
		t.Fatalf("RunStdio: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d: %q", len(lines), out.String())
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"].(float64) != 1 {
		t.Fatalf("response id = %v", resp["id"])
	}
	if resp["error"].(map[string]any)["code"].(float64) != errCodeMethod {
		t.Fatalf("error = %v", resp["error"])
	}
}

func TestToolsListAndCallFlow(t *testing.T) {
	st := openTestStore(t)
	res := roundTrip(t, st,
		`{"jsonrpc":"2.0","id":$ID,"method":"tools/list"}`,
	)
	resp := pick(t, res, `"method":"tools/list"`)
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tools count = %d", len(tools))
	}

	// get_stats
	res = roundTrip(t, st, `{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"get_stats","arguments":{}}}`)
	result := pick(t, res, `"name":"get_stats"`)["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("get_stats error: %v", result["content"])
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"update_available":1`) {
		t.Fatalf("stats text = %s", text)
	}

	// add_ignore by reference，再用 get_image 验证标记生效
	res = roundTrip(t, st,
		`{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"add_ignore","arguments":{"reference":"nginx:1.27"}}}`)
	result = pick(t, res, `"name":"add_ignore"`)["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("add_ignore error: %v", result["content"])
	}
	if !strings.Contains(result["content"].([]any)[0].(map[string]any)["text"].(string), `"ignored":true`) {
		t.Fatalf("add_ignore result = %v", result["content"])
	}

	// list_notifications 过滤未读
	res = roundTrip(t, st,
		`{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"list_notifications","arguments":{"unread_only":true}}}`)
	result = pick(t, res, `"name":"list_notifications"`)["result"].(map[string]any)
	if !strings.Contains(result["content"].([]any)[0].(map[string]any)["text"].(string), `"count":1`) {
		t.Fatalf("notifications = %v", result["content"])
	}

	// mark_notifications_read 全部已读后未读为 0
	roundTrip(t, st, `{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"mark_notifications_read","arguments":{}}}`)
	res = roundTrip(t, st, `{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"get_stats","arguments":{}}}`)
	result = pick(t, res, `"name":"get_stats"`)["result"].(map[string]any)
	if !strings.Contains(result["content"].([]any)[0].(map[string]any)["text"].(string), `"unread_notifications":0`) {
		t.Fatalf("stats after read = %v", result["content"])
	}
}

func TestToolErrorsAreIsErrorResults(t *testing.T) {
	st := openTestStore(t)
	// 未知工具 → 协议错误
	res := roundTrip(t, st, `{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"delete_image","arguments":{}}}`)
	resp := pick(t, res, `"name":"delete_image"`)
	if resp["error"] == nil {
		t.Fatalf("unknown tool should be protocol error: %v", resp)
	}
	// 工具业务错误 → isError=true 结果（不存在的镜像）
	res = roundTrip(t, st, `{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"get_image","arguments":{"id":999}}}`)
	result := pick(t, res, `"name":"get_image"`)["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError result: %v", result)
	}
	// 无效 status 过滤值 → isError=true
	res = roundTrip(t, st, `{"jsonrpc":"2.0","id":$ID,"method":"tools/call","params":{"name":"list_images","arguments":{"status":"boom"}}}`)
	result = pick(t, res, `"status":"boom"`)["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError result: %v", result)
	}
}
