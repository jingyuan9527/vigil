package version

import (
	"reflect"
	"testing"
)

// TestVersionKey 覆盖「同一版本」判等：末尾 0 段与 v 前缀归一，预发布后缀保留区分。
func TestVersionKey(t *testing.T) {
	cases := []struct {
		a, b  string
		equal bool
	}{
		{"0.31", "0.31.0", true},
		{"v1.4", "1.4.0", true},
		{"8.4.5", "8.4.5-alpine", false},
		{"0.31.0", "0.31.0-rc.2", false},
		{"latest", "LATEST", true},
	}
	for _, c := range cases {
		if got := VersionKey(c.a) == VersionKey(c.b); got != c.equal {
			t.Errorf("VersionKey(%q)==VersionKey(%q) = %v, want %v", c.a, c.b, got, c.equal)
		}
	}
}

// TestDedupTags 覆盖候选版本去重与排序：同版本别名只留展示优先级更高者，
// 不同预发布后缀各自保留，结果按版本号降序。
func TestDedupTags(t *testing.T) {
	got := DedupTags([]string{"0.31", "0.31.0-rc.2", "0.31.0", "26", "30", "30"})
	want := []string{"30", "26", "0.31.0", "0.31.0-rc.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DedupTags = %v, want %v", got, want)
	}
}

// TestDedupTagsEmpty 输入为空/仅不可解析 tag 时也应返回确定结果。
func TestDedupTagsUnparsable(t *testing.T) {
	got := DedupTags([]string{"latest", "edge", "latest"})
	want := []string{"edge", "latest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DedupTags = %v, want %v", got, want)
	}
}
