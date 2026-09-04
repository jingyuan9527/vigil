// Package version 提供 Docker 镜像 tag 的语义化版本解析与比较，
// 用于「发现仓库中比当前固定 tag 更新的版本」。
package version

import "strings"

// nums 表示一个 tag 解析出的语义版本数字序列，如 8.4.7 -> [8,4,7]。
type nums []int

// ParseTag 尝试把镜像 tag 解析为语义版本数字序列。
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

// IsRollingTag 报告 tag 是否为「移动/滚动」tag（latest、lts、edge、current 等）。
// 滚动 tag 采用 digest 追踪（有更新即提示），不参与「发现更高独立版本」。
func IsRollingTag(tag string) bool {
	_, ok := ParseTag(tag)
	return !ok
}

// Less 报告 a 是否严格低于 b。两者须都能解析为语义版本，否则返回 (false,false)。
func Less(a, b string) (bool, bool) {
	an, aok := ParseTag(a)
	bn, bok := ParseTag(b)
	if !aok || !bok {
		return false, false
	}
	return compare(an, bn) < 0, true
}

// Higher 返回 (a 是否高于 b, 两者是否都可解析)。
func Higher(a, b string) (bool, bool) {
	an, aok := ParseTag(a)
	bn, bok := ParseTag(b)
	if !aok || !bok {
		return false, false
	}
	return compare(an, bn) > 0, true
}

// NewerAvailable 在候选 tags 中查找比 current 更高的语义版本，返回其中最高的那个。
// 找不到可解析且更高的 tag 时返回 ""。
func NewerAvailable(current string, tags []string) string {
	cur, ok := ParseTag(current)
	if !ok {
		// current 本身不是固定版本（如 latest）→ 不做仓库级新版本发现
		return ""
	}
	var best string
	var bestN nums
	for _, t := range tags {
		n, ok := ParseTag(t)
		if !ok {
			continue
		}
		if compare(n, cur) > 0 && (best == "" || compare(n, bestN) > 0) {
			best = t
			bestN = n
		}
	}
	return best
}

// compare 按字典序比较两个数字序列，短的缺位视为 0。
// 返回 -1 / 0 / 1。
func compare(a, b nums) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
