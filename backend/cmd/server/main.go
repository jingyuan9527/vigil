package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vigil/internal/api"
	"vigil/internal/auth"
	"vigil/internal/config"
	"vigil/internal/docker"
	"vigil/internal/registry"
	"vigil/internal/scanner"
	"vigil/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	// JWT 密钥：优先使用环境变量，否则从数据库加载或自动生成。
	var jwtSecret []byte
	if cfg.JWTSecret != "" {
		jwtSecret = []byte(cfg.JWTSecret)
	} else if dbSecret := st.GetJWTSecret(); dbSecret != "" {
		jwtSecret = []byte(dbSecret)
	} else {
		jwtSecret = auth.GenerateSecret()
		_ = st.SaveJWTSecret(string(jwtSecret))
	}

	// 若环境变量设置了管理员账号且数据库中尚无管理员，自动创建。
	if cfg.AdminUser != "" && cfg.AdminPassword != "" {
		has, err := st.HasAdmin()
		if err != nil {
			log.Fatalf("check admin existence: %v", err) // 启动期 DB 不可用必须终止，避免带病运行
		}
		if !has {
			hash := auth.HashPassword(cfg.AdminPassword)
			if err := st.SetAdmin(cfg.AdminUser, hash); err != nil {
				log.Printf("auto-create admin failed: %v", err)
			} else {
				log.Printf("admin account created from env vars (user=%q)", cfg.AdminUser)
			}
		}
	}

	var dcli *docker.Client
	if c, err := docker.NewClient(cfg.DockerHost); err == nil {
		dcli = c
		log.Printf("docker client configured (DOCKER_HOST=%q)", cfg.DockerHost)
	} else {
		log.Printf("docker client unavailable: %v (continuing with watch list only)", err)
	}

	// 运行时可变配置：以环境变量为初值，并以数据库中持久化的设置覆盖（页面可改）。
	live := config.NewLiveSettings(int(cfg.ScanInterval.Seconds()), cfg.RegistryInsecure, cfg.RegistryMirror, cfg.DisableDefault, strings.TrimSpace(cfg.DingTalkWebhook), strings.TrimSpace(cfg.DingTalkSecret))
	if m, err := st.LoadSettingsMap(); err == nil && len(m) > 0 {
		live.Apply(config.SettingsFromMap(m))
	}

	// 注册表客户端以「生效设置」构造，确保页面修改后重启仍生效。
	liveSnap := live.Snapshot()
	reg := registry.NewClientWithMirror(liveSnap.RegistryInsecure, liveSnap.RegistryMirror)
	sc := scanner.New(cfg, st, dcli, reg, live)

	// 启动时立即扫描一次
	go sc.Run(context.Background(), false)

	// 周期/每日定时扫描：调度参数可由页面动态调整，变化时自动重新计时。
	go func() {
		for {
			// 先取变更通道再读快照：若变更落在快照后、取通道前，会错过广播死等一整轮
			ch := live.Changed()
			snap := live.Snapshot()
			var tm *time.Timer
			if snap.ScanMode == config.ScanModeDaily {
				// 每天定时：算出下一个触发点（服务器本地时区）
				next, ok := config.NextDailyRun(snap.ScanDailyTime, time.Now())
				if !ok {
					log.Printf("invalid scan_daily_time %q, daily scan suspended until settings change", snap.ScanDailyTime)
					<-ch
					continue
				}
				log.Printf("next daily scan scheduled at %s", next.Format("2006-01-02 15:04"))
				tm = time.NewTimer(time.Until(next))
			} else {
				d := live.ScanIntervalDuration()
				if d <= 0 {
					<-ch // 自动扫描已禁用，等待重新启用
					continue
				}
				tm = time.NewTimer(d)
			}
			select {
			case <-tm.C:
				sc.Run(context.Background(), false)
				// 扫描结束后重新读取调度参数（可能已被页面修改）
			case <-ch:
				tm.Stop()
			}
		}
	}()

	router := api.NewRouter(cfg.StaticDir, st, sc, reg, live, jwtSecret)
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("vigil listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// 优雅退出
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
