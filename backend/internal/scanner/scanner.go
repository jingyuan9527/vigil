package scanner

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"dockmon/internal/config"
	"dockmon/internal/docker"
	"dockmon/internal/models"
	"dockmon/internal/notification"
	"dockmon/internal/registry"
	"dockmon/internal/store"
	"dockmon/internal/version"
)

// Scanner 负责采集镜像、按检测模式检测远端版本变化并生成通知。
type Scanner struct {
	cfg      *config.Config
	store    *store.Store
	docker   *docker.Client
	reg      *registry.Client
	settings *config.LiveSettings
	running  atomic.Bool // 扫描单飞锁：同一时刻只允许一个扫描执行
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

// process 处理单个镜像：拉取远端摘要、比较、落库，按生效检测模式生成通知。
//
// force=true 时为「强制扫描」：对当前存在版本差异的镜像补发 update 通知
// （含 Pin-Watch 锁定 tag 被覆盖的情况），统一按 (image, digest) 去重。
func (s *Scanner) process(ctx context.Context, j job, force bool) (bool, error) {
	ref := registry.ParseRef(j.reference)
	existing, _ := s.store.GetImageByRef(j.reference)

	// 忽略优先级最高：跳过全部检测（digest 校验与 tag 巡检都不执行），
	// 行数据保持冻结，不产生任何通知。仅用于采集的本地摘要也不落库。
	if existing != nil && existing.Ignored {
		return false, nil
	}

	prevRemote := ""
	mode := models.ModeAuto
	if existing != nil {
		prevRemote = existing.RemoteDigest
		mode = existing.Mode
	}
	effective := models.ResolveMode(mode, ref.Tag)

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
		Mode:         mode,
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

	found := false
	if rerr == nil && remote != "" {
		// 是否存在「版本差异」：有本地摘要时比对本地与远端；纯远端监控时比对上次已知远端。
		diff := (j.localDigest != "" && j.localDigest != remote) ||
			(j.localDigest == "" && prevRemote != "" && prevRemote != remote)

		// 同一个 digest 常规扫描只通知一次（防刷屏）
		notified, _ := s.store.HasDigestNotification(img.ID, remote)
		shouldNotify := false
		switch {
		case effective == models.ModeDigestOnly && changed && status == models.StatusUpdateAvailable:
			// 常规扫描：浮动/仅摘要模式下当前 tag 内容更新 → 强提醒（系统+钉钉）
			shouldNotify = true
		case force && diff:
			// 强制扫描：对所有当前存在版本差异的镜像补发通知，
			// 含 Pin-Watch 锁定 tag 被覆盖（常规扫描对 Pin-Watch 的 digest 变化不告警）。
			shouldNotify = true
		}
		// 去重闸门：常规扫描下同一 digest 只通知一次（防刷屏）。
		// 强制扫描（「全部重新扫描」）语义为重新广播：无论历史是否通知过、
		// 用户是否已读，只要当前存在版本差异就再次通知（系统 + 钉钉）。
		if shouldNotify && (!notified || force) {
			// old 取「用户当前持有的」摘要：本地摘要优先，纯远端监控回退到上次远端
			old := j.localDigest
			if old == "" {
				old = prevRemote
			}
			msg := fmt.Sprintf("镜像 %s 检测到新版本（远端摘要已变更）", j.reference)
			_ = s.store.CreateNotification(&models.Notification{
				ImageID:   img.ID,
				ImageName: ref.Repo,
				Reference: j.reference,
				OldDigest: old,
				NewDigest: remote,
				OldTag:    ref.Tag,
				NewTag:    ref.Tag,
				Type:      models.NotifUpdate,
				Message:   msg,
			})
			if webhook := s.settings.Snapshot().DingTalkWebhook; webhook != "" {
				secret := s.settings.Snapshot().DingTalkSecret
				go func(ref, oldD, newD string) {
					if err := notification.NotifyUpdate(webhook, secret, ref, oldD, newD); err != nil {
						log.Printf("dingtalk notify failed for %s: %v", ref, err)
					}
				}(j.reference, old, remote)
			}
			found = true
		}
	}

	// Pin-Watch 模式：巡检仓库 tag 列表，发现从未见过的新版本 tag → 弱提醒。
	if rerr == nil && effective == models.ModePinWatch {
		if s.inspectTags(ctx, img) {
			found = true
		}
	}

	return found, nil
}

// inspectTags 巡检仓库标签列表（仅 Pin-Watch 模式）：
//
//	首次巡检把全部标签记为已见并建立基线（不通知）；
//	之后出现从未见过的版本 tag → 每个 tag 生成一条 type=new-tag 通知（每个 tag 仅一次），
//	并把新 tag 记入已见清单；非版本标签也记为已见但不会触发通知。
func (s *Scanner) inspectTags(ctx context.Context, img *models.Image) bool {
	if img == nil || img.ID == 0 {
		return false
	}
	tags, err := s.reg.ListTags(ctx, registry.ImageRef{Registry: img.Registry, Repo: img.Name, Tag: img.Tag})
	if err != nil || len(tags) == 0 {
		return false
	}

	seen, serr := s.store.GetSeenTags(img.ID)
	if serr != nil {
		seen = map[string]bool{}
	}

	// 首次巡检：建立基线，全部记为已见，不打扰
	if len(seen) == 0 {
		_ = s.store.AddSeenTags(img.ID, tags)
		return false
	}

	// 收集本次新出现的 tag（含非版本 tag），并筛出版本 tag 用于通知
	unseen := make([]string, 0, len(tags))
	var freshVersionTags []string
	for _, t := range tags {
		if seen[t] {
			continue
		}
		unseen = append(unseen, t)
		if _, ok := version.ParseTag(t); ok {
			freshVersionTags = append(freshVersionTags, t)
		}
	}
	if len(unseen) > 0 {
		_ = s.store.AddSeenTags(img.ID, unseen)
	}
	if len(freshVersionTags) == 0 {
		return false
	}

	webhook := s.settings.Snapshot().DingTalkWebhook
	secret := s.settings.Snapshot().DingTalkSecret
	for _, nt := range freshVersionTags {
		msg := fmt.Sprintf("仓库 %s 发布新版本标签 %s（当前锁定 %s）", img.Name, nt, img.Tag)
		_ = s.store.CreateNotification(&models.Notification{
			ImageID:   img.ID,
			ImageName: img.Name,
			Reference: img.Reference,
			OldTag:    img.Tag,
			NewTag:    nt,
			Type:      models.NotifNewTag,
			Message:   msg,
		})
		if webhook != "" {
			go func(ref, cur, newTag string) {
				if err := notification.NotifyNewTag(webhook, secret, ref, cur, newTag); err != nil {
					log.Printf("dingtalk notify-newtag failed for %s: %v", ref, err)
				}
			}(img.Reference, img.Tag, nt)
		}
	}
	return true
}

// computeStatus 依据本地摘要、远端摘要与上次远端摘要，判定镜像状态。
//
// changed 返回值表示「远端摘要发生了真实变更」：当 prevRemote 非空且与 remote 不等时为 true。
// 关键语义：
//   - 首扫（prevRemote == ""）时 changed=false，即使 local!=remote，也不会触发 last_update 更新，
//     从而避免首扫将「首次记录远端基线」误判为「有新版本」而刷通知。
//   - Digest-Only 常规告警的完整条件是 changed && status==update-available（见 process 通知闸门）：
//     local!=remote 且远端相对上次变化时同时满足 → 触发 update 通知并更新 last_update。
//     local==remote（如浮动 tag 重新推送为同一内容）时 status=up-to-date → 不告警。
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

// IsRunning 报告是否已有扫描正在执行（供 API 层提前返回友好提示）。
func (s *Scanner) IsRunning() bool { return s.running.Load() }

// Run 执行一次完整扫描。force=true 时为「强制扫描」：对当前所有存在版本差异的
// 镜像补发 update 通知（按 digest 去重），覆盖 Pin-Watch 锁定 tag 被覆盖等常规不告警的情况。
//
// 返回是否真正启动：定时器、手动扫描、添加镜像可能并发触发，CAS 保证单飞，
// 重复触发直接跳过（返回 false）。
func (s *Scanner) Run(ctx context.Context, force bool) bool {
	if !s.running.CompareAndSwap(false, true) {
		log.Printf("scan requested while already running, skipping")
		return false
	}
	defer s.running.Store(false)

	scanID, err := s.store.CreateScan()
	if err != nil {
		return false
	}
	jobs := s.collectJobs(ctx)
	if len(jobs) == 0 {
		_ = s.store.FinishScan(scanID, 0, 0, "done", "")
		return true
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
			uf, perr := s.process(jctx, j, force)
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
	return true
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
