package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// wecomConfig 企业微信群机器人渠道配置。
type wecomConfig struct {
	Webhook string `json:"webhook"`
}

// wecomMessage 企业微信机器人 Markdown 消息体。
type wecomMessage struct {
	MsgType  string        `json:"msgtype"`
	Markdown wecomMarkdown `json:"markdown"`
}

type wecomMarkdown struct {
	Content string `json:"content"`
}

// wecomProvider 企业微信群机器人渠道实现。
type wecomProvider struct{}

// Validate 校验企业微信配置（webhook 必填）。
func (wecomProvider) Validate(cfg string) error {
	c, err := decodeConfig[wecomConfig](cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Webhook) == "" {
		return fmt.Errorf("企业微信渠道缺少 webhook")
	}
	return nil
}

// Send 把统一消息以 Markdown 形式推送到企业微信群机器人。
func (wecomProvider) Send(ctx context.Context, cfg string, msg Message) error {
	c, err := decodeConfig[wecomConfig](cfg)
	if err != nil {
		return err
	}
	url := strings.TrimSpace(c.Webhook)
	if url == "" {
		return fmt.Errorf("wecom webhook is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	body, err := json.Marshal(wecomMessage{
		MsgType:  "markdown",
		Markdown: wecomMarkdown{Content: msg.Markdown},
	})
	if err != nil {
		return fmt.Errorf("marshal wecom message: %w", err)
	}
	return postJSON(ctx, url, body, "wecom")
}

func init() { Register("wecom", wecomProvider{}) }
