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
	ID           int64      `json:"id"`
	Name         string     `json:"name"`         // 仓库路径，如 library/nginx
	Reference    string     `json:"reference"`    // 被监控的完整引用，如 nginx:latest
	Registry     string     `json:"registry"`     // 注册表主机
	Tag          string     `json:"tag"`
	Source       string     `json:"source"`       // docker | manual
	LocalDigest  string     `json:"local_digest"` // 本地已拉取的摘要
	RemoteDigest string     `json:"remote_digest"`
	Status       ImageStatus `json:"status"`
	Ignored      bool       `json:"ignored"`      // 用户手动忽略该镜像的更新提醒（仍扫描，但不产生通知）
	NotifiedNewTag string   `json:"notified_new_tag,omitempty"` // （遗留字段）已弱提醒过的「更高新版本」目标 tag；新版改用 image_seen_tags 表
	Mode          string   `json:"mode"`                        // 用户覆写的检测模式（auto/digest-only/pin-watch），由 SetMode 维护，扫描不覆盖
	EffectiveMode string   `json:"effective_mode"`              // 生效检测模式（读取时由 ResolveMode 计算，不落库）
	LastCheck    *time.Time `json:"last_check"`
	LastUpdate   *time.Time `json:"last_update"` // 远端摘要最近一次变化时间
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
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
	ID        int64     `json:"id"`
	Type      NotificationKind `json:"type"`
	ImageID   int64     `json:"image_id"`
	ImageName string    `json:"image_name"`
	Reference string    `json:"reference"`
	OldDigest string    `json:"old_digest"`
	NewDigest string    `json:"new_digest"`
	OldTag    string    `json:"old_tag"`
	NewTag    string    `json:"new_tag"`
	Message   string    `json:"message"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
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
	Total            int `json:"total"`
	UpToDate         int `json:"up_to_date"`
	UpdateAvailable  int `json:"update_available"`
	Unknown          int `json:"unknown"`
	UnreadNotifs     int `json:"unread_notifications"`
	LastScanAt       *time.Time `json:"last_scan_at"`
	LastScanStatus   string     `json:"last_scan_status"`
}

// ImageDetail 是镜像详情，附带版本时间线与可用 tag。
type ImageDetail struct {
	Image   Image           `json:"image"`
	Versions []ImageVersion `json:"versions"`
	Tags     []string       `json:"tags,omitempty"`
}
