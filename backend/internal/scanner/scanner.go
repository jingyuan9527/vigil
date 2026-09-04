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
	"dockmon/internal/version"
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

	// 强更新：同一 tag 的远端 digest 发生变化（非首次）时通知，避免首扫噪声。
	// 被用户忽略的镜像（Ignored）仍会扫描、状态照常更新，但不再产生系统/钉钉通知。
	found := false
	if rerr == nil && changed {
		ignored := existing != nil && existing.Ignored
		if !ignored {
			msg := fmt.Sprintf("镜像 %s 检测到新版本（远端摘要已变更）", j.reference)
			_ = s.store.CreateNotification(&models.Notification{
				ImageID:    img.ID,
				ImageName:  ref.Repo,
				Reference:  j.reference,
				OldDigest:  prevRemote,
				NewDigest:  remote,
				OldTag:     ref.Tag,
				NewTag:     ref.Tag,
				Type:       models.NotifUpdate,
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
			found = true
		}
	}

	// 弱提醒：仓库出现比当前固定 tag 更高的独立版本（如 mysql:8.4.7 → 26）。
	// 仅对非滚动 tag 探测，且受 ignored 抑制、按目标去重，避免刷屏。
	if s.maybeNotifyNewerTag(ctx, img, existing) {
		found = true
	}

	return found, nil
}

// maybeNotifyNewerTag 探测仓库是否出现比当前固定 tag 更高的新版本。
// 是则生成一条 type=new-tag 的弱提醒（系统内，不发钉钉），并记录去重目标。
// 返回是否产生了提醒。
func (s *Scanner) maybeNotifyNewerTag(ctx context.Context, img *models.Image, existing *models.Image) bool {
	if img == nil || img.ID == 0 {
		return false
	}
	// 仅对固定语义版本 tag 探测；latest/lts 等滚动 tag 交给 digest 强更新。
	if version.IsRollingTag(img.Tag) {
		return false
	}
	// 忽略的镜像不打扰
	if existing != nil && existing.Ignored {
		return false
	}

	ref := registry.ImageRef{Registry: img.Registry, Repo: img.Name, Tag: img.Tag}
	tags, err := s.reg.ListTags(ctx, ref)
	if err != nil || len(tags) == 0 {
		return false
	}
	newer := version.NewerAvailable(img.Tag, tags)
	if newer == "" {
		return false
	}
	// 去重：同一目标（或更低目标）已弱提醒过则不再重复
	if existing != nil && existing.NotifiedNewTag == newer {
		return false
	}

	msg := fmt.Sprintf("镜像 %s 出现更新的独立版本 %s（当前 %s）", img.Name, newer, img.Tag)
	_ = s.store.CreateNotification(&models.Notification{
		ImageID:    img.ID,
		ImageName:  img.Name,
		Reference:  img.Reference,
		OldTag:     img.Tag,
		NewTag:     newer,
		Type:       models.NotifNewTag,
		Message:    msg,
	})
	_ = s.store.SetNotifiedNewTag(img.ID, newer)
	return true
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
	// 清理本机已删除镜像的库内残留：将 source=docker 且本轮不再存在的行标记为 stale（缺失）。
	s.pruneRemovedDockerImages(ctx)
	_ = s.store.FinishScan(scanID, checked, updates, "done", "")
}

// pruneRemovedDockerImages 对本机已不存在的 docker 镜像做陈旧标记。
// 仅当 Docker 守护进程可达时执行；manual / watch 来源不受影响。
func (s *Scanner) pruneRemovedDockerImages(ctx context.Context) {
	if s.docker == nil {
		return
	}
	if err := s.docker.Ping(ctx); err != nil {
		return
	}
	live, err := s.docker.ListImageRefs(ctx)
	if err != nil {
		return
	}
	liveRefs := make(map[string]bool, len(live))
	for ref := range live {
		liveRefs[ref] = true
	}
	if n, err := s.store.MarkDockerImagesMissing(liveRefs); err == nil && n > 0 {
		log.Printf("marked %d removed docker image(s) as stale", n)
	}
}
