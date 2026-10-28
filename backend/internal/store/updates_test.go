package store

import (
	"strings"
	"testing"

	"vigil/internal/models"
)

func mergeUpdate(t *testing.T, s *Store, in UpdateInput) (models.ImageUpdate, bool) {
	t.Helper()
	rec, notify, err := s.MergeImageUpdate(in)
	if err != nil {
		t.Fatalf("merge update: %v", err)
	}
	return rec, notify
}

// TestMergeImageUpdateDigestDedup 覆盖摘要类更新的去重：
// 首次只建基线、相同摘要不重复、摘要变化只推一次、已读后不复活。
func TestMergeImageUpdateDigestDedup(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	in := UpdateInput{
		ImageID: 1, ImageName: "library/nginx", Reference: "nginx:latest",
		CurrentVersion: "latest", CurrentDigest: "d1", BaseDigest: "d1",
		DigestNotifiable: true,
	}
	rec, notify := mergeUpdate(t, s, in)
	if notify {
		t.Fatal("first observation must not notify (baseline)")
	}
	if rec.Status != models.UpdateRead || rec.HasUpdate {
		t.Errorf("baseline record = %+v, want read without diff", rec)
	}

	if _, notify = mergeUpdate(t, s, in); notify {
		t.Fatal("same digest must not notify")
	}

	in.CurrentDigest = "d2" // 与基线 d1 存在差异
	rec, notify = mergeUpdate(t, s, in)
	if !notify {
		t.Fatal("digest change must notify once")
	}
	if rec.Status != models.UpdateUnread || rec.Reason != models.UpdateReasonDigest {
		t.Errorf("record = %+v, want unread/digest", rec)
	}

	if _, notify = mergeUpdate(t, s, in); notify {
		t.Fatal("repeated same digest must not notify again")
	}
	if n, _ := s.UnreadUpdateCount(); n != 1 {
		t.Errorf("unread = %d, want 1", n)
	}
	list, _ := s.ListImageUpdates("")
	if len(list) != 1 || list[0].CurrentDigest != "d2" {
		t.Fatalf("list = %+v, want one record for d2", list)
	}

	if err := s.MarkImageUpdateRead(1); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if n, _ := s.UnreadUpdateCount(); n != 0 {
		t.Errorf("unread after mark read = %d, want 0", n)
	}
	if _, notify = mergeUpdate(t, s, in); notify {
		t.Error("already-notified state must not re-notify after mark read")
	}
}

// TestMergeImageUpdateCandidateMerge 覆盖候选版本的合并、去重与排序：
// 同一检测周期多次发现的候选版本合并进一条记录，只推送一次。
func TestMergeImageUpdateCandidateMerge(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 2, ImageName: "library/mysql", Reference: "mysql:8.4.5",
		CurrentVersion: "8.4.5", CurrentDigest: "d1", BaseDigest: "d1",
		LatestVersion: "8.4.8", VersionKnown: true, Candidates: []string{"8.4.8"},
	}
	rec, notify := mergeUpdate(t, s, in)
	if notify {
		t.Fatal("baseline must not notify")
	}
	if len(rec.Candidates) != 1 || rec.Candidates[0] != "8.4.8" {
		t.Fatalf("baseline candidates = %v", rec.Candidates)
	}

	if _, notify = mergeUpdate(t, s, in); notify {
		t.Fatal("unchanged candidates must not notify")
	}

	// 同一周期出现多个候选（含重复项与别名），应合并排序后只推一次
	in.Candidates = []string{"30", "8.4.8", "30", "0.31", "0.31.0"}
	in.LatestVersion = "30"
	rec, notify = mergeUpdate(t, s, in)
	if !notify {
		t.Fatal("new candidate must notify once")
	}
	if rec.Reason != models.UpdateReasonVersion || rec.Status != models.UpdateUnread {
		t.Errorf("record = %+v, want unread/version", rec)
	}
	want := []string{"30", "8.4.8", "0.31.0"}
	if strings.Join(rec.Candidates, ",") != strings.Join(want, ",") {
		t.Errorf("candidates = %v, want %v", rec.Candidates, want)
	}

	if _, notify = mergeUpdate(t, s, in); notify {
		t.Error("same candidate set must not notify again")
	}
}

// TestMergeImageUpdateIgnoreUntilNextVersion 覆盖忽略语义：
// 忽略后同版本（含摘要变化）不再提醒，出现更高版本才恢复未读。
func TestMergeImageUpdateIgnoreUntilNextVersion(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 3, ImageName: "library/mysql", Reference: "mysql:8.4.5",
		CurrentVersion: "8.4.5", CurrentDigest: "d1", BaseDigest: "d1",
		LatestVersion: "30", VersionKnown: true, Candidates: []string{"30"},
		DigestNotifiable: true,
	}
	mergeUpdate(t, s, in) // 基线

	in.LatestVersion = "31"
	in.Candidates = []string{"31", "30"}
	rec, notify := mergeUpdate(t, s, in)
	if !notify || rec.Status != models.UpdateUnread {
		t.Fatalf("new version must notify unread: %+v", rec)
	}

	if err := s.IgnoreImageUpdate(3); err != nil {
		t.Fatalf("ignore: %v", err)
	}
	got := mustUpdate(t, s, 3)
	if got.Status != models.UpdateIgnored || got.IgnoredVersion != "31" {
		t.Fatalf("record after ignore = %+v, want ignored/31", got)
	}

	// 同版本摘要变化：忽略期间不提醒
	in.CurrentDigest = "d2"
	in.BaseDigest = "d1"
	in.LatestVersion = "31"
	in.Candidates = []string{"31", "30"}
	if _, notify = mergeUpdate(t, s, in); notify {
		t.Error("ignored version must suppress digest notification")
	}
	if got = mustUpdate(t, s, 3); got.Status != models.UpdateIgnored {
		t.Errorf("status = %q, want ignored", got.Status)
	}

	// 出现更高版本：解除忽略并恢复未读
	in.CurrentDigest = "d3"
	in.LatestVersion = "32"
	in.Candidates = []string{"32", "31", "30"}
	rec, notify = mergeUpdate(t, s, in)
	if !notify || rec.Status != models.UpdateUnread {
		t.Fatalf("newer version after ignore must notify unread: %+v", rec)
	}
	if rec.IgnoredVersion != "" {
		t.Errorf("ignored version = %q, want cleared", rec.IgnoredVersion)
	}
}

// TestMergeImageUpdateVersionUnknownKeepsState 覆盖 tags/list 抖动：
// 版本信息未知时沿用上轮记录，不因版本号变空而误报变化。
func TestMergeImageUpdateVersionUnknownKeepsState(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 4, ImageName: "library/mysql", Reference: "mysql:8.4.5",
		CurrentVersion: "8.4.5", CurrentDigest: "d1", BaseDigest: "d1",
		LatestVersion: "8.4.8", VersionKnown: true, Candidates: []string{"8.4.8"},
	}
	mergeUpdate(t, s, in)

	in.VersionKnown = false
	in.LatestVersion = ""
	in.Candidates = nil
	rec, notify := mergeUpdate(t, s, in)
	if notify {
		t.Fatal("unknown version info must not notify")
	}
	if rec.LatestVersion != "8.4.8" || len(rec.Candidates) != 1 || rec.Candidates[0] != "8.4.8" {
		t.Errorf("record = %+v, want previous version info preserved", rec)
	}
}

// TestMergeImageUpdateNewVersionTag 覆盖「低版本/其它分支新 tag 也提醒」：
// 即使没有可升级差异，也会产生未读记录，且已读后才从列表隐藏。
func TestMergeImageUpdateNewVersionTag(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 11, ImageName: "library/redis", Reference: "redis:8.4.8-alpine",
		CurrentVersion: "8.4.8-alpine", CurrentDigest: "d1", BaseDigest: "d1",
		LatestVersion: "8.4.8-alpine", VersionKnown: true,
	}
	mergeUpdate(t, s, in) // 基线

	in.NewVersionTags = []string{"7.4.12-alpine"}
	rec, notify := mergeUpdate(t, s, in)
	if !notify {
		t.Fatal("new lower version tag must notify")
	}
	if rec.Status != models.UpdateUnread || rec.Reason != models.UpdateReasonVersion {
		t.Errorf("record = %+v, want unread/version", rec)
	}
	if rec.HasUpdate {
		t.Error("lower branch tag is not an upgrade diff, has_update should be false")
	}
	if len(rec.NewTags) != 1 || rec.NewTags[0] != "7.4.12-alpine" {
		t.Errorf("new tags = %v, want [7.4.12-alpine]", rec.NewTags)
	}
	// 无差异但未读 → 页面仍可见、计入未读
	if list, _ := s.ListImageUpdates(""); len(list) != 1 {
		t.Errorf("unread record hidden from list: %+v", list)
	}
	if n, _ := s.UnreadUpdateCount(); n != 1 {
		t.Errorf("unread count = %d, want 1", n)
	}

	// 同一 tag 重复提交不重复提醒
	if _, notify = mergeUpdate(t, s, in); notify {
		t.Error("same new tag must not notify twice")
	}

	// 已读且无差异 → 列表隐藏（一次性的可选版本提醒）
	if err := s.MarkImageUpdateRead(11); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if list, _ := s.ListImageUpdates(""); len(list) != 0 {
		t.Errorf("read record with no diff still listed: %+v", list)
	}
}

// TestMergeImageUpdateVersionBaseline 覆盖 tags/list 恢复时的基线补建：
// 上轮拿不到版本信息、本轮首次拿到时只记基线，不把仓库既有版本刷屏；
// 而「上轮已确认仓库无版本 tag、本轮发布了首个版本」应正常提醒。
func TestMergeImageUpdateVersionBaseline(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 7, ImageName: "library/demo", Reference: "demo:1.0.0",
		CurrentVersion: "1.0.0", CurrentDigest: "d1", BaseDigest: "d1",
		VersionKnown: false,
	}
	mergeUpdate(t, s, in) // 基线（版本信息未知）

	in.VersionKnown = true
	in.LatestVersion = "2.0.0"
	in.Candidates = []string{"2.0.0"}
	if _, notify := mergeUpdate(t, s, in); notify {
		t.Fatal("first version info after failure must only build baseline")
	}

	in.LatestVersion = "2.1.0"
	in.Candidates = []string{"2.1.0", "2.0.0"}
	if _, notify := mergeUpdate(t, s, in); !notify {
		t.Fatal("new version after baseline must notify")
	}

	// 仓库此前确认没有版本 tag（versionKnown=true, latest 空），首个版本发布时正常提醒
	in2 := UpdateInput{
		ImageID: 8, ImageName: "library/demo2", Reference: "demo2:0.9.0",
		CurrentVersion: "0.9.0", CurrentDigest: "e1", BaseDigest: "e1",
		VersionKnown: true, LatestVersion: "", Candidates: nil,
	}
	mergeUpdate(t, s, in2)
	in2.LatestVersion = "1.0.0"
	in2.Candidates = []string{"1.0.0"}
	if _, notify := mergeUpdate(t, s, in2); !notify {
		t.Fatal("first published version tag must notify")
	}
}

// TestAutoReadAchievedUpdates 覆盖两条自动已读路径：推荐版本已用上 / 摘要已同步。
func TestAutoReadAchievedUpdates(t *testing.T) {
	s, _ := Open(":memory:")
	img := &models.Image{
		Name: "library/redis", Reference: "redis:8.4.0", Registry: "registry-1.docker.io",
		Tag: "8.4.0", Source: "docker", LocalDigest: "d1", RemoteDigest: "d3",
		Status: models.StatusUpdateAvailable,
	}
	if err := s.UpsertImage(img); err != nil {
		t.Fatalf("upsert image: %v", err)
	}
	in := UpdateInput{
		ImageID: img.ID, ImageName: img.Name, Reference: img.Reference,
		CurrentVersion: "8.4.0", CurrentDigest: "d3", BaseDigest: "d1",
		LatestVersion: "8.4.1", VersionKnown: true, Candidates: []string{"8.4.1"},
	}
	mergeUpdate(t, s, in) // 基线
	in.LatestVersion = "8.4.2"
	in.Candidates = []string{"8.4.2", "8.4.1"}
	if _, notify := mergeUpdate(t, s, in); !notify {
		t.Fatal("new version must notify")
	}

	// 本地用上了推荐版本 8.4.2 → 自动已读
	if err := s.UpsertImage(&models.Image{
		Name: "library/redis", Reference: "redis:8.4.2", Registry: "registry-1.docker.io",
		Tag: "8.4.2", Source: "docker", LocalDigest: "d9", RemoteDigest: "d9",
		Status: models.StatusUpToDate,
	}); err != nil {
		t.Fatalf("upsert adopted image: %v", err)
	}
	if n, err := s.AutoReadAchievedUpdates(); err != nil || n != 1 {
		t.Fatalf("auto-read updates = %d, %v; want 1", n, err)
	}
	if rec := mustUpdate(t, s, img.ID); rec.Status != models.UpdateRead {
		t.Errorf("status = %q, want read", rec.Status)
	}

	// 摘要同步路径：浮动 tag 收到 update 后本地拉取到同一摘要 → 自动已读
	img2 := &models.Image{
		Name: "library/nginx", Reference: "nginx:latest", Registry: "registry-1.docker.io",
		Tag: "latest", Source: "docker", LocalDigest: "a1", RemoteDigest: "a2",
		Status: models.StatusUpToDate,
	}
	if err := s.UpsertImage(img2); err != nil {
		t.Fatalf("upsert nginx: %v", err)
	}
	digestIn := UpdateInput{
		ImageID: img2.ID, ImageName: img2.Name, Reference: img2.Reference,
		CurrentVersion: "latest", CurrentDigest: "a1", BaseDigest: "a1",
		DigestNotifiable: true,
	}
	mergeUpdate(t, s, digestIn) // 基线
	digestIn.CurrentDigest = "a2"
	if _, notify := mergeUpdate(t, s, digestIn); !notify {
		t.Fatal("digest change must notify")
	}
	if err := s.UpsertImage(&models.Image{
		Name: "library/nginx", Reference: "nginx:latest", Registry: "registry-1.docker.io",
		Tag: "latest", Source: "docker", LocalDigest: "a2", RemoteDigest: "a2",
		Status: models.StatusUpToDate,
	}); err != nil {
		t.Fatalf("upsert synced nginx: %v", err)
	}
	if _, err := s.AutoReadAchievedUpdates(); err != nil {
		t.Fatalf("auto-read digest: %v", err)
	}
	if rec := mustUpdate(t, s, img2.ID); rec.Status != models.UpdateRead {
		t.Errorf("status = %q, want read after digest sync", rec.Status)
	}
}

// TestListImageUpdatesExcludesNoDiff 覆盖列表过滤：无差异的基线记录不展示。
func TestListImageUpdatesExcludesNoDiff(t *testing.T) {
	s, _ := Open(":memory:")
	mergeUpdate(t, s, UpdateInput{
		ImageID: 9, ImageName: "library/busybox", Reference: "busybox:latest",
		CurrentVersion: "latest", CurrentDigest: "same", BaseDigest: "same",
		DigestNotifiable: true,
	})
	if list, _ := s.ListImageUpdates(""); len(list) != 0 {
		t.Errorf("no-diff record listed: %+v", list)
	}
}

// TestMergeImageUpdateDigestOnlyPending 远端摘要变过但本地已同步时不应提醒：
// 只有仍存在待处理差异（base != current）才推送。
func TestMergeImageUpdateDigestOnlyPending(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 5, ImageName: "library/nginx", Reference: "nginx:latest",
		CurrentVersion: "latest", CurrentDigest: "d1", BaseDigest: "d1",
		DigestNotifiable: true,
	}
	mergeUpdate(t, s, in)

	// 本地已拉到 d2（base 同步为 d2）：远端虽变过但没有待处理差异
	in.CurrentDigest = "d2"
	in.BaseDigest = "d2"
	if _, notify := mergeUpdate(t, s, in); notify {
		t.Error("synced digest must not notify")
	}

	// 远端再次前进到 d3，本地仍是 d1：存在差异 → 推送
	in.CurrentDigest = "d3"
	in.BaseDigest = "d1"
	if _, notify := mergeUpdate(t, s, in); !notify {
		t.Error("pending digest difference must notify")
	}
}

// TestUnignoreDoesNotReplaySuppressedState 取消忽略后不追溯推送忽略期间的变化。
func TestUnignoreDoesNotReplaySuppressedState(t *testing.T) {
	s, _ := Open(":memory:")
	in := UpdateInput{
		ImageID: 6, ImageName: "library/mysql", Reference: "mysql:8.4.5",
		CurrentVersion: "8.4.5", CurrentDigest: "d1", BaseDigest: "d1",
		LatestVersion: "30", VersionKnown: true, Candidates: []string{"30"},
		DigestNotifiable: true,
	}
	mergeUpdate(t, s, in) // 基线

	in.LatestVersion = "31"
	in.Candidates = []string{"31", "30"}
	if _, notify := mergeUpdate(t, s, in); !notify {
		t.Fatal("new version must notify")
	}
	if err := s.IgnoreImageUpdate(6); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	// 忽略期间同版本摘要变化：不提醒，但应记为已见
	in.CurrentDigest = "d2"
	in.BaseDigest = "d1"
	if _, notify := mergeUpdate(t, s, in); notify {
		t.Fatal("ignored version must suppress")
	}
	if err := s.UnignoreImageUpdate(6); err != nil {
		t.Fatalf("unignore: %v", err)
	}
	if _, notify := mergeUpdate(t, s, in); notify {
		t.Error("unignore must not replay suppressed same-version change")
	}
}

func mustUpdate(t *testing.T, s *Store, imageID int64) *models.ImageUpdate {
	t.Helper()
	rec, err := s.GetImageUpdate(imageID)
	if err != nil || rec == nil {
		t.Fatalf("get update %d: %v (%v)", imageID, rec, err)
	}
	return rec
}
