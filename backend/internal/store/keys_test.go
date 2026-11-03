package store

import (
	"errors"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func TestMCPKeyLifecycle(t *testing.T) {
	st := openTestStore(t)

	// 创建两条
	k1, err := st.CreateMCPKey("我的笔记本", "vigil_mcp_aaaa…", "hash-1")
	if err != nil {
		t.Fatalf("create key1: %v", err)
	}
	if _, err := st.CreateMCPKey("CI 机器人", "vigil_mcp_bbbb…", "hash-2"); err != nil {
		t.Fatalf("create key2: %v", err)
	}
	if k1.Name != "我的笔记本" || k1.KeyPrefix != "vigil_mcp_aaaa…" {
		t.Fatalf("created key = %+v", k1)
	}

	// 列表
	keys, err := st.ListMCPKeys()
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys count = %d", len(keys))
	}

	// 哈希鉴权：命中返回 ID
	id, err := st.UseMCPKey("hash-1")
	if err != nil || id != k1.ID {
		t.Fatalf("UseMCPKey hash-1 = %d, %v", id, err)
	}
	// 未命中
	if _, err := st.UseMCPKey("hash-miss"); !errors.Is(err, ErrMCPKeyNotFound) {
		t.Fatalf("UseMCPKey miss = %v, want ErrMCPKeyNotFound", err)
	}

	// last_used_at 节流刷新：首次使用应写时间戳
	got, err := st.GetMCPKey(k1.ID)
	if err != nil || got == nil {
		t.Fatalf("get key: %v", err)
	}
	if got.LastUsedAt == nil {
		t.Fatal("last_used_at not set after first use")
	}

	// 删除后再次使用失败，列表减一
	if err := st.DeleteMCPKey(k1.ID); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if _, err := st.UseMCPKey("hash-1"); !errors.Is(err, ErrMCPKeyNotFound) {
		t.Fatalf("revoked key still usable: %v", err)
	}
	if err := st.DeleteMCPKey(k1.ID); !errors.Is(err, ErrMCPKeyNotFound) {
		t.Fatalf("double delete = %v", err)
	}
	keys, _ = st.ListMCPKeys()
	if len(keys) != 1 {
		t.Fatalf("keys count after delete = %d", len(keys))
	}
}

func TestMCPKeyUseTouchThrottle(t *testing.T) {
	st := openTestStore(t)
	k, err := st.CreateMCPKey("throttle", "p", "h")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 第一次使用写入 last_used_at
	if _, err := st.UseMCPKey("h"); err != nil {
		t.Fatalf("use 1: %v", err)
	}
	first, _ := st.GetMCPKey(k.ID)
	if first.LastUsedAt == nil {
		t.Fatal("last_used_at not set")
	}
	// 直接把 last_used_at 拨回很久以前，验证节流窗口内的刷新逻辑
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := st.db.Exec("UPDATE mcp_api_keys SET last_used_at=? WHERE id=?", old, k.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := st.UseMCPKey("h"); err != nil {
		t.Fatalf("use 2: %v", err)
	}
	second, _ := st.GetMCPKey(k.ID)
	if second.LastUsedAt.Before(*first.LastUsedAt) {
		t.Fatalf("last_used_at went backwards: %v -> %v", first.LastUsedAt, second.LastUsedAt)
	}
}
