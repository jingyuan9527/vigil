package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dockmon/internal/api"
	"dockmon/internal/config"
	"dockmon/internal/docker"
	"dockmon/internal/registry"
	"dockmon/internal/scanner"
	"dockmon/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	var dcli *docker.Client
	if c, err := docker.NewClient(cfg.DockerHost); err == nil {
		dcli = c
		log.Printf("docker client configured (DOCKER_HOST=%q)", cfg.DockerHost)
	} else {
		log.Printf("docker client unavailable: %v (continuing with watch list only)", err)
	}

	// 运行时可变配置：以环境变量为初值，并以数据库中持久化的设置覆盖（页面可改）。
	live := config.NewLiveSettings(int(cfg.ScanInterval.Seconds()), cfg.RegistryInsecure, cfg.RegistryMirror, cfg.DisableDefault)
	if m, err := st.LoadSettingsMap(); err == nil && len(m) > 0 {
		live.Apply(config.SettingsFromMap(m))
	}

	// 注册表客户端以「生效设置」构造，确保页面修改后重启仍生效。
	liveSnap := live.Snapshot()
	reg := registry.NewClientWithMirror(liveSnap.RegistryInsecure, liveSnap.RegistryMirror)
	sc := scanner.New(cfg, st, dcli, reg, live)

	// 启动时立即扫描一次
	go sc.Run(context.Background())

	// 周期扫描：间隔可由页面动态调整，变化时自动重新计时。
	go func() {
		for {
			d := live.ScanIntervalDuration()
			if d <= 0 {
				<-live.Changed() // 周期扫描已禁用，等待重新启用
				continue
			}
			tm := time.NewTimer(d)
			ch := live.Changed()
			select {
			case <-tm.C:
				sc.Run(context.Background())
			case <-ch:
				tm.Stop()
			}
		}
	}()

	router := api.NewRouter(cfg.StaticDir, st, sc, reg, live)
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("dockmon listening on :%s", cfg.Port)
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
