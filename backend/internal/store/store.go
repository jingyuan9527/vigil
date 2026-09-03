package store

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"

	"dockmon/internal/models"
)

// Store 封装 SQLite 存储与所有 CRUD。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并做迁移。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 单写者
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		// 非致命，忽略
		_ = err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS images (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		name          TEXT NOT NULL,
		reference     TEXT NOT NULL UNIQUE,
		registry      TEXT,
		tag           TEXT,
		source        TEXT,
		local_digest  TEXT,
		remote_digest TEXT,
		status        TEXT,
		last_check    TEXT,
		last_update   TEXT,
		error         TEXT,
		created_at    TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS image_versions (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		image_id   INTEGER NOT NULL,
		digest     TEXT,
		tag        TEXT,
		scanned_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS notifications (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		image_id    INTEGER NOT NULL,
		image_name  TEXT,
		reference   TEXT,
		old_digest  TEXT,
		new_digest  TEXT,
		old_tag     TEXT,
		new_tag     TEXT,
		message     TEXT,
		read        INTEGER NOT NULL DEFAULT 0,
		created_at  TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS scans (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at     TEXT NOT NULL,
		finished_at    TEXT,
		images_checked INTEGER NOT NULL DEFAULT 0,
		updates_found  INTEGER NOT NULL DEFAULT 0,
		status         TEXT,
		error          TEXT
	);
	CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT
	);
	CREATE TABLE IF NOT EXISTS auth_users (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		created_at    TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_img_ref ON images(reference);
	CREATE INDEX IF NOT EXISTS idx_ver_img ON image_versions(image_id);
	CREATE INDEX IF NOT EXISTS idx_notif_read ON notifications(read);
	`
	_, err := db.Exec(schema)
	return err
}

func nowStr() string { return time.Now().UTC().Format(time.RFC3339) }

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// ns 将可能为 NULL 的文本列安全地转为 Go string（NULL -> ""）。
func ns(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

// ---- Images ----

// GetImageByRef 按引用查找镜像，未找到返回 (nil, nil)。
func (s *Store) GetImageByRef(ref string) (*models.Image, error) {
	row := s.db.QueryRow(
		`SELECT id,name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,created_at
		 FROM images WHERE reference=?`, ref)
	return scanImage(row)
}

func (s *Store) GetImage(id int64) (*models.Image, error) {
	row := s.db.QueryRow(
		`SELECT id,name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,created_at
		 FROM images WHERE id=?`, id)
	return scanImage(row)
}

func scanImage(row *sql.Row) (*models.Image, error) {
	var (
		id                               int64
		name, reference, registry, tag  string
		source, localDigest, remoteDigest, status string
		lastCheck, lastUpdate, errMsg, createdAt sql.NullString
	)
	if err := row.Scan(&id, &name, &reference, &registry, &tag, &source, &localDigest,
		&remoteDigest, &status, &lastCheck, &lastUpdate, &errMsg, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &models.Image{
		ID: id, Name: name, Reference: reference, Registry: registry, Tag: tag,
		Source: source, LocalDigest: localDigest, RemoteDigest: remoteDigest,
		Status:     models.ImageStatus(status),
		LastCheck:  parseTime(ns(lastCheck)),
		LastUpdate: parseTime(ns(lastUpdate)),
		Error:      ns(errMsg),
		CreatedAt:  parseTime(ns(createdAt)).UTC(),
	}, nil
}

// UpsertImage 按 reference 插入或更新镜像记录。
func (s *Store) UpsertImage(img *models.Image) error {
	created := nowStr()
	if img.CreatedAt.IsZero() {
		img.CreatedAt = time.Now().UTC()
		created = img.CreatedAt.Format(time.RFC3339)
	}
	_, err := s.db.Exec(
		`INSERT INTO images (name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(reference) DO UPDATE SET
		   name=excluded.name, registry=excluded.registry, tag=excluded.tag,
		   source=excluded.source, local_digest=excluded.local_digest,
		   remote_digest=excluded.remote_digest, status=excluded.status,
		   last_check=excluded.last_check, last_update=excluded.last_update, error=excluded.error`,
		img.Name, img.Reference, img.Registry, img.Tag, img.Source,
		img.LocalDigest, img.RemoteDigest, string(img.Status),
		timeOrNil(img.LastCheck), timeOrNil(img.LastUpdate), img.Error, created)
	return err
}

func (s *Store) DeleteImage(id int64) error {
	_, err := s.db.Exec("DELETE FROM images WHERE id=?", id)
	return err
}

// ListImages 列出镜像，status 为空时返回全部。
func (s *Store) ListImages(status string) ([]models.Image, error) {
	q := `SELECT id,name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,created_at FROM images`
	var rows *sql.Rows
	var err error
	if status != "" {
		q += " WHERE status=?"
		rows, err = s.db.Query(q, status)
	} else {
		rows, err = s.db.Query(q)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Image
	for rows.Next() {
		var (
			id                                                       int64
			name, reference, registry, tag                          string
			source, localDigest, remoteDigest, st                  string
			lastCheck, lastUpdate, errMsg, createdAt               sql.NullString
		)
		if err := rows.Scan(&id, &name, &reference, &registry, &tag, &source, &localDigest,
			&remoteDigest, &st, &lastCheck, &lastUpdate, &errMsg, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, models.Image{
			ID: id, Name: name, Reference: reference, Registry: registry, Tag: tag,
			Source: source, LocalDigest: localDigest, RemoteDigest: remoteDigest,
			Status:       models.ImageStatus(st),
			LastCheck:    parseTime(ns(lastCheck)),
			LastUpdate:   parseTime(ns(lastUpdate)),
			Error:        ns(errMsg),
			CreatedAt:    parseTime(ns(createdAt)).UTC(),
		})
	}
	return out, rows.Err()
}

// ---- Image versions ----

func (s *Store) AddVersion(imageID int64, digest, tag string) error {
	_, err := s.db.Exec(
		`INSERT INTO image_versions (image_id,digest,tag,scanned_at) VALUES (?,?,?,?)`,
		imageID, digest, tag, nowStr())
	return err
}

func (s *Store) ListVersions(imageID int64) ([]models.ImageVersion, error) {
	rows, err := s.db.Query(
		`SELECT id,image_id,digest,tag,scanned_at FROM image_versions WHERE image_id=? ORDER BY scanned_at DESC LIMIT 50`,
		imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ImageVersion
	for rows.Next() {
		var (
			id        int64
			imageID   int64
			digest    string
			tag       string
			scannedAt string
		)
		if err := rows.Scan(&id, &imageID, &digest, &tag, &scannedAt); err != nil {
			return nil, err
		}
		out = append(out, models.ImageVersion{
			ID: id, ImageID: imageID, Digest: digest, Tag: tag,
			ScannedAt: parseTime(scannedAt).UTC(),
		})
	}
	return out, rows.Err()
}

// ---- Notifications ----

func (s *Store) CreateNotification(n *models.Notification) error {
	_, err := s.db.Exec(
		`INSERT INTO notifications (image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,message,read,created_at)
		 VALUES (?,?,?,?,?,?,?,?,0,?)`,
		n.ImageID, n.ImageName, n.Reference, n.OldDigest, n.NewDigest,
		n.OldTag, n.NewTag, n.Message, nowStr())
	return err
}

func (s *Store) ListNotifications(unreadOnly bool) ([]models.Notification, error) {
	q := `SELECT id,image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,message,read,created_at
	      FROM notifications`
	if unreadOnly {
		q += " WHERE read=0"
	}
	q += " ORDER BY created_at DESC LIMIT 200"
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Notification
	for rows.Next() {
		var (
			id                                   int64
			imageID                              int64
			imageName, reference                 string
			oldDigest, newDigest, oldTag, newTag, message string
			read                                 int
			createdAt                            string
		)
		if err := rows.Scan(&id, &imageID, &imageName, &reference, &oldDigest, &newDigest,
			&oldTag, &newTag, &message, &read, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, models.Notification{
			ID: id, ImageID: imageID, ImageName: imageName, Reference: reference,
			OldDigest: oldDigest, NewDigest: newDigest, OldTag: oldTag, NewTag: newTag,
			Message: message, Read: read == 1, CreatedAt: parseTime(createdAt).UTC(),
		})
	}
	return out, rows.Err()
}

func (s *Store) MarkRead(id int64) error {
	_, err := s.db.Exec("UPDATE notifications SET read=1 WHERE id=?", id)
	return err
}

func (s *Store) MarkAllRead() error {
	_, err := s.db.Exec("UPDATE notifications SET read=1 WHERE read=0")
	return err
}

func (s *Store) UnreadCount() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM notifications WHERE read=0").Scan(&n)
	return n, err
}

// ---- Scans ----

func (s *Store) CreateScan() (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO scans (started_at,images_checked,updates_found,status) VALUES (?,0,0,'running')`,
		nowStr())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishScan(id int64, checked, updates int, status, errMsg string) error {
	finished := nowStr()
	_, err := s.db.Exec(
		`UPDATE scans SET finished_at=?,images_checked=?,updates_found=?,status=?,error=? WHERE id=?`,
		finished, checked, updates, status, errMsg, id)
	return err
}

func (s *Store) ListScans(limit int) ([]models.Scan, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id,started_at,finished_at,images_checked,updates_found,status,error
		 FROM scans ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Scan
	for rows.Next() {
		var (
			id                                int64
			startedAt                        string
			finishedAt, status, errMsg       sql.NullString
			checked, updates                 int
		)
		if err := rows.Scan(&id, &startedAt, &finishedAt, &checked, &updates, &status, &errMsg); err != nil {
			return nil, err
		}
		out = append(out, models.Scan{
			ID: id, StartedAt: parseTime(startedAt).UTC(), FinishedAt: parseTime(ns(finishedAt)),
			ImagesChecked: checked, UpdatesFound: updates, Status: ns(status), Error: ns(errMsg),
		})
	}
	return out, rows.Err()
}

func (s *Store) LastScan() (*models.Scan, error) {
	rows, err := s.db.Query(
		`SELECT id,started_at,finished_at,images_checked,updates_found,status,error
		 FROM scans ORDER BY started_at DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var (
		id                                int64
		startedAt                        string
		finishedAt, status, errMsg       sql.NullString
		checked, updates                 int
	)
	if err := rows.Scan(&id, &startedAt, &finishedAt, &checked, &updates, &status, &errMsg); err != nil {
		return nil, err
	}
	return &models.Scan{
		ID: id, StartedAt: parseTime(startedAt).UTC(), FinishedAt: parseTime(ns(finishedAt)),
		ImagesChecked: checked, UpdatesFound: updates, Status: ns(status), Error: ns(errMsg),
	}, nil
}

// ---- Stats ----

func (s *Store) Stats() (*models.Stats, error) {
	st := &models.Stats{}
	count := func(status string) int {
		var n int
		s.db.QueryRow("SELECT COUNT(*) FROM images WHERE status=?", status).Scan(&n)
		return n
	}
	st.Total = count("up-to-date") + count("update-available") + count("unknown") + count("stale")
	st.UpToDate = count("up-to-date")
	st.UpdateAvailable = count("update-available")
	st.Unknown = count("unknown") + count("stale")
	unread, err := s.UnreadCount()
	if err == nil {
		st.UnreadNotifs = unread
	}
	last, err := s.LastScan()
	if err == nil && last != nil {
		t := last.StartedAt
		st.LastScanAt = &t
		st.LastScanStatus = last.Status
	}
	return st, nil
}

func timeOrNil(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// ---- Auth ----

// HasAdmin 检查是否已设置管理员账号。
func (s *Store) HasAdmin() bool {
	var n int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM auth_users").Scan(&n)
	return n > 0
}

// GetAdmin 获取管理员用户名与密码哈希；未设置时返回空字符串。
func (s *Store) GetAdmin() (username, hash string) {
	_ = s.db.QueryRow("SELECT username, password_hash FROM auth_users LIMIT 1").Scan(&username, &hash)
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
