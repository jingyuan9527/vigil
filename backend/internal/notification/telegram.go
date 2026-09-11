package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// telegramConfig Telegram Bot 渠道配置。
// APIBase 默认走官方 https://api.telegram.org；自建 Bot API 服务时可指向自己的端点。
type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	APIBase  string `json:"api_base"`
}

// telegramMessage Telegram sendMessage 消息体（HTML 解析模式）。
type telegramMessage struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// telegramProvider Telegram Bot 渠道实现。
type telegramProvider struct{}

// Validate 校验 Telegram 配置（bot_token 与 chat_id 必填）。
func (telegramProvider) Validate(cfg string) error {
	c, err := decodeConfig[telegramConfig](cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.BotToken) == "" {
		return fmt.Errorf("Telegram 渠道缺少 bot_token")
	}
	if strings.TrimSpace(c.ChatID) == "" {
		return fmt.Errorf("Telegram 渠道缺少 chat_id")
	}
	return nil
}

// Send 把统一消息转成 Telegram HTML 推送到 Bot 会话。
// APIBase 为空时使用官方服务地址。
func (telegramProvider) Send(ctx context.Context, cfg string, msg Message) error {
	c, err := decodeConfig[telegramConfig](cfg)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(c.BotToken)
	chatID := strings.TrimSpace(c.ChatID)
	if token == "" || chatID == "" {
		return fmt.Errorf("telegram bot_token / chat_id must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	apiBase := strings.TrimSuffix(strings.TrimSpace(c.APIBase), "/")
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	body, err := json.Marshal(telegramMessage{
		ChatID:                chatID,
		Text:                  mdToHTML(msg.Markdown),
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("marshal telegram message: %w", err)
	}
	return postJSON(ctx, apiBase+"/bot"+token+"/sendMessage", body, "telegram")
}

func init() { Register("telegram", telegramProvider{}) }
