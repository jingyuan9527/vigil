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
	ns, _ := s.ListNotifications(false)
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
