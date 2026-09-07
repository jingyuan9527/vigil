package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vigil/internal/config"
	"vigil/internal/models"
	"vigil/internal/registry"
	"vigil/internal/scanner"
	"vigil/internal/store"
)

// TestDisableDefaultWatchRemovesDemoImages 回归：设置页关闭「演示监控列表」后，
// 演示列表已产生的镜像行（nginx/redis 等）应立即从镜像列表消失；
// 本地 docker 行与手动添加的带本地摘要行不受影响。
func TestDisableDefaultWatchRemovesDemoImages(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	demo := []string{"nginx:latest", "redis:latest", "postgres:latest", "node:lts", "alpine:latest"}
	cfg := &config.Config{DefaultWatch: demo}
	live := config.NewLiveSettings(3600, false, "", false, "", "")
	sc := scanner.New(cfg, st, nil, registry.NewClient(false), live)
	router := NewRouter("./static", st, sc, registry.NewClient(false), live, []byte("test-secret"))
	srv := httptest.NewServer(router)
	defer srv.Close()

	// 种子数据：演示行（新 source=default + 旧版遗留 manual 纯远端）、
	// 本地 docker 行、手动带本地摘要行
	now := time.Now()
	upsert := func(reference, source, local string) {
		img := &models.Image{
			Name: reference, Reference: reference, Tag: "latest",
			Source: source, LocalDigest: local,
			Status: models.StatusUpToDate, CreatedAt: now,
		}
		if err := st.UpsertImage(img); err != nil {
			t.Fatalf("upsert %s: %v", reference, err)
		}
	}
	upsert("nginx:latest", "default", "")
	upsert("redis:latest", "manual", "") // 旧版演示行遗留形态
	upsert("myapp.local/x:v1", "docker", "sha256:local")
	upsert("alpine:latest", "manual", "sha256:userlocal") // 手动带本地摘要，不应删

	// 管理员初始化 + 取 token
	tok := setupToken(t, srv.URL)

	getImages := func() map[string]models.Image {
		req, _ := http.NewRequest("GET", srv.URL+"/api/images", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatalf("GET /api/images: %v", e)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out struct {
			Images []models.Image `json:"images"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal images: %v (body=%s)", err, body)
		}
		refs := map[string]models.Image{}
		for _, im := range out.Images {
			refs[im.Reference] = im
		}
		return refs
	}

	if refs := getImages(); refs["nginx:latest"].ID == 0 || refs["redis:latest"].ID == 0 {
		t.Fatalf("demo images missing before toggle: %d images", len(refs))
	}

	// 开启「关闭演示监控列表」
	body := `{"scan_interval":3600,"scan_mode":"interval","scan_daily_time":"","registry_insecure":false,"registry_mirror":"","disable_default_watch":true,"dingtalk_webhook":"","dingtalk_secret":""}`
	req, _ := http.NewRequest("PUT", srv.URL+"/api/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/settings: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d, want 200", resp.StatusCode)
	}

	refs := getImages()
	for _, ref := range []string{"nginx:latest", "redis:latest"} {
		if _, ok := refs[ref]; ok {
			t.Errorf("%s 关闭演示列表后仍出现在镜像列表", ref)
		}
	}
	if _, ok := refs["myapp.local/x:v1"]; !ok {
		t.Error("docker 本地镜像不应被误删")
	}
	if _, ok := refs["alpine:latest"]; !ok {
		t.Error("手动添加且带本地摘要的镜像不应被误删")
	}
}

// setupToken 初始化管理员并返回 Bearer token（沿用 TestAPIRouter 的流程）。
func setupToken(t *testing.T, baseURL string) string {
	t.Helper()
	setupResp, err := http.Post(baseURL+"/api/auth/setup", "application/json",
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
	return tok
}
