package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockmon/internal/config"
	"dockmon/internal/registry"
	"dockmon/internal/scanner"
	"dockmon/internal/store"
)

// TestAPIRouter 在进程内通过 httptest 验证路由 + 存储的关键接口，
// 覆盖 health / stats / images(增+查) / notifications / scans，避免依赖外部网络。
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

	get := func(path string) (int, []byte) {
		resp, e := http.Get(srv.URL + path)
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

	// health
	code, body := get("/api/health")
	if code != http.StatusOK {
		t.Fatalf("health status = %d, want 200 (body=%s)", code, body)
	}

	// 添加一个手动监控镜像
	resp, err := http.Post(srv.URL+"/api/images", "application/json",
		strings.NewReader(`{"reference":"nginx:latest"}`))
	if err != nil {
		t.Fatalf("POST /api/images: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/images status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	// 查询镜像列表，应至少包含刚添加的 nginx:latest
	code, body = get("/api/images")
	if code != http.StatusOK {
		t.Fatalf("GET /api/images status = %d, want 200", code)
	}
	var list struct {
		Images []struct {
			Reference string `json:"reference"`
			Source    string `json:"source"`
		} `json:"images"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal /api/images: %v (body=%s)", err, body)
	}
	if list.Count < 1 {
		t.Fatalf("images count = %d, want >=1", list.Count)
	}
	found := false
	for _, im := range list.Images {
		if im.Reference == "nginx:latest" && im.Source == "manual" {
			found = true
		}
	}
	if !found {
		t.Errorf("nginx:latest (manual) not found in /api/images response: %s", body)
	}

	// notifications / scans 不应报错
	if code, _ := get("/api/notifications"); code != http.StatusOK {
		t.Errorf("GET /api/notifications status = %d, want 200", code)
	}
	if code, _ := get("/api/scans"); code != http.StatusOK {
		t.Errorf("GET /api/scans status = %d, want 200", code)
	}
}
