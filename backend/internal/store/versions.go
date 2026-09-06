package store

import (
	"database/sql"

	"vigil/internal/models"
)

// ---- Image versions ----

func (s *Store) AddVersion(imageID int64, digest, tag string) error {
	_, err := s.db.Exec(
		`INSERT INTO image_versions (image_id,digest,tag,scanned_at) VALUES (?,?,?,?)`,
		imageID, digest, tag, nowStr())
	return err
}

// FirstVersionDigest 返回版本时间线中最早记录的非空摘要（该镜像首次记录到的远端版本），
// 时间线为空时返回 ("", nil)。纯远端监控镜像的 remote_digest 随扫描滚动覆盖，
// 强制扫描取此基线重建「首次记录 → 当前」的版本转移；是否与当前摘要存在差异由调用方判定
// （取「最早记录」而非「最早与当前不同」，避免远端回到首次记录时误报转移）。
func (s *Store) FirstVersionDigest(imageID int64) (string, error) {
	var d sql.NullString
	err := s.db.QueryRow(
		`SELECT digest FROM image_versions
		 WHERE image_id=? AND digest IS NOT NULL AND digest != ''
		 ORDER BY scanned_at ASC, id ASC LIMIT 1`, imageID).Scan(&d)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ns(d), nil
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
