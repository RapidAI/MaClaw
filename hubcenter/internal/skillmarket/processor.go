package skillmarket

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

const (
	maxZipRatio = 20 // 解压比率上限
	// Primary gate: total/single size. Entry count is only a DoS backstop so
	// necessary multi-thousand asset packs are allowed when volume is fine.
	maxTotalSize   = coreskill.MaxSkillMarketZipTotalBytes
	maxSingleFile  = coreskill.MaxSkillMarketZipSingleFileBytes
	maxFileCount   = coreskill.MaxSkillMarketZipEntries
	processorQueue = 64
)

// Processor 异步处理上传的 Skill 包。
type Processor struct {
	pendingDir     string
	sandboxBase    string
	store          *Store
	skillStore     *skill.SkillStore
	mailer         *mail.Service
	trialManager   *TrialManager
	versionManager *VersionManager
	searchSvc      *SearchService
	queue          chan string
}

// SetSearchService 设置搜索服务（用于发布后增量更新 FTS 索引）。
func (p *Processor) SetSearchService(svc *SearchService) {
	p.searchSvc = svc
}

// NewProcessor 创建异步处理器。
func NewProcessor(pendingDir, sandboxBase string, store *Store, skillStore *skill.SkillStore, mailer *mail.Service, trialMgr *TrialManager, versionMgr *VersionManager) *Processor {
	return &Processor{
		pendingDir:     pendingDir,
		sandboxBase:    sandboxBase,
		store:          store,
		skillStore:     skillStore,
		mailer:         mailer,
		trialManager:   trialMgr,
		versionManager: versionMgr,
		queue:          make(chan string, processorQueue),
	}
}

// Enqueue 将 submission_id 加入处理队列。
// 队列满时不丢弃：在后台 goroutine 中阻塞投递，避免“上传成功但永不处理”。
func (p *Processor) Enqueue(submissionID string) {
	if p == nil || strings.TrimSpace(submissionID) == "" {
		return
	}
	select {
	case p.queue <- submissionID:
	default:
		log.Printf("[skillmarket] processor queue full, deferring submission %s", submissionID)
		go func(id string) {
			// Prefer non-blocking first in case capacity frees immediately; otherwise
			// block so the id is never dropped. Channel is never closed by design.
			select {
			case p.queue <- id:
			default:
				p.queue <- id
			}
		}(submissionID)
	}
}

// RecoverPending re-enqueues unfinished submissions after process restart.
// The in-memory queue is not durable; without this, Hub uploads that were
// accepted (status=pending) but not yet processed stay invisible forever.
func (p *Processor) RecoverPending(ctx context.Context) {
	if p == nil || p.store == nil {
		return
	}
	ids, err := p.store.ListUnfinishedSubmissionIDs(ctx)
	if err != nil {
		log.Printf("[skillmarket] recover pending submissions: %v", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	log.Printf("[skillmarket] recovering %d unfinished submission(s)", len(ids))
	for _, id := range ids {
		p.Enqueue(id)
	}
}

// Run 启动后台处理 goroutine，阻塞直到 ctx 取消。
func (p *Processor) Run(ctx context.Context) {
	p.RecoverPending(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case subID := <-p.queue:
			if err := p.processOne(ctx, subID); err != nil {
				log.Printf("[skillmarket] process submission %s failed: %v", subID, err)
			}
		}
	}
}

func (p *Processor) processOne(ctx context.Context, subID string) error {
	sub, err := p.store.GetSubmissionByID(ctx, subID)
	if err != nil {
		return fmt.Errorf("get submission: %w", err)
	}

	// Skip terminal submissions (e.g. recovered queue race after success/fail).
	switch strings.ToLower(strings.TrimSpace(sub.Status)) {
	case "success", "failed":
		return nil
	}

	// 标记为 processing
	_ = p.store.UpdateSubmissionStatus(ctx, subID, "processing", "", "")

	// 创建 sandbox 目录
	sandboxDir := filepath.Join(p.sandboxBase, subID)
	defer os.RemoveAll(sandboxDir) // 无论成功失败都清理

	// 安全解压
	if err := SafeUnzip(sub.ZipPath, sandboxDir); err != nil {
		return p.failSubmission(ctx, sub, fmt.Sprintf("unzip failed: %v", err))
	}

	// 验证包
	result, err := ValidatePackage(sandboxDir)
	if err != nil {
		return p.failSubmission(ctx, sub, fmt.Sprintf("validation error: %v", err))
	}
	if !result.Valid {
		var msgs []string
		for _, e := range result.Errors {
			msgs = append(msgs, e.String())
		}
		return p.failSubmissionWithMeta(ctx, sub, result.Metadata, strings.Join(msgs, "; "))
	}

	// 安全扫描
	pkgRoot := result.PackageRoot
	meta := result.Metadata
	secReport, err := ScanPackage(pkgRoot)
	if err != nil {
		log.Printf("[skillmarket] security scan error for %s: %v", subID, err)
	}
	if HasHardcodedSecrets(secReport) {
		return p.failSubmissionWithMeta(ctx, sub, meta, "security scan failed: hardcoded secrets detected")
	}
	securityLabels := GenerateLabels(secReport)

	// 构建 HubSkillFull 并发布
	// 版本管理：判断新建还是升级
	fingerprint := sub.Email + ":" + meta.Name
	versionNum := 1
	var prevSkillID string
	skillID := ""
	publisherSkillID := "" // new format: publisher.skill-name (used for ownership binding)

	// Determine if the package declares a publisher.name skill ID or a legacy UUID.
	if meta.ID != "" {
		if _, err := uuid.Parse(meta.ID); err != nil {
			// Not a UUID → treat as publisher.name format skill ID
			publisherSkillID = meta.ID
			log.Printf("[skillmarket] package declares skill_id: %s", publisherSkillID)
			meta.ID = "" // clear so legacy UUID path doesn't use it
		}
	}

	// ──── NEW: Publisher.name skill ID ownership binding ────
	if publisherSkillID != "" {
		// Validate format
		if !isValidPublisherSkillID(publisherSkillID) {
			return p.failSubmission(ctx, sub, fmt.Sprintf(
				"skill id %q 格式无效（要求: publisher.skill-name，仅小写字母、数字、连字符，如 lovstudio.any2pdf）", publisherSkillID))
		}

		// Check ownership
		owner, err := p.store.GetSkillIDOwner(ctx, publisherSkillID)
		if err != nil {
			return p.failSubmission(ctx, sub, "skill_id 归属查询失败: "+err.Error())
		}
		if owner == nil {
			// First upload with this skill_id → register ownership
			if err := p.store.RegisterSkillIDOwnership(ctx, publisherSkillID, sub.UserID, sub.Email); err != nil {
				return p.failSubmission(ctx, sub, "skill_id 归属注册失败: "+err.Error())
			}
			// Re-read to confirm we won the race (ON CONFLICT DO NOTHING may silently no-op)
			owner, err = p.store.GetSkillIDOwner(ctx, publisherSkillID)
			if err != nil {
				return p.failSubmission(ctx, sub, "skill_id 归属确认失败: "+err.Error())
			}
			if owner == nil || owner.UserID != sub.UserID {
				// Another user registered it between our check and insert
				maskedOwner := ""
				if owner != nil {
					maskedOwner = owner.MaskedEmail()
				}
				return p.failSubmission(ctx, sub, fmt.Sprintf(
					"skill_id %q 已被其他用户注册（所有者: %s）。如果这是你的 skill，请联系管理员。",
					publisherSkillID, maskedOwner))
			}
			log.Printf("[skillmarket] skill_id %s registered to user %s (%s)", publisherSkillID, sub.UserID, sub.Email)
		} else if owner.UserID != sub.UserID {
			// Ownership mismatch → reject
			return p.failSubmission(ctx, sub, fmt.Sprintf(
				"skill_id %q 已被其他用户注册（所有者: %s）。如果这是你的 skill，请联系管理员。",
				publisherSkillID, owner.MaskedEmail()))
		} else {
			log.Printf("[skillmarket] skill_id %s ownership verified for user %s", publisherSkillID, sub.UserID)
		}

		// Find existing internal UUID for this skill_id (for update scenarios)
		existingBySkillID := p.skillStore.FindBySkillID(publisherSkillID)
		if existingBySkillID != nil {
			skillID = existingBySkillID.ID
			log.Printf("[skillmarket] reuse internal ID %s for skill_id %s", skillID, publisherSkillID)
		}
	}

	// ──── Legacy UUID handling (backward compat) ────
	if meta.ID != "" {
		// 校验 UUID 格式，防止路径穿越等注入
		if _, err := uuid.Parse(meta.ID); err != nil {
			log.Printf("[skillmarket] invalid skill ID format in package: %q, ignoring", meta.ID)
			meta.ID = ""
		}
	}
	if meta.ID != "" && skillID == "" {
		existing := p.skillStore.GetByID(meta.ID)
		if existing != nil {
			if existing.Fingerprint == fingerprint {
				// 归属匹配，复用 ID → update
				skillID = meta.ID
				log.Printf("[skillmarket] reuse skill ID %s (fingerprint match)", skillID)
			} else {
				// 归属不匹配，拒绝复用，当作新 skill
				log.Printf("[skillmarket] skill ID %s fingerprint mismatch (pkg=%s, existing=%s), treating as new", meta.ID, fingerprint, existing.Fingerprint)
			}
		} else {
			// 服务端没有这个 ID，首次上传，直接用包里的 UUID
			skillID = meta.ID
			log.Printf("[skillmarket] first upload with skill ID %s", skillID)
		}
	}

	// 没有可复用的 ID，生成新 UUID
	if skillID == "" {
		skillID = uuid.New().String()
		log.Printf("[skillmarket] generated new skill ID %s", skillID)
	}

	if p.versionManager != nil {
		resolution, err := p.versionManager.ResolveSubmission(ctx, fingerprint)
		if err != nil {
			log.Printf("[skillmarket] version resolve error: %v", err)
		} else {
			versionNum = resolution.NextVersion
			prevSkillID = resolution.PrevSkillID
			if resolution.IsUpgrade {
				log.Printf("[skillmarket] version upgrade: %s v%d (prev: %s)", meta.Name, versionNum, prevSkillID)
			}
		}
	}

	if len(securityLabels) > 0 {
		log.Printf("[skillmarket] skill %s security labels: %v", skillID, securityLabels)
	}

	// 更新 submission fingerprint
	_ = p.store.UpdateSubmissionFingerprint(ctx, subID, fingerprint)

	// ──── Version increment check (for skills with publisher.name ID + semver) ────
	if publisherSkillID != "" && meta.Version != "" {
		latestVersion, verErr := p.store.GetLatestVersionForSkillID(ctx, publisherSkillID)
		if verErr == nil && latestVersion != "" {
			if !isVersionGreaterThan(meta.Version, latestVersion) {
				return p.failSubmission(ctx, sub, fmt.Sprintf(
					"版本号 %s 必须大于已发布的最新版本 %s（skill_id: %s）",
					meta.Version, latestVersion, publisherSkillID))
			}
		}
	}

	// Status drives HubCenter market FTS visibility (trial/published).
	// trialManager present → trial (review/auto-publish path); else published immediately.
	publishStatus := "published"
	if p.trialManager != nil {
		publishStatus = "trial"
	}

	full := skill.HubSkillFull{
		HubSkillMeta: skill.HubSkillMeta{
			ID:                           skillID,
			SkillID:                      publisherSkillID, // publisher.name format (empty for legacy skills)
			Name:                         meta.Name,
			Description:                  meta.Description,
			Tags:                         meta.Tags,
			Version:                      fmt.Sprintf("%d", versionNum),
			SemVer:                       meta.Version, // semver from skill.yaml
			Author:                       meta.Author,
			TrustLevel:                   "trusted",
			Status:                       publishStatus,
			CreatedAt:                    fmtTime(sub.CreatedAt),
			UpdatedAt:                    fmtTime(sub.CreatedAt),
			Visible:                      true,
			SecurityLabels:               securityLabels,
			Permissions:                  meta.Permissions,
			RequiredEnv:                  meta.RequiredEnv,
			Platforms:                    meta.Platforms,
			RequiresGUI:                  meta.RequiresGUI,
			ProductKind:                  meta.ProductKind,
			IsMaclawApp:                  meta.IsMaclawApp,
			MaclawAppCount:               meta.MaclawAppCount,
			MaclawAppEntry:               meta.MaclawAppEntry,
			MaclawAppID:                  meta.MaclawAppID,
			MaclawAppName:                meta.MaclawAppName,
			MaclawAppDescription:         meta.MaclawAppDescription,
			MaclawAppCategory:            meta.MaclawAppCategory,
			MaclawAppIcon:                meta.MaclawAppIcon,
			MaclawAppInputMode:           meta.MaclawAppInputMode,
			MaclawAppOutputModes:         meta.MaclawAppOutputModes,
			MaclawAppDefinitionSHA256:    meta.MaclawAppDefinitionSHA256,
			MaclawAppTestEvidence:        hubMaclawAppTestEvidence(meta.MaclawAppTestEvidence),
			ArtifactContractRequired:     meta.ArtifactContractRequired,
			ArtifactContractOutputModes:  meta.ArtifactContractOutputModes,
			ArtifactContractPresentation: meta.ArtifactContractPresentation,
			UploaderEmail:                sub.Email,
			Fingerprint:                  fingerprint,
		},
		Triggers: meta.Triggers,
	}

	// 读取包内文件（仅白名单扩展名，单文件 ≤ 256KB）
	full.Files = make(map[string]string)
	allowedExts := map[string]bool{
		".sh": true, ".py": true, ".js": true, ".yaml": true,
		".json": true, ".txt": true, ".md": true,
	}
	_ = filepath.Walk(pkgRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(pkgRoot, path)
		rel = filepath.ToSlash(rel) // 统一为正斜杠
		if rel == "skill.yaml" {
			return nil // 元数据已解析
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if !allowedExts[ext] {
			return nil
		}
		if info.Size() > 256<<10 { // 256KB
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		full.Files[rel] = base64.StdEncoding.EncodeToString(data)
		return nil
	})

	if err := p.skillStore.Publish(full); err != nil {
		return p.failSubmissionWithMeta(ctx, sub, meta, fmt.Sprintf("publish failed: %v", err))
	}

	// Record version in history table (for skill_id version tracking)
	if publisherSkillID != "" && meta.Version != "" {
		if verErr := p.store.RecordSkillVersion(ctx, publisherSkillID, meta.Version, skillID, "", sub.UserID); verErr != nil {
			log.Printf("[skillmarket] record version %s/%s failed: %v", publisherSkillID, meta.Version, verErr)
		}
	}

	// 增量更新 FTS 搜索索引（与 Publish 的 Status 保持一致；trial/published 均可被市场检索）
	if p.searchSvc != nil {
		if err := p.searchSvc.IndexSkillWithProduct(ctx, skillID, meta.Name, meta.Description, meta.Tags, 0, 0, 0, publishStatus, fmtTime(sub.CreatedAt), meta.Version, meta.Author, skillSearchIndexProductOptions{ProductKind: meta.ProductKind, IsMaclawApp: meta.IsMaclawApp}); err != nil {
			log.Printf("[skillmarket] index skill %s error: %v", skillID, err)
		}
	}

	// 标记成功
	_ = p.store.UpdateSubmissionStatus(ctx, subID, "success", "", skillID)

	// Trial Manager：语法验证通过后进入 trial 状态
	if p.trialManager != nil {
		if err := p.trialManager.OnSkillValidated(ctx, skillID); err != nil {
			log.Printf("[skillmarket] trial manager error: %v", err)
		}
	}

	// 版本升级：旧版本暂不标记 superseded，等新版本 published 后再处理
	if prevSkillID != "" && prevSkillID != skillID {
		log.Printf("[skillmarket] new version %s replaces %s (will supersede on publish)", skillID, prevSkillID)
	}

	// 发送成功通知邮件
	p.sendNotification(ctx, sub.Email, fmt.Sprintf("SkillMarket: Skill Submitted - %s (v%d)", meta.Name, versionNum),
		formatSkillNotificationBody(meta, skillID, versionNum))

	return nil
}

func hubMaclawAppTestEvidence(e *MaclawAppTestEvidence) *skill.MaclawAppTestEvidence {
	if e == nil {
		return nil
	}
	return &skill.MaclawAppTestEvidence{
		RunID:                 e.RunID,
		VerifiedAt:            e.VerifiedAt,
		DefinitionFingerprint: e.DefinitionFingerprint,
		ArtifactPresent:       e.ArtifactPresent,
		ArtifactName:          e.ArtifactName,
		OutputCount:           e.OutputCount,
		PrimaryResult:         e.PrimaryResult,
		ResultPayload:         cloneSkillMarketAnyMap(e.ResultPayload),
	}
}

func (p *Processor) failSubmission(ctx context.Context, sub *SkillSubmission, errMsg string) error {
	return p.failSubmissionWithMeta(ctx, sub, nil, errMsg)
}

func (p *Processor) failSubmissionWithMeta(ctx context.Context, sub *SkillSubmission, meta *SkillMetadata, errMsg string) error {
	_ = p.store.UpdateSubmissionStatus(ctx, sub.ID, "failed", errMsg, "")
	subject := "SkillMarket: Submission Failed"
	body := fmt.Sprintf("Your skill submission failed.\nReason: %s", errMsg)
	if meta != nil {
		subject = fmt.Sprintf("SkillMarket: Submission Failed - %s", meta.Name)
		body = fmt.Sprintf("Your skill \"%s\" submission failed.\nReason: %s", meta.Name, errMsg)
		if meta.Description != "" {
			body += fmt.Sprintf("\nDescription: %s", meta.Description)
		}
	}
	p.sendNotification(ctx, sub.Email, subject, body)
	return fmt.Errorf("submission %s failed: %s", sub.ID, errMsg)
}

// formatSkillNotificationBody 构建详细的 skill 上传成功通知邮件内容。
func formatSkillNotificationBody(meta *SkillMetadata, skillID string, version int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your skill has entered trial period.\n\n")
	fmt.Fprintf(&b, "Name: %s\n", meta.Name)
	fmt.Fprintf(&b, "Version: %d\n", version)
	fmt.Fprintf(&b, "Skill ID: %s\n", skillID)
	if meta.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", meta.Description)
	}
	if meta.Author != "" {
		fmt.Fprintf(&b, "Author: %s\n", meta.Author)
	}
	if len(meta.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(meta.Tags, ", "))
	}
	if len(meta.Platforms) > 0 {
		fmt.Fprintf(&b, "Platforms: %s\n", strings.Join(meta.Platforms, ", "))
	}
	if len(meta.Permissions) > 0 {
		fmt.Fprintf(&b, "Permissions: %s\n", strings.Join(meta.Permissions, ", "))
	}
	if len(meta.Triggers) > 0 {
		fmt.Fprintf(&b, "Triggers: %s\n", strings.Join(meta.Triggers, ", "))
	}
	return b.String()
}

func (p *Processor) sendNotification(ctx context.Context, to, subject, body string) {
	if p.mailer == nil || to == "" {
		return
	}
	if err := p.mailer.Send(ctx, []string{to}, subject, body); err != nil {
		log.Printf("[skillmarket] send mail to %s failed: %v", to, err)
	}
}

// isVersionGreaterThan returns true if version a > version b using simple
// semver-like comparison. Pre-release versions are lower than releases
// (1.0.0 > 1.0.0-beta). Tolerates non-semver strings by falling back to
// lexicographic comparison.
func isVersionGreaterThan(a, b string) bool {
	va, vaOk := parseSemVerParts(a)
	vb, vbOk := parseSemVerParts(b)
	if vaOk && vbOk {
		if va.major != vb.major {
			return va.major > vb.major
		}
		if va.minor != vb.minor {
			return va.minor > vb.minor
		}
		if va.patch != vb.patch {
			return va.patch > vb.patch
		}
		// Same numeric version — compare pre-release:
		// release (no pre) > pre-release (has pre)
		if va.pre == "" && vb.pre != "" {
			return true // 1.0.0 > 1.0.0-beta
		}
		if va.pre != "" && vb.pre == "" {
			return false // 1.0.0-beta < 1.0.0
		}
		// Both have pre-release — lexicographic
		return va.pre > vb.pre
	}
	// Fallback: lexicographic (e.g. "2" > "1")
	return a > b
}

type semverParts struct {
	major, minor, patch int
	pre                 string
}

// parseSemVerParts parses "1.2.3-beta.1" into components. Returns false on failure.
func parseSemVerParts(s string) (semverParts, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return semverParts{}, false
	}
	var pre string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	nums := strings.Split(s, ".")
	if len(nums) < 1 || len(nums) > 3 {
		return semverParts{}, false
	}
	var parts [3]int
	for i, n := range nums {
		v := 0
		for _, ch := range n {
			if ch < '0' || ch > '9' {
				return semverParts{}, false
			}
			v = v*10 + int(ch-'0')
		}
		parts[i] = v
	}
	return semverParts{major: parts[0], minor: parts[1], patch: parts[2], pre: pre}, true
}

// isValidPublisherSkillID validates the publisher.skill-name format.
// This is a local copy of the validation logic from corelib/skill.IsValidSkillID.
// Keep in sync with corelib/skill/skill_id.go.
func isValidPublisherSkillID(id string) bool {
	if len(id) < 6 || len(id) > 129 { // min: 3 (pub) + 1 (dot) + 2 (name) = 6; max: 64+1+64=129
		return false
	}
	dot := strings.IndexByte(id, '.')
	if dot < 3 || dot == len(id)-1 { // publisher min 3 chars
		return false
	}
	namePart := id[dot+1:]
	if len(namePart) < 2 { // name min 2 chars
		return false
	}
	// Check each segment: [a-z0-9]([a-z0-9-]*[a-z0-9])
	for _, seg := range []string{id[:dot], namePart} {
		for i, ch := range seg {
			if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
				continue
			}
			if ch == '-' && i > 0 && i < len(seg)-1 {
				continue
			}
			return false
		}
	}
	return true
}

// SafeUnzip 安全解压 zip 到目标目录。
// 主闸门：解压总大小（≤500MB）、单文件（≤50MB）、解压比（≤20x）。
// 文件数上限（MaxSkillMarketZipEntries）仅防海量空文件 DoS，不阻止「体积正常、文件多」的资源包。
// 先完整预检再落盘，避免校验失败时留下半截沙箱内容。
func SafeUnzip(zipPath, destDir string) error {
	fi, err := os.Stat(zipPath)
	if err != nil {
		return fmt.Errorf("stat zip: %w", err)
	}
	zipSize := fi.Size()

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	// DoS backstop only — legitimate template/font/SVG packs may have thousands of files.
	if len(r.File) > maxFileCount {
		return fmt.Errorf("too many files: %d (max %d DoS limit); remove node_modules/.git/venv/cache if present, or split oversized asset packs", len(r.File), maxFileCount)
	}

	cleanDest := filepath.Clean(destDir)
	destPrefix := cleanDest + string(os.PathSeparator)

	type planned struct {
		file   *zip.File
		target string
		isDir  bool
	}
	plan := make([]planned, 0, len(r.File))
	var totalSize int64

	// Pass 1: validate every entry (zip-slip, sizes, ratio) before writing anything.
	for _, f := range r.File {
		name := strings.TrimSpace(f.Name)
		if name == "" || strings.Contains(name, "\x00") {
			return fmt.Errorf("zip contains empty or invalid entry name")
		}
		// Normalize zip paths to slash form before Join so "a\..\b" style
		// entries cannot bypass slip checks on Windows.
		name = filepath.ToSlash(name)
		if name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("zip slip detected: %s", f.Name)
		}
		target := filepath.Join(destDir, filepath.FromSlash(name))
		cleanTarget := filepath.Clean(target)
		if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, destPrefix) {
			return fmt.Errorf("zip slip detected: %s", f.Name)
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip contains unsupported symlink entry: %s", f.Name)
		}
		isDir := f.FileInfo().IsDir() || strings.HasSuffix(name, "/")
		if isDir {
			plan = append(plan, planned{file: f, target: cleanTarget, isDir: true})
			continue
		}
		usize := f.UncompressedSize64
		if usize > uint64(maxSingleFile) {
			return fmt.Errorf("file too large: %s (%d bytes, max %d)", f.Name, usize, maxSingleFile)
		}
		if usize > uint64(maxTotalSize) || uint64(totalSize)+usize > uint64(maxTotalSize) {
			return fmt.Errorf("total uncompressed size exceeds %d bytes", maxTotalSize)
		}
		totalSize += int64(usize)
		if zipSize > 0 && totalSize > zipSize*maxZipRatio {
			return fmt.Errorf("zip bomb detected: ratio %.1fx exceeds %dx", float64(totalSize)/float64(zipSize), maxZipRatio)
		}
		plan = append(plan, planned{file: f, target: cleanTarget, isDir: false})
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	// Pass 2: extract only after all checks pass.
	for _, p := range plan {
		if p.isDir {
			if err := os.MkdirAll(p.target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := extractFile(p.file, p.target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer rc.Close()

	mode := f.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", f.Name, err)
	}

	// LimitReader blocks zip bombs that under-declare UncompressedSize64.
	written, copyErr := io.Copy(out, io.LimitReader(rc, maxSingleFile+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("extract %s: %w", f.Name, copyErr)
	}
	if written > maxSingleFile {
		_ = os.Remove(target)
		return fmt.Errorf("file too large after decompression: %s", f.Name)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("close %s: %w", f.Name, closeErr)
	}
	return nil
}
