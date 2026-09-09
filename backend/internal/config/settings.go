package config

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// Settings 是可在页面上配置、并持久化到数据库的运行时设置。
// 字段与 Config 中对应的环境变量一一对应，但运行期可被用户覆盖。
type Settings struct {
	ScanInterval        int    `json:"scan_interval"`         // 间隔模式的扫描间隔（秒），<=0 表示禁用自动扫描
	ScanMode            string `json:"scan_mode"`             // 调度模式：interval（间隔）/ daily（每天定时）
	ScanDailyTime       string `json:"scan_daily_time"`       // daily 模式的每日触发时刻 HH:MM（服务器本地时区）
	RegistryInsecure    bool   `json:"registry_insecure"`     // 是否允许 http 注册表
	RegistryMirror      string `json:"registry_mirror"`       // 注册表镜像主机（非空时覆盖请求主机）
	DisableDefaultWatch bool   `json:"disable_default_watch"` // 关闭内置演示监控列表
	DingTalkWebhook     string `json:"dingtalk_webhook"`      // 钉钉通知 Webhook URL
	DingTalkSecret      string `json:"dingtalk_secret"`       // 钉钉机器人加签密钥（为空表示不加签）
}

// 扫描调度模式。
const (
	ScanModeInterval = "interval"
	ScanModeDaily    = "daily"
)

// ScanMinSeconds 是 scan_interval 在启用状态下允许的最小值，避免过于频繁地打注册表。
const ScanMinSeconds = 30

// ParseDailyTime 解析 "HH:MM"（24 小时制，容忍单数字时刻如 "3:30"）。
// 非法格式或超出范围返回 ok=false。
func ParseDailyTime(s string) (hour, minute int, ok bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// NextDailyRun 计算 now 之后下一次每日触发时刻（与 now 同一时区，按服务器本地时间）。
// 当天时刻已过或恰为当前时刻则顺延到明天；dailyTime 非法返回 ok=false。
func NextDailyRun(dailyTime string, now time.Time) (time.Time, bool) {
	h, m, ok := ParseDailyTime(dailyTime)
	if !ok {
		return time.Time{}, false
	}
	candidate := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate, true
}

// LiveSettings 是线程安全的运行时可变配置。
// 由环境变量初始化，可通过页面持久化覆盖；扫描节拍与注册表客户端会实时读取其值。
type LiveSettings struct {
	mu                  sync.RWMutex
	broadcast           chan struct{}
	ScanInterval        int
	ScanMode            string
	ScanDailyTime       string
	RegistryInsecure    bool
	RegistryMirror      string
	DisableDefaultWatch bool
	DingTalkWebhook     string
	DingTalkSecret      string
}

// NewLiveSettings 以环境变量初值构造 LiveSettings。
// 调度模式无环境变量入口：默认间隔模式，定时模式仅由页面配置。
func NewLiveSettings(scanSeconds int, insecure bool, mirror string, disableDefault bool, dingTalkWebhook, dingTalkSecret string) *LiveSettings {
	l := &LiveSettings{
		ScanInterval:        scanSeconds,
		ScanMode:            ScanModeInterval,
		RegistryInsecure:    insecure,
		RegistryMirror:      mirror,
		DisableDefaultWatch: disableDefault,
		DingTalkWebhook:     dingTalkWebhook,
		DingTalkSecret:      dingTalkSecret,
	}
	l.broadcast = make(chan struct{})
	return l
}

// Snapshot 返回当前设置的不可变副本，用于 JSON 序列化。
func (l *LiveSettings) Snapshot() Settings {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Settings{
		ScanInterval:        l.ScanInterval,
		ScanMode:            l.ScanMode,
		ScanDailyTime:       l.ScanDailyTime,
		RegistryInsecure:    l.RegistryInsecure,
		RegistryMirror:      l.RegistryMirror,
		DisableDefaultWatch: l.DisableDefaultWatch,
		DingTalkWebhook:     l.DingTalkWebhook,
		DingTalkSecret:      l.DingTalkSecret,
	}
}

// Apply 用请求体整体覆盖（调用方应发送完整对象）。
// 覆盖后会广播变更，使扫描节拍与依赖方（注册表客户端）即时响应。
func (l *LiveSettings) Apply(s Settings) {
	l.mu.Lock()
	l.ScanInterval = s.ScanInterval
	l.ScanMode = s.ScanMode
	l.ScanDailyTime = s.ScanDailyTime
	l.RegistryInsecure = s.RegistryInsecure
	l.RegistryMirror = s.RegistryMirror
	l.DisableDefaultWatch = s.DisableDefaultWatch
	l.DingTalkWebhook = s.DingTalkWebhook
	l.DingTalkSecret = s.DingTalkSecret
	old := l.broadcast
	l.broadcast = make(chan struct{})
	l.mu.Unlock()
	close(old)
}

// Changed 返回一个在设置变更时关闭的通道，供监听方重新计时/重载。
func (l *LiveSettings) Changed() <-chan struct{} {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.broadcast
}

// ScanIntervalDuration 返回周期扫描间隔；<=0 表示禁用周期扫描。
func (l *LiveSettings) ScanIntervalDuration() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.ScanInterval <= 0 {
		return 0
	}
	return time.Duration(l.ScanInterval) * time.Second
}

// SettingsToMap 将设置序列化为 key/value，便于存库。
func SettingsToMap(s Settings) map[string]string {
	return map[string]string{
		"scan_interval":         strconv.Itoa(s.ScanInterval),
		"scan_mode":             s.ScanMode,
		"scan_daily_time":       s.ScanDailyTime,
		"registry_insecure":     strconv.FormatBool(s.RegistryInsecure),
		"registry_mirror":       s.RegistryMirror,
		"disable_default_watch": strconv.FormatBool(s.DisableDefaultWatch),
		"dingtalk_webhook":      s.DingTalkWebhook,
		"dingtalk_secret":       s.DingTalkSecret,
	}
}

// SettingsFromMap 从库中的 key/value 还原设置（缺字段时使用零值；例外：
// disable_default_watch 缺省视为 true，与部署默认一致——演示列表默认关闭）。
// scan_mode 缺省为 interval，保证旧库（无该键）平滑升级。
func SettingsFromMap(m map[string]string) Settings {
	s := Settings{ScanMode: ScanModeInterval, DisableDefaultWatch: true}
	if v, ok := m["scan_interval"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			s.ScanInterval = n
		}
	}
	if v, ok := m["scan_mode"]; ok && v != "" {
		s.ScanMode = v
	}
	if v, ok := m["scan_daily_time"]; ok {
		s.ScanDailyTime = v
	}
	if v, ok := m["registry_insecure"]; ok {
		s.RegistryInsecure = v == "true" || v == "1"
	}
	if v, ok := m["registry_mirror"]; ok {
		s.RegistryMirror = v
	}
	if v, ok := m["disable_default_watch"]; ok {
		s.DisableDefaultWatch = v == "true" || v == "1"
	}
	if v, ok := m["dingtalk_webhook"]; ok {
		s.DingTalkWebhook = v
	}
	if v, ok := m["dingtalk_secret"]; ok {
		s.DingTalkSecret = v
	}
	return s
}
