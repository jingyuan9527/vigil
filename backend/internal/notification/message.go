package notification

import (
	"fmt"
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
func NewTagMessage(imageRef, currentTag, newerTag string) Message {
	now := time.Now().Format("2006-01-02 15:04:05")
	return Message{
		Title: "Vigil 可选新版本提醒",
		Markdown: fmt.Sprintf(
			"### ⭐ 镜像出现更新的独立版本\n\n"+
				"**镜像**: %s\n\n"+
				"**当前版本**: `%s`\n\n"+
				"**可选新版本**: `%s`\n\n"+
				"**说明**: 检测到仓库存在更高版本（如大版本升级），当前仍在监控旧版本，可按需升级。\n\n"+
				"**时间**: %s\n",
			imageRef, currentTag, newerTag, now),
		Text: fmt.Sprintf(
			"⭐ 镜像出现更新的独立版本\n\n"+
				"镜像: %s\n\n"+
				"当前版本: %s\n\n"+
				"可选新版本: %s\n\n"+
				"说明: 检测到仓库存在更高版本（如大版本升级），当前仍在监控旧版本，可按需升级。\n\n"+
				"时间: %s\n",
			imageRef, currentTag, newerTag, now),
	}
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
