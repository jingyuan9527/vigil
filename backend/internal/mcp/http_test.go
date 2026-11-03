package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "vigil_mcp_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newHTTPTestServer 打开测试库并注册一个已知哈希的 Key。
func newHTTPTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := openTestStore(t)
	// 与 HashAPIKey 相同口径预置一条密钥记录
	if _, err := st.CreateMCPKey("test", "prefix", HashAPIKey(testKey)); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	ts := httptest.NewServer(HTTPHandler(st))
	t.Cleanup(ts.Close)
	return ts
}

func TestHTTPAuthRequired(t *testing.T) {
	ts := newHTTPTestServer(t)

	post := func(key string, body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}

	// 无密钥 / 错误密钥 → 401
	for _, key := range []string{"", "vigil_mcp_wrong"} {
		res := post(key, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("key=%q status = %d, want 401", key, res.StatusCode)
		}
		var resp map[string]any
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			t.Fatalf("decode 401 body: %v", err)
		}
		if resp["error"] == nil {
			t.Fatal("401 body should contain jsonrpc error")
		}
	}

	// X-API-Key 头同样可用
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("X-API-Key", testKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("X-API-Key status = %d", res.StatusCode)
	}
}

func TestHTTPProtocolFlow(t *testing.T) {
	ts := newHTTPTestServer(t)

	call := func(body string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testKey)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer res.Body.Close()
		var resp map[string]any
		if res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return res.StatusCode, resp
	}

	// initialize
	status, resp := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if status != http.StatusOK || resp["error"] != nil {
		t.Fatalf("initialize: %d %v", status, resp)
	}
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}

	// tools/list：与 stdio 相同的 7 个工具
	_, resp = call(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tools count = %d", len(tools))
	}

	// 通知（无 id）→ 202 且无响应体
	status, _ = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", status)
	}

	// 未知方法 → JSON-RPC 错误帧
	_, resp = call(`{"jsonrpc":"2.0","id":3,"method":"no/such"}`)
	if resp["error"].(map[string]any)["code"].(float64) != errCodeMethod {
		t.Fatalf("unknown method error = %v", resp["error"])
	}

	// 非 POST → 405
	res, _ := http.Get(ts.URL + "/mcp")
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp status = %d", res.StatusCode)
	}

	// 坏 JSON → 400
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewBufferString("not-json"))
	req.Header.Set("Authorization", "Bearer "+testKey)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", res2.StatusCode)
	}
}
