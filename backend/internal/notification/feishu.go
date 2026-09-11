package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// feishuConfig 飞书自定义机器人渠道配置。
type feishuConfig struct {
	Webhook string `json:"webhook"`
}

// feishuCard 飞书 interactive 消息卡片（lark_md 不支持标题语法，用纯文本更稳）。
type feishuCard struct {
	MsgType string     `json:"msg_type"`
	Card    feishuBody `json:"card"`
}

type feishuBody struct {
	Header   feishuHeader    `json:"header"`
	Elements []feishuElement `json:"elements"`
}

type feishuHeader struct {
	Title    feishuTitle `json:"title"`
	Template string      `json:"template"`
}

type feishuTitle struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type feishuElement struct {
	Tag  string     `json:"tag"`
	Text feishuText `json:"text"`
}

type feishuText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// feishuProvider 飞书群自定义机器人渠道实现。
type feishuProvider struct{}

// Validate 校验飞书配置（webhook 必填）。
func (feishuProvider) Validate(cfg string) error {
	c, err := decodeConfig[feishuConfig](cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Webhook) == "" {
		return fmt.Errorf("飞书渠道缺少 webhook")
	}
	return nil
}

// Send 把统一消息包装成 interactive 卡片推送到飞书群机器人；内容走纯文本
// （lark_md 不支持部分 markdown 语法，纯文本渲染最稳）。
func (feishuProvider) Send(ctx context.Context, cfg string, msg Message) error {
	c, err := decodeConfig[feishuConfig](cfg)
	if err != nil {
		return err
	}
	url := strings.TrimSpace(c.Webhook)
	if url == "" {
		return fmt.Errorf("feishu webhook is empty")
	}
	body, err := json.Marshal(feishuCard{
		MsgType: "interactive",
		Card: feishuBody{
			Header: feishuHeader{
				Title:    feishuTitle{Tag: "plain_text", Content: msg.Title},
				Template: "blue",
			},
			Elements: []feishuElement{{
				Tag:  "div",
				Text: feishuText{Tag: "lark_md", Content: msg.Text},
			}},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal feishu message: %w", err)
	}
	return postJSON(ctx, url, body, "feishu")
}

func init() { Register("feishu", feishuProvider{}) }
