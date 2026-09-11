package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrChannelNotFound 更新/删除未命中渠道行时返回，供 API 层转 404。
var ErrChannelNotFound = errors.New("channel not found")

// Channel 通知渠道：一条渠道对应一种推送方式（钉钉/企微/飞书/Telegram/通用 Webhook 等）。
// Config 为渠道专属配置的 JSON 原文（如 {"webhook": "..."}），由 notification 包按 kind 解析。
type Channel struct {
	ID        int64
	Kind      string // 渠道类型（registry 中注册的 kind）
	Name      string // 展示名（自定义，如"运维群"）
	Enabled   bool
	Config    string // 配置 JSON 原文
	CreatedAt time.Time
}

// scanChannelRow 把一行渠道数据扫进 Channel（created_at 存 RFC3339 文本）。
func scanChannelRow(catcher func(dest ...interface{}) error) (*Channel, error) {
	var c Channel
	var enabled int
	var createdAt string
	if err := catcher(&c.ID, &c.Kind, &c.Name, &enabled, &c.Config, &createdAt); err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	c.CreatedAt = parseTime(createdAt)
	return &c, nil
}

// ListChannels 返回全部渠道；enabledOnly=true 时仅返回启用的渠道。
func (s *Store) ListChannels(enabledOnly bool) ([]Channel, error) {
	q := "SELECT id, kind, name, enabled, config, created_at FROM notify_channels"
	if enabledOnly {
		q += " WHERE enabled=1"
	}
	q += " ORDER BY id"
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannelRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetChannel 按 ID 读取单条渠道；不存在返回 (nil, nil)。
func (s *Store) GetChannel(id int64) (*Channel, error) {
	row := s.db.QueryRow(
		"SELECT id, kind, name, enabled, config, created_at FROM notify_channels WHERE id=?", id)
	c, err := scanChannelRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// CreateChannel 新建渠道，返回带真实自增 ID 的完整行。
func (s *Store) CreateChannel(kind, name, config string, enabled bool) (*Channel, error) {
	e := 0
	if enabled {
		e = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO notify_channels (kind, name, enabled, config, created_at) VALUES (?, ?, ?, ?, ?)`,
		kind, name, e, config, nowStr())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetChannel(id)
}

// UpdateChannel 更新渠道的展示名、配置与启用状态（kind 不可改：类型固定）。
func (s *Store) UpdateChannel(id int64, name, config string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	res, err := s.db.Exec(
		`UPDATE notify_channels SET name=?, enabled=?, config=? WHERE id=?`,
		name, e, config, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// DeleteChannel 删除渠道（危险操作，调用方须确认）。
func (s *Store) DeleteChannel(id int64) error {
	res, err := s.db.Exec("DELETE FROM notify_channels WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// HasChannelKind 判断是否存在指定 kind 的渠道（启用状态不限），迁移与去重校验用。
func (s *Store) HasChannelKind(kind string) (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM notify_channels WHERE kind=?", kind).Scan(&n)
	return n > 0, err
}

// MigrateDingtalkToChannel 把旧版本（settings 表 + 环境变量）的钉钉通知迁移成 dingtalk 渠道：
//   - 环境变量优先，其次 settings 表旧键 dingtalk_webhook/secret；
//   - 两者均无值时不做任何写入；
//   - 库内已存在 dingtalk 渠道时跳过（防止重复播种）；
//   - 无论是否播种，旧 settings 键一律清除，避免「渠道表之外还残存第二处配置」分叉。
//
// 返回是否实际创建了渠道。
func (s *Store) MigrateDingtalkToChannel(envWebhook, envSecret string) (bool, error) {
	settingsMap, err := s.LoadSettingsMap()
	if err != nil {
		return false, err
	}
	webhook := strings.TrimSpace(envWebhook)
	secret := strings.TrimSpace(envSecret)
	if webhook == "" {
		webhook = strings.TrimSpace(settingsMap["dingtalk_webhook"])
		secret = strings.TrimSpace(settingsMap["dingtalk_secret"])
	}

	// 旧 settings 键在任何情况下都不再是配置来源：有值迁移、无值也清除，
	// 让「渠道表是唯一来源」这一状态在新旧库上都成立。
	if _, ok := settingsMap["dingtalk_webhook"]; ok || webhook != "" {
		if err := s.DeleteSettingsKeys("dingtalk_webhook", "dingtalk_secret"); err != nil {
			return false, err
		}
	}

	if webhook == "" {
		return false, nil
	}
	has, err := s.HasChannelKind("dingtalk")
	if err != nil || has {
		return false, err
	}
	cfg, err := json.Marshal(map[string]string{"webhook": webhook, "secret": secret})
	if err != nil {
		return false, err
	}
	if _, err := s.CreateChannel("dingtalk", "钉钉", string(cfg), true); err != nil {
		return false, err
	}
	return true, nil
}
