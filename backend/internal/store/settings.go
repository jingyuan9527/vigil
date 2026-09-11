package store

// ---- Settings ----

// LoadSettingsMap 读取全部设置键值对；表为空时返回空 map（非 nil error）。
func (s *Store) LoadSettingsMap() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// SaveSettingsMap 写入（覆盖）全部设置键值对。
func (s *Store) SaveSettingsMap(m map[string]string) error {
	for k, v := range m {
		if _, err := s.db.Exec(
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			k, v); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSettingsKeys 删除一组设置键（渠道迁移后清理旧 dingtalk 键等遗留）。
func (s *Store) DeleteSettingsKeys(keys ...string) error {
	for _, k := range keys {
		if _, err := s.db.Exec("DELETE FROM settings WHERE key=?", k); err != nil {
			return err
		}
	}
	return nil
}

// ---- JWT secret ----

// GetJWTSecret 从 settings 表读取 JWT 密钥；不存在时返回空。
func (s *Store) GetJWTSecret() string {
	var v string
	_ = s.db.QueryRow("SELECT value FROM settings WHERE key='jwt_secret'").Scan(&v)
	return v
}

// SaveJWTSecret 保存 JWT 密钥到 settings 表。
func (s *Store) SaveJWTSecret(secret string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES ('jwt_secret', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, secret)
	return err
}
