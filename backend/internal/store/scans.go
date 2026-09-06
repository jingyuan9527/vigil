package store

import (
	"database/sql"
	"log"

	"vigil/internal/models"
)

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
			id                         int64
			startedAt                  string
			finishedAt, status, errMsg sql.NullString
			checked, updates           int
		)
		if err := rows.Scan(&id, &startedAt, &finishedAt, &checked, &updates, &status, &errMsg); err != nil {
			return nil, err
		}
		out = append(out, models.Scan{
			ID: id, StartedAt: parseTime(startedAt).UTC(), FinishedAt: parseTimePtr(ns(finishedAt)),
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
		id                         int64
		startedAt                  string
		finishedAt, status, errMsg sql.NullString
		checked, updates           int
	)
	if err := rows.Scan(&id, &startedAt, &finishedAt, &checked, &updates, &status, &errMsg); err != nil {
		return nil, err
	}
	return &models.Scan{
		ID: id, StartedAt: parseTime(startedAt).UTC(), FinishedAt: parseTimePtr(ns(finishedAt)),
		ImagesChecked: checked, UpdatesFound: updates, Status: ns(status), Error: ns(errMsg),
	}, nil
}

// ---- Stats ----

// Stats 汇总仪表盘统计。子项查询失败只降级对应字段（记日志），不整体失败：
// 统计是展示性数据，任一子项不可得都不应打挂仪表盘。
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
	if unread, err := s.UnreadCount(); err != nil {
		log.Printf("stats unread count: %v", err)
	} else {
		st.UnreadNotifs = unread
	}
	last, err := s.LastScan()
	if err != nil {
		log.Printf("stats last scan: %v", err)
	} else if last != nil {
		t := last.StartedAt
		st.LastScanAt = &t
		st.LastScanStatus = last.Status
	}
	return st, nil
}
