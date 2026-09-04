package notification

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DingTalkMessage 钉钉机器人消息体（Markdown 格式）。
type DingTalkMessage struct {
	MsgType  string           `json:"msgtype"`
	Markdown DingTalkMarkdown `json:"markdown"`
}

// DingTalkMarkdown Markdown 消息内容。
type DingTalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// signURL 若配置了加签 secret，则按钉钉加签算法为 Webhook 追加 timestamp 与 sign 参数。
// 加签算法：HMAC-SHA256(stringToSign, secret) → base64 → URLEncode，
// 其中 stringToSign = "<timestamp毫秒>\n<secret>"。secret 为空则原样返回。
func signURL(webhookURL, secret string) string {
	if strings.TrimSpace(secret) == "" {
		return webhookURL
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	sep := "?"
	if strings.Contains(webhookURL, "?") {
		sep = "&"
	}
	return webhookURL + sep + "timestamp=" + timestamp + "&sign=" + url.QueryEscape(sign)
}

// SendDingTalk 向钉钉 Webhook 发送 Markdown 通知。secret 非空时自动加签。
func SendDingTalk(webhookURL, secret, title, content string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}
	target := signURL(webhookURL, secret)
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
	resp, err := client.Post(target, "application/json", bytes.NewReader(body))
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

// NotifyUpdate 发送镜像更新通知到钉钉。secret 非空时自动加签。
func NotifyUpdate(webhookURL, secret, imageRef, oldDigest, newDigest string) error {
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
	return SendDingTalk(webhookURL, secret, title, content)
}

// NotifyNewTag 发送「更高独立版本」弱提醒到钉钉。secret 非空时自动加签。
func NotifyNewTag(webhookURL, secret, imageRef, currentTag, newerTag string) error {
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
	return SendDingTalk(webhookURL, secret, title, content)
}
