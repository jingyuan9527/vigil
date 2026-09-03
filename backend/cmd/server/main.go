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

	reg := registry.NewClientWithMirror(cfg.RegistryInsecure, cfg.RegistryMirror)
	sc := scanner.New(cfg, st, dcli, reg)

	// 启动时立即扫描一次
	go sc.Run(context.Background())

	// 周期扫描
	if cfg.ScanInterval > 0 {
		go func() {
			ticker := time.NewTicker(cfg.ScanInterval)
			defer ticker.Stop()
			for range ticker.C {
				sc.Run(context.Background())
			}
		}()
	}

	router := api.NewRouter(cfg.StaticDir, st, sc, reg)
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
