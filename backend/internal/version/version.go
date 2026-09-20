// Package version 提供 Docker 镜像 tag 的数字版本解析，
// 用于检测模式的自动识别（数字版本号 tag → Pin-Watch）。
package version

import (
	"sort"
	"strings"
)

// nums 表示一个 tag 解析出的数字版本序列，如 8.4.7 -> [8,4,7]。
type nums []int

// ParseTag 尝试把镜像 tag 解析为数字版本序列。
// 返回 (ok)。仅解析以纯数字段开头的 tag（可带 v 前缀与 -后缀）：
//
//	"8.4.7"        -> [8,4,7]   ok
//	"26"           -> [26]      ok
//	"v3.9"         -> [3,9]     ok
//	"1.2.3-alpine" -> [1,2,3]   ok
//	"latest"       -> 不可解析   !ok
//	"lts" / "edge" -> 不可解析   !ok
func ParseTag(tag string) (nums, bool) {
	tag = strings.TrimSpace(tag)
	// 去掉滚动标记常用的 -suffix（如 -alpine、-slim），保留数字主体
	if i := strings.Index(tag, "-"); i > 0 {
		tag = tag[:i]
	}
	tag = strings.TrimPrefix(tag, "v")
	if tag == "" {
		return nil, false
	}
	segs := strings.Split(tag, ".")
	out := make(nums, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			return nil, false
		}
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				return nil, false
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// LatestTag 返回 tags 中数字版本号最高的 tag（原样返回，如 "v1.4.1"）；
// 没有可解析为数字版本的 tag（如 latest/lts/edge）时返回空串。
// 缺失段按 0 处理，因此 "1.4" 与 "1.4.0" 等价，比较规则同 Compare。
func LatestTag(tags []string) string {
	best := ""
	var bestNums nums
	for _, t := range tags {
		n, ok := ParseTag(t)
		if !ok {
			continue
		}
		if best == "" || Compare(n, bestNums) > 0 {
			best, bestNums = t, n
		}
	}
	return best
}

// BestTag 返回一组 tag 中「最高」的代表 tag，用于同 digest 别名合并时挑选
// 主展示 tag。排序规则见 SortTags。组内没有可解析为版本号的 tag 时返回空串。
func BestTag(tags []string) string {
	sorted := append([]string(nil), tags...)
	SortTags(sorted)
	for _, t := range sorted {
		if _, ok := ParseTag(t); ok {
			return t
		}
	}
	return ""
}

// SortTags 按展示优先级原地排序 tag：版本号高者在前；同版本时正式版
// （无 - 后缀）优先，其次数字段更多者（0.31.0 优先 0.31），最后按字符串
// 升序兜底，保证结果确定。不可解析为版本的 tag 排在最后并按字符串升序。
func SortTags(tags []string) {
	sort.SliceStable(tags, func(i, j int) bool { return tagLess(tags[i], tags[j]) })
}

// CompareTag 比较两个原始 tag 的版本号，规则同 Compare；任一方不可解析为
// 版本号时退化为字符串比较。
func CompareTag(a, b string) int {
	na, oka := ParseTag(a)
	nb, okb := ParseTag(b)
	if oka && okb {
		return Compare(na, nb)
	}
	return strings.Compare(a, b)
}

// tagLess 定义 tag 的展示优先级排序（a 排在 b 前返回 true）。
func tagLess(a, b string) bool {
	na, oka := ParseTag(a)
	nb, okb := ParseTag(b)
	switch {
	case oka && okb:
		if c := Compare(na, nb); c != 0 {
			return c > 0
		}
		pa, pb := strings.Contains(a, "-"), strings.Contains(b, "-")
		if pa != pb {
			return !pa
		}
		if len(na) != len(nb) {
			return len(na) > len(nb)
		}
		return a < b
	case oka:
		return true
	case okb:
		return false
	default:
		return a < b
	}
}

// Compare 按段比较两个版本序列：a<b 返回 -1，相等返回 0，a>b 返回 1。
// 缺失的段按 0 处理（8.4 等价 8.4.0），因此 8.4 < 8.4.5、26 > 8.4.5。
func Compare(a, b nums) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}
