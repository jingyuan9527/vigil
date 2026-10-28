package models

import "time"

// ImageStatus 表示镜像的更新状态。
type ImageStatus string

const (
	StatusUnknown         ImageStatus = "unknown"
	StatusUpToDate        ImageStatus = "up-to-date"
	StatusUpdateAvailable ImageStatus = "update-available"
	StatusStale           ImageStatus = "stale"
)

// Image 是被监控的一个镜像引用（按 reference 唯一）。
type Image struct {
	ID            int64       `json:"id"`
	Name          string      `json:"name"`      // 仓库路径，如 library/nginx
	Reference     string      `json:"reference"` // 被监控的完整引用，如 nginx:latest
	Registry      string      `json:"registry"`  // 注册表主机
	Tag           string      `json:"tag"`
	Source        string      `json:"source"`       // docker | manual | default（内置演示监控列表）
	LocalDigest   string      `json:"local_digest"` // 本地已拉取的摘要
	RemoteDigest  string      `json:"remote_digest"`
	Status        ImageStatus `json:"status"`
	Ignored       bool        `json:"ignored"`        // 用户手动忽略该镜像的更新提醒（仍扫描，但不产生通知）
	Mode          string      `json:"mode"`           // 用户覆写的检测模式（auto/digest-only/pin-watch），由 SetMode 维护，扫描不覆盖
	EffectiveMode string      `json:"effective_mode"` // 生效检测模式（读取时由 ResolveMode 计算，不落库）
	LastCheck     *time.Time  `json:"last_check"`
	LastUpdate    *time.Time  `json:"last_update"` // 远端摘要最近一次变化时间
	Error         string      `json:"error,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
}

// ImageVersion 是某镜像每次扫描记录到的摘要快照（用于版本时间线）。
type ImageVersion struct {
	ID        int64     `json:"id"`
	ImageID   int64     `json:"image_id"`
	Digest    string    `json:"digest"`
	Tag       string    `json:"tag"`
	ScannedAt time.Time `json:"scanned_at"`
}

// NotificationKind 区分通知的强弱类型。
type NotificationKind string

const (
	// NotifUpdate 强更新：同一 tag 的远端 digest 发生变化（如 latest 移动）。
	NotifUpdate NotificationKind = "update"
	// NotifNewTag 弱提醒：仓库出现比当前固定 tag 更高的独立版本（如 8.4.7 → 26）。
	NotifNewTag NotificationKind = "new-tag"
)

// Notification 是检测到新版本时产生的更新通知。
type Notification struct {
	ID        int64            `json:"id"`
	Type      NotificationKind `json:"type"`
	ImageID   int64            `json:"image_id"`
	ImageName string           `json:"image_name"`
	Reference string           `json:"reference"`
	OldDigest string           `json:"old_digest"`
	NewDigest string           `json:"new_digest"`
	OldTag    string           `json:"old_tag"`
	NewTag    string           `json:"new_tag"`
	// LatestTag 是检测时仓库远端版本号最高的 tag（如 v1.27.1），供页面直观展示版本号；
	// tags/list 不可用或仓库无版本 tag 时为空。
	LatestTag string    `json:"latest_tag"`
	Message   string    `json:"message"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

// ImageUpdateStatus 是聚合更新记录的处理状态。
type ImageUpdateStatus string

const (
	// UpdateUnread 版本变化已推送、用户尚未处理（计入未读角标）。
	UpdateUnread ImageUpdateStatus = "unread"
	// UpdateRead 用户已确认当前版本，不再计入未读角标。
	UpdateRead ImageUpdateStatus = "read"
	// UpdateIgnored 用户忽略当前版本：同版本不再提醒，出现更高版本时自动恢复未读。
	UpdateIgnored ImageUpdateStatus = "ignored"
)

// 聚合更新记录的推送原因：用于自动已读判定与页面提示。
const (
	// UpdateReasonDigest 同一 tag 的远端摘要发生变化（浮动 tag 的内容更新）。
	UpdateReasonDigest = "digest"
	// UpdateReasonVersion 出现更高版本（候选版本集合或最高版本号变化）。
	UpdateReasonVersion = "version"
	// UpdateReasonBoth 摘要与版本号同时变化。
	UpdateReasonBoth = "both"
)

// ImageUpdate 是按镜像聚合的更新记录：每个镜像至多一条，随检测周期原地合并，
// 保存当前监控版本、最新版本与候选版本集合，并承载已读/忽略状态。
// 与 notifications（推送历史流水）分离：页面展示聚合状态，历史表只做留档。
type ImageUpdate struct {
	ImageID        int64             `json:"image_id"`
	ImageName      string            `json:"image_name"`
	Reference      string            `json:"reference"`
	CurrentVersion string            `json:"current_version"` // 当前监控的 tag（如 8.4.7）
	CurrentDigest  string            `json:"current_digest"`  // 当前监控 tag 的远端摘要
	BaseDigest     string            `json:"base_digest"`     // 差异基线（本地持有摘要；纯远端监控为上次记录）
	LatestVersion  string            `json:"latest_version"`  // 仓库最高版本号 tag，取不到时为空
	Candidates     []string          `json:"candidates"`      // 高于当前版本的候选版本，去重并按版本降序
	NewTags        []string          `json:"new_tags"`        // 最近一次提醒中仓库新发布的版本 tag（含低版本/其它分支）
	HasUpdate      bool              `json:"has_update"`      // 当前是否存在待处理差异（无差异的已读记录不展示）
	Status         ImageUpdateStatus `json:"status"`
	IgnoredVersion string            `json:"ignored_version,omitempty"` // 被忽略的版本号（忽略到下一个版本）
	IgnoredDigest  string            `json:"ignored_digest,omitempty"`  // 无版本号仓库按摘要忽略
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason,omitempty"` // 最近一次推送原因，空表示仅建立基线
	FirstSeenAt    time.Time         `json:"first_seen_at"`
	LastSeenAt     time.Time         `json:"last_seen_at"`
	NotifiedAt     *time.Time        `json:"notified_at"`
}

// Scan 是一次扫描任务的记录。
type Scan struct {
	ID            int64      `json:"id"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	ImagesChecked int        `json:"images_checked"`
	UpdatesFound  int        `json:"updates_found"`
	Status        string     `json:"status"` // running | done | error
	Error         string     `json:"error,omitempty"`
}

// Stats 是仪表盘统计聚合。
type Stats struct {
	Total           int        `json:"total"`
	UpToDate        int        `json:"up_to_date"`
	UpdateAvailable int        `json:"update_available"`
	Unknown         int        `json:"unknown"`
	UnreadNotifs    int        `json:"unread_notifications"`
	LastScanAt      *time.Time `json:"last_scan_at"`
	LastScanStatus  string     `json:"last_scan_status"`
}

// ImageDetail 是镜像详情，附带版本时间线与可用 tag。
type ImageDetail struct {
	Image    Image          `json:"image"`
	Versions []ImageVersion `json:"versions"`
	Tags     []string       `json:"tags,omitempty"`
}
