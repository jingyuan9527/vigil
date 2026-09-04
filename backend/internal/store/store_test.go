package store

import (
	"testing"
	"time"

	"dockmon/internal/models"
)

func TestStoreCRUD(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	img := &models.Image{
		Name: "library/nginx", Reference: "nginx:latest",
		Registry: "registry-1.docker.io", Tag: "latest",
		Source: "manual", Status: models.StatusUnknown, CreatedAt: time.Now(),
	}
	if err := s.UpsertImage(img); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetImageByRef("nginx:latest")
	if err != nil || got == nil {
		t.Fatalf("get: %v (%v)", err, got)
	}
	if got.Status != models.StatusUnknown {
		t.Fatalf("status = %q", got.Status)
	}

	// 更新（按 reference 幂等）
	got.Status = models.StatusUpToDate
	got.RemoteDigest = "sha256:abc"
	if err := s.UpsertImage(got); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	got2, _ := s.GetImageByRef("nginx:latest")
	if got2.Status != models.StatusUpToDate || got2.RemoteDigest != "sha256:abc" {
		t.Fatalf("update not applied: %+v", got2)
	}

	// 列表
	imgs, _ := s.ListImages("")
	if len(imgs) != 1 {
		t.Fatalf("list count = %d", len(imgs))
	}

	// 版本快照
	if err := s.AddVersion(got2.ID, "sha256:abc", "latest"); err != nil {
		t.Fatalf("add version: %v", err)
	}
	vers, _ := s.ListVersions(got2.ID)
	if len(vers) != 1 {
		t.Fatalf("versions count = %d", len(vers))
	}

	// 通知
	if err := s.CreateNotification(&models.Notification{
		ImageID: got2.ID, ImageName: "library/nginx", Reference: "nginx:latest", Message: "new version",
	}); err != nil {
		t.Fatalf("notif: %v", err)
	}
	ns, _ := s.ListNotifications(false, 0)
	if len(ns) != 1 {
		t.Fatalf("notif count = %d", len(ns))
	}
	unread, _ := s.UnreadCount()
	if unread != 1 {
		t.Fatalf("unread = %d", unread)
	}
	if err := s.MarkRead(ns[0].ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	unread, _ = s.UnreadCount()
	if unread != 0 {
		t.Fatalf("unread after read = %d", unread)
	}

	// 扫描记录 + 统计
	scanID, _ := s.CreateScan()
	if err := s.FinishScan(scanID, 1, 1, "done", ""); err != nil {
		t.Fatalf("finish scan: %v", err)
	}
	scans, _ := s.ListScans(10)
	if len(scans) != 1 {
		t.Fatalf("scans count = %d", len(scans))
	}
	st, _ := s.Stats()
	if st.Total != 1 || st.UpToDate != 1 {
		t.Fatalf("stats = %+v", st)
	}

	// 删除
	if err := s.DeleteImage(got2.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	imgs, _ = s.ListImages("")
	if len(imgs) != 0 {
		t.Fatalf("after delete count = %d", len(imgs))
	}
}

// TestIgnored 验证忽略标记：SetIgnored 持久化、可读回、且不被后续 UpsertImage 覆盖。
func TestIgnored(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	img := &models.Image{
		Name: "library/mysql", Reference: "mysql:8", Source: "docker",
		Registry: "registry-1.docker.io", Tag: "8", CreatedAt: time.Now(),
	}
	if err := s.UpsertImage(img); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ := s.GetImageByRef("mysql:8")
	if got.Ignored {
		t.Fatal("new image should default to ignored=false")
	}

	if err := s.SetIgnored(got.ID, true); err != nil {
		t.Fatalf("set ignored: %v", err)
	}
	got2, _ := s.GetImageByRef("mysql:8")
	if !got2.Ignored {
		t.Fatal("ignored not persisted via GetImageByRef")
	}

	// 模拟一次扫描触发的 Upsert：不应把 ignored 重置回 false
	got2.RemoteDigest = "sha256:zzz"
	if err := s.UpsertImage(got2); err != nil {
		t.Fatalf("upsert while ignored: %v", err)
	}
	got3, _ := s.GetImageByRef("mysql:8")
	if !got3.Ignored {
		t.Fatal("UpsertImage must not reset ignored")
	}

	// ListImages 应带出 ignored 字段
	list, _ := s.ListImages("")
	if len(list) != 1 || !list[0].Ignored {
		t.Fatalf("list ignored roundtrip failed: %+v", list)
	}

	// 取消忽略
	if err := s.SetIgnored(got.ID, false); err != nil {
		t.Fatalf("unignore: %v", err)
	}
	got4, _ := s.GetImageByRef("mysql:8")
	if got4.Ignored {
		t.Fatal("unignore not applied")
	}
}

// TestMarkDockerImagesMissing 验证本机已删除 docker 镜像被标记为 stale，
// 而仍存活或 manual 来源的行不受影响。
func TestMarkDockerImagesMissing(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seed := func(ref string, src string) *models.Image {
		img := &models.Image{Name: ref, Reference: ref, Source: src,
			Registry: "registry-1.docker.io", Tag: "latest",
			Status: models.StatusUpToDate, CreatedAt: time.Now()}
		if err := s.UpsertImage(img); err != nil {
			t.Fatalf("seed %s: %v", ref, err)
		}
		got, _ := s.GetImageByRef(ref)
		return got
	}
	alive := seed("nginx:latest", "docker")
	removed := seed("mysql:8", "docker")
	manual := seed("redis:7", "manual")

	n, err := s.MarkDockerImagesMissing(map[string]bool{"nginx:latest": true, "redis:7": true})
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if n != 1 {
		t.Errorf("marked = %d, want 1", n)
	}

	gotAlive, _ := s.GetImageByRef(alive.Reference)
	if gotAlive.Status != models.StatusUpToDate {
		t.Errorf("alive docker image status = %q, want up-to-date", gotAlive.Status)
	}
	gotRemoved, _ := s.GetImageByRef(removed.Reference)
	if gotRemoved.Status != models.StatusStale {
		t.Errorf("removed docker image status = %q, want stale", gotRemoved.Status)
	}
	gotManual, _ := s.GetImageByRef(manual.Reference)
	if gotManual.Status != models.StatusUpToDate {
		t.Errorf("manual image must not be touched, status = %q", gotManual.Status)
	}

	// 空 liveRefs（docker 不可用信号）应安全地不做任何改动
	if err := s.UpsertImage(&models.Image{ID: gotRemoved.ID, Name: gotRemoved.Name,
		Reference: gotRemoved.Reference, Source: "docker", Registry: gotRemoved.Registry,
		Tag: gotRemoved.Tag, Status: models.StatusUpToDate, CreatedAt: gotRemoved.CreatedAt}); err != nil {
		t.Fatalf("re-upsert removed: %v", err)
	}
	n2, err := s.MarkDockerImagesMissing(nil)
	if err != nil {
		t.Fatalf("mark with empty: %v", err)
	}
	if n2 != 0 {
		t.Errorf("marked with empty liveRefs = %d, want 0", n2)
	}
}

// TestSeenTagsAndMode 验证 seen 标签清单 CRUD 与检测模式覆写持久化，
// 以及通知按 digest 去重（HasDigestNotification）。
func TestSeenTagsAndMode(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	img := &models.Image{Name: "library/mysql", Reference: "mysql:8.4.5", Tag: "8.4.5",
		Source: "docker", Registry: "registry-1.docker.io", CreatedAt: time.Now()}
	if err := s.UpsertImage(img); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ := s.GetImageByRef("mysql:8.4.5")

	// seen 清单：首次为空，建立基线后再读回应包含全部
	if seen, _ := s.GetSeenTags(got.ID); len(seen) != 0 {
		t.Fatalf("initial seen = %d, want 0", len(seen))
	}
	if err := s.AddSeenTags(got.ID, []string{"8.4.5", "8.4.6", "8.5.0"}); err != nil {
		t.Fatalf("add seen: %v", err)
	}
	seen, _ := s.GetSeenTags(got.ID)
	if len(seen) != 3 || !seen["8.5.0"] {
		t.Errorf("seen after add = %+v, want 3 incl 8.5.0", seen)
	}
	// 幂等：重复添加不翻倍
	_ = s.AddSeenTags(got.ID, []string{"8.4.5", "9.0.0"})
	if seen2, _ := s.GetSeenTags(got.ID); len(seen2) != 4 {
		t.Errorf("seen after re-add = %d, want 4 (idempotent)", len(seen2))
	}

	// 模式覆写持久化且不被扫描的 Upsert 覆盖
	if err := s.SetMode(got.ID, models.ModePinWatch); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	got2, _ := s.GetImageByRef("mysql:8.4.5")
	if got2.Mode != models.ModePinWatch || got2.EffectiveMode != models.ModePinWatch {
		t.Errorf("mode = %q / effective = %q, want pin-watch", got2.Mode, got2.EffectiveMode)
	}
	// 扫描触发的 Upsert（更新 remote/status）不应重置 mode
	got2.RemoteDigest = "sha256:zzz"
	got2.Status = models.StatusUpdateAvailable
	if err := s.UpsertImage(got2); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got3, _ := s.GetImageByRef("mysql:8.4.5")
	if got3.Mode != models.ModePinWatch {
		t.Errorf("mode reset after Upsert = %q, want pin-watch (must not be overwritten)", got3.Mode)
	}

	// HasDigestNotification：按 (image, digest) 去重
	has, _ := s.HasDigestNotification(got.ID, "sha256:zzz")
	if has {
		t.Fatal("HasDigestNotification before any notif = true, want false")
	}
	_ = s.CreateNotification(&models.Notification{ImageID: got.ID, NewDigest: "sha256:zzz", Type: models.NotifUpdate, Message: "x"})
	has2, _ := s.HasDigestNotification(got.ID, "sha256:zzz")
	if !has2 {
		t.Error("HasDigestNotification after insert = false, want true")
	}
	has3, _ := s.HasDigestNotification(got.ID, "sha256:other")
	if has3 {
		t.Error("HasDigestNotification for other digest = true, want false")
	}

	// DeleteImage 应清理 seen 清单
	if err := s.DeleteImage(got.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if seen3, _ := s.GetSeenTags(got.ID); len(seen3) != 0 {
		t.Errorf("seen after delete = %d, want 0 (cleaned up)", len(seen3))
	}
}

// TestFirstVersionDigest 验证版本时间线最早记录查询：返回首次记录的非空摘要；
// 时间线为空时返回空串（不视为错误）。与当前摘要的差异判断由调用方完成。
func TestFirstVersionDigest(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	img := &models.Image{
		Name: "library/nginx", Reference: "nginx:latest",
		Source: "manual", Status: models.StatusUnknown, CreatedAt: time.Now(),
	}
	if err := s.UpsertImage(img); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ := s.GetImageByRef("nginx:latest")

	// 空时间线
	if d, err := s.FirstVersionDigest(got.ID); err != nil || d != "" {
		t.Fatalf("empty timeline = (%q, %v), want (\"\", nil)", d, err)
	}

	// 时间线 R1 → R2 → R1（时间顺序写入），最早记录应为 R1（首次记录，而非「最早与 R2 不同」）
	_ = s.AddVersion(got.ID, "sha256:r1", "latest")
	_ = s.AddVersion(got.ID, "sha256:r2", "latest")
	_ = s.AddVersion(got.ID, "sha256:r1", "latest")
	if d, err := s.FirstVersionDigest(got.ID); err != nil || d != "sha256:r1" {
		t.Fatalf("first version = (%q, %v), want sha256:r1", d, err)
	}

	// 时间线仅单一摘要：返回该摘要本身，差异判断交给调用方（无转移时调用方不广播）
	img2 := &models.Image{
		Name: "library/redis", Reference: "redis:latest",
		Source: "manual", Status: models.StatusUnknown, CreatedAt: time.Now(),
	}
	if err := s.UpsertImage(img2); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	got2, _ := s.GetImageByRef("redis:latest")
	_ = s.AddVersion(got2.ID, "sha256:same", "latest")
	_ = s.AddVersion(got2.ID, "sha256:same", "latest")
	if d, _ := s.FirstVersionDigest(got2.ID); d != "sha256:same" {
		t.Fatalf("single-digest timeline = %q, want sha256:same (caller compares)", d)
	}

	// 空摘要行应被忽略：追加空行后最早记录仍为 R1
	if err := s.AddVersion(got.ID, "", "latest"); err != nil {
		t.Fatalf("add empty version: %v", err)
	}
	if d, _ := s.FirstVersionDigest(got.ID); d != "sha256:r1" {
		t.Fatalf("with empty digest row = %q, want sha256:r1", d)
	}
}
