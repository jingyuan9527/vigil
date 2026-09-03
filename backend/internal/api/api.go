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

	"dockmon/internal/config"
	"dockmon/internal/models"
	"dockmon/internal/registry"
	"dockmon/internal/scanner"
	"dockmon/internal/store"
)

type api struct {
	store     *store.Store
	scanner   *scanner.Scanner
	reg       *registry.Client
	staticDir string
	settings  *config.LiveSettings
}

// NewRouter 构造 HTTP 处理器：/api 走接口，其余路径回退到前端静态资源（SPA）。
func NewRouter(staticDir string, st *store.Store, sc *scanner.Scanner, reg *registry.Client, settings *config.LiveSettings) http.Handler {
	a := &api{store: st, scanner: sc, reg: reg, staticDir: staticDir, settings: settings}
	mux := http.NewServeMux()
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
	return a.withStatic(mux)
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
		go a.scanner.Run(context.Background())
		writeJSON(w, http.StatusAccepted, map[string]string{"result": "queued", "reference": ref})
	default:
		methodNotAllowed(w)
	}
}

func (a *api) handleImageByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/images/")
	id, err := strconv.ParseInt(strings.Trim(idStr, "/"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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
	go a.scanner.Run(context.Background())
	writeJSON(w, http.StatusAccepted, map[string]string{"result": "scan started"})
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
	list, err := a.store.ListNotifications(unread)
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
