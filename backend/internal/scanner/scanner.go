package scanner

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"dockmon/internal/config"
	"dockmon/internal/docker"
	"dockmon/internal/models"
	"dockmon/internal/notification"
	"dockmon/internal/registry"
	"dockmon/internal/store"
)

// Scanner 负责采集镜像、检测远端版本变化并生成通知。
type Scanner struct {
	cfg     *config.Config
	store   *store.Store
	docker  *docker.Client
	reg     *registry.Client
	settings *config.LiveSettings
}

func New(cfg *config.Config, st *store.Store, dcli *docker.Client, reg *registry.Client, settings *config.LiveSettings) *Scanner {
	return &Scanner{cfg: cfg, store: st, docker: dcli, reg: reg, settings: settings}
}

// SetRegistry 在运行时（页面修改注册表设置后）热替换注册表客户端。
func (s *Scanner) SetRegistry(reg *registry.Client) { s.reg = reg }

type job struct {
	reference   string
	localDigest string
	source      string
}

func timePtr(t time.Time) *time.Time { return &t }

// collectJobs 汇总所有需要监控的镜像引用：
// Docker 守护进程中的镜像 + 数据库已有 manual 项 + 配置 WATCH + 默认演示列表。
func (s *Scanner) collectJobs(ctx context.Context) []job {
	seen := map[string]bool{}
	var jobs []job

	add := func(ref, localDigest, source string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		jobs = append(jobs, job{reference: ref, localDigest: localDigest, source: source})
	}

	if s.docker != nil {
		if err := s.docker.Ping(ctx); err == nil {
			if refs, derr := s.docker.ListImageRefs(ctx); derr == nil {
				for ref, dig := range refs {
					add(ref, dig, "docker")
				}
			}
		}
	}

	if existing, err := s.store.ListImages(""); err == nil {
		for _, img := range existing {
			if img.Source == "manual" {
				add(img.Reference, "", "manual")
			}
		}
	}

	for _, ref := range s.cfg.Watch {
		add(ref, "", "manual")
	}
	if !s.settings.Snapshot().DisableDefaultWatch {
		for _, ref := range s.cfg.DefaultWatch {
			add(ref, "", "manual")
		}
	}
	return jobs
}

// process 处理单个镜像：拉取远端摘要、比较、落库、必要时生成通知。
func (s *Scanner) process(ctx context.Context, j job) (bool, error) {
	ref := registry.ParseRef(j.reference)
	existing, _ := s.store.GetImageByRef(j.reference)

	prevRemote := ""
	if existing != nil {
		prevRemote = existing.RemoteDigest
	}

	remote, rerr := s.reg.ManifestDigest(ctx, ref)

	now := time.Now()
	img := &models.Image{
		Name:         ref.Repo,
		Reference:    j.reference,
		Registry:     ref.Registry,
		Tag:          ref.Tag,
		Source:       j.source,
		LocalDigest:  j.localDigest,
		RemoteDigest: remote,
		Status:       models.StatusUnknown,
		LastCheck:    &now,
		CreatedAt:    now,
	}
	if existing != nil {
		img.ID = existing.ID
		img.CreatedAt = existing.CreatedAt
	}

	status, changed := computeStatus(j.localDigest, remote, prevRemote)
	img.Status = status
	if rerr != nil {
		img.Status = models.StatusUnknown
		img.Error = rerr.Error()
		img.RemoteDigest = prevRemote // 保留上次已知值
	} else {
		if changed || prevRemote == "" {
			img.LastUpdate = &now
		} else if existing != nil {
			img.LastUpdate = existing.LastUpdate
		}
	}

	if err := s.store.UpsertImage(img); err != nil {
		return false, err
	}
	// 重新读取以拿到（新插入行的）真实自增 ID，保证版本快照与通知关联正确
	stored, _ := s.store.GetImageByRef(j.reference)
	if stored == nil {
		return false, fmt.Errorf("image row missing after upsert: %s", j.reference)
	}
	img.ID = stored.ID

	// 版本快照：远端摘要首次记录或发生变化时
	if rerr == nil && remote != "" && remote != prevRemote {
		_ = s.store.AddVersion(img.ID, remote, ref.Tag)
	}

	// 仅在真正发生变化（非首次）时通知，避免首扫噪声
	if rerr == nil && changed {
		msg := fmt.Sprintf("镜像 %s 检测到新版本（远端摘要已变更）", j.reference)
		_ = s.store.CreateNotification(&models.Notification{
			ImageID:    img.ID,
			ImageName:  ref.Repo,
			Reference:  j.reference,
			OldDigest:  prevRemote,
			NewDigest:  remote,
			OldTag:     ref.Tag,
			NewTag:     ref.Tag,
			Message:    msg,
		})

		// 钉钉通知
		if webhook := s.settings.Snapshot().DingTalkWebhook; webhook != "" {
			go func() {
				if err := notification.NotifyUpdate(webhook, j.reference, prevRemote, remote); err != nil {
					log.Printf("dingtalk notify failed for %s: %v", j.reference, err)
				}
			}()
		}

		return true, nil
	}
	return false, nil
}

// computeStatus 依据本地摘要、远端摘要与上次远端摘要，判定镜像状态。
// 返回 (状态, 是否发生真正变化)。changed 仅当 prevRemote 非空且与 remote 不同，
// 用于区分「首次建立基线」与「远端确实更新了」，避免首扫误报通知。
func computeStatus(local, remote, prevRemote string) (models.ImageStatus, bool) {
	changed := prevRemote != "" && remote != "" && prevRemote != remote
	var status models.ImageStatus
	switch {
	case remote == "":
		// 注册表不可达或未找到该摘要
		status = models.StatusUnknown
	case local != "":
		// 有本地镜像：直接比对本地与远端摘要
		if local == remote {
			status = models.StatusUpToDate
		} else {
			status = models.StatusUpdateAvailable
		}
	default:
		// 仅远端监控（无本地镜像）：以首次看到的远端为基线
		if changed {
			status = models.StatusUpdateAvailable
		} else {
			status = models.StatusUpToDate
		}
	}
	return status, changed
}

// Run 执行一次完整扫描。
func (s *Scanner) Run(ctx context.Context) {
	scanID, err := s.store.CreateScan()
	if err != nil {
		return
	}
	jobs := s.collectJobs(ctx)
	if len(jobs) == 0 {
		_ = s.store.FinishScan(scanID, 0, 0, "done", "")
		return
	}

	var mu sync.Mutex
	var checked, updates int

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			jctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			uf, perr := s.process(jctx, j)
			if perr != nil {
				mu.Lock()
				checked++
				mu.Unlock()
				return
			}
			mu.Lock()
			checked++
			if uf {
				updates++
			}
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	_ = s.store.FinishScan(scanID, checked, updates, "done", "")
}
