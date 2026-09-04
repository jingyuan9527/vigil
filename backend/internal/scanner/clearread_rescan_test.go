package scanner

// 回归测试（反馈回路）：修复用户报告 ——
// 「清空历史消息后，点击立即扫描/全部重新扫描，都没有扫出来可选更新的内容」。
//
// 三个子场景分别覆盖三种镜像形态：
//   W: watch-only 演示镜像（source=manual、无本地摘要、浮动 tag → digest-only）
//   P: pin-watch 镜像（new-tag「可选更新」弱提醒）
//   D: docker 本地镜像且当前存在 local!=remote 的活差异（对照组：强制扫描应重新广播）
//
// 用户操作流统一为：产生通知 → 全部已读 → 清空已读 → 立即扫描 → 全部重新扫描。
// 修复后语义：W/D 清空后强制扫描按「从未通知过」从版本时间线重建基线找回提醒；
// 常规扫描（立即扫描）保持去重基线不重复提醒；P 的 new-tag 重新广播属已知缺口
// （方案 B，未实施），seen-tags 基线独立于通知历史，断言锁定现状。

import (
	"context"
	"testing"

	"dockmon/internal/config"
	"dockmon/internal/models"
	"dockmon/internal/store"
)

func notifCount(t *testing.T, st *store.Store, typ models.NotificationKind) int {
	t.Helper()
	all, err := st.ListNotifications(false, 0)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	n := 0
	for _, x := range all {
		if x.Type == typ {
			n++
		}
	}
	return n
}

func clearHistory(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.MarkAllRead(); err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	if _, err := st.ClearReadNotifications(); err != nil {
		t.Fatalf("clear read: %v", err)
	}
	if all, _ := st.ListNotifications(false, 0); len(all) != 0 {
		t.Fatalf("history not empty after clear: %d", len(all))
	}
}

// TestClearReadThenRescanWatchOnly 场景 W：watch-only 镜像（demo watch 列表形态）的
// digest 转移通知，清空已读后强制扫描按「从未通知过」从版本时间线重建基线重新广播。
func TestClearReadThenRescanWatchOnly(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/nginx", "latest", "r1digest", nil)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"nginx:latest"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫基线（无通知）
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("baseline scan notifs = %d, want 0", got)
	}

	// 远端 latest 被上游重新推送 → 转移扫描产生 update 通知
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/nginx", "latest", "r2digest", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Fatalf("transition scan notifs = %d, want 1", got)
	}

	// 用户：全部已读 + 清空已读
	clearHistory(t, st)

	// 用户：立即扫描（常规）——去重基线拦截 + 无转移，不重复提醒（清空弹窗承诺的语义）
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("regular scan after clear-read notifs = %d, want 0 (dedup preserved)", got)
	}

	// 用户：全部重新扫描（强制）——重新广播，old 取版本时间线基线
	sc.Run(context.Background(), true)
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 {
		t.Fatalf("force rescan after clear-read notifs = %d, want 1", len(all))
	}
	if all[0].OldDigest != "r1digest" || all[0].NewDigest != "r2digest" {
		t.Errorf("rebroadcast digest = %s -> %s, want r1digest -> r2digest", all[0].OldDigest, all[0].NewDigest)
	}

	// 再次强制扫描：重新广播语义，每次触发都会再次通知
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 2 {
		t.Errorf("second force rescan notifs = %d, want 2 (force re-broadcasts every time)", got)
	}

	if img, _ := st.GetImageByRef("nginx:latest"); img != nil {
		t.Logf("image status after force rescan = %q（纯远端监控的状态展示语义不在本次修复范围）", img.Status)
	}
}

// TestForceScanFreshTransitionWatchOnly 覆盖强制扫描时远端恰处于「全新转移」的分支
// （prevRemote != remote，尚未常规扫描过渡）：基线一律取版本时间线最早记录，
// 广播 old=首次记录、new=当前，保证与清空后再强制扫描的 OldDigest 一致。
func TestForceScanFreshTransitionWatchOnly(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/busybox", "latest", "r1digest", nil)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"busybox:latest"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫基线（无通知）
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("baseline scan notifs = %d, want 0", got)
	}

	// 远端转移但尚未常规扫描，直接强制扫描：old 应为首次记录 r1，而非上轮远端
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/busybox", "latest", "r2digest", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), true)
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 {
		t.Fatalf("force scan (fresh transition) notifs = %d, want 1", len(all))
	}
	if all[0].OldDigest != "r1digest" || all[0].NewDigest != "r2digest" {
		t.Errorf("rebroadcast digest = %s -> %s, want r1digest -> r2digest", all[0].OldDigest, all[0].NewDigest)
	}
}

// TestForceScanWatchOnlyNeverMoved 负例：纯远端监控镜像自首次记录以来远端从未变化
// （时间线只有当前摘要）时，强制扫描没有可广播的转移，不应产生通知。
func TestForceScanWatchOnlyNeverMoved(t *testing.T) {
	reg, regSrv, _, _ := newFakeRegistry(t, "library/redis", "latest", "samedigest", nil)
	defer regSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"redis:latest"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg, config.NewLiveSettings(3600, false, "", false, "", ""))

	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Errorf("force scan notifs for never-moved watch-only image = %d, want 0", got)
	}
}

// TestClearReadThenRescanPinWatchNewTag 场景 P：pin-watch 的 new-tag「可选更新」通知，
// 清空已读后任何扫描都不会再生——seen-tags 基线独立于通知历史。
// 已知缺口（方案 B，未实施）：new-tag 通知的重新广播需要另行设计，
// 当前断言锁定现状，防止无意识行为变更。
func TestClearReadThenRescanPinWatchNewTag(t *testing.T) {
	tags1 := []string{"8.4.7", "8.4.8", "latest"}
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "fixeddigest", tags1)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫建立 seen 基线
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Fatalf("baseline scan new-tag notifs = %d, want 0", got)
	}

	// 仓库发布新 tag 30 → new-tag 通知
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "fixeddigest", append(tags1, "30"))
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Fatalf("new-tag scan notifs = %d, want 1", got)
	}

	// 用户：全部已读 + 清空已读，再立即扫描与强制扫描——均无再生
	clearHistory(t, st)
	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Errorf("rescan after clear-read new-tag notifs = %d, want 0 (seen-tags baseline independent of history)", got)
	}
}

// TestClearReadThenRescanDockerDiff 对照组 D：docker 本地镜像当前存在
// local!=remote 的活差异时，清空已读后强制扫描应能重新广播（机制本身健康）。
func TestClearReadThenRescanDockerDiff(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "rbdigest", nil)
	defer regSrv1.Close()
	dcli, dockerSrv := newFakeDocker(t, "mysql:8.4.7", "aadigest")
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false, "", ""))

	// 首扫基线：local!=remote 但 changed=false → 不通知
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("baseline scan notifs = %d, want 0", got)
	}

	// 强制扫描补发一条，随后用户已读并清空
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Fatalf("force scan notifs = %d, want 1", got)
	}
	clearHistory(t, st)

	// 清空后强制扫描：活差异仍在 → 应重新广播
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got < 1 {
		t.Errorf("force rescan after clear-read (live diff): update notifs = %d, want >=1", got)
	} else {
		t.Logf("[force rescan, live diff] notifs = %d", got)
	}
}
