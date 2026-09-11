package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Provider 渠道推送实现：把一条统一 Message 发到指定 kind 的真实端点。
// Config 是渠道配置 JSON 原文（由各 provider 自行解析并校验必要字段）。
type Provider interface {
	// Send 发送消息；配置缺失/非法、网络失败均返回错误（日志层负责记录）。
	Send(ctx context.Context, cfg string, msg Message) error
}

// validator 需要做保存前配置校验的 provider 额外实现该接口。
// Validate 在 api 层创建/更新渠道时调用，让用户保存前就拿到必填项反馈。
type validator interface {
	Validate(cfg string) error
}

// SendFunc 把普通函数适配成 Provider，避免每渠道都写一个空结构体。
type SendFunc func(ctx context.Context, cfg string, msg Message) error

// Send 实现 Provider 接口。
func (f SendFunc) Send(ctx context.Context, cfg string, msg Message) error { return f(ctx, cfg, msg) }

var providers = map[string]Provider{}

// Register 注册一种渠道 kind 与其推送实现；重复注册 panic（启动期编码错误）。
func Register(kind string, p Provider) {
	if _, dup := providers[kind]; dup {
		panic("notification: provider " + kind + " already registered")
	}
	providers[kind] = p
}

// decodeConfig 把渠道配置 JSON 解码为具体类型；空串/空对象均视为合法空配置，
// 必填项校验交给各 provider 的 Validate（发送前也会兜底校验）。
func decodeConfig[T any](cfg string) (*T, error) {
	cfg = strings.TrimSpace(cfg)
	if cfg == "" {
		cfg = "{}"
	}
	c := new(T)
	if err := json.Unmarshal([]byte(cfg), c); err != nil {
		return nil, fmt.Errorf("invalid channel config: %w", err)
	}
	return c, nil
}

// Send 向指定 kind 的渠道发送消息。未知 kind 返回错误（配置了未实现的渠道类型）。
func Send(ctx context.Context, kind, cfg string, msg Message) error {
	p, ok := providers[kind]
	if !ok {
		return fmt.Errorf("notification: unknown channel kind %q", kind)
	}
	return p.Send(ctx, cfg, msg)
}

// Validate 校验渠道配置必填项；未实现 validator 的 kind、或配置解析失败均报错。
func Validate(kind, cfg string) error {
	p, ok := providers[kind]
	if !ok {
		return fmt.Errorf("notification: unknown channel kind %q", kind)
	}
	if v, ok := p.(validator); ok {
		return v.Validate(cfg)
	}
	return nil
}

// Kinds 返回已注册的全部渠道类型（排序后返回，API 校验与前端引导用）。
func Kinds() []string {
	ks := make([]string, 0, len(providers))
	for k := range providers {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
