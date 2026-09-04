package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DingTalkMessage 钉钉机器人消息体（Markdown 格式）。
type DingTalkMessage struct {
	MsgType  string          `json:"msgtype"`
	Markdown DingTalkMarkdown `json:"markdown"`
}

// DingTalkMarkdown Markdown 消息内容。
type DingTalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// SendDingTalk 向钉钉 Webhook 发送 Markdown 通知。
func SendDingTalk(webhookURL, title, content string) error {
	if webhookURL == "" {
		return nil
	}
	msg := DingTalkMessage{
		MsgType: "markdown",
		Markdown: DingTalkMarkdown{
			Title: title,
			Text:  content,
		},
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal dingtalk message: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send dingtalk: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dingtalk returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// NotifyUpdate 发送镜像更新通知到钉钉。
func NotifyUpdate(webhookURL, imageRef, oldDigest, newDigest string) error {
	title := "DockMon 镜像更新通知"
	content := fmt.Sprintf(
		"### 🔔 镜像更新提醒\n\n"+
			"**镜像**: %s\n\n"+
			"**旧摘要**: `%s`\n\n"+
			"**新摘要**: `%s`\n\n"+
			"**时间**: %s\n",
		imageRef, oldDigest, newDigest,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	return SendDingTalk(webhookURL, title, content)
}

// NotifyNewTag 发送「更高独立版本」弱提醒到钉钉。
func NotifyNewTag(webhookURL, imageRef, currentTag, newerTag string) error {
	title := "DockMon 可选新版本提醒"
	content := fmt.Sprintf(
		"### ⭐ 镜像出现更新的独立版本\n\n"+
			"**镜像**: %s\n\n"+
			"**当前版本**: `%s`\n\n"+
			"**可选新版本**: `%s`\n\n"+
			"**说明**: 检测到仓库存在更高版本（如大版本升级），当前仍在监控旧版本，可按需升级。\n\n"+
			"**时间**: %s\n",
		imageRef, currentTag, newerTag,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	return SendDingTalk(webhookURL, title, content)
}
