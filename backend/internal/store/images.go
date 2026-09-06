package store

import (
	"database/sql"
	"time"

	"vigil/internal/models"
)

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

// imageScanner 同时满足 *sql.Row 与 *sql.Rows，使单行/多行查询共用一份列映射。
type imageScanner interface {
	Scan(dest ...any) error
}

// scanImage 从单行结果构造 Image，未找到（ErrNoRows）返回 (nil, nil)。
func scanImage(row *sql.Row) (*models.Image, error) {
	if err := row.Err(); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return imageFromScan(row)
}

// imageFromScan 扫描 images 表的标准 15 列并构造 Image；
// 列清单必须与 SELECT 列序一致（见 GetImageByRef / GetImage / ListImages）。
func imageFromScan(sc imageScanner) (*models.Image, error) {
	var (
		id                                        int64
		name, reference, registry, tag            string
		source, localDigest, remoteDigest, status string
		lastCheck, lastUpdate, errMsg, createdAt  sql.NullString
		ignored                                   int
		mode                                      sql.NullString
	)
	if err := sc.Scan(&id, &name, &reference, &registry, &tag, &source, &localDigest,
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
		Status:        models.ImageStatus(status),
		Ignored:       ignored == 1,
		Mode:          m,
		EffectiveMode: models.ResolveMode(m, tag),
		LastCheck:     parseTimePtr(ns(lastCheck)),
		LastUpdate:    parseTimePtr(ns(lastUpdate)),
		Error:         ns(errMsg),
		CreatedAt:     parseTime(ns(createdAt)).UTC(),
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

// DeleteImage 删除镜像及其派生数据：已见标签清单、版本快照、去重基线。
// 通知行保留（image_name/reference 冗余存储可独立展示，作为事件历史留档），
// 但该镜像的去重基线一并清除：重新添加同名引用会从零开始建立基线，
// 首次扫描按新基线记录、不会误报「有更新」，也不会被旧基线吞掉新告警。
func (s *Store) DeleteImage(id int64) error {
	_, err := s.db.Exec("DELETE FROM images WHERE id=?", id)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec("DELETE FROM image_seen_tags WHERE image_id=?", id)
	_, _ = s.db.Exec("DELETE FROM image_versions WHERE image_id=?", id)
	_, _ = s.db.Exec("DELETE FROM image_digest_notified WHERE image_id=?", id)
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
		img, err := imageFromScan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *img)
	}
	return out, rows.Err()
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
