package config

import (
	"strconv"
	"sync"
	"time"
)

// Settings 是可在页面上配置、并持久化到数据库的运行时设置。
// 字段与 Config 中对应的环境变量一一对应，但运行期可被用户覆盖。
type Settings struct {
	ScanInterval        int    `json:"scan_interval"`          // 周期扫描间隔（秒），<=0 表示禁用周期扫描
	RegistryInsecure    bool   `json:"registry_insecure"`      // 是否允许 http 注册表
	RegistryMirror      string `json:"registry_mirror"`        // 注册表镜像主机（非空时覆盖请求主机）
	DisableDefaultWatch bool   `json:"disable_default_watch"`  // 关闭内置演示监控列表
}

// ScanMinSeconds 是 scan_interval 在启用状态下允许的最小值，避免过于频繁地打注册表。
const ScanMinSeconds = 30

// LiveSettings 是线程安全的运行时可变配置。
// 由环境变量初始化，可通过页面持久化覆盖；扫描节拍与注册表客户端会实时读取其值。
type LiveSettings struct {
	mu                 sync.RWMutex
	broadcast          chan struct{}
	ScanInterval        int
	RegistryInsecure    bool
	RegistryMirror      string
	DisableDefaultWatch bool
}

// NewLiveSettings 以环境变量初值构造 LiveSettings。
func NewLiveSettings(scanSeconds int, insecure bool, mirror string, disableDefault bool) *LiveSettings {
	l := &LiveSettings{
		ScanInterval:        scanSeconds,
		RegistryInsecure:    insecure,
		RegistryMirror:      mirror,
		DisableDefaultWatch: disableDefault,
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
		RegistryInsecure:    l.RegistryInsecure,
		RegistryMirror:      l.RegistryMirror,
		DisableDefaultWatch: l.DisableDefaultWatch,
	}
}

// Apply 用请求体整体覆盖（调用方应发送完整对象）。
// 覆盖后会广播变更，使扫描节拍与依赖方（注册表客户端）即时响应。
func (l *LiveSettings) Apply(s Settings) {
	l.mu.Lock()
	l.ScanInterval = s.ScanInterval
	l.RegistryInsecure = s.RegistryInsecure
	l.RegistryMirror = s.RegistryMirror
	l.DisableDefaultWatch = s.DisableDefaultWatch
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
		"registry_insecure":     strconv.FormatBool(s.RegistryInsecure),
		"registry_mirror":       s.RegistryMirror,
		"disable_default_watch": strconv.FormatBool(s.DisableDefaultWatch),
	}
}

// SettingsFromMap 从库中的 key/value 还原设置（缺字段时使用零值）。
func SettingsFromMap(m map[string]string) Settings {
	s := Settings{}
	if v, ok := m["scan_interval"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			s.ScanInterval = n
		}
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
	return s
}
