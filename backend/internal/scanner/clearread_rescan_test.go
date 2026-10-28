package scanner

// 回归测试（反馈回路）：通知页改为「按镜像聚合」后，去重语义由聚合记录承载。
//
// 用户操作流：产生通知 → 全部已读 → 清空推送历史 → 立即扫描 → 全部重新扫描。
// 新语义：
//   - 聚合记录保存「已通知状态」，同一版本/摘要不会重复推送（定时与强制扫描一致）；
//   - 清空推送历史不影响聚合记录，因此不会因清空历史而重复提醒；
//   - 版本号（候选版本集合/最高版本）或浮动 tag 摘要实际变化时，才推送一次；
//   - 接入监控的首次扫描只建立基线，不把仓库既有版本一次性刷屏。

import (
	"context"
	"strings"
	"testing"

	"vigil/internal/config"
	"vigil/internal/models"
	"vigil/internal/store"
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

// clearHistory 模拟用户「全部已读 + 清空已读」：聚合记录转已读（状态保留），
// 推送历史行删除。聚合去重基线不受影响。
func clearHistory(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.MarkAllRead(); err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	if err := st.MarkAllImageUpdatesRead(); err != nil {
		t.Fatalf("mark all updates read: %v", err)
	}
	if _, err := st.ClearReadNotifications(); err != nil {
		t.Fatalf("clear read: %v", err)
	}
	if all, _ := st.ListNotifications(false, 0); len(all) != 0 {
		t.Fatalf("history not empty after clear: %d", len(all))
	}
}

func updateRecord(t *testing.T, st *store.Store, ref string) *models.ImageUpdate {
	t.Helper()
	img, err := st.GetImageByRef(ref)
	if err != nil || img == nil {
		t.Fatalf("image %s missing: %v", ref, err)
	}
	rec, err := st.GetImageUpdate(img.ID)
	if err != nil || rec == nil {
		t.Fatalf("update record for %s missing: %v", ref, err)
	}
	return rec
}

// TestDigestTransitionNotifiesOnce 场景 W：watch-only 浮动镜像的摘要转移。
// 转移只推送一次；清空历史、重复常规扫描与强制扫描都不会重复提醒。
func TestDigestTransitionNotifiesOnce(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/nginx", "latest", "r1digest", nil)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"nginx:latest"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false))

	// 首扫基线（无通知）
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("baseline scan notifs = %d, want 0", got)
	}

	// 远端 latest 被上游重新推送 → 推送一条 update
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/nginx", "latest", "r2digest", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Fatalf("transition scan notifs = %d, want 1", got)
	}
	rec := updateRecord(t, st, "nginx:latest")
	if rec.BaseDigest != "r1digest" || rec.CurrentDigest != "r2digest" {
		t.Errorf("record digest = %s -> %s, want r1digest -> r2digest", rec.BaseDigest, rec.CurrentDigest)
	}
	if rec.Status != models.UpdateUnread {
		t.Errorf("record status = %q, want unread", rec.Status)
	}

	// 相同摘要再扫：不重复
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Errorf("repeat scan notifs = %d, want 1 (same digest)", got)
	}

	// 用户全部已读 + 清空历史：聚合状态仍在，常规扫描与强制扫描都不重复提醒
	clearHistory(t, st)
	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Errorf("scan after clear-read notifs = %d, want 0 (aggregate dedup)", got)
	}
	if rec := updateRecord(t, st, "nginx:latest"); rec.Status != models.UpdateRead {
		t.Errorf("record status after clear = %q, want read", rec.Status)
	}
}

// TestForceScanFreshTransitionWatchOnly 覆盖尚未常规扫描过渡、直接强制扫描的分支：
// 相对基线确实发生了摘要变化 → 推送一次，而不是按「重新广播」每次触发。
func TestForceScanFreshTransitionWatchOnly(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/busybox", "latest", "r1digest", nil)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"busybox:latest"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false))

	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("baseline scan notifs = %d, want 0", got)
	}

	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/busybox", "latest", "r2digest", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), true)
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 {
		t.Fatalf("force scan (fresh transition) notifs = %d, want 1", len(all))
	}
	if all[0].OldDigest != "r1digest" || all[0].NewDigest != "r2digest" {
		t.Errorf("notify digest = %s -> %s, want r1digest -> r2digest", all[0].OldDigest, all[0].NewDigest)
	}

	// 再次强制扫描：同一状态不重复推送
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Errorf("second force scan notifs = %d, want 1 (dedup)", got)
	}
}

// TestForceScanWatchOnlyNeverMoved 负例：纯远端监控镜像自首次记录以来远端从未变化，
// 强制扫描没有版本变化，不应产生通知。
func TestForceScanWatchOnlyNeverMoved(t *testing.T) {
	reg, regSrv, _, _ := newFakeRegistry(t, "library/redis", "latest", "samedigest", nil)
	defer regSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"redis:latest"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg, config.NewLiveSettings(3600, false, "", false))

	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Errorf("force scan notifs for never-moved watch-only image = %d, want 0", got)
	}
}

// TestPinWatchCandidatesMergedAndNotifiedOnce 场景 P：Pin-Watch 镜像的候选版本聚合。
// 同一检测周期内出现的多个更高版本合并为一条通知，候选版本去重排序；
// 相同候选集合不再重复推送（含强制扫描）。
func TestPinWatchCandidatesMergedAndNotifiedOnce(t *testing.T) {
	tags1 := []string{"8.4.7", "8.4.8", "latest"}
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "fixeddigest", tags1)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false))

	// 首扫建立基线：已有更高版本只展示，不推送
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Fatalf("baseline scan new-tag notifs = %d, want 0", got)
	}
	rec := updateRecord(t, st, "mysql:8.4.7")
	if len(rec.Candidates) != 1 || rec.Candidates[0] != "8.4.8" || rec.LatestVersion != "8.4.8" {
		t.Errorf("baseline record = latest %q candidates %v, want 8.4.8", rec.LatestVersion, rec.Candidates)
	}

	// 仓库新增 30：候选 [30, 8.4.8]，一条 new-tag 通知
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "fixeddigest", append(tags1, "30"))
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Fatalf("new-tag scan notifs = %d, want 1", got)
	}
	rec = updateRecord(t, st, "mysql:8.4.7")
	if rec.LatestVersion != "30" {
		t.Errorf("latest version = %q, want 30", rec.LatestVersion)
	}
	if len(rec.Candidates) != 2 || rec.Candidates[0] != "30" || rec.Candidates[1] != "8.4.8" {
		t.Errorf("candidates = %v, want [30 8.4.8] (dedup + desc)", rec.Candidates)
	}
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 || all[0].Type != models.NotifNewTag || all[0].NewTag != "30" {
		t.Fatalf("notification = %+v, want one new-tag with NewTag=30", all)
	}

	// 相同候选集合：常规扫描与强制扫描都不重复
	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Errorf("repeat scans notifs = %d, want 1 (same candidate set)", got)
	}

	// 清空历史后再强制扫描：不重播
	clearHistory(t, st)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Errorf("force scan after clear-read notifs = %d, want 0", got)
	}
}

// TestForceScanDigestDiffNotifiesOnce 对照组：浮动 tag 存在 local!=remote 的活差异时，
// 首次扫描只建立基线；强制扫描不重复；远端再次变化才推送一次。
func TestForceScanDigestDiffNotifiesOnce(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/nginx", "latest", "bbdigest", nil)
	defer regSrv1.Close()
	dcli, dockerSrv := newFakeDocker(t, "nginx:latest", "aadigest")
	defer dockerSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"nginx:latest"}, DisableDefault: false}
	sc := New(cfg, st, dcli, reg1, config.NewLiveSettings(3600, false, "", false))

	// 首扫基线：local!=remote 但无「转移」→ 不通知，聚合记录展示差异
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("baseline scan notifs = %d, want 0", got)
	}
	rec := updateRecord(t, st, "nginx:latest")
	if !rec.HasUpdate || rec.BaseDigest != "aadigest" || rec.CurrentDigest != "bbdigest" {
		t.Errorf("baseline record = %+v, want live digest diff aa->bb", rec)
	}

	// 强制扫描：同一摘要不重复
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 0 {
		t.Fatalf("force scan notifs = %d, want 0 (same state)", got)
	}

	// 远端再次变化 → 推送一次；再强制扫描不重复
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/nginx", "latest", "ccdigest", nil)
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Fatalf("digest change notifs = %d, want 1", got)
	}
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifUpdate); got != 1 {
		t.Errorf("force scan after digest change notifs = %d, want 1", got)
	}
}

// TestBaselineCandidatesVisibleWithoutNotify 用户场景回归：监控 mysql:8.4.5 时
// 仓库早已存在更高版本（26、8.4.6）。首巡只建立基线不推送，但聚合记录中可见；
// 新版本出现时才推送一条，候选版本合并排序展示。
func TestBaselineCandidatesVisibleWithoutNotify(t *testing.T) {
	tags := []string{"8.4.5", "8.4.6", "26", "latest"}
	reg, regSrv, _, _ := newFakeRegistry(t, "library/mysql", "8.4.5", "fixeddigest", tags)
	defer regSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.5"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg, config.NewLiveSettings(3600, false, "", false))

	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Fatalf("baseline/force new-tag notifs = %d, want 0", got)
	}
	rec := updateRecord(t, st, "mysql:8.4.5")
	if rec.Status != models.UpdateRead {
		t.Errorf("baseline status = %q, want read", rec.Status)
	}
	if len(rec.Candidates) != 2 || rec.Candidates[0] != "26" || rec.Candidates[1] != "8.4.6" {
		t.Errorf("baseline candidates = %v, want [26 8.4.6]", rec.Candidates)
	}

	// 新增 27：一条通知，候选 [27 26 8.4.6]
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/mysql", "8.4.5", "fixeddigest", append(tags, "27"))
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Fatalf("new version notifs = %d, want 1", got)
	}
	rec = updateRecord(t, st, "mysql:8.4.5")
	if len(rec.Candidates) != 3 || rec.Candidates[0] != "27" || rec.Candidates[2] != "8.4.6" {
		t.Errorf("candidates after 27 = %v, want [27 26 8.4.6]", rec.Candidates)
	}

	// 常规扫描仍不重复
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Errorf("regular scan after notify = %d, want 1", got)
	}
}

// TestForceRescanNoNewerVersionTagSilent 负例：仓库不存在比锁定 tag 更高的版本
// （含同版本 -alpine 变体、更老的 8.4.4）时不推送。
func TestForceRescanNoNewerVersionTagSilent(t *testing.T) {
	tags := []string{"8.4.5", "8.4.5-alpine", "8.4.4", "latest"}
	reg, regSrv, _, _ := newFakeRegistry(t, "library/nginx", "8.4.5", "fixeddigest", tags)
	defer regSrv.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"nginx:8.4.5"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg, config.NewLiveSettings(3600, false, "", false))

	sc.Run(context.Background(), false)
	sc.Run(context.Background(), true)
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Errorf("force rescan with no newer version tag notifs = %d, want 0", got)
	}
	if rec := updateRecord(t, st, "nginx:8.4.5"); len(rec.Candidates) != 0 {
		t.Errorf("candidates = %v, want empty (same-version variants excluded)", rec.Candidates)
	}
}

// TestLowerVersionTagNotifiesWithLegacyTemplate 回归：低版本/其它分支新 tag 也要提醒。
// 文案恢复旧版「⭐ 镜像出现更新的独立版本 / 可选新版本」，同一镜像当轮只发一条；
// 即使没有可升级差异（当前已在最高版本），该未读条目在页面仍可见。
func TestLowerVersionTagNotifiesWithLegacyTemplate(t *testing.T) {
	tags := []string{"8.4.8-alpine", "8.4.0", "7.4.12-alpine", "latest"}
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/redis", "8.4.8-alpine", "fixeddigest", tags)
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"redis:8.4.8-alpine"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false))

	sc.Run(context.Background(), false) // 首扫建立已见标签基线
	if got := notifCount(t, st, models.NotifNewTag); got != 0 {
		t.Fatalf("baseline new-tag notifs = %d, want 0", got)
	}

	// 仓库发布 7.4 分支补丁 7.4.13-alpine（低于当前 8.4.8）→ 也要提醒
	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/redis", "8.4.8-alpine", "fixeddigest",
		append(append([]string{}, tags...), "7.4.13-alpine"))
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), false)

	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Fatalf("lower version tag notifs = %d, want 1", got)
	}
	all, _ := st.ListNotifications(false, 0)
	if all[0].NewTag != "7.4.13-alpine" {
		t.Errorf("notified tag = %q, want 7.4.13-alpine", all[0].NewTag)
	}
	for _, want := range []string{"镜像出现更新的独立版本", "可选新版本", "7.4.13-alpine"} {
		if !strings.Contains(all[0].Message, want) {
			t.Errorf("message missing %q: %q", want, all[0].Message)
		}
	}

	rec := updateRecord(t, st, "redis:8.4.8-alpine")
	if rec.Status != models.UpdateUnread {
		t.Errorf("status = %q, want unread", rec.Status)
	}
	if len(rec.NewTags) != 1 || rec.NewTags[0] != "7.4.13-alpine" {
		t.Errorf("new_tags = %v, want [7.4.13-alpine]", rec.NewTags)
	}
	if rec.HasUpdate {
		t.Error("lower branch tag should not create an upgrade diff")
	}
	if list, _ := st.ListImageUpdates(""); len(list) != 1 {
		t.Errorf("unread lower-tag record not listed: %+v", list)
	}

	// 相同 tag 下轮不再重复
	sc.Run(context.Background(), false)
	if got := notifCount(t, st, models.NotifNewTag); got != 1 {
		t.Errorf("repeat scan notifs = %d, want 1", got)
	}
}

// TestForceScanSameRoundFreshTagNotDuplicated 强制扫描与全新 tag 上线同轮发生时，
// 仍只产生一条聚合通知（候选版本合并），不会因强制扫描额外重播。
func TestForceScanSameRoundFreshTagNotDuplicated(t *testing.T) {
	reg1, regSrv1, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "fixeddigest", []string{"8.4.7", "latest"})
	defer regSrv1.Close()

	st, _ := store.Open(":memory:")
	cfg := &config.Config{DefaultWatch: []string{"mysql:8.4.7"}, DisableDefault: false}
	sc := New(cfg, st, nil, reg1, config.NewLiveSettings(3600, false, "", false))

	sc.Run(context.Background(), false)

	reg2, regSrv2, _, _ := newFakeRegistry(t, "library/mysql", "8.4.7", "fixeddigest", []string{"8.4.7", "30", "latest"})
	defer regSrv2.Close()
	sc.SetRegistry(reg2)
	sc.Run(context.Background(), true)
	all, _ := st.ListNotifications(false, 0)
	if len(all) != 1 {
		t.Fatalf("force scan (fresh tag same round) notifs = %d, want 1", len(all))
	}
	if all[0].NewTag != "30" {
		t.Errorf("notified new tag = %q, want 30", all[0].NewTag)
	}
}
