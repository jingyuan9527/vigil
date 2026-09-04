package models

import "testing"

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name string
		mode string
		tag  string
		want string
	}{
		{"浮动标签 latest", ModeAuto, "latest", ModeDigestOnly},
		{"浮动标签 nightly", ModeAuto, "nightly", ModeDigestOnly},
		{"浮动标签 beta", ModeAuto, "beta", ModeDigestOnly},
		{"版本号 8.4.5", ModeAuto, "8.4.5", ModePinWatch},
		{"版本号带后缀 8.4.5-alpine", ModeAuto, "8.4.5-alpine", ModePinWatch},
		{"版本号 v3.9", ModeAuto, "v3.9", ModePinWatch},
		{"非版本非浮动 lts → 默认 digest-only", ModeAuto, "lts", ModeDigestOnly},
		{"非版本非浮动 edge → 默认 digest-only", ModeAuto, "edge", ModeDigestOnly},
		{"用户覆写 digest-only 优先于版本号", ModeDigestOnly, "8.4.5", ModeDigestOnly},
		{"用户覆写 pin-watch 优先于浮动标签", ModePinWatch, "latest", ModePinWatch},
		{"空 mode 按 auto 处理", "", "8.4.5", ModePinWatch},
		{"未知 mode 按 auto 处理", "garbage", "latest", ModeDigestOnly},
	}
	for _, c := range cases {
		got := ResolveMode(c.mode, c.tag)
		if got != c.want {
			t.Errorf("%s: ResolveMode(%q,%q)=%q, want %q", c.name, c.mode, c.tag, got, c.want)
		}
	}
}

func TestIsFloatingTag(t *testing.T) {
	for _, tag := range []string{"latest", "nightly", "dev", "canary", "beta"} {
		if !IsFloatingTag(tag) {
			t.Errorf("IsFloatingTag(%q)=false, want true", tag)
		}
	}
	for _, tag := range []string{"8.4.5", "lts", "stable", ""} {
		if IsFloatingTag(tag) {
			t.Errorf("IsFloatingTag(%q)=true, want false", tag)
		}
	}
}
