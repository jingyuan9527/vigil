package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// webhookConfig 通用 Webhook 渠道配置。
// Format 为 "json"（默认，POST {title,message,markdown,time}）或 "text"（POST 纯文本正文）。
type webhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"` // 可选自定义请求头
	Format  string            `json:"format"`
}

// webhookPayload 通用 Webhook 的 JSON 载荷（format=json 时发送）。
type webhookPayload struct {
	Title    string `json:"title"`
	Message  string `json:"message"`  // 纯文本
	Markdown string `json:"markdown"` // Markdown
	Time     string `json:"time"`
}

// webhookProvider 通用 Webhook 渠道实现：任何支持 HTTP POST 的接收端点。
type webhookProvider struct{}

// Validate 校验通用 Webhook 配置（url 必填，format 取值受限）。
func (webhookProvider) Validate(cfg string) error {
	c, err := decodeConfig[webhookConfig](cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("通用 Webhook 渠道缺少 url")
	}
	if f := strings.ToLower(c.Format); f != "" && f != "json" && f != "text" {
		return fmt.Errorf("通用 Webhook format 仅支持 json / text")
	}
	return nil
}

// Send 发送消息：json 格式带元数据，text 格式只发纯文本正文。
func (webhookProvider) Send(ctx context.Context, cfg string, msg Message) error {
	c, err := decodeConfig[webhookConfig](cfg)
	if err != nil {
		return err
	}
	url := strings.TrimSpace(c.URL)
	if url == "" {
		return fmt.Errorf("webhook url is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	contentType := "application/json"
	var payload []byte
	if strings.ToLower(c.Format) == "text" {
		contentType = "text/plain; charset=utf-8"
		payload = []byte(msg.Text)
	} else {
		body := webhookPayload{
			Title:    msg.Title,
			Message:  msg.Text,
			Markdown: msg.Markdown,
			Time:     time.Now().Format("2006-01-02 15:04:05"),
		}
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal webhook payload: %w", err)
		}
		payload = b
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func init() { Register("webhook", webhookProvider{}) }
