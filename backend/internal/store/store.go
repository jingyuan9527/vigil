package store

import (
	"database/sql"
	"strings"
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
		ignored       INTEGER NOT NULL DEFAULT 0,
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
		type        TEXT NOT NULL DEFAULT 'update',
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
	CREATE TABLE IF NOT EXISTS image_seen_tags (
		image_id INTEGER NOT NULL,
		tag      TEXT NOT NULL,
		seen_at  TEXT NOT NULL,
		PRIMARY KEY (image_id, tag)
	);
	CREATE INDEX IF NOT EXISTS idx_img_ref ON images(reference);
	CREATE INDEX IF NOT EXISTS idx_ver_img ON image_versions(image_id);
	CREATE INDEX IF NOT EXISTS idx_notif_read ON notifications(read);
	CREATE INDEX IF NOT EXISTS idx_seen_img ON image_seen_tags(image_id);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}
	// 轻量幂等迁移：为旧版本库补充新增列（SQLite 无 ADD COLUMN IF NOT EXISTS，
	// 通过忽略 duplicate column 错误实现幂等）。
	_ = addColumnIfMissing(db, "images", "ignored", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "images", "mode", "TEXT NOT NULL DEFAULT 'auto'")
	_ = addColumnIfMissing(db, "notifications", "type", "TEXT NOT NULL DEFAULT 'update'")
	return nil
}

// addColumnIfMissing 尝试为表添加列；若列已存在则忽略错误。
func addColumnIfMissing(db *sql.DB, table, column, ddl string) error {
	_, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + ddl)
	if err != nil && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
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
		`SELECT id,name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,ignored,mode,created_at
		 FROM images WHERE reference=?`, ref)
	return scanImage(row)
}

func (s *Store) GetImage(id int64) (*models.Image, error) {
	row := s.db.QueryRow(
		`SELECT id,name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,ignored,mode,created_at
		 FROM images WHERE id=?`, id)
	return scanImage(row)
}

func scanImage(row *sql.Row) (*models.Image, error) {
	var (
		id                     int64
		name, reference, registry, tag                 string
		source, localDigest, remoteDigest, status       string
		lastCheck, lastUpdate, errMsg, createdAt        sql.NullString
		ignored                                         int
		mode                                            sql.NullString
	)
	if err := row.Scan(&id, &name, &reference, &registry, &tag, &source, &localDigest,
		&remoteDigest, &status, &lastCheck, &lastUpdate, &errMsg, &ignored, &mode, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m := ns(mode)
	if m == "" {
		m = models.ModeAuto
	}
	return &models.Image{
		ID: id, Name: name, Reference: reference, Registry: registry, Tag: tag,
		Source: source, LocalDigest: localDigest, RemoteDigest: remoteDigest,
		Status:         models.ImageStatus(status),
		Ignored:        ignored == 1,
		Mode:           m,
		EffectiveMode:  models.ResolveMode(m, tag),
		LastCheck:      parseTime(ns(lastCheck)),
		LastUpdate:     parseTime(ns(lastUpdate)),
		Error:          ns(errMsg),
		CreatedAt:      parseTime(ns(createdAt)).UTC(),
	}, nil
}

// UpsertImage 按 reference 插入或更新镜像记录。
// 刻意不写 mode / ignored 列（防覆盖点）：
// mode 只能由 SetMode 修改；ignored 只能由 SetIgnored 修改；扫描每轮只更新采集与检测得到的字段。
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
	// 同步清理该镜像的已见标签清单与版本快照，避免遗留
	_, err := s.db.Exec("DELETE FROM images WHERE id=?", id)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec("DELETE FROM image_seen_tags WHERE image_id=?", id)
	_, _ = s.db.Exec("DELETE FROM image_versions WHERE image_id=?", id)
	return nil
}

// SetIgnored 设置镜像的忽略状态：忽略后跳过全部检测（不校验摘要、不巡检标签），
// 行数据保持冻结，也不会产生任何通知。
func (s *Store) SetIgnored(id int64, ignored bool) error {
	v := 0
	if ignored {
		v = 1
	}
	_, err := s.db.Exec("UPDATE images SET ignored=? WHERE id=?", v, id)
	return err
}

// SetMode 设置镜像的检测模式覆写（auto / digest-only / pin-watch），仅由此方法写入，
// 扫描的 UpsertImage 不会覆盖。生效模式由读取时 ResolveMode 计算。
func (s *Store) SetMode(id int64, mode string) error {
	_, err := s.db.Exec("UPDATE images SET mode=? WHERE id=?", mode, id)
	return err
}

// ---- Seen tags（pin-watch 模式巡检用）----

// GetSeenTags 返回某镜像已见过的仓库标签集合，用于新版本 tag 去重。
func (s *Store) GetSeenTags(imageID int64) (map[string]bool, error) {
	rows, err := s.db.Query("SELECT tag FROM image_seen_tags WHERE image_id=?", imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out[tag] = true
	}
	return out, rows.Err()
}

// AddSeenTags 记录标签为已见（幂等，INSERT OR IGNORE）。用于首次巡检建立基线与后续记录新 tag。
func (s *Store) AddSeenTags(imageID int64, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	now := nowStr()
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO image_seen_tags (image_id,tag,seen_at) VALUES (?,?,?)")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, t := range tags {
		if _, err := stmt.Exec(imageID, t, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ---- Notifications ----

// HasDigestNotification 报告某镜像是否已对指定远端摘要发过 type=update 的通知，
// 用于「同一个 digest 只通知一次」的去重（常规转移通知与强制扫描补发共用此闸门）。
func (s *Store) HasDigestNotification(imageID int64, digest string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM notifications WHERE image_id=? AND type='update' AND new_digest=?`,
		imageID, digest).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// MarkDockerImagesMissing 将「source=docker 且当前本机已不再出现」的镜像行标记为 stale（缺失），
// 用于清理本机已删除镜像在库内的残留。liveRefs 为本轮 Docker 仍存在的引用集合；
// 仅更新未忽略的项，避免打扰用户手动忽略、但镜像其实已从本机移除的记录。
// 返回受影响的行数。
func (s *Store) MarkDockerImagesMissing(liveRefs map[string]bool) (int64, error) {
	if len(liveRefs) == 0 {
		// 无存活引用说明 Docker 扫描不可用或守护进程异常，不贸然清理。
		return 0, nil
	}
	var marked int64
	rows, err := s.db.Query(`SELECT id, reference FROM images WHERE source='docker'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var stale []int64
	for rows.Next() {
		var id int64
		var ref string
		if err := rows.Scan(&id, &ref); err != nil {
			return 0, err
		}
		if !liveRefs[ref] {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range stale {
		if _, err := s.db.Exec(`UPDATE images SET status=? WHERE id=? AND ignored=0`,
			string(models.StatusStale), id); err != nil {
			return 0, err
		}
		marked++
	}
	return marked, nil
}

// ListImages 列出镜像，status 为空时返回全部。
func (s *Store) ListImages(status string) ([]models.Image, error) {
	q := `SELECT id,name,reference,registry,tag,source,local_digest,remote_digest,status,last_check,last_update,error,ignored,mode,created_at FROM images`
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
			id                                                     int64
			name, reference, registry, tag                          string
			source, localDigest, remoteDigest, st                  string
			lastCheck, lastUpdate, errMsg, createdAt               sql.NullString
			ignored                                                  int
			mode                                                    sql.NullString
		)
		if err := rows.Scan(&id, &name, &reference, &registry, &tag, &source, &localDigest,
			&remoteDigest, &st, &lastCheck, &lastUpdate, &errMsg, &ignored, &mode, &createdAt); err != nil {
			return nil, err
		}
		m := ns(mode)
		if m == "" {
			m = models.ModeAuto
		}
		out = append(out, models.Image{
			ID: id, Name: name, Reference: reference, Registry: registry, Tag: tag,
			Source: source, LocalDigest: localDigest, RemoteDigest: remoteDigest,
			Status:         models.ImageStatus(st),
			Ignored:        ignored == 1,
				Mode:           m,
			EffectiveMode:  models.ResolveMode(m, tag),
			LastCheck:      parseTime(ns(lastCheck)),
			LastUpdate:     parseTime(ns(lastUpdate)),
			Error:          ns(errMsg),
			CreatedAt:      parseTime(ns(createdAt)).UTC(),
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
	typ := n.Type
	if typ == "" {
		typ = models.NotifUpdate
	}
	_, err := s.db.Exec(
		`INSERT INTO notifications (image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,type,message,read,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,0,?)`,
		n.ImageID, n.ImageName, n.Reference, n.OldDigest, n.NewDigest,
		n.OldTag, n.NewTag, string(typ), n.Message, nowStr())
	return err
}

func (s *Store) ListNotifications(unreadOnly bool, cursorID int64) ([]models.Notification, error) {
	q := `SELECT id,image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,type,message,read,created_at
	      FROM notifications`
	where := ""
	if unreadOnly {
		where = "WHERE read=0"
	}
	if cursorID > 0 {
		if where != "" {
			where += " AND "
		} else {
			where = "WHERE "
		}
		where += "id < ?"
	}
	// 按 id 倒序分页：id 单调递增等价于时间倒序，且不存在 created_at 同秒
	// tie-breaking 导致的行丢失（游标 id < ? 与排序键一致，分页严格确定）。
	q += " " + where + " ORDER BY id DESC LIMIT 100"
	args := []interface{}{}
	if cursorID > 0 {
		args = append(args, cursorID)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Notification
	for rows.Next() {
		var (
			id                                                     int64
			imageID                                                int64
			imageName, reference                                   string
			oldDigest, newDigest, oldTag, newTag, typ, message     string
			read                                                   int
			createdAt                                              string
		)
		if err := rows.Scan(&id, &imageID, &imageName, &reference, &oldDigest, &newDigest,
			&oldTag, &newTag, &typ, &message, &read, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, models.Notification{
			ID: id, ImageID: imageID, ImageName: imageName, Reference: reference,
			OldDigest: oldDigest, NewDigest: newDigest, OldTag: oldTag, NewTag: newTag,
			Type:    models.NotificationKind(typ),
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
	// 单次 GROUP BY 替代 4 次独立 COUNT，减少 SQLite 扫描次数。
	// status IS NOT NULL：旧库可能残留 NULL 状态行，排除以免 Scan 报错打挂整个接口。
	rows, err := s.db.Query("SELECT status, COUNT(*) FROM images WHERE status IS NOT NULL GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		switch status {
		case "up-to-date":
			st.UpToDate = n
			st.Total += n
		case "update-available":
			st.UpdateAvailable = n
			st.Total += n
		case "unknown", "stale":
			st.Unknown += n
			st.Total += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
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
