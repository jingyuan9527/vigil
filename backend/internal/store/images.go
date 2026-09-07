package store

import (
	"database/sql"
	"strings"
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
	return s.deleteImageRow(id)
}

// deleteImageRow 删除镜像行及其派生数据（已见标签、版本快照、去重基线）。
// 通知行保留（见 DeleteImage 语义说明）。
func (s *Store) deleteImageRow(id int64) error {
	if _, err := s.db.Exec("DELETE FROM images WHERE id=?", id); err != nil {
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

// DeleteDefaultWatchImages 删除「内置演示监控列表」产生的镜像行及其派生数据。
// 命中条件（同一行需满足）：
//   - reference 属于演示清单 refs（由调用方传入当前 DefaultWatch）
//   - source 为 "default"（演示列表新产生，见 scanner.collectJobs），
//     或旧版本遗留的 manual 纯远端 watch 形态（source=manual 且无本地摘要、
//     无 registry 前缀）——升级前演示行正是这种外形；
//
// 本地 Docker 镜像行（source=docker）、带本地摘要或私有 registry 前缀的
// 手动行不受影响。通知历史保留（与 DeleteImage 语义一致）。
// 返回删除的行数。
func (s *Store) DeleteDefaultWatchImages(refs []string) (int64, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	ph := strings.Repeat("?,", len(refs))
	ph = ph[:len(ph)-1]
	args := make([]interface{}, 0, len(refs)+1)
	for _, r := range refs {
		args = append(args, r)
	}
	args = append(args, "default")
	rows, err := s.db.Query(
		`SELECT id FROM images
		 WHERE reference IN (`+ph+`)
		   AND (source = ? OR (source = 'manual' AND (local_digest IS NULL OR local_digest = '') AND (registry IS NULL OR registry = '')))`,
		args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var n int64
	for _, id := range ids {
		if err := s.deleteImageRow(id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DeleteRemovedDockerImages 删除「source=docker 且当前本机已不再存在」的镜像行。
// 镜像列表与版本对比以当前 Docker 守护进程的本地镜像为源：本机删除的镜像
// 随下次扫描移除（含派生数据，见 deleteImageRow），而不是残留为 stale（缺失）
// 标记——避免「docker rmi 之后镜像仍出现在列表中并显示缺失」。
// 保护规则沿用旧标记实现：liveRefs 为空（守护进程异常/扫描不可用）不贸然清理；
// manual / default / watch 来源与 ignored 项不受影响；通知行保留可查。
// 返回删除的行数。
func (s *Store) DeleteRemovedDockerImages(liveRefs map[string]bool) (int64, error) {
	if len(liveRefs) == 0 {
		// 无存活引用说明 Docker 扫描不可用或守护进程异常，不贸然清理。
		return 0, nil
	}
	var removed int64
	rows, err := s.db.Query(`SELECT id, reference FROM images WHERE source='docker' AND ignored=0`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var gone []int64
	for rows.Next() {
		var id int64
		var ref string
		if err := rows.Scan(&id, &ref); err != nil {
			return 0, err
		}
		if !liveRefs[ref] {
			gone = append(gone, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range gone {
		if err := s.deleteImageRow(id); err != nil {
			return 0, err
		}
		removed++
	}
	return removed, nil
}
