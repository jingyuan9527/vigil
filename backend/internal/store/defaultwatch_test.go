package store

import (
	"testing"
	"time"

	"vigil/internal/models"
)

// TestDeleteDefaultWatchImages 验证关闭演示监控列表时只清理演示形态的行：
//   - source=default 的新演示行（scanner.collectJobs 现标记）→ 删除；
//   - 旧版本遗留的 manual 纯远端 watch 形态（source=manual、无本地摘要，
//     registry 为 docker.io 默认值 registry-1.docker.io）→ 删除
//     （升级前的演示行正是这种外形，registry 判定不再区分空值/docker.io）;
//   - source=docker 的本地镜像行、带本地摘要的手动行、带私有 registry 前缀
//     reference 的手动行（不在演示清单内）、清单外 manual watch-only 行 → 保留；
//   - 派生数据（版本快照/已见标签/去重基线）级联删除，通知历史保留。
func TestDeleteDefaultWatchImages(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	mk := func(reference, source, localDigest, registry string) *models.Image {
		return &models.Image{
			Name: reference, Reference: reference,
			Registry: registry, Tag: "latest",
			Source: source, LocalDigest: localDigest,
			Status: models.StatusUpToDate, CreatedAt: time.Now(),
		}
	}
	// 应删除：新演示行 + 旧版遗留演示行（registry 为 docker.io 默认值）
	if err := s.UpsertImage(mk("nginx:latest", "default", "", "")); err != nil {
		t.Fatalf("upsert default demo: %v", err)
	}
	if err := s.UpsertImage(mk("redis:latest", "manual", "", "registry-1.docker.io")); err != nil {
		t.Fatalf("upsert legacy demo: %v", err)
	}
	// 应保留：docker 本地行 / 手动带本地摘要 / 私有 registry（带前缀 reference）/
	// 清单外手动行
	if err := s.UpsertImage(mk("postgres:latest", "docker", "sha256:loc", "")); err != nil {
		t.Fatalf("upsert docker: %v", err)
	}
	if err := s.UpsertImage(mk("node:lts", "manual", "sha256:userlocal", "")); err != nil {
		t.Fatalf("upsert manual with local: %v", err)
	}
	if err := s.UpsertImage(mk("harbor.example.com/alpine:latest", "manual", "", "harbor.example.com")); err != nil {
		t.Fatalf("upsert manual private reg: %v", err)
	}
	if err := s.UpsertImage(mk("busybox:latest", "manual", "", "")); err != nil {
		t.Fatalf("upsert unrelated: %v", err)
	}

	// 给演示行挂上派生数据：版本快照、已见标签、（经 CreateNotification 写入的）去重基线
	demo, _ := s.GetImageByRef("nginx:latest")
	if demo == nil {
		t.Fatal("nginx:latest demo row missing")
	}
	if err := s.AddVersion(demo.ID, "sha256:v1", "latest"); err != nil {
		t.Fatalf("add version: %v", err)
	}
	if err := s.AddSeenTags(demo.ID, []string{"latest"}); err != nil {
		t.Fatalf("add seen tags: %v", err)
	}
	if err := s.CreateNotification(&models.Notification{
		ImageID: demo.ID, ImageName: "library/nginx", Reference: "nginx:latest",
		NewDigest: "sha256:v1", Type: models.NotifUpdate, Message: "update",
	}); err != nil {
		t.Fatalf("create notif: %v", err)
	}

	refs := []string{"nginx:latest", "redis:latest", "postgres:latest", "node:lts", "alpine:latest"}
	n, err := s.DeleteDefaultWatchImages(refs)
	if err != nil {
		t.Fatalf("delete default watch images: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d, want 2 (default demo + legacy manual watch-only)", n)
	}

	for _, ref := range []string{"nginx:latest", "redis:latest"} {
		if got, _ := s.GetImageByRef(ref); got != nil {
			t.Errorf("%s 应被删除，仍在库中", ref)
		}
	}
	for _, ref := range []string{"postgres:latest", "node:lts", "harbor.example.com/alpine:latest", "busybox:latest"} {
		if got, _ := s.GetImageByRef(ref); got == nil {
			t.Errorf("%s 应被保留，却已被删除", ref)
		}
	}

	// 派生数据级联清理：行没了，版本快照与已见标签、去重基线都要消失
	if vers, _ := s.ListVersions(demo.ID); len(vers) != 0 {
		t.Errorf("versions not cascaded, count = %d", len(vers))
	}
	if seen, _ := s.GetSeenTags(demo.ID); len(seen) != 0 {
		t.Errorf("seen tags not cascaded, count = %d", len(seen))
	}
	if ok, _ := s.HasDigestNotification(demo.ID, "sha256:v1"); ok {
		t.Error("dedup baseline not cascaded")
	}
	// 通知历史保留（删除镜像不清通知是既有语义）
	if all, _ := s.ListNotifications(false, 0); len(all) != 1 {
		t.Errorf("notifications should be retained, count = %d", len(all))
	}
}
