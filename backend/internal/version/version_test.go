package version

import "testing"

func TestParseTag(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		nums []int
	}{
		{"8.4.7", true, []int{8, 4, 7}},
		{"26", true, []int{26}},
		{"v3.9", true, []int{3, 9}},
		{"1.2.3-alpine", true, []int{1, 2, 3}},
		{"9.0.1-innovation", true, []int{9, 0, 1}},
		{"8.0", true, []int{8, 0}},
		{"latest", false, nil},
		{"lts", false, nil},
		{"edge", false, nil},
		{"8.4.x", false, nil},
		{"", false, nil},
		{"not-a-version", false, nil},
	}
	for _, c := range cases {
		n, ok := ParseTag(c.in)
		if ok != c.ok {
			t.Errorf("ParseTag(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if len(n) != len(c.nums) {
			t.Errorf("ParseTag(%q) len=%d want %d", c.in, len(n), len(c.nums))
			continue
		}
		for i := range n {
			if n[i] != c.nums[i] {
				t.Errorf("ParseTag(%q)[%d]=%d want %d", c.in, i, n[i], c.nums[i])
			}
		}
	}
}

func TestIsRollingTag(t *testing.T) {
	rolling := []string{"latest", "lts", "edge", "rolling", "alpine", "8.4.x"}
	for _, tag := range rolling {
		if !IsRollingTag(tag) {
			t.Errorf("IsRollingTag(%q)=false, want true (rolling)", tag)
		}
	}
	fixed := []string{"8.4.7", "26", "v3.9", "1.2.3-slim"}
	for _, tag := range fixed {
		if IsRollingTag(tag) {
			t.Errorf("IsRollingTag(%q)=true, want false (fixed)", tag)
		}
	}
}

func TestCompare(t *testing.T) {
	// (a, b, aHigher, bHigher, parseable)
	cases := []struct {
		a, b      string
		aHigher   bool
		parseable bool
	}{
		{"26", "8.4.7", true, true},
		{"8.4.8", "8.4.7", true, true},
		{"8.0.36", "8.4.7", false, true},
		{"8.4.7", "8.4.7", false, true},
		{"8.4", "8.4.7", false, true},
		{"5.7.44", "8.4.7", false, true},
		{"latest", "8.4.7", false, false},
	}
	for _, c := range cases {
		ah, ok := Higher(c.a, c.b)
		if ok != c.parseable {
			t.Errorf("Higher(%q,%q) ok=%v want %v", c.a, c.b, ok, c.parseable)
			continue
		}
		if ah != c.aHigher {
			t.Errorf("Higher(%q,%q)=%v want %v", c.a, c.b, ah, c.aHigher)
		}
	}
}

func TestNewerAvailable(t *testing.T) {
	tags := []string{"8.4.7", "8.4.8", "8.0.36", "5.7.44", "26", "latest", "8.4"}
	if got := NewerAvailable("8.4.7", tags); got != "26" {
		t.Errorf("NewerAvailable(8.4.7)=%q want 26", got)
	}
	if got := NewerAvailable("26", tags); got != "" {
		t.Errorf("NewerAvailable(26)=%q want ''", got)
	}
	if got := NewerAvailable("latest", tags); got != "" {
		t.Errorf("NewerAvailable(latest)=%q want ''", got)
	}
	if got := NewerAvailable("8.4", tags); got != "26" {
		t.Errorf("NewerAvailable(8.4)=%q want 26", got)
	}
	// 无更高版本
	if got := NewerAvailable("9.0", []string{"9.0", "8.4.7", "latest"}); got != "" {
		t.Errorf("NewerAvailable(no higher)=%q want ''", got)
	}
}
