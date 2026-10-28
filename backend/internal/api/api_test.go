package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"vigil/internal/config"
	"vigil/internal/registry"
	"vigil/internal/scanner"
	"vigil/internal/store"
)

// TestAPIRouter 在进程内通过 httptest 验证路由 + 存储的关键接口，
// 覆盖 health / stats / images(增+查+忽略) / notifications / scans，避免依赖外部网络。
func TestAPIRouter(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// docker 传 nil（无守护进程），registry 客户端不会被调用（扫描无任务）。
	sc := scanner.New(&config.Config{DisableDefault: true}, st, nil, registry.NewClient(false), config.NewLiveSettings(3600, false, "", true))
	router := NewRouter("./static", st, sc, registry.NewClient(false), config.NewLiveSettings(3600, false, "", true), []byte("test-secret"))
	srv := httptest.NewServer(router)
	defer srv.Close()

	// 首次启动需设置管理员：登录态写入 httpOnly cookie，测试从 Set-Cookie 提取 token
	// 作为后续请求的 Bearer（受保护 API 同时接受 Authorization 头）。
	setupResp, err := http.Post(srv.URL+"/api/auth/setup", "application/json",
		strings.NewReader(`{"username":"admin","password":"secret123"}`))
	if err != nil {
		t.Fatalf("POST /api/auth/setup: %v", err)
	}
	setupResp.Body.Close()
	tok := ""
	for _, part := range strings.Split(setupResp.Header.Get("Set-Cookie"), ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "vigil_token=") {
			tok = strings.TrimPrefix(part, "vigil_token=")
			break
		}
	}
	if tok == "" {
		t.Fatal("setup did not set vigil_token cookie")
	}

	get := func(path string) (int, []byte) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatalf("GET %s: %v", path, e)
		}
		defer resp.Body.Close()
		body, re := io.ReadAll(resp.Body)
		if re != nil {
			t.Fatalf("read GET %s: %v", path, re)
		}
		return resp.StatusCode, body
	}

	// health（无需 token 也应可达）
	if code, _ := get("/api/health"); code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", code)
	}

	// 添加一个手动监控镜像
	req, _ := http.NewRequest("POST", srv.URL+"/api/images",
		strings.NewReader(`{"reference":"nginx:latest"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/images: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/images status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	// 查询镜像列表，应至少包含刚添加的 nginx:latest
	code, body := get("/api/images")
	if code != http.StatusOK {
		t.Fatalf("GET /api/images status = %d, want 200", code)
	}
	var list struct {
		Images []struct {
			ID        int64  `json:"id"`
			Reference string `json:"reference"`
			Source    string `json:"source"`
			Ignored   bool   `json:"ignored"`
		} `json:"images"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal /api/images: %v (body=%s)", err, body)
	}
	if list.Count < 1 {
		t.Fatalf("images count = %d, want >=1", list.Count)
	}
	var nginxID int64
	found := false
	for _, im := range list.Images {
		if im.Reference == "nginx:latest" && im.Source == "manual" {
			found = true
			nginxID = im.ID
			if im.Ignored {
				t.Fatal("nginx should default to ignored=false")
			}
		}
	}
	if !found {
		t.Errorf("nginx:latest (manual) not found in /api/images response: %s", body)
	}

	// 忽略 nginx 再取消，验证 ignored 开关接口
	putIgnored := func(flag bool) bool {
		pr, _ := http.NewRequest("PUT", srv.URL+"/api/images/"+strconv.FormatInt(nginxID, 10)+"/ignored",
			strings.NewReader(fmt.Sprintf(`{"ignored":%v}`, flag)))
		pr.Header.Set("Authorization", "Bearer "+tok)
		pr.Header.Set("Content-Type", "application/json")
		rsp, e := http.DefaultClient.Do(pr)
		if e != nil {
			t.Fatalf("PUT ignored: %v", e)
		}
		defer rsp.Body.Close()
		var rb struct {
			Ignored bool `json:"ignored"`
		}
		_ = json.NewDecoder(rsp.Body).Decode(&rb)
		return rsp.StatusCode == http.StatusOK && rb.Ignored == flag
	}
	if !putIgnored(true) {
		t.Error("PUT ignored=true failed")
	}
	code, body = get("/api/images")
	if code != http.StatusOK {
		t.Fatalf("GET /api/images status = %d", code)
	}
	_ = json.Unmarshal(body, &list)
	stillIgnored := false
	for _, im := range list.Images {
		if im.ID == nginxID {
			stillIgnored = im.Ignored
		}
	}
	if !stillIgnored {
		t.Error("ignored flag not reflected after PUT=true")
	}
	if !putIgnored(false) {
		t.Error("PUT ignored=false failed")
	}

	// notifications / scans 不应报错
	if code, _ := get("/api/notifications"); code != http.StatusOK {
		t.Errorf("GET /api/notifications status = %d, want 200", code)
	}
	if code, _ := get("/api/scans"); code != http.StatusOK {
		t.Errorf("GET /api/scans status = %d, want 200", code)
	}

	// 聚合更新记录：先建基线、再模拟摘要变化，验证列表 / 未读 / 标记已读接口
	base := store.UpdateInput{
		ImageID: nginxID, ImageName: "library/nginx", Reference: "nginx:latest",
		CurrentVersion: "latest", CurrentDigest: "d1", BaseDigest: "d1",
		DigestNotifiable: true,
	}
	if _, _, err := st.MergeImageUpdate(base); err != nil {
		t.Fatalf("merge baseline: %v", err)
	}
	base.CurrentDigest = "d2"
	if _, notify, err := st.MergeImageUpdate(base); err != nil || !notify {
		t.Fatalf("merge changed (notify=%v): %v", notify, err)
	}
	code, body = get("/api/updates")
	if code != http.StatusOK {
		t.Fatalf("GET /api/updates status = %d, want 200", code)
	}
	var updates struct {
		Updates []struct {
			ImageID int64  `json:"image_id"`
			Status  string `json:"status"`
		} `json:"updates"`
		Unread int `json:"unread"`
		Count  int `json:"count"`
	}
	if err := json.Unmarshal(body, &updates); err != nil {
		t.Fatalf("unmarshal /api/updates: %v (body=%s)", err, body)
	}
	if updates.Count < 1 || updates.Unread < 1 {
		t.Fatalf("updates = %s, want at least one unread record", body)
	}
	post := func(path string) int {
		pr, _ := http.NewRequest("POST", srv.URL+path, nil)
		pr.Header.Set("Authorization", "Bearer "+tok)
		rsp, e := http.DefaultClient.Do(pr)
		if e != nil {
			t.Fatalf("POST %s: %v", path, e)
		}
		defer rsp.Body.Close()
		return rsp.StatusCode
	}
	if got := post("/api/updates/" + strconv.FormatInt(nginxID, 10) + "/read"); got != http.StatusOK {
		t.Fatalf("POST update read status = %d, want 200", got)
	}
	code, body = get("/api/updates?status=unread")
	if code != http.StatusOK {
		t.Fatalf("GET unread updates status = %d", code)
	}
	_ = json.Unmarshal(body, &updates)
	if updates.Count != 0 {
		t.Errorf("unread updates = %s, want empty after mark read", body)
	}
}
