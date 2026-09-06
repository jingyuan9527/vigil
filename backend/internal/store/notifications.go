package store

import (
	"vigil/internal/models"
)

// ---- Notifications ----

// MaxRetainedReadNotifs 已读通知自动保留上限：超过此数的最老已读记录在扫描后被清理，
// 未读通知永不自动删除（避免丢失提醒信息）。
const MaxRetainedReadNotifs = 500

// HasDigestNotification 报告某镜像是否已对指定远端摘要发过 type=update 的通知。
// 去重基线独立存于 image_digest_notified 表（与历史通知记录解耦），
// 因此清理/清空已读通知后，常规扫描仍不会对同一 digest 重复告警。
func (s *Store) HasDigestNotification(imageID int64, digest string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM image_digest_notified WHERE image_id=? AND digest=?`,
		imageID, digest).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateNotification 写入通知；type=update 的通知在同一事务内登记去重基线
// （保持「创建 update 通知 == 已通知该 digest」的不变式）。
func (s *Store) CreateNotification(n *models.Notification) error {
	typ := n.Type
	if typ == "" {
		typ = models.NotifUpdate
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO notifications (image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,type,message,read,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,0,?)`,
		n.ImageID, n.ImageName, n.Reference, n.OldDigest, n.NewDigest,
		n.OldTag, n.NewTag, string(typ), n.Message, nowStr()); err != nil {
		return err
	}
	if typ == models.NotifUpdate && n.NewDigest != "" {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO image_digest_notified (image_id,digest,notified_at) VALUES (?,?,?)`,
			n.ImageID, n.NewDigest, nowStr()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TrimReadNotifications 自动保留策略：只保留最近 keep 条已读通知，更老的删除；
// 未读通知不受影响。返回删除条数。
func (s *Store) TrimReadNotifications(keep int) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}
	res, err := s.db.Exec(
		`DELETE FROM notifications
		 WHERE read=1 AND id < (
		   SELECT id FROM notifications WHERE read=1 ORDER BY id DESC LIMIT 1 OFFSET ?
		 )`, keep-1)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearReadNotifications 清空全部已读通知（手动归档），未读不受影响。返回删除条数。
// 去重基线在 image_digest_notified 表，不受影响。
func (s *Store) ClearReadNotifications() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM notifications WHERE read=1`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AutoReadAchievedNotifications 将「目标已达成」的未读通知自动转已读：
//   - new-tag：本地已存在同名且同 tag 的非缺失镜像（通知推荐的版本用户已用上）；
//   - update：该引用的本地摘要已等于通知记录的新摘要（内容已同步到位）。
//
// 通知行仅转已读、不删除，事件历史仍可在列表查看，未读角标不再被过期提醒占用。
// 纯远端监控（无本地摘要）与已删除（stale）的镜像不会命中，保持人工处理。
func (s *Store) AutoReadAchievedNotifications() (int64, error) {
	r1, err := s.db.Exec(`
		UPDATE notifications SET read=1
		WHERE read=0 AND type='new-tag' AND new_tag <> '' AND EXISTS (
			SELECT 1 FROM images i
			WHERE i.name = notifications.image_name
			  AND i.tag = notifications.new_tag
			  AND i.status <> 'stale'
		)`)
	if err != nil {
		return 0, err
	}
	r2, err := s.db.Exec(`
		UPDATE notifications SET read=1
		WHERE read=0 AND type='update' AND new_digest <> '' AND EXISTS (
			SELECT 1 FROM images i
			WHERE i.reference = notifications.reference
			  AND i.local_digest = notifications.new_digest
		)`)
	if err != nil {
		return 0, err
	}
	n1, _ := r1.RowsAffected()
	n2, _ := r2.RowsAffected()
	return n1 + n2, nil
}

// ListNotifications 按创建时间倒序列出通知（每页上限 100），支持未读筛选与游标分页。
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
			id                                                 int64
			imageID                                            int64
			imageName, reference                               string
			oldDigest, newDigest, oldTag, newTag, typ, message string
			read                                               int
			createdAt                                          string
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
