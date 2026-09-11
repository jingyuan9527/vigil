package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"vigil/internal/auth"
	"vigil/internal/config"
	"vigil/internal/models"
	"vigil/internal/notification"
	"vigil/internal/registry"
	"vigil/internal/scanner"
	"vigil/internal/store"
)

type api struct {
	store        *store.Store
	scanner      *scanner.Scanner
	reg          atomic.Pointer[registry.Client] // 原子指针：设置页热替换时 handler 并发读取
	staticDir    string
	settings     *config.LiveSettings
	jwtSecret    []byte
	loginLimiter *auth.LoginLimiter
}

// NewRouter 构造 HTTP 处理器：/api 走接口，其余路径回退到前端静态资源（SPA）。
func NewRouter(staticDir string, st *store.Store, sc *scanner.Scanner, reg *registry.Client, settings *config.LiveSettings, jwtSecret []byte) http.Handler {
	a := &api{store: st, scanner: sc, staticDir: staticDir, settings: settings, jwtSecret: jwtSecret, loginLimiter: auth.NewLoginLimiter()}
	a.reg.Store(reg)
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
	mux.HandleFunc("/api/notifications/clear-read", a.notificationsClearRead)
	mux.HandleFunc("/api/notifications/", a.notificationByID)
	mux.HandleFunc("/api/dingtalk/test", a.testDingTalk)
	mux.HandleFunc("/api/channels", a.handleChannels)
	mux.HandleFunc("/api/channels/", a.handleChannelByID)

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

// serverError 统一服务端错误响应：对外只给固定文案，内部细节（SQL、路径等）
// 全部只进日志，避免 err.Error() 泄露给客户端。
func serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("api %s %s: %v", r.Method, r.URL.Path, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误，请查看服务日志"})
}

// storeUnavailable 数据库故障下的统一响应：语义为暂时不可用而非内部错误，
// 提示调用方稍后重试（如 setup 窗口期间拒绝创建管理员）。
func storeUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("api %s %s: store unavailable: %v", r.Method, r.URL.Path, err)
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "数据库暂不可用，请稍后再试"})
}

func (a *api) registry() *registry.Client { return a.reg.Load() }

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
	has, err := a.store.HasAdmin()
	if err != nil {
		storeUnavailable(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setup_required": !has,
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
	has, err := a.store.HasAdmin()
	if err != nil {
		// 必须先失败：DB 故障时吞错会让 setup 重新开放，攻击者可接管管理员
		storeUnavailable(w, r, err)
		return
	}
	if has {
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
		serverError(w, r, err)
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
	has, err := a.store.HasAdmin()
	if err != nil {
		storeUnavailable(w, r, err)
		return
	}
	if !has {
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
	storedUser, storedHash, err := a.store.GetAdmin()
	if err != nil {
		storeUnavailable(w, r, err)
		return
	}
	if body.Username != storedUser || !auth.CheckPassword(storedHash, body.Password) {
		a.loginLimiter.RecordFailure(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
		return
	}
	// 旧格式哈希（单轮 SHA-256+salt）登录成功即升级为 bcrypt，之后自动淘汰旧格式
	if auth.NeedsRehash(storedHash) {
		if h := auth.HashPassword(body.Password); a.store.SetAdmin(storedUser, h) == nil {
			log.Printf("admin password rehashed to bcrypt on login")
		} else {
			log.Printf("admin password rehash persist failed (login continues with legacy hash)")
		}
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
		serverError(w, r, err)
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
			serverError(w, r, err)
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
				serverError(w, r, err)
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
			serverError(w, r, err)
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
			serverError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"mode": body.Mode})
		return
	}

	img, err := a.store.GetImage(id)
	if err != nil {
		serverError(w, r, err)
		return
	}
	if img == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		versions, err := a.store.ListVersions(id)
		if err != nil {
			// 时间线查询失败只降级（详情主体仍可用），但必须留痕
			log.Printf("api %s %s: list versions: %v", r.Method, r.URL.Path, err)
		}
		detail := models.ImageDetail{Image: *img, Versions: versions}
		if img.Registry != "" {
			ref := registry.ImageRef{Registry: img.Registry, Repo: img.Name, Tag: img.Tag}
			if tags, terr := a.registry().ListTags(context.Background(), ref); terr == nil {
				detail.Tags = tags
			}
		}
		writeJSON(w, http.StatusOK, detail)
	case http.MethodDelete:
		if err := a.store.DeleteImage(id); err != nil {
			serverError(w, r, err)
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
		// 扫描调度模式校验：interval 缺省兼容旧客户端；daily 必须带合法 HH:MM
		if body.ScanMode == "" {
			body.ScanMode = config.ScanModeInterval
		}
		if body.ScanMode != config.ScanModeInterval && body.ScanMode != config.ScanModeDaily {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scan_mode 仅支持 interval / daily"})
			return
		}
		body.ScanDailyTime = strings.TrimSpace(body.ScanDailyTime)
		if body.ScanMode == config.ScanModeDaily {
			if _, _, ok := config.ParseDailyTime(body.ScanDailyTime); !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scan_daily_time 格式应为 HH:MM（如 03:30）"})
				return
			}
		}
		body.RegistryMirror = strings.TrimSpace(body.RegistryMirror)

		prev := a.settings.Snapshot()
		a.settings.Apply(body)
		next := a.settings.Snapshot()

		if err := a.store.SaveSettingsMap(config.SettingsToMap(next)); err != nil {
			serverError(w, r, err)
			return
		}

		// 关闭内置演示监控列表：只要保存后处于关闭状态就立即清理演示列表
		// 已产生的镜像行，避免「功能已关闭但 nginx/redis/postgres 等演示镜像
		// 仍出现在列表中」。覆盖两种情形：
		//   - 本次保存恰好关闭（旧值 true→false 跳变）；
		//   - 旧版本/旧代码遗留的 disable=true（升级后再次保存同样生效）。
		// 之后扫描不再采集它们（scan.collectJobs 已按 source 识别），
		// 重新开启后由下次扫描自动重建。清理只命中演示清单内的行
		// （source=default 或 legacy manual 纯远端 watch，见 store.DeleteDefaultWatchImages），
		// 本地 docker 行、带本地摘要的手动行、清单外引用不受影响。
		if next.DisableDefaultWatch {
			if n, err := a.store.DeleteDefaultWatchImages(a.scanner.DefaultWatchRefs()); err != nil {
				log.Printf("cleanup default watch images after disable failed: %v", err)
			} else if n > 0 {
				log.Printf("removed %d default-watch image row(s) after disabling demo list", n)
			}
		}

		// 注册表相关字段变更：热重建客户端并同步 scanner 与接口自身（原子替换，无锁竞争）
		if prev.RegistryInsecure != next.RegistryInsecure || prev.RegistryMirror != next.RegistryMirror {
			newReg := registry.NewClientWithMirror(next.RegistryInsecure, next.RegistryMirror)
			a.reg.Store(newReg)
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
		serverError(w, r, err)
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
		serverError(w, r, err)
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
			serverError(w, r, err)
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
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

// notificationsClearRead 清空全部已读通知（手动归档），未读不受影响。
// 去重基线独立存储，清理不影响常规扫描的防刷屏。
func (a *api) notificationsClearRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	n, err := a.store.ClearReadNotifications()
	if err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": "ok", "deleted": n})
}

// testDingTalk 校验钉钉连通性（向后兼容保留的旧端点）：
// 请求体可携带 webhook/secret（便于保存前先试），缺省则使用渠道表中首条启用的
// dingtalk 渠道配置。渠道表无钉钉渠道时返回提示，引导去「通知渠道」配置。
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
		chans, err := a.store.ListChannels(true)
		if err != nil {
			serverError(w, r, err)
			return
		}
		for _, c := range chans {
			if c.Kind != "dingtalk" {
				continue
			}
			var cfg struct {
				Webhook string `json:"webhook"`
				Secret  string `json:"secret"`
			}
			if json.Unmarshal([]byte(c.Config), &cfg) == nil {
				webhook = strings.TrimSpace(cfg.Webhook)
				secret = strings.TrimSpace(cfg.Secret)
			}
			break
		}
	}
	if webhook == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "未配置钉钉通知渠道，请先在「设置 → 通知渠道」中添加或直接填写 webhook"})
		return
	}
	test := notification.TestMessage()
	err := notification.SendDingTalk(context.Background(), webhook, secret, "Vigil 连通性测试", test.Markdown)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// ---- 通知渠道 ----

// channelDTO 渠道对外形态：config 以键值对象给出（非 JSON 原文），前端无需自行解析。
type channelDTO struct {
	ID        int64                  `json:"id"`
	Kind      string                 `json:"kind"`
	Name      string                 `json:"name"`
	Enabled   bool                   `json:"enabled"`
	Config    map[string]interface{} `json:"config"`
	CreatedAt time.Time              `json:"created_at"`
}

// channelRequest 创建/更新渠道的请求体。enabled 用指针以区分「未提供 vs 显式 false」。
type channelRequest struct {
	Kind    string                 `json:"kind"`
	Name    string                 `json:"name"`
	Enabled *bool                  `json:"enabled"`
	Config  map[string]interface{} `json:"config"`
}

func channelToDTO(c *store.Channel) channelDTO {
	dto := channelDTO{ID: c.ID, Kind: c.Kind, Name: c.Name, Enabled: c.Enabled, CreatedAt: c.CreatedAt}
	m := map[string]interface{}{}
	_ = json.Unmarshal([]byte(c.Config), &m)
	dto.Config = m
	return dto
}

// marshalConfig 把渠道配置对象序列化为 JSON 字符串（nil 归一为空对象）。
func marshalConfig(cfg map[string]interface{}) (string, error) {
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// handleChannels 提供通知渠道列表（GET）与创建（POST）。
func (a *api) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		chans, err := a.store.ListChannels(false)
		if err != nil {
			serverError(w, r, err)
			return
		}
		out := make([]channelDTO, 0, len(chans))
		for i := range chans {
			out = append(out, channelToDTO(&chans[i]))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"channels": out, "kinds": notification.Kinds()})
	case http.MethodPost:
		var req channelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		kind := strings.TrimSpace(req.Kind)
		if !slices.Contains(notification.Kinds(), kind) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持的渠道类型 " + kind})
			return
		}
		cfg, err := marshalConfig(req.Config)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "渠道配置不是合法对象"})
			return
		}
		if err := notification.Validate(kind, cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		created, err := a.store.CreateChannel(kind, strings.TrimSpace(req.Name), cfg, enabled)
		if err != nil {
			serverError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, channelToDTO(created))
	default:
		methodNotAllowed(w)
	}
}

// handleChannelByID 提供单条渠道的读取（GET）、更新（PUT）、删除（DELETE）
// 与连通性测试（POST /test）。kind 创建后不可变，更新请求携带不同 kind 会被拒绝。
func (a *api) handleChannelByID(w http.ResponseWriter, r *http.Request) {
	id, sub, ok := parseChannelPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	ch, err := a.store.GetChannel(id)
	if err != nil {
		serverError(w, r, err)
		return
	}
	if ch == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "渠道不存在"})
		return
	}

	if sub == "test" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		testMsg := notification.TestMessage()
		if err := notification.Send(context.Background(), ch.Kind, ch.Config, testMsg); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, channelToDTO(ch))
	case http.MethodPut:
		var req channelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if strings.TrimSpace(req.Kind) != "" && req.Kind != ch.Kind {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "渠道类型创建后不可变更"})
			return
		}
		cfg := ch.Config
		if req.Config != nil {
			cfg, err = marshalConfig(req.Config)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "渠道配置不是合法对象"})
				return
			}
			if err := notification.Validate(ch.Kind, cfg); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		enabled := ch.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		if err := a.store.UpdateChannel(id, strings.TrimSpace(req.Name), cfg, enabled); err != nil {
			if err == store.ErrChannelNotFound {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "渠道不存在"})
				return
			}
			serverError(w, r, err)
			return
		}
		updated, _ := a.store.GetChannel(id)
		writeJSON(w, http.StatusOK, channelToDTO(updated))
	case http.MethodDelete:
		// 删除为危险操作：接口层要求前端显式确认（confirm），此处不再二次拦截。
		if err := a.store.DeleteChannel(id); err != nil {
			if err == store.ErrChannelNotFound {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "渠道不存在"})
				return
			}
			serverError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
	default:
		methodNotAllowed(w)
	}
}

// parseChannelPath 解析 `/api/channels/{id}[/{sub}]`，失败返回 ok=false。
func parseChannelPath(p string) (int64, string, bool) {
	parts := strings.Split(strings.TrimPrefix(p, "/api/channels/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	return id, sub, true
}
