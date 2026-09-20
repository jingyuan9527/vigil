package notification

import (
	"fmt"
	"strings"
	"time"
)

// Message 统一通知载体：同一条更新按各渠道能力取用 Markdown 或纯文本。
// Markdown 面向钉钉/企微等「吃 md」的渠道；Text 面向 Telegram/飞书/通用 text webhook 等。
type Message struct {
	Title    string
	Markdown string
	Text     string
}

// UpdateMessage 构建「有新版本」提醒。latestTag 为仓库远端版本号最高的 tag，
// 非空时附在提醒里，为空时跳过 tag 行（保持老格式）。
func UpdateMessage(imageRef, oldDigest, newDigest, latestTag string) Message {
	now := time.Now().Format("2006-01-02 15:04:05")
	tagMd := ""
	tagText := ""
	if latestTag != "" {
		tagMd = fmt.Sprintf("**最新Tag**: `%s`\n\n", latestTag)
		tagText = fmt.Sprintf("最新Tag: %s\n\n", latestTag)
	}
	return Message{
		Title: "Vigil 镜像更新通知",
		Markdown: fmt.Sprintf(
			"### 🔔 镜像更新提醒\n\n"+
				"**镜像**: %s\n\n"+
				"%s"+
				"**旧摘要**: `%s`\n\n"+
				"**新摘要**: `%s`\n\n"+
				"**时间**: %s\n",
			imageRef, tagMd, oldDigest, newDigest, now),
		Text: fmt.Sprintf(
			"🔔 镜像更新提醒\n\n"+
				"镜像: %s\n\n"+
				"%s"+
				"旧摘要: %s\n\n"+
				"新摘要: %s\n\n"+
				"时间: %s\n",
			imageRef, tagText, oldDigest, newDigest, now),
	}
}

// NewTagMessage 构建「可选更新」（出现更高独立版本 tag）弱提醒。
// aliases 是同一远端镜像（同 digest）的其余别名 tag，展示时并入括号，
// 避免同一版本被多个标签重复提醒；为空则与老格式一致。
func NewTagMessage(imageRef, currentTag, newerTag string, aliases []string) Message {
	now := time.Now().Format("2006-01-02 15:04:05")
	label := FormatNewerTag(newerTag, aliases)
	return Message{
		Title: "Vigil 可选新版本提醒",
		Markdown: fmt.Sprintf(
			"### ⭐ 镜像出现更新的独立版本\n\n"+
				"**镜像**: %s\n\n"+
				"**当前版本**: `%s`\n\n"+
				"**可选新版本**: `%s`\n\n"+
				"**说明**: 检测到仓库存在更高版本（如大版本升级），当前仍在监控旧版本，可按需升级。\n\n"+
				"**时间**: %s\n",
			imageRef, currentTag, label, now),
		Text: fmt.Sprintf(
			"⭐ 镜像出现更新的独立版本\n\n"+
				"镜像: %s\n\n"+
				"当前版本: %s\n\n"+
				"可选新版本: %s\n\n"+
				"说明: 检测到仓库存在更高版本（如大版本升级），当前仍在监控旧版本，可按需升级。\n\n"+
				"时间: %s\n",
			imageRef, currentTag, label, now),
	}
}

// maxTagAliases 是展示同 digest 别名 tag 的最大个数，超出折叠为「…等N个」，
// 避免一个镜像挂几十个别名时消息行无限长。
const maxTagAliases = 4

// FormatNewerTag 把主版本 tag 与同 digest 别名合并展示为 `主(别名, 别名…)`；
// 无别名时原样返回。别名超过 maxTagAliases 个时截断并附「…等N个」。
func FormatNewerTag(tag string, aliases []string) string {
	if len(aliases) == 0 {
		return tag
	}
	if len(aliases) <= maxTagAliases {
		return fmt.Sprintf("%s(%s)", tag, strings.Join(aliases, ", "))
	}
	return fmt.Sprintf("%s(%s, …等%d个)", tag, strings.Join(aliases[:maxTagAliases], ", "), len(aliases))
}

// TestMessage 构建渠道连通性测试消息。
func TestMessage() Message {
	now := time.Now().Format("2006-01-02 15:04:05")
	return Message{
		Title: "Vigil 连通性测试",
		Markdown: fmt.Sprintf(
			"### ✅ 连通性测试成功\n\n"+
				"这是一条来自 Vigil 的测试消息，说明该通知渠道配置正确。\n\n"+
				"**时间**: %s\n", now),
		Text: fmt.Sprintf(
			"✅ 连通性测试成功\n\n"+
				"这是一条来自 Vigil 的测试消息，说明该通知渠道配置正确。\n\n"+
				"时间: %s\n", now),
	}
}
