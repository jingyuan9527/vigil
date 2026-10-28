package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"vigil/internal/models"
	"vigil/internal/version"
)

// ---- Image updates（按镜像聚合的更新记录）----

// MaxUpdateCandidates 是聚合记录保留的候选版本上限（按版本号降序取前 N 个）。
// 仓库常年累积上百个历史 tag，全量展示既无信息量，也会撑大通知与记录体积。
const MaxUpdateCandidates = 20

// UpdateInput 是一轮检测得到的镜像最新状态。由扫描器在每次 process 结束时提交，
// 存储层负责与上轮状态合并、判定「版本是否真的变化」并决定是否推送。
type UpdateInput struct {
	ImageID        int64
	ImageName      string
	Reference      string
	CurrentVersion string // 当前监控 tag
	CurrentDigest  string // 当前 tag 的远端摘要
	BaseDigest     string // 差异基线（本地持有摘要 / 上次远端）
	LatestVersion  string // 仓库最高版本 tag
	// VersionKnown 表示本轮成功拿到 tags/list：false 时沿用已存版本信息，
	// 避免仓库接口抖动把版本号清空而误判成「版本变化」。
	VersionKnown bool
	Candidates   []string // 高于当前版本的候选版本（页面「可升级」）
	// NewVersionTags 是本轮首次出现在仓库中的版本 tag（含低版本/其它分支补丁），
	// 由扫描器基于已见标签基线计算。非空即触发一次「可选新版本」提醒。
	NewVersionTags []string
	// DigestNotifiable 表示当前检测模式需要因摘要变化推送（仅 digest-only/auto 浮动 tag；
	// Pin-Watch 锁定 tag 被上游覆盖时不告警，与既有语义一致）。
	DigestNotifiable bool
	Message          string
}

// MergeImageUpdate 合并一轮检测结果并返回聚合记录与是否需要推送。
//
// 去重不变式：只有「相对上次已通知状态」出现新版本/新摘要时才返回 true；
// 同一状态重复提交（定时扫描、强制扫描）不会重复推送。首次见到某镜像只建立
// 基线（status=read，不推送），避免接入监控时把仓库既有版本一次性刷屏。
func (s *Store) MergeImageUpdate(in UpdateInput) (models.ImageUpdate, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return models.ImageUpdate{}, false, err
	}
	defer tx.Rollback()

	prev, err := getImageUpdateTx(tx, in.ImageID)
	if err != nil {
		return models.ImageUpdate{}, false, err
	}

	// 版本信息未知（tags/list 失败）时沿用上轮结果，避免瞬时故障制造假变更。
	candidates := normalizeCandidates(in.Candidates)
	latest := in.LatestVersion
	newTags := normalizeCandidates(in.NewVersionTags)
	if !in.VersionKnown && prev != nil {
		candidates = prev.Candidates
		latest = prev.LatestVersion
		newTags = nil
	}
	if prev != nil && len(newTags) > 0 {
		// 已进入聚合记录（基线候选或上次已提醒）的版本不再重复播报，
		// 兜住已见标签表缺失/滞后导致扫描器重复提交的情况。
		already := make([]string, 0, len(prev.Candidates)+len(prev.NewTags))
		already = append(already, prev.Candidates...)
		already = append(already, prev.NewTags...)
		newTags = excludeTags(newTags, already)
	}

	now := time.Now().UTC()
	rec := models.ImageUpdate{
		ImageID:        in.ImageID,
		ImageName:      in.ImageName,
		Reference:      in.Reference,
		CurrentVersion: in.CurrentVersion,
		CurrentDigest:  in.CurrentDigest,
		BaseDigest:     in.BaseDigest,
		LatestVersion:  latest,
		Candidates:     candidates,
		Status:         models.UpdateRead,
		FirstSeenAt:    now,
		LastSeenAt:     now,
	}

	notifiedDigest := in.CurrentDigest
	notifiedLatest := latest
	notifiedCandidates := candidates
	notifiedAt := ""
	shouldNotify := false
	// versionKnown 记录「是否曾成功拿到版本信息」：用于区分「仓库没有版本 tag」
	// 与「上轮 tags/list 失败」，后者首次拿到信息时只补建基线、不应刷屏。
	versionKnown := in.VersionKnown

	if prev != nil {
		rec.FirstSeenAt = prev.FirstSeenAt
		rec.Status = prev.Status
		rec.Message = prev.Message
		rec.NewTags = prev.NewTags
		rec.Reason = prev.Reason
		rec.IgnoredVersion = prev.IgnoredVersion
		rec.IgnoredDigest = prev.IgnoredDigest
		notifiedDigest = prev.notifiedDigest
		notifiedLatest = prev.notifiedLatest
		notifiedCandidates = prev.notifiedCandidates
		notifiedAt = prev.notifiedAtStr
		versionKnown = in.VersionKnown || prev.versionKnown

		// 摘要变化只有在「仍有待处理差异」时才算更新：本地已同步（base == current）
		// 时即使远端相对上轮变过，也不该再提醒用户拉取。
		digestChanged := in.DigestNotifiable && in.CurrentDigest != "" &&
			in.CurrentDigest != prev.notifiedDigest && in.BaseDigest != in.CurrentDigest
		// 上轮 tags/list 不可用、本轮首次拿到版本信息：只补建基线，
		// 否则接入监控/接口恢复时会把仓库既有版本一次性刷屏。
		versionBaseline := !prev.versionKnown && versionKnown
		// 新版本判定：相对「已通知状态」出现了未播报过的候选版本，或最高版本号抬升。
		// 只撤下不新增（版本被撤回）不算变化，不打扰用户。
		appeared := candidatesAppeared(prev.notifiedCandidates, candidates)
		latestIsNewer := latest != "" && in.CurrentVersion != "" &&
			version.CompareTag(latest, in.CurrentVersion) > 0
		// 仓库新发布的版本 tag（含低版本/其它分支补丁）同样视为版本变化：
		// 这是旧版「可选新版本」提醒的语义，不能只盯最高版本号。
		versionBumped := !versionBaseline && (len(newTags) > 0 || appeared ||
			(latestIsNewer && (prev.notifiedLatest == "" ||
				version.CompareTag(latest, prev.notifiedLatest) > 0)))

		switch {
		case digestChanged && versionBumped:
			rec.Reason = models.UpdateReasonBoth
			shouldNotify = true
		case versionBumped:
			rec.Reason = models.UpdateReasonVersion
			shouldNotify = true
		case digestChanged:
			rec.Reason = models.UpdateReasonDigest
			shouldNotify = true
		}

		// 忽略语义：忽略当前版本后同版本不再提醒；出现更高版本才恢复未读。
		ignoreSuppressed := false
		if shouldNotify && prev.Status == models.UpdateIgnored {
			switch {
			case prev.IgnoredVersion != "":
				if latest == "" || version.CompareTag(latest, prev.IgnoredVersion) <= 0 {
					ignoreSuppressed = true
				}
			case prev.IgnoredDigest != "":
				if in.CurrentDigest == prev.IgnoredDigest {
					ignoreSuppressed = true
				}
			default:
				if in.CurrentDigest == prev.notifiedDigest {
					ignoreSuppressed = true
				}
			}
		}
		if ignoreSuppressed {
			shouldNotify = false
			// 忽略期间的变化同样记为已见：取消忽略后不应追溯推送早已被忽略的版本。
			rec.Reason = prev.Reason
		}
		if !shouldNotify && (ignoreSuppressed || versionBaseline) {
			notifiedDigest = in.CurrentDigest
			notifiedLatest = latest
			notifiedCandidates = candidates
		}
	}

	if shouldNotify {
		rec.Status = models.UpdateUnread
		rec.Message = in.Message
		// 只保留本次提醒的新版本；摘要类提醒不携带版本列表。
		rec.NewTags = newTags
		rec.IgnoredVersion = ""
		rec.IgnoredDigest = ""
		notifiedDigest = in.CurrentDigest
		notifiedLatest = latest
		notifiedCandidates = candidates
		notifiedAt = now.Format(time.RFC3339)
	}
	rec.NotifiedAt = parseTimePtr(notifiedAt)
	rec.HasUpdate = computeHasUpdate(in.CurrentVersion, in.CurrentDigest, in.BaseDigest, latest, candidates)

	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return models.ImageUpdate{}, false, err
	}
	notifiedJSON, err := json.Marshal(notifiedCandidates)
	if err != nil {
		return models.ImageUpdate{}, false, err
	}
	storedNewTags := rec.NewTags
	if storedNewTags == nil {
		storedNewTags = []string{}
	}
	newTagsJSON, err := json.Marshal(storedNewTags)
	if err != nil {
		return models.ImageUpdate{}, false, err
	}
	if _, err := tx.Exec(
		`INSERT INTO image_updates (
		   image_id,image_name,reference,current_version,current_digest,base_digest,
		   latest_version,candidates,new_tags,has_update,version_known,
		   status,ignored_version,ignored_digest,
		   notified_digest,notified_latest,notified_candidates,message,reason,
		   first_seen_at,last_seen_at,notified_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(image_id) DO UPDATE SET
		   image_name=excluded.image_name, reference=excluded.reference,
		   current_version=excluded.current_version, current_digest=excluded.current_digest,
		   base_digest=excluded.base_digest, latest_version=excluded.latest_version,
		   candidates=excluded.candidates, new_tags=excluded.new_tags,
		   has_update=excluded.has_update, version_known=excluded.version_known,
		   status=excluded.status, ignored_version=excluded.ignored_version,
		   ignored_digest=excluded.ignored_digest, notified_digest=excluded.notified_digest,
		   notified_latest=excluded.notified_latest, notified_candidates=excluded.notified_candidates,
		   message=excluded.message, reason=excluded.reason,
		   first_seen_at=excluded.first_seen_at, last_seen_at=excluded.last_seen_at,
		   notified_at=excluded.notified_at`,
		rec.ImageID, rec.ImageName, rec.Reference, rec.CurrentVersion, rec.CurrentDigest,
		rec.BaseDigest, rec.LatestVersion, string(candidatesJSON), string(newTagsJSON),
		boolToInt(rec.HasUpdate), boolToInt(versionKnown),
		string(rec.Status), rec.IgnoredVersion, rec.IgnoredDigest,
		notifiedDigest, notifiedLatest, string(notifiedJSON), rec.Message, rec.Reason,
		rec.FirstSeenAt.Format(time.RFC3339), rec.LastSeenAt.Format(time.RFC3339),
		nullIfEmpty(notifiedAt)); err != nil {
		return models.ImageUpdate{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return models.ImageUpdate{}, false, err
	}
	return rec, shouldNotify, nil
}

// GetImageUpdate 返回单个镜像的聚合更新记录，未找到返回 (nil, nil)。
func (s *Store) GetImageUpdate(imageID int64) (*models.ImageUpdate, error) {
	rec, err := getImageUpdateTx(s.db, imageID)
	if rec == nil || err != nil {
		return nil, err
	}
	return &rec.ImageUpdate, nil
}

// ListImageUpdates 列出需要展示的聚合记录，按未读优先、最近检测时间倒序：
// 存在当前差异（has_update=1）或仍处于未读/已忽略状态。这样「低版本分支新 tag」
// 这类没有升级差异、但确实发过提醒的条目也不会从页面消失。
// status 为空表示全部；镜像级 ignored 的项目不展示。
func (s *Store) ListImageUpdates(status string) ([]models.ImageUpdate, error) {
	q := `SELECT u.image_id,u.image_name,u.reference,u.current_version,u.current_digest,u.base_digest,
	             u.latest_version,u.candidates,u.new_tags,u.has_update,u.version_known,u.status,
	             u.ignored_version,u.ignored_digest,
	             u.notified_digest,u.notified_latest,u.notified_candidates,u.message,u.reason,
	             u.first_seen_at,u.last_seen_at,u.notified_at
	      FROM image_updates u
	      WHERE (u.has_update=1 OR u.status IN ('unread','ignored'))
	        AND NOT EXISTS (SELECT 1 FROM images i WHERE i.id=u.image_id AND i.ignored=1)`
	args := []interface{}{}
	if status != "" {
		q += " AND u.status=?"
		args = append(args, status)
	}
	q += ` ORDER BY CASE u.status WHEN 'unread' THEN 0 WHEN 'read' THEN 1 ELSE 2 END,
	               u.last_seen_at DESC, u.image_id DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ImageUpdate
	for rows.Next() {
		rec, err := scanImageUpdateRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec.ImageUpdate)
	}
	return out, rows.Err()
}

// MarkImageUpdateRead 将未读记录转为已读（已忽略项不受影响）。
func (s *Store) MarkImageUpdateRead(imageID int64) error {
	_, err := s.db.Exec(
		`UPDATE image_updates SET status='read' WHERE image_id=? AND status='unread'`, imageID)
	return err
}

// MarkAllImageUpdatesRead 将全部未读记录转为已读。
func (s *Store) MarkAllImageUpdatesRead() error {
	_, err := s.db.Exec(`UPDATE image_updates SET status='read' WHERE status='unread'`)
	return err
}

// IgnoreImageUpdate 忽略当前版本：记录被忽略的版本号（无版本号时记录摘要），
// 同版本不再推送，出现更高版本时由 MergeImageUpdate 自动解除忽略。
func (s *Store) IgnoreImageUpdate(imageID int64) error {
	_, err := s.db.Exec(
		`UPDATE image_updates
		 SET status='ignored', ignored_version=latest_version, ignored_digest=current_digest
		 WHERE image_id=?`, imageID)
	return err
}

// UnignoreImageUpdate 取消忽略并转已读：状态变更由用户显式触发，
// 不立即重推同版本（后续出现更高版本仍会正常提醒）。
func (s *Store) UnignoreImageUpdate(imageID int64) error {
	_, err := s.db.Exec(
		`UPDATE image_updates
		 SET status='read', ignored_version='', ignored_digest=''
		 WHERE image_id=?`, imageID)
	return err
}

// UnreadUpdateCount 返回未读聚合记录数（页面未读角标与统计使用）。
func (s *Store) UnreadUpdateCount() (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM image_updates u
		 WHERE u.status='unread'
		   AND NOT EXISTS (SELECT 1 FROM images i WHERE i.id=u.image_id AND i.ignored=1)`).Scan(&n)
	return n, err
}

// AutoReadAchievedUpdates 将「目标已达成」的聚合记录自动转已读：
//   - 本地已存在 latest_version 对应 tag 的镜像（推荐版本已用上）；
//   - 最近一次推送原因是摘要变化且本地摘要已同步到当前远端。
//
// 仅转已读、不删除记录：差异消失后记录会在下一次扫描时因 has_update=0 不再展示。
func (s *Store) AutoReadAchievedUpdates() (int64, error) {
	r1, err := s.db.Exec(`
		UPDATE image_updates SET status='read'
		WHERE status='unread' AND latest_version <> ''
		  AND latest_version <> current_version
		  AND EXISTS (
			SELECT 1 FROM images i
			WHERE i.name = image_updates.image_name
			  AND i.tag = image_updates.latest_version
			  AND i.status <> 'stale'
		)`)
	if err != nil {
		return 0, err
	}
	r2, err := s.db.Exec(`
		UPDATE image_updates SET status='read'
		WHERE status='unread' AND reason IN ('digest','both')
		  AND current_digest <> '' AND EXISTS (
			SELECT 1 FROM images i
			WHERE i.reference = image_updates.reference
			  AND i.local_digest = image_updates.current_digest
		)`)
	if err != nil {
		return 0, err
	}
	n1, _ := r1.RowsAffected()
	n2, _ := r2.RowsAffected()
	return n1 + n2, nil
}

// ---- 内部工具 ----

// mergedUpdateRow 在对外模型之外额外保留「已通知状态」快照，
// 用于比较本轮是否出现真正的新版本/新摘要。
type mergedUpdateRow struct {
	models.ImageUpdate
	versionKnown       bool
	notifiedDigest     string
	notifiedLatest     string
	notifiedCandidates []string
	notifiedAtStr      string
}

type rowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func getImageUpdateTx(q rowQuerier, imageID int64) (*mergedUpdateRow, error) {
	row := q.QueryRow(
		`SELECT image_id,image_name,reference,current_version,current_digest,base_digest,
		        latest_version,candidates,new_tags,has_update,version_known,status,
		        ignored_version,ignored_digest,
		        notified_digest,notified_latest,notified_candidates,message,reason,
		        first_seen_at,last_seen_at,notified_at
		 FROM image_updates WHERE image_id=?`, imageID)
	return scanImageUpdateRow(row)
}

func scanImageUpdateRow(sc imageScanner) (*mergedUpdateRow, error) {
	var (
		imageID                                       int64
		imageName, reference, currentVersion          sql.NullString
		currentDigest, baseDigest, latestVersion      sql.NullString
		candidates, newTags, status, ignoredVersion   sql.NullString
		ignoredDigest, notifiedDigest, notifiedLatest sql.NullString
		notifiedCandidates, message, reason           sql.NullString
		firstSeenAt, lastSeenAt, notifiedAt           sql.NullString
		hasUpdate, versionKnown                       int
	)
	if err := sc.Scan(&imageID, &imageName, &reference, &currentVersion, &currentDigest,
		&baseDigest, &latestVersion, &candidates, &newTags, &hasUpdate, &versionKnown, &status,
		&ignoredVersion, &ignoredDigest, &notifiedDigest, &notifiedLatest,
		&notifiedCandidates, &message, &reason, &firstSeenAt, &lastSeenAt,
		&notifiedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &mergedUpdateRow{
		ImageUpdate: models.ImageUpdate{
			ImageID:        imageID,
			ImageName:      ns(imageName),
			Reference:      ns(reference),
			CurrentVersion: ns(currentVersion),
			CurrentDigest:  ns(currentDigest),
			BaseDigest:     ns(baseDigest),
			LatestVersion:  ns(latestVersion),
			Candidates:     decodeCandidates(ns(candidates)),
			NewTags:        decodeCandidates(ns(newTags)),
			HasUpdate:      hasUpdate == 1,
			Status:         models.ImageUpdateStatus(ns(status)),
			IgnoredVersion: ns(ignoredVersion),
			IgnoredDigest:  ns(ignoredDigest),
			Message:        ns(message),
			Reason:         ns(reason),
			FirstSeenAt:    parseTime(ns(firstSeenAt)).UTC(),
			LastSeenAt:     parseTime(ns(lastSeenAt)).UTC(),
			NotifiedAt:     parseTimePtr(ns(notifiedAt)),
		},
		versionKnown:       versionKnown == 1,
		notifiedDigest:     ns(notifiedDigest),
		notifiedLatest:     ns(notifiedLatest),
		notifiedCandidates: decodeCandidates(ns(notifiedCandidates)),
		notifiedAtStr:      ns(notifiedAt),
	}, nil
}

// normalizeCandidates 去重（同一版本的别名 tag 只留展示优先级更高者）、
// 版本号降序并截断到上限，保证存储与展示稳定有序。
func normalizeCandidates(tags []string) []string {
	out := version.DedupTags(tags)
	if len(out) > MaxUpdateCandidates {
		out = out[:MaxUpdateCandidates]
	}
	if out == nil {
		return []string{}
	}
	return out
}

// candidatesAppeared 报告 now 中是否存在从未推送给用户的候选版本：
// 只关心新增（版本发布），撤下不触发提醒。
func candidatesAppeared(notified, now []string) bool {
	if len(now) == 0 {
		return false
	}
	seen := make(map[string]bool, len(notified))
	for _, t := range notified {
		seen[t] = true
	}
	for _, t := range now {
		if !seen[t] {
			return true
		}
	}
	return false
}

// excludeTags 从 tags 中剔除 already 里已出现过的 tag（按原串比较）。
func excludeTags(tags, already []string) []string {
	if len(tags) == 0 || len(already) == 0 {
		return tags
	}
	skip := make(map[string]bool, len(already))
	for _, t := range already {
		skip[t] = true
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !skip[t] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func computeHasUpdate(currentVersion, currentDigest, baseDigest, latestVersion string, candidates []string) bool {
	if len(candidates) > 0 {
		return true
	}
	if latestVersion != "" && currentVersion != "" && version.CompareTag(latestVersion, currentVersion) > 0 {
		return true
	}
	return baseDigest != "" && currentDigest != "" && baseDigest != currentDigest
}

func decodeCandidates(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
