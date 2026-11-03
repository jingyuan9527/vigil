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

	"vigil/internal/config"
	"vigil/internal/models"
	"vigil/internal/registry"
	"vigil/internal/store"
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
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false))

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
	sc2 := New(cfg, st, dcli2, reg3, config.NewLiveSettings(3600, false, "", false))
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
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false))

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
	sc2 := New(cfg, st, dcli2, reg2, config.NewLiveSettings(3600, false, "", false))
	sc2.Run(context.Background(), false)

	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 0 {
		t.Fatalf("synced update notif still unread, want auto-read: %v", got)
	}
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 || !all[0].Read {
		t.Fatalf("notification row = %+v, want single read row", all)
	}
}

// searxngTransition 复现浮动 tag 被上游反复重推：本地停在 v1，远端两轮推进
// 到 v2、v3，留下两条未读 update；返回当前（v3）远端客户端供后续轮次复用。
func searxngTransition(t *testing.T) (*store.Store, *config.Config, *registry.Client) {
	t.Helper()
	reg1, regSrv1, _, _ := newFakeRegistry(t, "searxng/searxng", "latest", "v1", nil)
	t.Cleanup(regSrv1.Close)
	dcli, dockerSrv := newFakeDocker(t, "searxng/searxng:latest", "v1")
	t.Cleanup(dockerSrv.Close)

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := &config.Config{DefaultWatch: []string{"searxng/searxng:latest"}, DisableDefault: false}
	scan := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false))
	scan.Run(context.Background(), false) // 基线：local==remote，无通知

	for _, d := range []string{"v2", "v3"} {
		reg, srv, _, _ := newFakeRegistry(t, "searxng/searxng", "latest", d, nil)
		t.Cleanup(srv.Close)
		scan.SetRegistry(reg)
		scan.Run(context.Background(), false)
	}
	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 2 {
		t.Fatalf("unread update notifs = %v, want 2 (v2, v3)", got)
	}

	latest, latestSrv, _, _ := newFakeRegistry(t, "searxng/searxng", "latest", "v3", nil)
	t.Cleanup(latestSrv.Close)
	return st, cfg, latest
}

// TestAutoReadUpdateSupersededByNewerPull 覆盖 searxng:latest 这类被上游反复
// 重推的浮动 tag：同一引用积累多条不同摘要的未读提醒，用户拉取到最新后，
// 所有更早版本的「有新版本」提醒都应一并转已读，而不是只消化摘要完全相同的那条。
func TestAutoReadUpdateSupersededByNewerPull(t *testing.T) {
	st, cfg, latest := searxngTransition(t)

	dcli, dockerSrv := newFakeDocker(t, "searxng/searxng:latest", "v3")
	defer dockerSrv.Close()
	New(cfg, st, dcli, latest, config.NewLiveSettings(3600, false, "", false)).Run(context.Background(), false)

	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 0 {
		t.Fatalf("unread update after pulling latest = %v, want none", got)
	}
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 2 {
		t.Fatalf("notification rows = %d, want 2", len(all))
	}
	for _, n := range all {
		if !n.Read {
			t.Errorf("notification %s -> %s still unread, want auto-read", n.OldDigest, n.NewDigest)
		}
	}
}

// TestAutoReadUpdateOnlyOlderThanPulled 用户拉到的版本仍落后于当前远端
// （拉取后上游又推了一版）时，只消化「已被跨过」的历史提醒，
// 比本地更新的那条必须保持未读。
func TestAutoReadUpdateOnlyOlderThanPulled(t *testing.T) {
	st, cfg, latest := searxngTransition(t)

	// 本地持有 v2，远端已是 v3：v2 的提醒已达成，v3 的提醒仍需处理
	cli, dockerSrv := newFakeDocker(t, "searxng/searxng:latest", "v2")
	defer dockerSrv.Close()
	New(cfg, st, cli, latest, config.NewLiveSettings(3600, false, "", false)).Run(context.Background(), false)

	got := unreadNotifs(t, st, models.NotifUpdate)
	if len(got) != 1 || got[0].NewDigest != "v3" {
		t.Fatalf("unread update = %v, want only v3 (newer than local)", got)
	}
}

// TestAutoReadNewTagByDigestAfterPullLatest 可选更新的目标版本摘要落到本地 latest 上：
// 通知写入时记录推荐 tag 的 digest，用户拉 latest（tag 字符串完全不同）也能自动已读。
func TestAutoReadNewTagByDigestAfterPullLatest(t *testing.T) {
	reg1, regSrv1 := newFakeRegistryMulti(t, "neosmemo/memos",
		map[string]string{"0.31.0": "d031"}, []string{"0.31.0"})
	defer regSrv1.Close()
	dcli, dockerSrv := newFakeDocker(t, "neosmemo/memos:0.31.0", "d031")
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"neosmemo/memos:0.31.0"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false))
	sc.Run(context.Background(), false) // 首扫：建立 seen 基线

	// 上游发布 26.10.0 → 未读提醒，并记录推荐版本的摘要
	reg2, regSrv2 := newFakeRegistryMulti(t, "neosmemo/memos",
		map[string]string{"0.31.0": "d031", "26.10.0": "d2610"},
		[]string{"0.31.0", "26.10.0"})
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	got := unreadNotifs(t, st, models.NotifNewTag)
	if len(got) != 1 || got[0].NewDigest != "d2610" {
		t.Fatalf("unread new-tag = %+v, want one with NewDigest=d2610", got)
	}

	// 用户改跑 latest（内容即 26.10.0）→ tag 字符串不同但摘要一致，自动转已读
	reg3, regSrv3 := newFakeRegistryMulti(t, "neosmemo/memos",
		map[string]string{"latest": "d2610"}, []string{"0.31.0", "26.10.0", "latest"})
	defer regSrv3.Close()
	dcli2, dockerSrv2 := newFakeDocker(t, "neosmemo/memos:latest", "d2610")
	defer dockerSrv2.Close()
	New(cfg, st, dcli2, reg3, config.NewLiveSettings(3600, false, "", false)).Run(context.Background(), false)

	if got := unreadNotifs(t, st, models.NotifNewTag); len(got) != 0 {
		t.Fatalf("unread new-tag after pulling latest = %v, want none", got)
	}
}

// TestAutoReadUpdateWhenLocalJumpsToUnnotifiedDigest 用户拉到的版本 Vigil 从未
// 通知过（拉取发生在两轮扫描之间）：本地与当前远端一致即视为已达成，
// 该引用历史摘要的提醒一并转已读。
func TestAutoReadUpdateWhenLocalJumpsToUnnotifiedDigest(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "searxng/searxng", "latest", "v1", nil)
	defer regSrv1.Close()
	dcli, dockerSrv := newFakeDocker(t, "searxng/searxng:latest", "v1")
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"searxng/searxng:latest"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false))
	sc.Run(context.Background(), false)

	// 远端推进到 v2 → 一条未读提醒（本地仍是 v1）
	reg2, regSrv2, _, _ := newFakeRegistry(t, "searxng/searxng", "latest", "v2", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 1 {
		t.Fatalf("unread update after remote moved = %v, want 1", got)
	}

	// 用户在下一轮扫描前直接拉到当时最新的 v3（Vigil 未通知过 v3）
	reg3, regSrv3, _, _ := newFakeRegistry(t, "searxng/searxng", "latest", "v3", nil)
	defer regSrv3.Close()
	dcli2, dockerSrv2 := newFakeDocker(t, "searxng/searxng:latest", "v3")
	defer dockerSrv2.Close()

	New(cfg, st, dcli2, reg3, config.NewLiveSettings(3600, false, "", false)).Run(context.Background(), false)

	if got := unreadNotifs(t, st, models.NotifUpdate); len(got) != 0 {
		t.Fatalf("unread update after jump to latest = %v, want none", got)
	}
}
