package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// ---- Password hashing (SHA-256 + salt) ----

func HashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	data := append([]byte{}, salt...)
	data = append(data, []byte(password)...)
	h := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(h[:])
}

func CheckPassword(stored, password string) bool {
	parts := strings.SplitN(stored, ":", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	expected, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	data := append([]byte{}, salt...)
	data = append(data, []byte(password)...)
	h := sha256.Sum256(data)
	return hmac.Equal(h[:], expected)
}

// ---- JWT (HMAC-SHA256, stdlib only) ----

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtPayload struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

func GenerateToken(username string, secret []byte, expire time.Duration) (string, error) {
	header := jwtHeader{Alg: "HS256", Typ: "JWT"}
	now := time.Now()
	payload := jwtPayload{Sub: username, Iat: now.Unix(), Exp: now.Add(expire).Unix()}

	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(payload)

	encH := base64.RawURLEncoding.EncodeToString(hb)
	encP := base64.RawURLEncoding.EncodeToString(pb)
	sigInput := encH + "." + encP

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigInput))
	sig := mac.Sum(nil)
	encS := base64.RawURLEncoding.EncodeToString(sig)

	return encH + "." + encP + "." + encS, nil
}

func ValidateToken(token string, secret []byte) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid token format")
	}
	sigInput := parts[0] + "." + parts[1]

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigInput))
	expected := mac.Sum(nil)

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, expected) {
		return "", errors.New("invalid signature")
	}

	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid payload")
	}
	var payload jwtPayload
	if err := json.Unmarshal(pb, &payload); err != nil {
		return "", errors.New("invalid payload")
	}
	if time.Now().Unix() > payload.Exp {
		return "", errors.New("token expired")
	}
	return payload.Sub, nil
}

// ---- HTTP Middleware ----

// 令牌 cookie 相关：登录态由 httpOnly cookie 维持（同源部署，防 XSS 窃取）。
const (
	// TokenCookieName 令牌 cookie 名。
	TokenCookieName = "dockmon_token"
	// TokenMaxAge 令牌有效期（与 GenerateToken 的 72h 保持一致）。
	TokenMaxAge = 72 * time.Hour
)

// SetTokenCookie 将 JWT 写入 httpOnly cookie。
// SameSite=Lax：跨站请求不携带 cookie，天然抵御 CSRF；同源 fetch 自动携带。
func SetTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     TokenCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(TokenMaxAge.Seconds()),
	})
}

// ClearTokenCookie 清除令牌 cookie（登出）。
func ClearTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     TokenCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// RequestAuthenticated 报告请求是否携带有效令牌（Authorization 头或 cookie）。
func RequestAuthenticated(r *http.Request, secret []byte) bool {
	token := extractToken(r)
	if token == "" {
		return false
	}
	_, err := ValidateToken(token, secret)
	return err == nil
}

func GenerateSecret() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

// Middleware 拦截 /api/* 请求，校验 Bearer token。
// /api/auth/*、/api/health 不受保护。
func Middleware(next http.Handler, secret []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		// 放行无需认证的路径
		if p == "/api/health" || strings.HasPrefix(p, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		// 静态资源不拦截（由 withStatic 处理）
		if !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		token := extractToken(r)
		if token == "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		if _, err := ValidateToken(token, secret); err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("dockmon_token"); err == nil {
		return c.Value
	}
	return ""
}
