package config

import (
	"testing"
	"time"
)

func TestParseDailyTime(t *testing.T) {
	cases := []struct {
		in    string
		h, m  int
		valid bool
	}{
		{"03:30", 3, 30, true},
		{"23:59", 23, 59, true},
		{"00:00", 0, 0, true},
		{"3:30", 3, 30, true},  // 容忍单数字时刻
		{"24:00", 0, 0, false}, // 小时越界
		{"12:60", 0, 0, false}, // 分钟越界
		{"12", 0, 0, false},    // 缺分钟
		{"", 0, 0, false},
		{"ab:cd", 0, 0, false},
	}
	for _, c := range cases {
		h, m, ok := ParseDailyTime(c.in)
		if ok != c.valid || (ok && (h != c.h || m != c.m)) {
			t.Errorf("ParseDailyTime(%q) = (%d,%d,%v), want (%d,%d,%v)", c.in, h, m, ok, c.h, c.m, c.valid)
		}
	}
}

func TestNextDailyRun(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.Local)

	// 当天时刻未到 → 今天
	got, ok := NextDailyRun("23:00", now)
	if !ok || !got.Equal(time.Date(2026, 9, 6, 23, 0, 0, 0, time.Local)) {
		t.Errorf("future today: got %v ok=%v", got, ok)
	}

	// 当天时刻已过 → 明天
	got, ok = NextDailyRun("09:00", now)
	if !ok || !got.Equal(time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)) {
		t.Errorf("passed today: got %v ok=%v", got, ok)
	}

	// 恰为当前时刻 → 顺延明天（避免同刻重复触发）
	got, ok = NextDailyRun("10:00", now)
	if !ok || !got.Equal(time.Date(2026, 9, 7, 10, 0, 0, 0, time.Local)) {
		t.Errorf("exact now: got %v ok=%v", got, ok)
	}

	// 非法格式
	if _, ok := NextDailyRun("25:00", now); ok {
		t.Error("invalid time should return ok=false")
	}
}

func TestSettingsMapRoundTrip(t *testing.T) {
	in := Settings{
		ScanInterval:        86400,
		ScanMode:            ScanModeDaily,
		ScanDailyTime:       "03:30",
		RegistryInsecure:    true,
		RegistryMirror:      "mirror.example.com",
		DisableDefaultWatch: true,
		DingTalkWebhook:     "https://oapi.dingtalk.com/robot/send?access_token=x",
		DingTalkSecret:      "SEC123",
	}
	out := SettingsFromMap(SettingsToMap(in))
	if out != in {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
}

// 旧库无 scan_mode / scan_daily_time 键时应默认间隔模式，平滑升级。
func TestSettingsFromMapLegacy(t *testing.T) {
	s := SettingsFromMap(map[string]string{"scan_interval": "3600"})
	if s.ScanMode != ScanModeInterval || s.ScanInterval != 3600 || s.ScanDailyTime != "" {
		t.Errorf("legacy map default wrong: %+v", s)
	}
}
