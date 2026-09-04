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