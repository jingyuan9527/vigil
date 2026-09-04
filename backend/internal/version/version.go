// Package version 提供 Docker 镜像 tag 的数字版本解析，
// 用于检测模式的自动识别（数字版本号 tag → Pin-Watch）。
package version

import "strings"

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