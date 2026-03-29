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

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

const (
	maxZipRatio    = 20    // 解压比率上限
	maxTotalSize   = 500 << 20 // 500MB
	maxSingleFile  = 50 << 20  // 50MB
	maxFileCount   = 1000
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
func (p *Processor) Enqueue(submissionID string) {
	select {
	case p.queue <- submissionID:
	default:
		log.Printf("[skillmarket] processor queue full, dropping submission %s", submissionID)
	}
}

// Run 启动后台处理 goroutine，阻塞直到 ctx 取消。
func (p *Processor) Run(ctx context.Context) {
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

	// 如果包里带了 UUID，尝试复用（防洗包：fingerprint 必须匹配）
	if meta.ID != "" {
		// 校验 UUID 格式，防止路径穿越等注入
		if _, err := uuid.Parse(meta.ID); err != nil {
			log.Printf("[skillmarket] invalid skill ID format in package: %q, ignoring", meta.ID)
			meta.ID = ""
		}
	}
	if meta.ID != "" {
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

	full := skill.HubSkillFull{
		HubSkillMeta: skill.HubSkillMeta{
			ID:             skillID,
			Name:           meta.Name,
			Description:    meta.Description,
			Tags:           meta.Tags,
			Version:        fmt.Sprintf("%d", versionNum),
			Author:         meta.Author,
			TrustLevel:     "community",
			CreatedAt:      fmtTime(sub.CreatedAt),
			UpdatedAt:      fmtTime(sub.CreatedAt),
			Visible:        true,
			SecurityLabels: securityLabels,
			Permissions:    meta.Permissions,
			RequiredEnv:    meta.RequiredEnv,
			Platforms:      meta.Platforms,
			RequiresGUI:    meta.RequiresGUI,
			UploaderEmail:  sub.Email,
			Fingerprint:    fingerprint,
		},
		Triggers: meta.Triggers,
	}

	// 读取包内文件（仅白名单扩展名，单文件 ≤ 256KB）
	full.Files = make(map[string]string)
	allowedExts := map[string]bool{
		".sh": true, ".py": true, ".js": true, ".yaml": true,
		".yml": true, ".json": true, ".txt": true, ".md": true,
	}
	_ = filepath.Walk(pkgRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(pkgRoot, path)
		rel = filepath.ToSlash(rel) // 统一为正斜杠
		if rel == "skill.yaml" || rel == "skill.yml" {
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

	// 增量更新 FTS 搜索索引（新上传的 skill 初始状态为 trial）
	if p.searchSvc != nil {
		indexStatus := "trial"
		if p.trialManager == nil {
			indexStatus = "published"
		}
		if err := p.searchSvc.IndexSkill(ctx, skillID, meta.Name, meta.Description, meta.Tags, 0, 0, 0, indexStatus, fmtTime(sub.CreatedAt)); err != nil {
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

// SafeUnzip 安全解压 zip 文件到目标目录。
// 检查解压比率（≤20x）、总大小（≤500MB）、单文件（≤50MB）、文件数量（≤1000）。
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

	if len(r.File) > maxFileCount {
		return fmt.Errorf("too many files: %d (max %d)", len(r.File), maxFileCount)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	var totalSize int64
	for _, f := range r.File {
		// 防止 zip slip
		target := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) &&
			filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("zip slip detected: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		// 单文件大小检查
		if f.UncompressedSize64 > uint64(maxSingleFile) {
			return fmt.Errorf("file too large: %s (%d bytes, max %d)", f.Name, f.UncompressedSize64, maxSingleFile)
		}

		totalSize += int64(f.UncompressedSize64)
		if totalSize > maxTotalSize {
			return fmt.Errorf("total uncompressed size exceeds %d bytes", maxTotalSize)
		}

		// 解压比率检查
		if zipSize > 0 && totalSize > zipSize*maxZipRatio {
			return fmt.Errorf("zip bomb detected: ratio %.1fx exceeds %dx", float64(totalSize)/float64(zipSize), maxZipRatio)
		}

		if err := extractFile(f, target); err != nil {
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

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()&0o755|0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", f.Name, err)
	}
	defer out.Close()

	// 使用 LimitReader 作为额外保护
	if _, err := io.Copy(out, io.LimitReader(rc, maxSingleFile+1)); err != nil {
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	return nil
}
