package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dockmon/internal/config"
	"dockmon/internal/docker"
	"dockmon/internal/models"
	"dockmon/internal/registry"
	"dockmon/internal/store"
)

// newFakeRegistry 构造一个伪造 registry（401->token->200 鉴权路径），
// 并用返回的摘要/tag 数据驱动扫描。manifestCalls/tagsCalls 用于断言「忽略/digest-only 是否跳过巡检」。
func newFakeRegistry(t *testing.T, repo, tag, digest string, tags []string) (*registry.Client, *httptest.Server, *int32, *int32) {
	t.Helper()
	var (
		manifestCalls int32
		tagsCalls     int32
		srvURL        string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "fake-token"})
	})
	mux.HandleFunc("/v2/"+repo+"/manifests/"+tag, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&manifestCalls, 1)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+srvURL+`/token",service="registry.docker.io",scope="repository:`+repo+`:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:"+digest)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v2/"+repo+"/tags/list", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tagsCalls, 1)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+srvURL+`/token",service="registry.docker.io",scope="repository:`+repo+`:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
	})
	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	host := strings.TrimPrefix(srv.URL, "http://")
	regHTTP := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	return registry.NewClientWithMirrorAndHTTP(true, host, regHTTP), srv, &manifestCalls, &tagsCalls
}

// newFakeDocker 构造一个伪造 Docker 守护进程，上报给定引用（裸引用，如 "mysql:8"，
// 与真实 Docker 的 RepoTags 一致）及其本地摘要。RepoDigests 用裸仓库名。
func newFakeDocker(t *testing.T, ref, localDigest string) (*docker.Client, *httptest.Server) {
	t.Helper()
	name := ref
	if i := strings.Index(ref, ":"); i > 0 {
		name = ref[:i]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "sha256:1", "RepoTags": []string{ref}, "RepoDigests": []string{name + "@sha256:" + localDigest}},
		})
	})
	srv := httptest.NewServer(mux)
	return docker.NewClientForTest(srv.URL, srv.Client()), srv
}

// TestScannerFullPipeline 验证 digest-only（nginx:latest）的基线与转移通知：
//  1. 首扫 local==remote → up-to-date、无通知、记录版本快照基线；
//  2. 远端摘要变化 → update-available + update 通知（按 digest 去重）。
func TestScannerFullPipeline(t *testing.T) {
	localDigest := "deadbeef"
	remoteDigest := "deadbeef"
	reg, regSrv, _, _ := newFakeRegistry(t, "library/nginx", "latest", remoteDigest, nil)
	defer regSrv.Close()
	dcli, dockerSrv := newFakeDocker(t, "nginx:latest", localDigest)
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"nginx:latest"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	sc.Run(context.Background(), false)
	img, _ := st.GetImageByRef("nginx:latest")
	if img.Status != models.StatusUpToDate {
		t.Errorf("status = %q, want up-to-date (local==remote)", img.Status)
	}
	if u, _ := st.UnreadCount(); u != 0 {
		t.Errorf("unread = %d, want 0 on first baseline", u)
	}

	remoteDigest = "feedface"
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/nginx", "latest", remoteDigest, nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)

	img2, _ := st.GetImageByRef("nginx:latest")
	if img2.Status != models.StatusUpdateAvailable {
		t.Errorf("status after change = %q, want update-available", img2.Status)
	}
	if u, _ := st.UnreadCount(); u < 1 {
		t.Errorf("unread after change = %d, want >=1", u)
	}
}

// TestIgnoredSkipsAllDetection 验证「忽略 = 跳过全部检测」语义：
// 镜像被忽略后，远端 manifest 与 tags/list 都不应被请求，
// 行数据（状态/远端摘要）保持冻结，也不产生任何通知。
func TestIgnoredSkipsAllDetection(t *testing.T) {
	localDigest := "aaaaaa"
	remoteDigest := "aaaaaa"
	reg, regSrv, mcalls, _ := newFakeRegistry(t, "library/mysql", "8", remoteDigest, nil)
	defer regSrv.Close()
	dcli, dockerSrv := newFakeDocker(t, "mysql:8", localDigest)
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫建立基线
	sc.Run(context.Background(), false)
	img, _ := st.GetImageByRef("mysql:8")
	if img == nil {
		t.Fatal("mysql:8 not recorded")
	}
	baseManifests := atomic.LoadInt32(mcalls)

	// 忽略该镜像
	if err := st.SetIgnored(img.ID, true); err != nil {
		t.Fatalf("set ignored: %v", err)
	}

	// 远端更新后再次扫描：应跳过 manifest 拉取，状态/远端摘要不变，无通知
	remoteDigest = "bbbbbb"
	sc.Run(context.Background(), false)

	img2, _ := st.GetImageByRef("mysql:8")
	if img2.Status != models.StatusUpToDate {
		t.Errorf("status = %q, want up-to-date (frozen, detection skipped)", img2.Status)
	}
	if img2.RemoteDigest != "aaaaaa" {
		t.Errorf("remote digest = %q, want frozen aaaaaa", img2.RemoteDigest)
	}
	if got := atomic.LoadInt32(mcalls) - baseManifests; got != 0 {
		t.Errorf("manifest calls during ignored scan = %d, want 0 (skip all detection)", got)
	}
	if u, _ := st.UnreadCount(); u != 0 {
		t.Errorf("unread = %d, want 0 for ignored image", u)
	}
}

// TestPinWatchBaselineThenNewTag 验证 Pin-Watch（固定版本 tag）新版本巡检语义：
//  1. 首扫建立 seen 标签基线（含已有所有版本 tag），不通知；
//  2. 仓库不变 → 不重复通知（seen 去重）；
//  3. 仓库新增单个新版本 tag → 一条 type=new-tag 通知；
//  4. 仓库再同时新增两个新版本 tag → 两条通知（每个 tag 仅一次）。
func TestPinWatchBaselineThenNewTag(t *testing.T) {
	digest := "c0ffee"
	allTags := []string{"8.0.36", "8.4.7", "8.4.8", "26", "latest"}
	reg, regSrv, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", digest, allTags)
	defer regSrv.Close()
	dcli, dockerSrv := newFakeDocker(t, "mysql:8.4.7", digest)
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	newTagNotifs := func() []models.Notification {
		all, _ := st.ListNotifications(false, 0)
		var out []models.Notification
		for _, n := range all {
			if n.Type == models.NotifNewTag {
				out = append(out, n)
			}
		}
		return out
	}

	// 首扫：建立 seen 基线，无新版本通知
	sc.Run(context.Background(), false)
	if n1 := newTagNotifs(); len(n1) != 0 {
		t.Fatalf("scan1 new-tag notifs = %d, want 0 (baseline)", len(n1))
	}

	// 第二轮：registry 未变 → 不重复
	sc.Run(context.Background(), false)
	if n2 := newTagNotifs(); len(n2) != 0 {
		t.Errorf("scan2 new-tag notifs = %d, want 0 (dedup, no repeat)", len(n2))
	}

	// 第三轮：新增单个 30 → 一条
	allTags2 := append(append([]string{}, allTags...), "30")
	reg3, regSrv3, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", digest, allTags2)
	defer regSrv3.Close()
	sc.SetRegistry(reg3)
	sc.Run(context.Background(), false)
	n3 := newTagNotifs()
	if len(n3) != 1 || n3[0].NewTag != "30" {
		t.Fatalf("scan3 new-tag notifs = %+v, want one with NewTag=30", n3)
	}

	// 第四轮：同时新增两个新版本 8.5.0 与 9.0.0 → 两条（每个 tag 仅一次）
	allTags3 := append(append([]string{}, allTags2...), "8.5.0", "9.0.0")
	reg4, regSrv4, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", digest, allTags3)
	defer regSrv4.Close()
	sc.SetRegistry(reg4)
	sc.Run(context.Background(), false)
	n4 := newTagNotifs()
	if len(n4) != 3 {
		t.Fatalf("scan4 new-tag notifs total = %d, want 3 (1+2 new)", len(n4))
	}
}

// TestRollingTagNoTagInspection 验证浮动标签（latest）默认 Digest-Only，
// 不会触发仓库 tag 巡检（即便仓库暴露了更高版本也不产生 new-tag 通知）。
func TestRollingTagNoTagInspection(t *testing.T) {
	digest := "beef00"
	reg, regSrv, _, tcalls := newFakeRegistry(t, "library/mysql", "latest", digest, []string{"26", "latest"})
	defer regSrv.Close()
	dcli, dockerSrv := newFakeDocker(t, "mysql:latest", digest)
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:latest"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	sc.Run(context.Background(), false)
	if atomic.LoadInt32(tcalls) != 0 {
		t.Errorf("tags/list called %d times for rolling tag, want 0 (digest-only skips inspection)", *tcalls)
	}
	all, _ := st.ListNotifications(false, 0)
	for _, n := range all {
		if n.Type == models.NotifNewTag {
			t.Errorf("rolling tag produced new-tag notify: %+v", n)
		}
	}
}

// TestForceScanBackfillsAndDedups 验证强制扫描：对当前存在版本差异的镜像补发 update 通知，
// 语义为重新广播——每次强制扫描都会再次通知（不受已读/历史通知影响）。
// 覆盖 Pin-Watch 锁定 tag 被覆盖的情况。
func TestForceScanBackfillsAndDedups(t *testing.T) {
	localDigest := "aaaaaa"
	remoteDigest := "bbbbbb" // 远端从一开始就与本地不同
	// tags/list 返回空：Pin-Watch 巡检直接 return，不影响本测试焦点
	reg, regSrv, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", remoteDigest, nil)
	defer regSrv.Close()
	dcli, dockerSrv := newFakeDocker(t, "mysql:8.4.7", localDigest)
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 常规扫描：Pin-Watch 首扫基线 + local!=remote 但 digest 变化非「转移」（changed=false，首扫）→ 不通知
	sc.Run(context.Background(), false)
	img, _ := st.GetImageByRef("mysql:8.4.7")
	if img.Status != models.StatusUpdateAvailable {
		t.Errorf("status = %q, want update-available (local!=remote)", img.Status)
	}
	if u, _ := st.UnreadCount(); u != 0 {
		t.Fatalf("unread after routine scan = %d, want 0 (pin-watch baseline, no transition notify)", u)
	}

	// 强制扫描：重新广播语义，存在版本差异即再次通知（无视去重与已读）
	sc.Run(context.Background(), true)
	if u, _ := st.UnreadCount(); u != 1 {
		t.Fatalf("unread after force scan = %d, want 1", u)
	}

	// 再次强制扫描：强制扫描=重新广播，同一 digest 已通知过也再次通知
	sc.Run(context.Background(), true)
	if u, _ := st.UnreadCount(); u != 2 {
		t.Errorf("unread after second force scan = %d, want 2 (force re-broadcasts)", u)
	}
}

// TestModeOverrideDigestOnly 验证手动覆写：把固定版本 tag 强制设为 digest-only 后，
// 即便仓库出现新版本 tag，也不应巡检 tags/list、不产生 new-tag 通知。
func TestModeOverrideDigestOnly(t *testing.T) {
	digest := "c0ffee"
	allTags := []string{"8.4.7", "8.4.8", "26", "latest"}
	reg, regSrv, _, tcalls := newFakeRegistry(t, "library/mysql", "8.4.7", digest, allTags)
	defer regSrv.Close()
	dcli, dockerSrv := newFakeDocker(t, "mysql:8.4.7", digest)
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫建立基线（此时还是 auto→pin-watch，会建立 seen）
	sc.Run(context.Background(), false)
	img, _ := st.GetImageByRef("mysql:8.4.7")
	if img == nil {
		t.Fatal("mysql:8.4.7 not recorded")
	}
	// 覆写为 digest-only
	if err := st.SetMode(img.ID, models.ModeDigestOnly); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	baseTags := atomic.LoadInt32(tcalls)

	// 新增新版本 tag，再扫：digest-only 不巡检 → 无 new-tag 通知
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", digest, append(allTags, "30"))
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := atomic.LoadInt32(tcalls) - baseTags; got != 0 {
		t.Errorf("tags/list calls after digest-only override = %d, want 0", got)
	}
	all, _ := st.ListNotifications(false, 0)
	for _, n := range all {
		if n.Type == models.NotifNewTag {
			t.Errorf("digest-only override produced new-tag notify: %+v", n)
		}
	}
}
