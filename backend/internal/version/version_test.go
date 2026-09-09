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

func TestLatestTag(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"版本+浮动混合取最高版本", []string{"latest", "v1.2.0", "1.2.1", "v1.4.1", "dev"}, "v1.4.1"},
		{"v 前缀与无前缀同版本保留先见者", []string{"v1.4.1", "1.4.1"}, "v1.4.1"},
		{"非版本 tag 全被忽略", []string{"latest", "lts", "edge", "dev"}, ""},
		{"大版本号高于细分版本", []string{"8.4.5", "26", "v3.9"}, "26"},
		{"带后缀只比数字主体", []string{"1.2.3-alpine", "1.10"}, "1.10"},
		{"空列表", nil, ""},
		{"无 v 前缀版本原样返回", []string{"latest", "8.4.7"}, "8.4.7"},
	}
	for _, c := range cases {
		if got := LatestTag(c.in); got != c.want {
			t.Errorf("%s: LatestTag(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"8.4.5", "8.4.5", 0},
		{"8.4", "8.4.0", 0},
		{"8.4.5-alpine", "8.4.5", 0},
		{"8.4.6", "8.4.5", 1},
		{"8.4.5", "8.4.6", -1},
		{"26", "8.4.5", 1},
		{"8.4.5", "26", -1},
		{"9", "8.4.5", 1},
		{"1.2.3", "1.10", -1},
	}
	for _, c := range cases {
		na, okA := ParseTag(c.a)
		nb, okB := ParseTag(c.b)
		if !okA || !okB {
			t.Fatalf("ParseTag(%q/%q) unexpectedly unparseable", c.a, c.b)
		}
		if got := Compare(na, nb); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
