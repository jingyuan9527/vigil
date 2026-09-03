package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 聚合所有运行时配置，全部来自环境变量，便于容器化部署。
type Config struct {
	Port             string
	DBPath           string
	StaticDir        string
	DockerHost       string
	Watch            []string
	DefaultWatch     []string
	ScanInterval     time.Duration
	RegistryInsecure bool
	RegistryMirror   string
	DisableDefault   bool
	AdminUser        string
	AdminPassword    string
	JWTSecret        string
	DingTalkWebhook  string
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	return def
}

// Load 读取环境变量并构造配置。
func Load() *Config {
	c := &Config{
		Port:             getEnv("PORT", "54321"),
		DBPath:           getEnv("DB_PATH", "/data/monitor.db"),
		StaticDir:        getEnv("STATIC_DIR", "./static"),
		DockerHost:       getEnv("DOCKER_HOST", ""),
		ScanInterval:     time.Duration(getEnvInt("SCAN_INTERVAL", 3600)) * time.Second,
		RegistryInsecure: getEnvBool("REGISTRY_INSECURE", false),
		RegistryMirror:   getEnv("REGISTRY_MIRROR", ""),
		DisableDefault:   getEnvBool("DISABLE_DEFAULT_WATCH", false),
		AdminUser:        getEnv("ADMIN_USER", ""),
		AdminPassword:    getEnv("ADMIN_PASSWORD", ""),
		JWTSecret:        getEnv("JWT_SECRET", ""),
		DingTalkWebhook:  getEnv("DINGTALK_WEBHOOK", ""),
	}
	if w := getEnv("WATCH", ""); w != "" {
		for _, p := range strings.Split(w, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				c.Watch = append(c.Watch, p)
			}
		}
	}
	// 内置一组常用镜像作为演示 watch 列表，使开箱即有数据；
	// 设置 DISABLE_DEFAULT_WATCH=1 或提供 WATCH 可覆盖。
	c.DefaultWatch = []string{
		"nginx:latest",
		"redis:latest",
		"postgres:latest",
		"node:lts",
		"alpine:latest",
	}
	return c
}
