package store

import (
	"database/sql"
)

// ---- Auth ----

// HasAdmin 检查是否已设置管理员账号。
// 查询失败必须暴露：吞错会让 /api/auth/setup 在 DB 故障窗口重新开放，
// 攻击者可借机接管管理员。
func (s *Store) HasAdmin() (bool, error) {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM auth_users").Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetAdmin 获取管理员用户名与密码哈希；未设置时返回空串（非错误）。
func (s *Store) GetAdmin() (username, hash string, err error) {
	err = s.db.QueryRow("SELECT username, password_hash FROM auth_users LIMIT 1").Scan(&username, &hash)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return
}

// SetAdmin 创建或更新管理员账号。
func (s *Store) SetAdmin(username, hash string) error {
	_, err := s.db.Exec(
		`INSERT INTO auth_users (username, password_hash, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash`,
		username, hash, nowStr())
	return err
}
