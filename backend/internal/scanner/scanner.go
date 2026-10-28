package scanner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vigil/internal/config"
	"vigil/internal/docker"
	"vigil/internal/models"
	"vigil/internal/notification"
	"vigil/internal/registry"
	"vigil/internal/store"
	"vigil/internal/version"
)

// Scanner 负责采集镜像、按检测模式检测远端版本变化并生成通知。
type Scanner struct {
	cfg      *config.Config
	store    *store.Store
	docker   *docker.Client
	reg      atomic.Pointer[registry.Client] // 原子指针：设置页热替换时扫描 goroutine 并发读取
	settings *config.LiveSettings
	running  atomic.Bool // 扫描单飞锁：同一时刻只允许一个扫描执行
}

func New(cfg *config.Config, st *store.Store, dcli *docker.Client, reg *registry.Client, settings *config.LiveSettings) *Scanner {
	s := &Scanner{cfg: cfg, store: st, docker: dcli, settings: settings}
	s.reg.Store(reg)
	return s
}

// SetRegistry 在运行时（页面修改注册表设置后）热替换注册表客户端。
func (s *Scanner) SetRegistry(reg *registry.Client) { s.reg.Store(reg) }

// registry 返回当前生效的注册表客户端。
func (s *Scanner) registry() *registry.Client { return s.reg.Load() }

// DefaultWatchRefs 返回内置演示监控列表的引用，
// 供设置页关闭演示列表时清理已产生的演示镜像行。
func (s *Scanner) DefaultWatchRefs() []string { return s.cfg.DefaultWatch }

// notifyUpdate 异步扇出「有新版本」通知到所有启用渠道；未配置渠道时静默跳过。
// 聚合记录已同步落库，外部推送失败只记日志、不回滚。
func (s *Scanner) notifyUpdate(ref string, msg notification.Message) {
	channels, err := s.store.ListChannels(true)
	if err != nil || len(channels) == 0 {
		return
	}
	s.fanout(channels, msg, "update", ref)
}

// fanout 把消息并行发送到全部启用渠道，逐渠道上报失败（不互相阻塞、不打断扫描）。
func (s *Scanner) fanout(channels []store.Channel, msg notification.Message, notifType, ref string) {
	for _, c := range channels {
		go func(c store.Channel) {
			if err := notification.Send(context.Background(), c.Kind, c.Config, msg); err != nil {
				log.Printf("notify %s(%s) %s failed for %s: %v", c.Kind, c.Name, notifType, ref, err)
			}
		}(c)
	}
}

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
			// source=default 标识「演示监控列表」产生的行，与用户手动添加
			// （manual）区分开：关闭演示列表时据此精确清理历史数据。
			add(ref, "", "default")
		}
	}
	return jobs
}

// process 处理单个镜像：拉取远端摘要、比较、落库，并提交聚合更新记录。
//
// 推送判定由 store.MergeImageUpdate 统一负责（同一镜像同一状态只推一次），
// 因此定时扫描与「全部重新扫描」都不会对相同版本重复提醒；强制扫描只重新检测、
// 刷新聚合记录，不改变去重语义。
func (s *Scanner) process(ctx context.Context, j job) (bool, error) {
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

	remote, rerr := s.registry().ManifestDigest(ctx, ref)

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
		// 差异基线（old 即「用户当前持有的」摘要）：
		//   - 有本地镜像：本地摘要就是持有版本，差异=本地≠远端（持久，直到拉取）；
		//   - 纯远端监控：上轮远端 prevRemote（随扫描滚动覆盖，仅用于当轮展示）。
		baseline := j.localDigest
		if baseline == "" && prevRemote != "" {
			baseline = prevRemote
		}

		digestNotifiable := effective == models.ModeDigestOnly
		// 版本信息按需获取：Pin-Watch 每轮巡检 tags/list（本来就要拉取）；
		// Digest-Only 仅在摘要发生变化（可能推送）时补查一次，避免给每个镜像
		// 每轮都增加一次 tags/list 请求。拉取失败时 VersionKnown=false，
		// 存储层沿用上轮版本，避免接口抖动被误判成版本变化。
		needVersionInfo := effective == models.ModePinWatch || (digestNotifiable && changed)
		var (
			latestVersion string
			candidates    []string
			versionKnown  bool
		)
		if needVersionInfo {
			tags, terr := s.listTags(ctx, ref)
			switch {
			case terr != nil:
				log.Printf("tags/list for %s failed, keeping previous version info: %v", j.reference, terr)
			case len(tags) > 0:
				versionKnown = true
				// 用 BestTag 而非 LatestTag：同版本别名（0.31 / 0.31.0）比较相等时
				// 选展示优先级更高（段数更全）的那个，页面/通知里的版本号更明确。
				latestVersion = version.BestTag(tags)
				candidates = candidateVersions(tags, ref.Tag, effective == models.ModePinWatch)
			}
		}

		msg := notification.UpdateSummaryMessage(j.reference, ref.Tag, baseline, remote, latestVersion, candidates)
		rec, notify, uerr := s.store.MergeImageUpdate(store.UpdateInput{
			ImageID:          img.ID,
			ImageName:        ref.Repo,
			Reference:        j.reference,
			CurrentVersion:   ref.Tag,
			CurrentDigest:    remote,
			BaseDigest:       baseline,
			LatestVersion:    latestVersion,
			VersionKnown:     versionKnown,
			Candidates:       candidates,
			DigestNotifiable: digestNotifiable,
			Message:          msg.Text,
		})
		if uerr != nil {
			return false, uerr
		}
		if notify {
			// 推送历史留档：按触发原因落 type，页面主数据仍是聚合记录。
			typ := models.NotifUpdate
			newTag := ref.Tag
			if rec.Reason == models.UpdateReasonVersion {
				typ = models.NotifNewTag
				newTag = rec.LatestVersion
				if newTag == "" && len(rec.Candidates) > 0 {
					newTag = rec.Candidates[0]
				}
			}
			_ = s.store.CreateNotification(&models.Notification{
				ImageID:   img.ID,
				ImageName: ref.Repo,
				Reference: j.reference,
				OldDigest: baseline,
				NewDigest: remote,
				OldTag:    ref.Tag,
				NewTag:    newTag,
				LatestTag: rec.LatestVersion,
				Type:      typ,
				Message:   msg.Text,
			})
			s.notifyUpdate(j.reference, msg)
			found = true
		}
	}

	return found, nil
}

// candidateVersions 返回仓库中的候选版本 tag（去重后按版本号降序）：
//   - 当前 tag 可解析为版本号：只取版本号更高者（Digest-Only 浮动 tag 无大小语义）；
//   - 当前 tag 不可解析但处于 Pin-Watch（如手动锁定 stable）：仓库全部版本 tag
//     都算可选版本，保持「出现新版本 tag 就提醒」的既有语义。
//
// 去重规则见 version.DedupTags：同版本别名（0.31 / 0.31.0）只留一个。
func candidateVersions(tags []string, current string, pinWatch bool) []string {
	curNums, ok := version.ParseTag(current)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		n, nok := version.ParseTag(t)
		if !nok {
			continue
		}
		if ok && version.Compare(n, curNums) <= 0 {
			continue
		}
		if !ok && !pinWatch {
			continue
		}
		out = append(out, t)
	}
	return version.DedupTags(out)
}

// listTags 拉取仓库 tag 列表：5s 超时兜底，失败由调用方决定降级策略。
func (s *Scanner) listTags(ctx context.Context, ref registry.ImageRef) ([]string, error) {
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.registry().ListTags(tctx, ref)
}

// computeStatus 依据本地摘要、远端摘要与上次远端摘要，判定镜像状态。
//
// changed 返回值表示「远端摘要发生了真实变更」：当 prevRemote 非空且与 remote 不等时为 true。
// 关键语义：
//   - 首扫（prevRemote == ""）时 changed=false，即使 local!=remote，也不会触发 last_update 更新，
//     从而避免首扫将「首次记录远端基线」误判为「有新版本」而刷通知。
//   - Digest-Only 的推送条件是 changed && status==update-available（见 process 与
//     store.MergeImageUpdate 的双重校验）：local!=remote 且远端相对上次变化时才提醒；
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

// Run 执行一次完整扫描。force=true 为「全部重新扫描」：仅表示重新检测并刷新
// 聚合记录（用于补齐失败轮次、同步版本信息），不再对相同版本重复推送——
// 去重由 store.MergeImageUpdate 按「相对已通知状态是否变化」统一裁决。
//
// 返回是否真正启动：定时器、手动扫描、添加镜像可能并发触发，CAS 保证单飞，
// 重复触发直接跳过（返回 false）。
func (s *Scanner) Run(ctx context.Context, force bool) bool {
	if !s.running.CompareAndSwap(false, true) {
		log.Printf("scan requested while already running, skipping")
		return false
	}
	defer s.running.Store(false)
	log.Printf("scan started (force=%v)", force)

	scanID, err := s.store.CreateScan()
	if err != nil {
		// 占位失败即放弃本轮：不记日志的话「扫描一直没跑」会无从排查
		log.Printf("create scan row failed, skip this round: %v", err)
		return false
	}
	jobs := s.collectJobs(ctx)
	if len(jobs) == 0 {
		_ = s.store.FinishScan(scanID, 0, 0, "done", "")
		return true
	}

	var mu sync.Mutex
	var checked, updates int
	var procErrs []string

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
			mu.Lock()
			defer mu.Unlock()
			checked++
			if perr != nil {
				procErrs = append(procErrs, fmt.Sprintf("%s: %v", j.reference, perr))
				return
			}
			if uf {
				updates++
			}
		}(j)
	}
	wg.Wait()
	// 清理本机已删除镜像的库内残留：将 source=docker 且本轮不再存在的行标记为 stale（缺失）。
	s.pruneRemovedDockerImages(ctx)
	errMsg := truncateErrs(procErrs, 500)
	if errMsg != "" {
		log.Printf("scan finished with %d error(s): %s", len(procErrs), errMsg)
	}
	_ = s.store.FinishScan(scanID, checked, updates, "done", errMsg)
	// 目标已达成的未读记录自动转已读（本地已用上推荐版本 / 摘要已同步），
	// 过期提醒不再占用未读角标；聚合记录与推送历史都保留可查。
	// 强制扫描现在只做重新检测，同样执行消化，避免残留过期未读。
	if n, err := s.store.AutoReadAchievedUpdates(); err == nil && n > 0 {
		log.Printf("auto-read %d achieved update record(s)", n)
	}
	if n, err := s.store.AutoReadAchievedNotifications(); err == nil && n > 0 {
		log.Printf("auto-read %d achieved notification(s)", n)
	}
	// 自动保留策略：扫描结束后裁剪超出上限的最老已读通知（未读永不自动删）。
	if n, err := s.store.TrimReadNotifications(store.MaxRetainedReadNotifs); err == nil && n > 0 {
		log.Printf("trimmed %d read notification(s) (keep latest %d)", n, store.MaxRetainedReadNotifs)
	}
	return true
}

// truncateErrs 把逐镜像错误聚合成单条落库字符串，超长按 rune 截断（引用含中文时防断字）。
func truncateErrs(errs []string, maxRunes int) string {
	if len(errs) == 0 {
		return ""
	}
	joined := strings.Join(errs, "; ")
	r := []rune(joined)
	if len(r) <= maxRunes {
		return joined
	}
	return string(r[:maxRunes]) + "…"
}

// pruneRemovedDockerImages 清理本机已不存在的 docker 镜像行：镜像列表与版本
// 对比以当前 Docker 守护进程的本地镜像为源，本机删除的镜像随扫描移除，
// 不再残留为「缺失」标记。仅当 Docker 守护进程可达时执行；
// manual / watch / 演示列表来源与 ignored 项不受影响（见 store.DeleteRemovedDockerImages）。
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
	if n, err := s.store.DeleteRemovedDockerImages(liveRefs); err == nil && n > 0 {
		log.Printf("removed %d docker image row(s) no longer present locally", n)
	}
}
