package store

import (
	"database/sql"

	"vigil/internal/models"
	"vigil/internal/version"
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
		`INSERT INTO notifications (image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,latest_tag,type,message,read,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,0,?)`,
		n.ImageID, n.ImageName, n.Reference, n.OldDigest, n.NewDigest,
		n.OldTag, n.NewTag, n.LatestTag, string(typ), n.Message, nowStr()); err != nil {
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

// HasNotificationMissingLatestTag 报告该镜像是否存在 latest_tag 为空的 update 通知。
// 扫描时据此按需触发回填，避免对正常镜像每次扫描都额外请求仓库 tags/list。
func (s *Store) HasNotificationMissingLatestTag(imageID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM notifications
		 WHERE image_id=? AND type='update' AND (latest_tag IS NULL OR latest_tag='')`,
		imageID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// BackfillNotificationLatestTag 为该镜像下缺版本号的 update 通知补写最新版本号。
// latest_tag 是后加列，历史行没有该值，页面只能展示无信息量的回退文案；
// 扫描时补上当前最新版本号即可恢复「最新版本 vX.Y.Z」的展示。返回受影响行数。
func (s *Store) BackfillNotificationLatestTag(imageID int64, latestTag string) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE notifications SET latest_tag=?
		 WHERE image_id=? AND type='update' AND (latest_tag IS NULL OR latest_tag='')`,
		latestTag, imageID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
//   - new-tag：本地已存在推荐版本的镜像——tag 完全同名、或推荐版本的摘要已落到
//     本地任一 tag（见下方 digest 规则）、或本地版本号已不低于推荐版本
//     （见 autoReadSupersededNewTagNotifs，覆盖别名 tag 与跨版本升级）；
//   - update：该引用的本地摘要已等于通知记录的新摘要（内容已同步到位），或本地
//     已与当前远端一致、或本地持有版本比该条通知更新（同一引用下更早摘要的提醒
//     已被这次升级跨过）。
//
// 后两条不可省：浮动 tag（如 searxng:latest）会被上游反复推送，同一引用会积累
// 多条不同摘要的历史提醒；只按「本地摘要 == 通知新摘要」消化的话，用户升级到
// 最新后旧版本提醒会永久占着未读角标，与「目标已达成」的事实不符。
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
	// done 子查询按 id 判断「已跨过」：id 单调递增即时间序，本地持有版本的摘要
	// 命中某条通知时，id 不大于它的历史摘要提醒都属于升级前的旧版本。
	r2, err := s.db.Exec(`
		UPDATE notifications SET read=1
		WHERE read=0 AND type='update' AND new_digest <> '' AND EXISTS (
			SELECT 1 FROM images i
			WHERE i.reference = notifications.reference
			  AND i.local_digest <> ''
			  AND (
			    i.local_digest = notifications.new_digest
			    OR i.local_digest = i.remote_digest
			    OR EXISTS (
			      SELECT 1 FROM notifications done
			      WHERE done.reference = notifications.reference
			        AND done.type = 'update'
			        AND done.new_digest = i.local_digest
			        AND done.id >= notifications.id
			    )
			  )
		)`)
	if err != nil {
		return 0, err
	}
	// new-tag 的摘要规则：通知落库时记录了推荐版本的 manifest 摘要（new_digest），
	// 本地任一 tag 的摘要与之相同即说明用户已用上该版本——用户可能拉的是 latest
	// 或别名 tag，tag 字符串对不上但内容一致。
	r3, err := s.db.Exec(`
		UPDATE notifications SET read=1
		WHERE read=0 AND type='new-tag' AND new_digest <> '' AND EXISTS (
			SELECT 1 FROM images i
			WHERE i.name = notifications.image_name
			  AND i.local_digest <> ''
			  AND i.local_digest = notifications.new_digest
			  AND i.status <> 'stale'
		)`)
	if err != nil {
		return 0, err
	}
	n1, _ := r1.RowsAffected()
	n2, _ := r2.RowsAffected()
	n3, _ := r3.RowsAffected()
	n4, err := s.autoReadSupersededNewTagNotifs()
	if err != nil {
		return n1 + n2 + n3, err
	}
	return n1 + n2 + n3 + n4, nil
}

// autoReadSupersededNewTagNotifs 是 new-tag 的版本规则：同一仓库下本地已存在
// 版本号不低于推荐版本的 tag，说明提醒推荐的版本已被跨过，未读转已读。
//
// 必要性：上游同一版本常打多个别名 tag（26.10 与 26.10.0、带 v 前缀），用户也可能
// 直接升到更高版本；只按 tag 字符串相等判定会把这些提醒永久留在未读角标。
// 比较必须在 Go 侧完成（SQLite 无法解析版本号）；未读 new-tag 通知量级很小，
// 且只 JOIN 同仓库的本地镜像行，不构成扫描热点。
func (s *Store) autoReadSupersededNewTagNotifs() (int64, error) {
	rows, err := s.db.Query(`
		SELECT n.id, n.new_tag, i.tag
		FROM notifications n
		JOIN images i ON i.name = n.image_name
		WHERE n.read=0 AND n.type='new-tag' AND n.new_tag <> ''
		  AND i.tag <> '' AND i.status <> 'stale'`)
	if err != nil {
		return 0, err
	}
	// SQLite 单连接：必须先读完并关闭结果集，才能在同一连接上执行 UPDATE，
	// 否则会等待自己持有的连接而死锁。
	var ids []int64
	seen := map[int64]bool{}
	for rows.Next() {
		var (
			id             int64
			want, localTag string
		)
		if err := rows.Scan(&id, &want, &localTag); err != nil {
			rows.Close()
			return 0, err
		}
		if seen[id] {
			continue
		}
		wantNums, okWant := version.ParseTag(want)
		localNums, okLocal := version.ParseTag(localTag)
		if okWant && okLocal && version.Compare(localNums, wantNums) >= 0 {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	var n int64
	for _, id := range ids {
		res, err := s.db.Exec("UPDATE notifications SET read=1 WHERE id=? AND read=0", id)
		if err != nil {
			return n, err
		}
		if affected, err := res.RowsAffected(); err == nil {
			n += affected
		}
	}
	return n, nil
}

// ListNotifications 按创建时间倒序列出通知（每页上限 100），支持未读筛选与游标分页。
func (s *Store) ListNotifications(unreadOnly bool, cursorID int64) ([]models.Notification, error) {
	q := `SELECT id,image_id,image_name,reference,old_digest,new_digest,old_tag,new_tag,latest_tag,type,message,read,created_at
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
		// 文本列均可空：latest_tag 是后加列，历史行值为 NULL；其余列虽由本包统一写入空串，
		// 也允许外部导入/手工改库产生 NULL。统一用 NullString 承接后经 ns() 降级为空串，
		// 避免 "converting NULL to string is unsupported" 打挂整个列表接口。
		var (
			id                                                                             int64
			imageID                                                                        int64
			typ                                                                            string
			read                                                                           int
			createdAt                                                                      string
			imageName, reference, oldDigest, newDigest, oldTag, newTag, latestTag, message sql.NullString
		)
		if err := rows.Scan(&id, &imageID, &imageName, &reference, &oldDigest, &newDigest,
			&oldTag, &newTag, &latestTag, &typ, &message, &read, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, models.Notification{
			ID: id, ImageID: imageID, ImageName: ns(imageName), Reference: ns(reference),
			OldDigest: ns(oldDigest), NewDigest: ns(newDigest),
			OldTag: ns(oldTag), NewTag: ns(newTag), LatestTag: ns(latestTag),
			Type:    models.NotificationKind(typ),
			Message: ns(message), Read: read == 1, CreatedAt: parseTime(createdAt).UTC(),
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
