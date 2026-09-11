package notification

import "strings"

// mdToHTML 把通知里的受限 Markdown 子集转成 Telegram 可用的 HTML。
// 先转义 HTML 特殊字符，再应用加粗/行内代码规则。通知内容受项目控制
// （来源仅为镜像引用与摘要），无需通用 Markdown 解析器。
//
// 支持：`### ` 行首标题 → <b>，`**x**` → <b>x</b>，“ `x` “ → <code>x</code>。
// 顺序敏感：先转义防注入，再套标签（标签为 ASCII，转义不影响它们）。
func mdToHTML(s string) string {
	esc := func(b byte) string {
		switch b {
		case '&':
			return "&amp;"
		case '<':
			return "&lt;"
		case '>':
			return "&gt;"
		case '"':
			return "&quot;"
		default:
			return ""
		}
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if e := esc(s[i]); e != "" {
			b.WriteString(e)
			continue
		}
		b.WriteByte(s[i])
	}

	out := b.String()
	lines := strings.Split(out, "\n")
	b.Reset()
	for _, ln := range lines {
		t := strings.TrimPrefix(ln, "### ")
		if t != ln { // 行首标题 → 加粗
			t = "<b>" + t + "</b>"
		}
		t = applyBold(t)
		t = applyCode(t)
		b.WriteString(t + "\n")
	}
	// Split 会在末尾产生一个空段（字符串以 \n 结尾时），一并收尾，避免双换行噪音。
	return strings.TrimRight(b.String(), "\n")
}

// applyBold 把 **x** 替换为 <b>x</b>（不匹配则原样返回）。
func applyBold(s string) string {
	for {
		start := strings.Index(s, "**")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start+2:], "**")
		if end < 0 {
			return s
		}
		end = start + 2 + end
		inner := s[start+2 : end]
		s = s[:start] + "<b>" + inner + "</b>" + s[end+2:]
	}
}

// applyCode 把 `x` 替换为 <code>x</code>（不匹配则原样返回）。
func applyCode(s string) string {
	for {
		start := strings.Index(s, "`")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start+1:], "`")
		if end < 0 {
			return s
		}
		end = start + 1 + end
		inner := s[start+1 : end]
		s = s[:start] + "<code>" + inner + "</code>" + s[end+1:]
	}
}
