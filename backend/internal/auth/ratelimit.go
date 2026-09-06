package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// LoginLimiter 登录失败限流：按客户端 IP 计数 + 全局失败上限双层闸门。
// 内存实现（单实例部署足够），登录成功清零该 IP；条目惰性过期，不启后台清理。
//
// 双层的必要性：仅按 IP 限流时，反向代理 / 端口映射后所有用户共享同一出口 IP，
// 攻击者爆破 5 次即可把所有真实用户锁在门外（拒绝服务）；全局上限保证单 IP
// 低频爆破无法得手的同时，误伤面收敛为「整体延迟 15 分钟」而非「全员锁定」。
type LoginLimiter struct {
	mu     sync.Mutex
	fails  map[string]*loginAttempt
	global loginAttempt
}

type loginAttempt struct {
	count    int
	until    time.Time // 锁定截止时间
	lastSeen time.Time // 最近一次失败时间，供空闲条目清理
}

const (
	maxLoginFails    = 5    // 单 IP 连续失败次数阈值
	loginLockMinutes = 15   // 锁定时长（分钟）
	maxGlobalFails   = 100  // 全局失败阈值：窗口内累计失败达到即整体锁定
	maxTrackIPs      = 4096 // 追踪 IP 数上限：超过即清理空闲条目，防止公网刷 IP 撑爆内存
)

// NewLoginLimiter 构造空限流器。
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{fails: map[string]*loginAttempt{}}
}

// Allow 报告当前是否允许尝试登录（全局或该 IP 任一处于锁定期即拒绝）。
func (l *LoginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Before(l.global.until) {
		return false
	}
	a := l.fails[ip]
	if a == nil {
		return true
	}
	if a.until.IsZero() {
		return true // 尚未锁定，计数保留供 RecordFailure 累加
	}
	if now.After(a.until) {
		delete(l.fails, ip) // 锁定到期，自动解除并清空计数
		return true
	}
	return false
}

// RecordFailure 记录一次登录失败；单 IP 连续失败达到阈值或全局累计达到阈值即锁定。
func (l *LoginLimiter) RecordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	a := l.fails[ip]
	if a == nil {
		a = &loginAttempt{}
		l.fails[ip] = a
	} else if !a.until.IsZero() && now.After(a.until) {
		// 锁定已到期：重新计数（零值 until 表示从未锁定，不能据此重建，
		// 否则每次失败都清零计数、阈值永远到不了——历史实现正栽在这里）
		*a = loginAttempt{}
	}
	a.count++
	a.lastSeen = now
	if a.count >= maxLoginFails {
		a.until = now.Add(loginLockMinutes * time.Minute)
		a.count = 0 // 计数清零，until 到期前 Allow 恒为 false
	}

	l.global.count++
	l.global.lastSeen = now
	if l.global.count >= maxGlobalFails {
		l.global.until = now.Add(loginLockMinutes * time.Minute)
		l.global.count = 0
	}

	if len(l.fails) > maxTrackIPs {
		l.pruneIdle(now)
	}
}

// pruneIdle 清理既未锁定、又超过一个锁定窗口没有活动的条目（调用方须持锁）。
func (l *LoginLimiter) pruneIdle(now time.Time) {
	idle := loginLockMinutes * time.Minute
	for ip, a := range l.fails {
		if now.After(a.until) && now.Sub(a.lastSeen) > idle {
			delete(l.fails, ip)
		}
	}
}

// Reset 登录成功后清零该 IP 的失败记录（全局计数无法归属，保持累计）。
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
