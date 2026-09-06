package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestHashPasswordBcrypt(t *testing.T) {
	h := HashPassword("s3cret-pass")
	if h == "" || h == "s3cret-pass" {
		t.Fatalf("hash empty or plaintext: %q", h)
	}
	if !CheckPassword(h, "s3cret-pass") {
		t.Error("correct password rejected")
	}
	if CheckPassword(h, "wrong-pass") {
		t.Error("wrong password accepted")
	}
}

// 旧格式哈希（单轮 SHA-256+salt）必须仍可校验，保证存量库平滑升级。
func TestCheckPasswordLegacyFormat(t *testing.T) {
	salt := []byte("0123456789abcdef")
	data := append(append([]byte{}, salt...), []byte("old-pass")...)
	sum := sha256.Sum256(data)
	stored := base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(sum[:])

	if !CheckPassword(stored, "old-pass") {
		t.Error("legacy hash rejected for correct password")
	}
	if CheckPassword(stored, "new-pass") {
		t.Error("legacy hash accepted wrong password")
	}
	if !NeedsRehash(stored) {
		t.Error("legacy hash must be flagged for rehash")
	}
}

func TestNeedsRehashBcryptFalse(t *testing.T) {
	if NeedsRehash(HashPassword("x")) {
		t.Error("bcrypt hash flagged for rehash")
	}
	if !NeedsRehash("") || !NeedsRehash("garbage") {
		t.Error("malformed hashes should be treated as legacy")
	}
}

func TestLoginLimiterPerIPLock(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < maxLoginFails; i++ {
		if !l.Allow("1.1.1.1") {
			t.Fatalf("locked too early (fail %d)", i+1)
		}
		l.RecordFailure("1.1.1.1")
	}
	if l.Allow("1.1.1.1") {
		t.Error("IP not locked after threshold")
	}
	if !l.Allow("2.2.2.2") {
		t.Error("unrelated IP locked")
	}
	l.Reset("1.1.1.1")
	if !l.Allow("1.1.1.1") {
		t.Error("Reset did not unlock IP")
	}
}

// 全局上限：单 IP 低频轮换打满全局阈值后整体锁定（反代共享出口 IP 场景的兜底）。
func TestLoginLimiterGlobalLock(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < maxGlobalFails; i++ {
		ip := "10.0.0." + string(rune('a'+i%26)) // 轮换 IP，不触发单 IP 锁
		if !l.Allow(ip) {
			t.Fatalf("global lock engaged too early (fail %d)", i+1)
		}
		l.RecordFailure(ip)
	}
	if l.Allow("never-seen-before") {
		t.Error("global threshold reached but new IP still allowed")
	}
}

// 空闲条目清理：超过上限后，未锁定且长时间无活动的条目应被回收。
func TestLoginLimiterPruneIdle(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < maxTrackIPs+10; i++ {
		l.RecordFailure("10.1.0." + string(rune(i%256)))
	}
	// 伪造一批已过锁定窗口的旧条目，再触发一次清理
	now := time.Now()
	l.mu.Lock()
	for ip, a := range l.fails {
		a.lastSeen = now.Add(-2 * loginLockMinutes * time.Minute)
		a.until = now.Add(-time.Minute)
		_ = ip
	}
	l.mu.Unlock()
	l.RecordFailure("trigger-prune")
	l.mu.Lock()
	n := len(l.fails)
	l.mu.Unlock()
	if n > maxTrackIPs {
		t.Errorf("prune did not bound map size: %d", n)
	}
}
