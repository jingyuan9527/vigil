package scanner

// 回归测试：目标达成即自动已读。
//
// 用户场景：redis:8.4.0 收到「仓库出现 8.4.1」的可选更新提醒 → 用户升级到 8.4.1 →
// 该未读提醒不应继续占用未读角标，但通知行保留（转已读，可在列表查看历史）。
// 对称场景：浮动 tag 收到摘要变更提醒 → 用户拉取同步后自动转已读。
// 负例：目标未达成（版本没用上 / 摘要没同步）的通知不受影响。

import (
	"context"
	"testing"

	"dockmon/internal/config"
	"dockmon/internal/models"
	"dockmon/internal/store"
)

func unreadNotifs(t *testing.T, st *store.Store, typ models.NotificationKind) []models.Notification {
	t.Helper()
	all, err := st.ListNotifications(true, 0)
	if err != nil {
		t.Fatalf("list unread notifications: %v", err)
	}
	out := make([]models.Notification, 0, len(all))
	for _, n := range all {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

// TestAutoReadNewTagOnceAdopted new-tag：本地出现通知指向的版本 tag 后，
// 下一次常规扫描自动转已读；更新到别的更高版本（目标未达成）的不受影响。
func TestAutoReadNewTagOnceAdopted(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/redis", "8.4.0", "d840", []string{"8.4.0"})
	defer regSrv1.Close()
	dcli, dockerSrv := newFakeDocker(t, "redis:8.4.0", "d840")
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"redis:8.4.0"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首巡基线（无通知）
	sc.Run(context.Background(), false)
	if got := unreadNotifs(t, st, models.NotifNewTag); len(got) != 0 {
		t.Fatalf("baseline new-tag notifs = %d, want 0", len(got))
	}

	// 仓库新增 8.4.1 → 常规扫描产生一条未读 new-tag
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/redis", "8.4.0", "d840", []string{"8.4.0", "8.4.1"})
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := unreadNotifs(t, st, models.NotifNewTag); len(got) != 1 || got[0].NewTag != "8.4.1" {
		t.Fatalf("after new tag published, unread new-tag = %v, want exactly 8.4.1", got)
	}

	// 用户升级：本机运行 redis:8.4.1（8.4.0 已删）→ 常规扫描后提醒自动转已读
	reg3, regSrv3, _, _ := newFakeRegistry(t, "library/redis", "8.4.1", "d841", []string{"8.4.0", "8.4.1"})
	defer regSrv3.Close()
	dcli2, dockerSrv2 := newFakeDocker(t, "redis:8.4.1", "d841")
	defer dockerSrv2.Close()
	sc2 := New(cfg, st, dcli2, reg3, config.NewLiveSettings(3600, false, "", false, "", ""))
	sc2.Run(context.Background(), false)

	if got := unreadNotifs(t, st, models.NotifNewTag); len(got) != 0 {
		t.Fatalf("adopted new-tag notif still unread, want auto-read: %v", got)
	}
	// 通知行保留（转已读，不删除）
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 || !all[0].Read {
		t.Fatalf("notification row = %+v, want single read row", all)
	}

	// 仓库再出现 8.4.2：目标未达成，常规扫描后必须保持未读
	reg4, regSrv4, _, _ := newFakeRegistry(t, "library/redis", "8.4.1", "d841", []string{"8.4.0", "8.4.1", "8.4.2"})
	defer regSrv4.Close()
	sc2.SetRegistry(reg4)
	sc2.Run(context.Background(), false)
	if got := unreadNotifs(t, st, models.NotifNewTag); len(got) != 1 || got[0].NewTag != "8.4.2" {
		t.Fatalf("unachieved new-tag notif = %v, want exactly unread 8.4.2", got)
	}
}

// TestAutoReadUpdateOnceSynced update：本地摘要同步到通知记录的新摘要后，
// 下一次常规扫描自动转已读；仍存在活差异（本地未同步）的不受影响。
func TestAutoReadUpdateOnceSynced(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/nginx", "latest", "aadigest", nil)
	defer regSrv1.Close()
	dcli, dockerSrv := newFakeDocker(t, "nginx:latest", "aadigest")
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"nginx:latest"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫 local==remote：基线，无通知
	sc.Run(context.Background(), false)

	// 远端摘要转移到 bb → update 通知（未读）
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/nginx", "latest", "bbdigest", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 1 || got[0].NewDigest != "bbdigest" {
		t.Fatalf("after remote moved, unread update = %v, want digest bbdigest", got)
	}

	// 活差异未同步（本地仍是 aa）：常规扫描不消化
	sc.Run(context.Background(), false)
	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 1 {
		t.Fatalf("live-diff update notif must stay unread, got %d", len(got))
	}

	// 用户拉取同步：本地摘要变为 bb → 常规扫描后自动转已读
	dcli2, dockerSrv2 := newFakeDocker(t, "nginx:latest", "bbdigest")
	defer dockerSrv2.Close()
	sc2 := New(cfg, st, dcli2, reg2, config.NewLiveSettings(3600, false, "", false, "", ""))
	sc2.Run(context.Background(), false)

	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 0 {
		t.Fatalf("synced update notif still unread, want auto-read: %v", got)
	}
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 || !all[0].Read {
		t.Fatalf("notification row = %+v, want single read row", all)
	}
}
