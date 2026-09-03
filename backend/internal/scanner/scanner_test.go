package scanner

import (
	"testing"

	"dockmon/internal/models"
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
