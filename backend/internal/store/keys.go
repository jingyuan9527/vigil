package store

import (
	"database/sql"
	"errors"
	"time"

	"vigil/internal/models"
)

// ---- MCP API Keys ----

// ErrMCPKeyNotFound 删除/查询未命中 Key 行时返回，供 API 层转 404。
var ErrMCPKeyNotFound = errors.New("mcp key not found")

// useTouchInterval 内成功鉴权后 last_used_at 的最小更新间隔：
// MCP 调用频次不可控，避免每次工具调用都产生一次 SQLite 写。
const useTouchInterval = time.Minute

func scanMCPKey(catcher func(dest ...interface{}) error) (*models.MCPKey, error) {
	var k models.MCPKey
	var createdAt string
	var lastUsed sql.NullString
	if err := catcher(&k.ID, &k.Name, &k.KeyPrefix, &createdAt, &lastUsed); err != nil {
		return nil, err
	}
	k.CreatedAt = parseTime(createdAt)
	k.LastUsedAt = parseTimePtr(ns(lastUsed))
	return &k, nil
}

const mcpKeyCols = "id, name, key_prefix, created_at, last_used_at"

// CreateMCPKey 写入一条 Key 元数据（哈希与前缀由调用方生成）。
func (s *Store) CreateMCPKey(name, keyPrefix, keyHash string) (*models.MCPKey, error) {
	res, err := s.db.Exec(
		`INSERT INTO mcp_api_keys (name, key_prefix, key_hash, created_at) VALUES (?,?,?,?)`,
		name, keyPrefix, keyHash, nowStr())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetMCPKey(id)
}

// GetMCPKey 按 ID 读取 Key 元数据；不存在返回 (nil, nil)。
func (s *Store) GetMCPKey(id int64) (*models.MCPKey, error) {
	row := s.db.QueryRow(`SELECT `+mcpKeyCols+` FROM mcp_api_keys WHERE id=?`, id)
	k, err := scanMCPKey(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// ListMCPKeys 返回全部 Key 元数据（按创建顺序）。
func (s *Store) ListMCPKeys() ([]models.MCPKey, error) {
	rows, err := s.db.Query(`SELECT ` + mcpKeyCols + ` FROM mcp_api_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MCPKey
	for rows.Next() {
		k, err := scanMCPKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// DeleteMCPKey 删除（吊销）一条 Key。
func (s *Store) DeleteMCPKey(id int64) error {
	res, err := s.db.Exec("DELETE FROM mcp_api_keys WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMCPKeyNotFound
	}
	return nil
}

// CountMCPKeys 返回 Key 总数（页面提示用，可低成本判断是否已有接入）。
func (s *Store) CountMCPKeys() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM mcp_api_keys").Scan(&n)
	return n, err
}

// UseMCPKey 以 SHA-256 哈希校验 Key：命中返回 Key ID 并节流式刷新 last_used_at
// （距上次使用不足 useTouchInterval 时不写库，避免高频 MCP 调用放大写入）。
// 未命中返回 ErrMCPKeyNotFound。
func (s *Store) UseMCPKey(keyHash string) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM mcp_api_keys WHERE key_hash=?`, keyHash).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrMCPKeyNotFound
	}
	if err != nil {
		return 0, err
	}
	threshold := time.Now().UTC().Add(-useTouchInterval).Format(time.RFC3339)
	_, _ = s.db.Exec(
		`UPDATE mcp_api_keys SET last_used_at=?
		 WHERE id=? AND (last_used_at IS NULL OR last_used_at < ?)`,
		nowStr(), id, threshold)
	return id, nil
}
