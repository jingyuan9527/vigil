package store

// 回归测试：可选更新（new-tag）的「目标达成即自动已读」不能只靠 tag 字符串相等。
// 用户场景：neosmemo/memos:0.31.0 收到「仓库发布 26.10.0」提醒后升级到 26.10.0，
// 该提醒必须转已读；上游别名 tag（26.10）、v 前缀、更高的 26.11.0 同样算已达成；
// 仍停在 0.31.0 时保持未读。

import (
	"testing"
	"time"

	"vigil/internal/models"
)

func testImage(t *testing.T, st *Store, name, tag, localDigest string) *models.Image {
	t.Helper()
	img := &models.Image{
		Name: name, Reference: name + ":" + tag,
		Registry: "registry-1.docker.io", Tag: tag, Source: "docker",
		LocalDigest: localDigest, RemoteDigest: localDigest,
		Status: models.StatusUpToDate, CreatedAt: time.Now(),
	}
	if err := st.UpsertImage(img); err != nil {
		t.Fatalf("upsert image %s: %v", img.Reference, err)
	}
	got, err := st.GetImageByRef(img.Reference)
	if err != nil || got == nil {
		t.Fatalf("reload image %s: %v", img.Reference, err)
	}
	return got
}

func testNewTagNotif(t *testing.T, st *Store, img *models.Image, newTag, newDigest string) {
	t.Helper()
	if err := st.CreateNotification(&models.Notification{
		ImageID: img.ID, ImageName: img.Name, Reference: img.Reference,
		OldTag: img.Tag, NewTag: newTag, NewDigest: newDigest,
		Type: models.NotifNewTag, Message: "test",
	}); err != nil {
		t.Fatalf("create notification: %v", err)
	}
}

// TestAutoReadNewTagVersionSupersede 覆盖推荐版本被「跨过」的几种写法：
// 同版本补零/别名、v 前缀、更高版本都应转已读；未升级或版本更低则保持未读。
func TestAutoReadNewTagVersionSupersede(t *testing.T) {
	cases := []struct {
		name       string
		localTag   string
		wantUnread int
	}{
		{"升级到推荐版本", "26.10.0", 0},
		{"升级到同版本别名", "26.10", 0},
		{"升级到更高版本", "26.11.0", 0},
		{"v 前缀别名", "v26.10.0", 0},
		{"未升级", "0.31.0", 1},
		{"仍是更低版本", "26.9.0", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			base := testImage(t, st, "neosmemo/memos", "0.31.0", "d031")
			// 存量通知：老版本没有记录推荐版本摘要，只能靠版本号判定
			testNewTagNotif(t, st, base, "26.10.0", "")
			if c.localTag != "0.31.0" {
				testImage(t, st, "neosmemo/memos", c.localTag, "d-local")
			}

			if _, err := st.AutoReadAchievedNotifications(); err != nil {
				t.Fatalf("auto read: %v", err)
			}
			unread, err := st.UnreadCount()
			if err != nil {
				t.Fatalf("unread count: %v", err)
			}
			if unread != c.wantUnread {
				t.Fatalf("unread = %d, want %d", unread, c.wantUnread)
			}
		})
	}
}

// TestAutoReadNewTagByDigest 推荐版本摘要落到本地任一 tag（如 latest 或别名）即算
// 已用上该版本：tag 字符串无关，只认内容。摘要不同（拉的是别的版本）保持未读。
func TestAutoReadNewTagByDigest(t *testing.T) {
	cases := []struct {
		name        string
		localTag    string
		localDigest string
		wantUnread  int
	}{
		{"latest 拉到推荐版本", "latest", "d2610", 0},
		{"latest 是别的版本", "latest", "d-other", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			base := testImage(t, st, "neosmemo/memos", "0.31.0", "d031")
			testNewTagNotif(t, st, base, "26.10.0", "d2610")
			testImage(t, st, "neosmemo/memos", c.localTag, c.localDigest)

			if _, err := st.AutoReadAchievedNotifications(); err != nil {
				t.Fatalf("auto read: %v", err)
			}
			unread, err := st.UnreadCount()
			if err != nil {
				t.Fatalf("unread count: %v", err)
			}
			if unread != c.wantUnread {
				t.Fatalf("unread = %d, want %d", unread, c.wantUnread)
			}
		})
	}
}

// TestAutoReadNewTagOtherRepoUnaffected 负例：其他仓库出现同版本 tag 不算达成。
func TestAutoReadNewTagOtherRepoUnaffected(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := testImage(t, st, "neosmemo/memos", "0.31.0", "d031")
	testNewTagNotif(t, st, base, "26.10.0", "d2610")
	testImage(t, st, "someone/other", "26.10.0", "d2610")

	if _, err := st.AutoReadAchievedNotifications(); err != nil {
		t.Fatalf("auto read: %v", err)
	}
	if unread, _ := st.UnreadCount(); unread != 1 {
		t.Fatalf("unread = %d, want 1 (other repo must not count)", unread)
	}
}
