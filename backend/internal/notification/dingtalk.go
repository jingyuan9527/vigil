package notification

import (
	"bytes"
	"context"
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

// dingtalkConfig 钉钉渠道配置。
type dingtalkConfig struct {
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"` // 加签密钥，可选
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
func SendDingTalk(ctx context.Context, webhookURL, secret, title, content string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("dingtalk webhook url is empty")
	}
	if ctx == nil {
		ctx = context.Background()
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build dingtalk request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
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

// dingtalkProvider 钉钉渠道实现。
type dingtalkProvider struct{}

// Validate 校验钉钉配置是否具备发送条件（webhook 必填）。
func (dingtalkProvider) Validate(cfg string) error {
	c, err := decodeConfig[dingtalkConfig](cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Webhook) == "" {
		return fmt.Errorf("钉钉渠道缺少 webhook")
	}
	return nil
}

// Send 把统一消息以 Markdown 形式推送到钉钉机器人。
func (dingtalkProvider) Send(ctx context.Context, cfg string, msg Message) error {
	c, err := decodeConfig[dingtalkConfig](cfg)
	if err != nil {
		return err
	}
	return SendDingTalk(ctx, c.Webhook, c.Secret, msg.Title, msg.Markdown)
}

func init() { Register("dingtalk", dingtalkProvider{}) }
