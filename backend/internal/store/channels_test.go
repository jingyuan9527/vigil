package store

import (
	"testing"
)

func TestChannelCRUD(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	ch, err := st.CreateChannel("dingtalk", "运维群", `{"webhook":"https://x"}`, true)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if ch.ID == 0 || ch.Kind != "dingtalk" || !ch.Enabled {
		t.Fatalf("created channel wrong: %+v", ch)
	}

	got, err := st.GetChannel(ch.ID)
	if err != nil || got == nil {
		t.Fatalf("get channel: %v (%v)", got, err)
	}
	if got.Config != `{"webhook":"https://x"}` {
		t.Errorf("config mismatch: %q", got.Config)
	}

	if err := st.UpdateChannel(ch.ID, "新名", `{"webhook":"https://y"}`, false); err != nil {
		t.Fatalf("update channel: %v", err)
	}
	got, _ = st.GetChannel(ch.ID)
	if got.Name != "新名" || got.Enabled || got.Config != `{"webhook":"https://y"}` {
		t.Errorf("updated channel wrong: %+v", got)
	}

	// enabledOnly 过滤
	ch2, _ := st.CreateChannel("wecom", "", `{"webhook":"https://w"}`, true)
	enabled, err := st.ListChannels(true)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != ch2.ID {
		t.Errorf("enabledOnly list wrong: %+v", enabled)
	}

	// 更新/删除不存在的渠道 → 明确错误
	if err := st.UpdateChannel(9999, "x", "{}", true); err != ErrChannelNotFound {
		t.Errorf("update missing channel err = %v, want ErrChannelNotFound", err)
	}
	if err := st.DeleteChannel(9999); err != ErrChannelNotFound {
		t.Errorf("delete missing channel err = %v, want ErrChannelNotFound", err)
	}

	if err := st.DeleteChannel(ch2.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if has, _ := st.HasChannelKind("wecom"); has {
		t.Error("HasChannelKind('wecom') should be false after delete")
	}
}

func TestMigrateDingtalkToChannel(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// 旧 settings 表残键 + 无环境变量 → 迁移成 dingtalk 渠道，并清掉旧键
	st.SaveSettingsMap(map[string]string{"dingtalk_webhook": "https://oapi/xxx", "dingtalk_secret": "SEC"})
	migrated, err := st.MigrateDingtalkToChannel("", "")
	if err != nil || !migrated {
		t.Fatalf("migrate from settings: migrated=%v err=%v", migrated, err)
	}
	chans, _ := st.ListChannels(false)
	if len(chans) != 1 || chans[0].Kind != "dingtalk" {
		t.Fatalf("expected one dingtalk channel, got %+v", chans)
	}
	if m, _ := st.LoadSettingsMap(); m["dingtalk_webhook"] != "" || m["dingtalk_secret"] != "" {
		t.Errorf("legacy dingtalk keys should be removed, got %v", m)
	}
	if has, _ := st.HasChannelKind("dingtalk"); !has {
		t.Error("dingtalk channel should exist")
	}

	// 已有 dingtalk 渠道时再次迁移 → 不重复播种
	if again, _ := st.MigrateDingtalkToChannel("https://env-other", "SEC2"); again {
		t.Error("migration should not duplicate dingtalk channel")
	}
}

func TestMigrateDingtalkNoConfig(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if migrated, err := st.MigrateDingtalkToChannel("", ""); err != nil || migrated {
		t.Fatalf("empty config should not migrate: migrated=%v err=%v", migrated, err)
	}
	if chans, _ := st.ListChannels(false); len(chans) != 0 {
		t.Errorf("no channel should be created: %+v", chans)
	}
}

func TestMigrateDingtalkEnvPriority(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// 环境变量优先于 settings 旧键
	st.SaveSettingsMap(map[string]string{"dingtalk_webhook": "https://old"})
	migrated, err := st.MigrateDingtalkToChannel("https://env", "SEC")
	if err != nil || !migrated {
		t.Fatalf("migrate with env: migrated=%v err=%v", migrated, err)
	}
	chans, _ := st.ListChannels(false)
	if chans[0].Config != `{"secret":"SEC","webhook":"https://env"}` &&
		chans[0].Config != `{"webhook":"https://env","secret":"SEC"}` {
		t.Errorf("env config should win, got %q", chans[0].Config)
	}
}
