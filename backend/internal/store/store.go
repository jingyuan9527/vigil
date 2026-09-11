package store

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store 封装 SQLite 存储与所有 CRUD。按域拆分在 images/versions/notifications/
// scans/auth/settings 各文件，本文件只保留连接、迁移与通用工具函数。
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
	CREATE TABLE IF NOT EXISTS image_digest_notified (
		image_id    INTEGER NOT NULL,
		digest      TEXT NOT NULL,
		notified_at TEXT NOT NULL,
		PRIMARY KEY (image_id, digest)
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
	CREATE TABLE IF NOT EXISTS notify_channels (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		kind        TEXT NOT NULL,
		name        TEXT,
		enabled     INTEGER NOT NULL DEFAULT 1,
		config      TEXT NOT NULL DEFAULT '{}',
		created_at  TEXT NOT NULL
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
	// 去重基线回填：存量 update 通知导入独立去重表（(image_id,digest) 主键 + OR IGNORE 幂等），
	// 保证清理历史后常规扫描不会对已通知过的 digest 重复告警。
	_, _ = db.Exec(
		`INSERT OR IGNORE INTO image_digest_notified (image_id, digest, notified_at)
		 SELECT image_id, new_digest, created_at FROM notifications
		 WHERE type='update' AND new_digest != ''`)
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

// parseTime 解析 RFC3339 时间串，非法/空串返回零值。
// 必须返回值类型：历史实现返回 *time.Time，调用方直接 .UTC() 解引用，
// 任何脏时间数据（外部导入、手工改库）都会 nil panic 打挂整个接口。
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseTimePtr 供 *time.Time 字段使用：零值映射为 nil，JSON 输出保持 null 语义。
func parseTimePtr(s string) *time.Time {
	t := parseTime(s)
	if t.IsZero() {
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

func timeOrNil(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
