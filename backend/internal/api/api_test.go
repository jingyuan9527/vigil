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

	"dockmon/internal/config"
	"dockmon/internal/registry"
	"dockmon/internal/scanner"
	"dockmon/internal/store"
)

// TestAPIRouter 在进程内通过 httptest 验证路由 + 存储的关键接口，
// 覆盖 health / stats / images(增+查+忽略) / notifications / scans，避免依赖外部网络。
func TestAPIRouter(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// docker 传 nil（无守护进程），registry 客户端不会被调用（扫描无任务）。
	sc := scanner.New(&config.Config{DisableDefault: true}, st, nil, registry.NewClient(false), config.NewLiveSettings(3600, false, "", true, ""))
	router := NewRouter("./static", st, sc, registry.NewClient(false), config.NewLiveSettings(3600, false, "", true, ""), []byte("test-secret"))
	srv := httptest.NewServer(router)
	defer srv.Close()

	// 首次启动需设置管理员以取得 token（受保护 API 均要求 Bearer 认证）
	setupResp, err := http.Post(srv.URL+"/api/auth/setup", "application/json",
		strings.NewReader(`{"username":"admin","password":"secret123"}`))
	if err != nil {
		t.Fatalf("POST /api/auth/setup: %v", err)
	}
	var setupBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(setupResp.Body).Decode(&setupBody); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	setupResp.Body.Close()
	if setupBody.Token == "" {
		t.Fatal("setup did not return a token")
	}
	tok := setupBody.Token

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
}
