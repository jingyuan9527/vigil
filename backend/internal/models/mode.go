package models

import "dockmon/internal/version"

// 检测模式（每个镜像二选一，默认由 ResolveMode 按 tag 自动判定，用户可手动覆写）。
const (
	// ModeAuto 自动识别：浮动标签 → Digest-Only，数字版本号 → Pin-Watch，其余默认 Digest-Only。
	ModeAuto = "auto"
	// ModeDigestOnly 仅校验当前标签摘要：当前 tag 内容被更新即告警；
	// 不拉取仓库全部 tag 列表，不会通知别的新版本。
	ModeDigestOnly = "digest-only"
	// ModePinWatch 锁定版本 + 监视新标签：仍对比当前 tag 摘要，
	// 但锁定 tag 被仓库覆盖时不告警；额外巡检仓库 tag 列表，
	// 出现从未见过的新版本 tag → 发送「新版本发布通知」（每个 tag 仅一次）。
	ModePinWatch = "pin-watch"
)

// floatingTags 内置浮动标签名单：内容随时间移动，只需 digest 追踪，无需版本巡检。
var floatingTags = map[string]bool{
	"latest": true,
	"nightly": true,
	"dev":    true,
	"canary": true,
	"beta":   true,
}

// IsFloatingTag 报告 tag 是否为内置浮动标签。
func IsFloatingTag(tag string) bool { return floatingTags[tag] }

// ResolveMode 解析镜像的生效检测模式。
//
// 用户覆写（digest-only / pin-watch）优先；mode 为空或 auto 时按 tag 自动识别：
//   - 浮动标签（latest/nightly/dev/canary/beta）→ Digest-Only
//   - 可解析为语义版本号（如 8.4.5、v3.9、1.2.3-alpine）→ Pin-Watch
//   - 其余 → Digest-Only（默认更保守，避免无谓 tag 巡检）
func ResolveMode(mode, tag string) string {
	switch mode {
	case ModeDigestOnly, ModePinWatch:
		return mode
	}
	if IsFloatingTag(tag) {
		return ModeDigestOnly
	}
	if _, ok := version.ParseTag(tag); ok {
		return ModePinWatch
	}
	return ModeDigestOnly
}
