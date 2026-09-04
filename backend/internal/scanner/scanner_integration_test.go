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
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

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

// TestIgnoredSuppressesNotification 验证「忽略 = 仍扫描但不提醒」语义：
// 镜像被忽略后，远端摘要再次变化应照常判定为 update-available，
// 但既不写入 notifications，也不计入未读 —— 便于用户取消忽略后不产生误报。
func TestIgnoredSuppressesNotification(t *testing.T) {
	localDigest := "aaaaaa"
	remoteDigest := "aaaaaa"

	dockerMux := http.NewServeMux()
	dockerMux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	dockerMux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "sha256:1", "RepoTags": []string{"mysql:8"}, "RepoDigests": []string{"mysql@sha256:" + localDigest}},
		})
	})
	dockerSrv := httptest.NewServer(dockerMux)
	defer dockerSrv.Close()

	var registrySrvURL string
	registryMux := http.NewServeMux()
	registryMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fake-token"})
	})
	registryMux.HandleFunc("/v2/library/mysql/manifests/8", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+registrySrvURL+`/token",service="registry.docker.io",scope="repository:library/mysql:pull"`)
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

	st, _ := store.Open(":memory:")
	dcli := docker.NewClientForTest(dockerSrv.URL, dockerSrv.Client())
	regHTTP := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	reg := registry.NewClientWithMirrorAndHTTP(true, regHost, regHTTP)
	cfg := &config.Config{DefaultWatch: []string{"mysql:8"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫：建立 up-to-date 基线，且无通知
	sc.Run(context.Background())
	img, _ := st.GetImageByRef("mysql:8")
	if img == nil {
		t.Fatal("mysql:8 not recorded")
	}
	if u, _ := st.UnreadCount(); u != 0 {
		t.Fatalf("unread after baseline = %d, want 0", u)
	}

	// 用户忽略该镜像（仍扫描不提醒）
	if err := st.SetIgnored(img.ID, true); err != nil {
		t.Fatalf("set ignored: %v", err)
	}

	// 远端更新，再次扫描：应 update-available，但不得产生通知
	remoteDigest = "bbbbbb"
	sc.Run(context.Background())

	img2, _ := st.GetImageByRef("mysql:8")
	if img2.Status != models.StatusUpdateAvailable {
		t.Errorf("status = %q, want %q (ignored should still scan & update status)", img2.Status, models.StatusUpdateAvailable)
	}
	if !img2.Ignored {
		t.Error("ignored flag lost after scan (Upsert must not reset it)")
	}
	if u, _ := st.UnreadCount(); u != 0 {
		t.Errorf("unread = %d, want 0 (ignored image must not notify)", u)
	}
	if notifs, _ := st.ListNotifications(false); len(notifs) != 0 {
		t.Errorf("notifications = %d, want 0 for ignored image", len(notifs))
	}
}

// TestNewerTagWeakNotify 验证「固定 tag 发现更高独立版本 → 弱提醒」语义：
//  1. mysql:8.4.7 首扫即发现仓库 tags 里有更高的 26 → 生成 type=new-tag 弱提醒；
//  2. registry 不变时再次扫描 → 去重，不重复提醒；
//  3. 仓库再出现更高的 30 → 生成新的弱提醒（指向 30）。
// latest 等滚动 tag 不参与该探测（交给 digest 强更新）。
func TestNewerTagWeakNotify(t *testing.T) {
	digest := "c0ffee"
	// 可变 tags：模拟仓库版本演进
	allTags := []string{"8.0.36", "8.4.7", "8.4.8", "26", "latest"}

	dockerMux := http.NewServeMux()
	dockerMux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	dockerMux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "sha256:1", "RepoTags": []string{"mysql:8.4.7"}, "RepoDigests": []string{"mysql@sha256:" + digest}},
		})
	})
	dockerSrv := httptest.NewServer(dockerMux)
	defer dockerSrv.Close()

	var registrySrvURL string
	registryMux := http.NewServeMux()
	registryMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fake-token"})
	})
	registryMux.HandleFunc("/v2/library/mysql/manifests/8.4.7", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+registrySrvURL+`/token",service="registry.docker.io",scope="repository:library/mysql:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:"+digest)
		w.WriteHeader(http.StatusOK)
	})
	registryMux.HandleFunc("/v2/library/mysql/tags/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+registrySrvURL+`/token",service="registry.docker.io",scope="repository:library/mysql:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "library/mysql", "tags": allTags})
	})
	registrySrv := httptest.NewServer(registryMux)
	defer registrySrv.Close()
	registrySrvURL = registrySrv.URL
	regHost := strings.TrimPrefix(registrySrv.URL, "http://")

	st, _ := store.Open(":memory:")
	dcli := docker.NewClientForTest(dockerSrv.URL, dockerSrv.Client())
	regHTTP := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	reg := registry.NewClientWithMirrorAndHTTP(true, regHost, regHTTP)
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	newTagNotifs := func() []models.Notification {
		all, _ := st.ListNotifications(false)
		var out []models.Notification
		for _, n := range all {
			if n.Type == models.NotifNewTag {
				out = append(out, n)
			}
		}
		return out
	}

	// 第 1 轮：local==remote → up-to-date 无强更新；但发现更高 26 → 弱提醒 1 条
	sc.Run(context.Background())
	n1 := newTagNotifs()
	if len(n1) != 1 {
		t.Fatalf("scan1 new-tag notifs = %d, want 1", len(n1))
	}
	if n1[0].NewTag != "26" {
		t.Errorf("scan1 NewTag = %q, want 26", n1[0].NewTag)
	}
	img, _ := st.GetImageByRef("mysql:8.4.7")
	if img == nil {
		t.Fatal("mysql:8.4.7 not recorded")
	}
	if img.NotifiedNewTag != "26" {
		t.Errorf("NotifiedNewTag = %q, want 26", img.NotifiedNewTag)
	}
	if img.Status != models.StatusUpToDate {
		t.Errorf("status = %q, want up-to-date (no digest change)", img.Status)
	}

	// 第 2 轮：registry 未变 → 去重，不再新增弱提醒
	sc.Run(context.Background())
	if n2 := newTagNotifs(); len(n2) != 1 {
		t.Errorf("scan2 new-tag notifs = %d, want 1 (dedup, no repeat)", len(n2))
	}

	// 第 3 轮：仓库出现更高的 30 → 产生指向 30 的新弱提醒
	allTags = append(allTags, "30")
	sc.Run(context.Background())
	n3 := newTagNotifs()
	if len(n3) != 2 {
		t.Fatalf("scan3 new-tag notifs = %d, want 2", len(n3))
	}
	img3, _ := st.GetImageByRef("mysql:8.4.7")
	if img3.NotifiedNewTag != "30" {
		t.Errorf("NotifiedNewTag after 30 = %q, want 30", img3.NotifiedNewTag)
	}
}

// TestRollingTagNoWeakNotify 验证 latest 等滚动 tag 不触发仓库级弱提醒
// （滚动 tag 的更新由 digest 强更新承担，避免无谓的 tags/list 噪音）。
func TestRollingTagNoWeakNotify(t *testing.T) {
	digest := "beef00"
	dockerMux := http.NewServeMux()
	dockerMux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	dockerMux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "sha256:1", "RepoTags": []string{"mysql:latest"}, "RepoDigests": []string{"mysql@sha256:" + digest}},
		})
	})
	dockerSrv := httptest.NewServer(dockerMux)
	defer dockerSrv.Close()

	var registrySrvURL string
	registryMux := http.NewServeMux()
	registryMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fake-token"})
	})
	registryMux.HandleFunc("/v2/library/mysql/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+registrySrvURL+`/token",service="registry.docker.io",scope="repository:library/mysql:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:"+digest)
		w.WriteHeader(http.StatusOK)
	})
	registryMux.HandleFunc("/v2/library/mysql/tags/list", func(w http.ResponseWriter, r *http.Request) {
		// 故意暴露更高版本，但 latest 是滚动 tag，不应因此弱提醒
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "library/mysql", "tags": []string{"26", "latest"}})
	})
	registrySrv := httptest.NewServer(registryMux)
	defer registrySrv.Close()
	registrySrvURL = registrySrv.URL
	regHost := strings.TrimPrefix(registrySrv.URL, "http://")

	st, _ := store.Open(":memory:")
	dcli := docker.NewClientForTest(dockerSrv.URL, dockerSrv.Client())
	regHTTP := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	reg := registry.NewClientWithMirrorAndHTTP(true, regHost, regHTTP)
	cfg := &config.Config{DefaultWatch: []string{"mysql:latest"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	sc.Run(context.Background())
	all, _ := st.ListNotifications(false)
	for _, n := range all {
		if n.Type == models.NotifNewTag {
			t.Errorf("rolling tag produced new-tag weak notify: %+v", n)
		}
	}
}
