package scanner

import (
	"testing"

	"vigil/internal/models"
)

func TestComputeStatus(t *testing.T) {
	cases := []struct {
		name       string
		local      string
		remote     string
		prevRemote string
		want       models.ImageStatus
		wantChange bool
	}{
		{"远端不可达", "sha256:a", "", "", models.StatusUnknown, false},
		{"有本地且一致", "sha256:a", "sha256:a", "sha256:a", models.StatusUpToDate, false},
		{"有本地且过期", "sha256:a", "sha256:b", "sha256:a", models.StatusUpdateAvailable, true},
		{"仅远端首次基线", "", "sha256:a", "", models.StatusUpToDate, false},
		{"仅远端发生变化", "", "sha256:b", "sha256:a", models.StatusUpdateAvailable, true},
		{"仅远端未变", "", "sha256:a", "sha256:a", models.StatusUpToDate, false},
	}
	for _, c := range cases {
		got, changed := computeStatus(c.local, c.remote, c.prevRemote)
		if got != c.want || changed != c.wantChange {
			t.Errorf("%s: computeStatus=%q changed=%v, want %q %v", c.name, got, changed, c.want, c.wantChange)
		}
	}
}

// TestCandidateVersions 覆盖候选版本筛选：版本 tag 取更高版本并去重排序；
// 浮动 tag 在 Digest-Only 下无候选，在 Pin-Watch 下全部版本 tag 均为候选。
func TestCandidateVersions(t *testing.T) {
	tags := []string{"latest", "1.0", "1.0.0", "1.1.0", "1.1.0-rc.1", "2.0.0"}

	got := candidateVersions(tags, "1.0.0", false)
	want := []string{"2.0.0", "1.1.0", "1.1.0-rc.1"}
	if len(got) != len(want) {
		t.Fatalf("candidates for 1.0.0 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates for 1.0.0 = %v, want %v", got, want)
		}
	}

	if got := candidateVersions(tags, "stable", false); len(got) != 0 {
		t.Errorf("digest-only floating tag candidates = %v, want none", got)
	}
	got = candidateVersions(tags, "stable", true)
	if len(got) != 4 || got[0] != "2.0.0" || got[3] != "1.0.0" {
		t.Errorf("pin-watch floating tag candidates = %v, want [2.0.0 1.1.0 1.1.0-rc.1 1.0.0]", got)
	}
}
