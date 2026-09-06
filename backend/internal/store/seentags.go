package store

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
