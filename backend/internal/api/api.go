package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dockmon/internal/auth"
	"dockmon/internal/config"
	"dockmon/internal/models"
	"dockmon/internal/notification"
	"dockmon/internal/registry"
	"dockmon/internal/scanner"
	"dockmon/internal/store"
)

type api struct {
	store        *store.Store
	scanner      *scanner.Scanner
	reg          *registry.Client
	staticDir    string
	settings     *config.LiveSettings
	jwtSecret    []byte
	loginLimiter *auth.LoginLimiter
}

// NewRouter 构造 HTTP 处理器：/api 走接口，其余路径回退到前端静态资源（SPA）。
func NewRouter(staticDir string, st *store.Store, sc *scanner.Scanner, reg *registry.Client, settings *config.LiveSettings, jwtSecret []byte) http.Handler {
	a := &api{store: st, scanner: sc, reg: reg, staticDir: staticDir, settings: settings, jwtSecret: jwtSecret, loginLimiter: auth.NewLoginLimiter()}
	mux := http.NewServeMux()

	// 认证相关（无需 token）
	mux.HandleFunc("/api/auth/check", a.authCheck)
	mux.HandleFunc("/api/auth/setup", a.authSetup)
	mux.HandleFunc("/api/auth/login", a.authLogin)
	mux.HandleFunc("/api/auth/logout", a.authLogout)

	// 受保护的 API
	mux.HandleFunc("/api/health", a.health)
	mux.HandleFunc("/api/stats", a.stats)
	mux.HandleFunc("/api/images", a.handleImages)
	mux.HandleFunc("/api/images/", a.handleImageByID)
	mux.HandleFunc("/api/scan", a.scan)
	mux.HandleFunc("/api/scans", a.scans)
	mux.HandleFunc("/api/settings", a.handleSettings)
	mux.HandleFunc("/api/notifications", a.notifications)
	mux.HandleFunc("/api/notifications/read-all", a.notificationsReadAll)
	mux.HandleFunc("/api/notifications/", a.notificationByID)
	mux.HandleFunc("/api/dingtalk/test", a.testDingTalk)

	// 认证中间件包裹 API 路由，静态资源不受影响
	return a.withAuth(a.withStatic(mux))
}

func (a *api) withStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		a.serveStatic(w, r)
	})
}

// withAuth 对 /api/* 路径施加 JWT 认证（/api/auth/* 和 /api/health 除外）。
func (a *api) withAuth(next http.Handler) http.Handler {
	return auth.Middleware(next, a.jwtSecret)
}

func (a *api) serveStatic(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean(r.URL.Path)
	if clean == "/" || clean == "." {
		clean = "/index.html"
	}
	full := filepath.Join(a.staticDir, clean)
	if _, err := os.Stat(full); err != nil {
		// SPA fallback：前端路由由 React 处理
		a.serveIndex(w)
		return
	}
	http.ServeFile(w, r, full)
}

func (a *api) serveIndex(w http.ResponseWriter) {
	idx := filepath.Join(a.staticDir, "index.html")
	data, err := os.ReadFile(idx)
	if err != nil {
		http.Error(w, "frontend not built (STATIC_DIR)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

// ---- handlers ----

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": "1.0.0",
		"time":    time.Now().UTC(),
	})
}

// ---- Auth handlers ----

// authCheck 返回是否需要初始化设置、当前请求是否已认证。
func (a *api) authCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_required": !a.store.HasAdmin(),
		"authenticated":  auth.RequestAuthenticated(r, a.jwtSecret),
	})
}

// authSetup 首次部署时设置管理员账号（仅在无管理员时可用）。
// 登录态写入 httpOnly cookie（前端不接触令牌）；响应体不再回传 token。
func (a *api) authSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ip := auth.ClientIP(r)
	if !a.loginLimiter.Allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "尝试过于频繁，请稍后再试"})
		return
	}
	if a.store.HasAdmin() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "管理员已设置，请使用登录接口"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.loginLimiter.RecordFailure(ip)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		a.loginLimiter.RecordFailure(ip)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "用户名和密码不能为空"})
		return
	}
	if len(body.Password) < 6 {
		a.loginLimiter.RecordFailure(ip)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "密码长度至少 6 位"})
		return
	}
	hash := auth.HashPassword(body.Password)
	if err := a.store.SetAdmin(body.Username, hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	token, err := auth.GenerateToken(body.Username, a.jwtSecret, 72*time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 token 失败"})
		return
	}
	auth.SetTokenCookie(w, token)
	a.loginLimiter.Reset(ip)
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

// authLogin 用户登录：校验凭据后写入 httpOnly cookie。
func (a *api) authLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ip := auth.ClientIP(r)
	if !a.loginLimiter.Allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "尝试过于频繁，请稍后再试"})
		return
	}
	if !a.store.HasAdmin() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请先完成初始设置"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.loginLimiter.RecordFailure(ip)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	storedUser, storedHash := a.store.GetAdmin()
	if body.Username != storedUser || !auth.CheckPassword(storedHash, body.Password) {
		a.loginLimiter.RecordFailure(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
		return
	}
	token, err := auth.GenerateToken(body.Username, a.jwtSecret, 72*time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成 token 失败"})
		return
	}
	auth.SetTokenCookie(w, token)
	a.loginLimiter.Reset(ip)
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

// authLogout 清除令牌 cookie。
func (a *api) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	auth.ClearTokenCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (a *api) stats(w http.ResponseWriter, r *http.Request) {
	st, err := a.store.Stats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *api) handleImages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		imgs, err := a.store.ListImages(status)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"images": imgs, "count": len(imgs)})
	case http.MethodPost:
		var body struct {
			Reference string `json:"reference"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		ref := strings.TrimSpace(body.Reference)
		if ref == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reference required"})
			return
		}
		if existing, _ := a.store.GetImageByRef(ref); existing == nil {
			pr := registry.ParseRef(ref)
			img := &models.Image{
				Name: pr.Repo, Reference: ref, Registry: pr.Registry, Tag: pr.Tag,
				Source: "manual", Status: models.StatusUnknown, CreatedAt: time.Now(),
			}
			if err := a.store.UpsertImage(img); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		go a.scanner.Run(context.Background(), false)
		writeJSON(w, http.StatusAccepted, map[string]string{"result": "queued", "reference": ref})
	default:
		methodNotAllowed(w)
	}
}

func (a *api) handleImageByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/images/"), "/")
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	// 子资源：PUT /api/images/{id}/ignored  设置/取消忽略（仍扫描不提醒）
	if len(parts) >= 2 && parts[1] == "ignored" && r.Method == http.MethodPut {
		var body struct {
			Ignored bool `json:"ignored"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if err := a.store.SetIgnored(id, body.Ignored); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": body.Ignored})
		return
	}

	// 子资源：PUT /api/images/{id}/mode  设置检测模式覆写（auto/digest-only/pin-watch）
	if len(parts) >= 2 && parts[1] == "mode" && r.Method == http.MethodPut {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		switch body.Mode {
		case models.ModeAuto, models.ModeDigestOnly, models.ModePinWatch:
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mode, must be auto / digest-only / pin-watch"})
			return
		}
		if err := a.store.SetMode(id, body.Mode); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"mode": body.Mode})
		return
	}

	img, err := a.store.GetImage(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if img == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		versions, _ := a.store.ListVersions(id)
		detail := models.ImageDetail{Image: *img, Versions: versions}
		if img.Registry != "" {
			ref := registry.ImageRef{Registry: img.Registry, Repo: img.Name, Tag: img.Tag}
			if tags, terr := a.reg.ListTags(context.Background(), ref); terr == nil {
				detail.Tags = tags
			}
		}
		writeJSON(w, http.StatusOK, detail)
	case http.MethodDelete:
		if err := a.store.DeleteImage(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": "deleted"})
	default:
		methodNotAllowed(w)
	}
}

func (a *api) scan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	force := r.URL.Query().Get("force") == "1"
	if a.scanner.IsRunning() {
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": "scan already running", "force": force})
		return
	}
	go a.scanner.Run(context.Background(), force)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"result": "scan started", "force": force})
}

// handleSettings 提供运行时设置的读取与持久化更新。
// GET 返回当前设置；PUT 整体覆盖并落库，注册表相关字段变化时热重建注册表客户端。
func (a *api) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.settings.Snapshot())
	case http.MethodPut:
		var body config.Settings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.ScanInterval < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scan_interval 不能为负"})
			return
		}
		// 启用状态下设置最小间隔，避免误配导致频繁请求注册表
		if body.ScanInterval > 0 && body.ScanInterval < config.ScanMinSeconds {
			body.ScanInterval = config.ScanMinSeconds
		}
		body.RegistryMirror = strings.TrimSpace(body.RegistryMirror)

		prev := a.settings.Snapshot()
		a.settings.Apply(body)
		next := a.settings.Snapshot()

		if err := a.store.SaveSettingsMap(config.SettingsToMap(next)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// 注册表相关字段变更：热重建客户端并同步 scanner 与接口自身
		if prev.RegistryInsecure != next.RegistryInsecure || prev.RegistryMirror != next.RegistryMirror {
			newReg := registry.NewClientWithMirror(next.RegistryInsecure, next.RegistryMirror)
			a.reg = newReg
			a.scanner.SetRegistry(newReg)
		}

		writeJSON(w, http.StatusOK, next)
	default:
		methodNotAllowed(w)
	}
}

func (a *api) scans(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListScans(20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"scans": list, "count": len(list)})
}

func (a *api) notifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	unread := r.URL.Query().Get("unread") == "1"
	cursorID := int64(0)
	if v := r.URL.Query().Get("cursor"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cursorID = n
		}
	}
	list, err := a.store.ListNotifications(unread, cursorID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"notifications": list, "count": len(list)})
}

func (a *api) notificationByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/notifications/"), "/")
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if len(parts) >= 2 && parts[1] == "read" && r.Method == http.MethodPost {
		if err := a.store.MarkRead(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (a *api) notificationsReadAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := a.store.MarkAllRead(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

// testDingTalk 校验钉钉 Webhook 连通性：发送一条测试消息并回传结果。
// 请求体可携带 webhook（便于保存前先试），缺省则使用当前已保存的设置。
func (a *api) testDingTalk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		Webhook string `json:"webhook"`
		Secret  string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	webhook := strings.TrimSpace(body.Webhook)
	secret := strings.TrimSpace(body.Secret)
	if webhook == "" {
		snap := a.settings.Snapshot()
		webhook = strings.TrimSpace(snap.DingTalkWebhook)
		secret = strings.TrimSpace(snap.DingTalkSecret)
	}
	if webhook == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "未配置钉钉 Webhook，请先在上方填写"})
		return
	}
	err := notification.SendDingTalk(webhook, secret,
		"DockMon 连通性测试",
		"### ✅ 连通性测试成功\n\n这是一条来自 DockMon 的测试消息，说明钉钉通知配置正确。\n\n**时间**: "+time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
