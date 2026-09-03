package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dockmon/internal/config"
	"dockmon/internal/docker"
	"dockmon/internal/models"
	"dockmon/internal/registry"
	"dockmon/internal/store"
)

// TestScannerFullPipeline 通过真实 registry/docker 客户端 + 内存 SQLite，
// 端到端验证「本地摘要 vs 远端摘要」的比对逻辑。重点：
//  1. 真实 Docker 返回 "nginx@sha256:deadbeef"，客户端须剥离 sha256: 前缀；
//  2. 远端 registry 返回裸摘要 deadbeef，二者必须判定为 up-to-date（而非误报 update-available）；
//  3. 远端摘要变化后应正确判定 update-available 并产生通知。
//
// 该测试正是捕捉「docker 保留前缀 / registry 剥离前缀」导致每次都误报更新回归的关键用例。
func TestScannerFullPipeline(t *testing.T) {
	// ---- 1) 伪造 Docker 守护进程：返回带 sha256: 前缀的 RepoDigests ----
	// localDigest 为本地镜像摘要（固定不变），remoteDigest 为远端摘要（第二次扫描时变更）。
	localDigest := "deadbeef"
	remoteDigest := "deadbeef"
	dockerMux := http.NewServeMux()
	dockerMux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	dockerMux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		// 真实 Docker Engine 返回 "nginx@sha256:deadbeef"，客户端负责剥离前缀
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"Id":          "sha256:1",
				"RepoTags":    []string{"nginx:latest"},
				"RepoDigests": []string{"nginx@sha256:" + localDigest},
			},
		})
	})
	dockerSrv := httptest.NewServer(dockerMux)
	defer dockerSrv.Close()

	// ---- 2) 伪造 Registry（含 401 -> token -> 200 鉴权路径）----
	var registrySrvURL string
	registryMux := http.NewServeMux()
	registryMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fake-token"})
	})
	registryMux.HandleFunc("/v2/library/nginx/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			realm := registrySrvURL + "/token"
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+realm+`",service="registry.docker.io",scope="repository:library/nginx:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:"+remoteDigest)
		w.WriteHeader(http.StatusOK)
	})
	registrySrv := httptest.NewServer(registryMux)
	defer registrySrv.Close()
	registrySrvURL = registrySrv.URL
	regHost := strings.TrimPrefix(registrySrv.URL, "http://")

	// ---- 3) 内存 SQLite ----
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// ---- 4) 构造依赖（真实客户端，指向 httptest 服务）----
	dcli := docker.NewClientForTest(dockerSrv.URL, dockerSrv.Client())
	// 注入显式关闭代理的 http.Client，避免测试环境的 HTTP(S)_PROXY 拦截到 httptest 服务。
	regHTTP := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	reg := registry.NewClientWithMirrorAndHTTP(true, regHost, regHTTP)

	cfg := &config.Config{
		DefaultWatch:   []string{"nginx:latest"},
		DisableDefault: false,
	}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false))

	// ---- 5) 第一次扫描：本地 == 远端，应为 up-to-date，不产生误报告知 ----
	sc.Run(context.Background())

	img, err := st.GetImageByRef("nginx:latest")
	if err != nil {
		t.Fatalf("GetImageByRef: %v", err)
	}
	if img == nil {
		t.Fatal("nginx:latest not recorded after scan")
	}
	if img.LocalDigest != remoteDigest {
		t.Errorf("local digest = %q, want %q (sha256: prefix must be stripped)", img.LocalDigest, remoteDigest)
	}
	if img.RemoteDigest != remoteDigest {
		t.Errorf("remote digest = %q, want %q", img.RemoteDigest, remoteDigest)
	}
	if img.Status != models.StatusUpToDate {
		t.Errorf("status = %q, want %q (local==remote must be up-to-date)", img.Status, models.StatusUpToDate)
	}
	t.Logf("SCAN1 local=%q remote=%q status=%q", img.LocalDigest, img.RemoteDigest, img.Status)
	unread, _ := st.UnreadCount()
	if unread != 0 {
		t.Errorf("unread notifications = %d, want 0 (no false alarm on first baseline scan)", unread)
	}
	vs, _ := st.ListVersions(img.ID)
	if len(vs) != 1 {
		t.Errorf("version snapshots = %d, want 1 (baseline recorded)", len(vs))
	}

	// ---- 6) 第二次扫描：远端摘要变化，应判定 update-available 并产生通知 ----
	remoteDigest = "feedface"
	sc.Run(context.Background())

	img2, _ := st.GetImageByRef("nginx:latest")
	if img2.Status != models.StatusUpdateAvailable {
		t.Errorf("status after remote change = %q, want %q", img2.Status, models.StatusUpdateAvailable)
	}
	unread2, _ := st.UnreadCount()
	if unread2 < 1 {
		t.Errorf("unread notifications = %d, want >=1 after remote digest changed", unread2)
	}
	notifs, _ := st.ListNotifications(false)
	if len(notifs) < 1 {
		t.Errorf("notifications = %d, want >=1 after remote digest changed", len(notifs))
	}
}
