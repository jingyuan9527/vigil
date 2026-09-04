package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// LoginLimiter 登录失败限流：按客户端 IP 计数，连续失败达到阈值后锁定一段时间。
// 内存实现（单实例部署足够），登录成功清零。条目惰性过期，不启后台清理。
type LoginLimiter struct {
	mu    sync.Mutex
	fails map[string]*loginAttempt
}

type loginAttempt struct {
	count int
	until time.Time // 锁定截止时间
}

const (
	maxLoginFails    = 5   // 连续失败次数阈值
	loginLockMinutes = 15  // 锁定时长（分钟）
)

// NewLoginLimiter 构造空限流器。
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{fails: map[string]*loginAttempt{}}
}

// Allow 报告该 IP 当前是否允许尝试登录。
func (l *LoginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.fails[ip]
	if a == nil {
		return true
	}
	if time.Now().After(a.until) {
		delete(l.fails, ip) // 锁定到期，自动解除并清空计数
		return true
	}
	return false
}

// RecordFailure 记录一次登录失败；连续失败达到阈值即锁定该 IP。
func (l *LoginLimiter) RecordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.fails[ip]
	if a == nil || time.Now().After(a.until) {
		a = &loginAttempt{}
		l.fails[ip] = a
	}
	a.count++
	if a.count >= maxLoginFails {
		a.until = time.Now().Add(loginLockMinutes * time.Minute)
		a.count = 0 // 计数清零，until 到期前 Allow 恒为 false
	}
}

// Reset 登录成功后清零该 IP 的失败记录。
func (l *LoginLimiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

// ClientIP 提取客户端 IP（RemoteAddr 去端口）。
// 单机/单容器部署下不信任 X-Forwarded-For，避免伪造绕过限流。
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}