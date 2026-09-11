package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"vigil/internal/config"
	"vigil/internal/registry"
	"vigil/internal/scanner"
	"vigil/internal/store"
)

// helperOpts 便捷构造测试环境：起真实路由 + 初始化管理员 token。
// webhookSrv 返回一个可被渠道配置引用的假推送端点（记录收到请求的数量）。
func newChannelsEnv(t *testing.T) (st *store.Store, srv *httptest.Server, tok string) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	live := config.NewLiveSettings(3600, false, "", true)
	sc := scanner.New(&config.Config{DisableDefault: true}, st, nil, registry.NewClient(false), live)
	router := NewRouter("./static", st, sc, registry.NewClient(false), live, []byte("test-secret"))
	srv = httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return st, srv, setupToken(t, srv.URL)
}

func doReq(t *testing.T, method, url, token string, body interface{}) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestChannelsCRUDAndTest(t *testing.T) {
	// 假推送端点：统计收到多少条请求，并记录 dingtalk JSON
	var mu sync.Mutex
	received := 0
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received++
		mu.Unlock()
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	_, srv, tok := newChannelsEnv(t)
	base := srv.URL + "/api/channels"

	// 创建：无 webhook 配置 → 校验失败
	if code, _ := doReq(t, "POST", base, tok, map[string]interface{}{"kind": "dingtalk", "name": "x", "enabled": true, "config": map[string]string{}}); code != http.StatusBadRequest {
		t.Fatalf("create without webhook status = %d, want 400", code)
	}
	// 创建：未知 kind → 400
	if code, _ := doReq(t, "POST", base, tok, map[string]interface{}{"kind": "nope"}); code != http.StatusBadRequest {
		t.Fatalf("create unknown kind status = %d, want 400", code)
	}
	// 创建成功
	code, body := doReq(t, "POST", base, tok, map[string]interface{}{
		"kind": "dingtalk", "name": "运维群", "enabled": true,
		"config": map[string]string{"webhook": webhook.URL},
	})
	if code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", code, body)
	}
	var created channelDTO
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created: %v (%s)", err, body)
	}

	// 列表：kinds 全量下发，且含新渠道
	code, body = doReq(t, "GET", base, tok, nil)
	if code != http.StatusOK {
		t.Fatalf("list status = %d", code)
	}
	var list struct {
		Channels []channelDTO `json:"channels"`
		Kinds    []string     `json:"kinds"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal list: %v (%s)", err, body)
	}
	if len(list.Channels) != 1 || list.Channels[0].ID != created.ID {
		t.Fatalf("list wrong: %+v", list.Channels)
	}
	if len(list.Kinds) < 5 {
		t.Errorf("kinds should include registered providers, got %v", list.Kinds)
	}

	// 各业务 kind 都能创建（避免 frontend 可以选择但后端拒绝）
	kinds := []struct {
		kind   string
		config map[string]interface{}
	}{
		{kind: "webhook", config: map[string]interface{}{"url": webhook.URL}},
		{kind: "wecom", config: map[string]interface{}{"webhook": webhook.URL}},
		{kind: "feishu", config: map[string]interface{}{"webhook": webhook.URL}},
		{kind: "telegram", config: map[string]interface{}{"bot_token": "123:x", "chat_id": "1"}},
	}
	for _, k := range kinds {
		req := map[string]interface{}{"kind": k.kind, "name": k.kind + "-x", "enabled": false, "config": k.config}
		if code, b := doReq(t, "POST", base, tok, req); code != http.StatusCreated {
			t.Fatalf("create %s status = %d body=%s", k.kind, code, b)
		}
	}

	// 测试连通性
	code, body = doReq(t, "POST", base+"/"+itoa(created.ID)+"/test", tok, nil)
	if code != http.StatusOK {
		t.Fatalf("test status = %d body=%s", code, body)
	}
	var tres struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &tres); err != nil || !tres.OK {
		t.Fatalf("test result wrong: ok=%v err=%v", tres.OK, err)
	}
	mu.Lock()
	wantReceived := received
	mu.Unlock()
	if wantReceived == 0 {
		t.Error("test message should have hit the webhook endpoint")
	}

	// 更新（改名 + 停用）
	code, _ = doReq(t, "PUT", base+"/"+itoa(created.ID), tok, map[string]interface{}{
		"kind": "dingtalk", "name": "新群名", "enabled": false,
		"config": map[string]string{"webhook": webhook.URL},
	})
	if code != http.StatusOK {
		t.Fatalf("update status = %d", code)
	}
	code, body = doReq(t, "GET", base+"/"+itoa(created.ID), tok, nil)
	if code != http.StatusOK {
		t.Fatalf("get status = %d", code)
	}
	var got channelDTO
	json.Unmarshal(body, &got)
	if got.Name != "新群名" || got.Enabled {
		t.Errorf("updated channel wrong: %+v", got)
	}

	// 修改 kind 被拒（类型不可变）
	if code, _ := doReq(t, "PUT", base+"/"+itoa(created.ID), tok, map[string]interface{}{"kind": "feishu"}); code != http.StatusBadRequest {
		t.Errorf("change kind status = %d, want 400", code)
	}

	// 删除（不存在的渠道走 404 → 依赖 store.ErrChannelNotFound 映射）
	code, _ = doReq(t, "DELETE", base+"/"+itoa(created.ID), tok, nil)
	if code != http.StatusOK {
		t.Fatalf("delete status = %d", code)
	}
	if code, _ := doReq(t, "DELETE", base+"/"+itoa(created.ID), tok, nil); code != http.StatusNotFound {
		t.Errorf("delete missing status = %d, want 404", code)
	}
	code, _ = doReq(t, "PUT", base+"/"+itoa(created.ID), tok, map[string]interface{}{"name": "x"})
	if code != http.StatusNotFound {
		t.Errorf("update missing status = %d, want 404", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestDingTalkTestAlias 旧端点 /api/dingtalk/test 使用渠道表中 dingtalk 渠道配置。
func TestDingTalkTestAlias(t *testing.T) {
	var hit bool
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验加签：旧端点对渠道里的 secret 生效
		hit = r.URL.Query().Get("sign") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	_, srv, tok := newChannelsEnv(t)
	base := srv.URL + "/api/channels"

	// 先建一条带 secret 的 dingtalk 渠道
	code, body := doReq(t, "POST", base, tok, map[string]interface{}{
		"kind": "dingtalk", "name": "钉钉", "enabled": true,
		"config": map[string]string{"webhook": webhook.URL, "secret": "SEC"},
	})
	if code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", code, body)
	}

	code, body = doReq(t, "POST", srv.URL+"/api/dingtalk/test", tok, map[string]interface{}{})
	if code != http.StatusOK {
		t.Fatalf("dingtalk test status = %d body=%s", code, body)
	}
	var res struct {
		OK bool `json:"ok"`
	}
	json.Unmarshal(body, &res)
	if !res.OK {
		t.Fatalf("dingtalk test not ok: %s", body)
	}
	if !hit {
		t.Error("old endpoint should use the channel config and apply sign secret")
	}
}
