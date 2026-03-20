package skillmarket

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

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
	queue          chan string
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
		return p.failSubmission(ctx, sub, strings.Join(msgs, "; "))
	}

	// 安全扫描
	secReport, err := ScanPackage(sandboxDir)
	if err != nil {
		log.Printf("[skillmarket] security scan error for %s: %v", subID, err)
	}
	if HasHardcodedSecrets(secReport) {
		return p.failSubmission(ctx, sub, "security scan failed: hardcoded secrets detected")
	}
	securityLabels := GenerateLabels(secReport)

	// 构建 HubSkillFull 并发布
	meta := result.Metadata
	skillID := generateID()

	if len(securityLabels) > 0 {
		log.Printf("[skillmarket] skill %s security labels: %v", skillID, securityLabels)
	}

	// 版本管理：判断新建还是升级
	fingerprint := sub.Email + ":" + meta.Name
	versionNum := 1
	var prevSkillID string
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

	// 更新 submission fingerprint
	_ = p.store.UpdateSubmissionFingerprint(ctx, subID, fingerprint)

	full := skill.HubSkillFull{
		HubSkillMeta: skill.HubSkillMeta{
			ID:          skillID,
			Name:        meta.Name,
			Description: meta.Description,
			Tags:        meta.Tags,
			Version:     fmt.Sprintf("%d", versionNum),
			Author:      meta.Author,
			TrustLevel:  "community",
			CreatedAt:   fmtTime(sub.CreatedAt),
			UpdatedAt:   fmtTime(sub.CreatedAt),
			Visible:     true,
		},
		Triggers: meta.Triggers,
	}

	// 读取包内文件
	full.Files = make(map[string]string)
	_ = filepath.Walk(sandboxDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(sandboxDir, path)
		if rel == "skill.yaml" {
			return nil // 元数据已解析
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		full.Files[rel] = string(data)
		return nil
	})

	if err := p.skillStore.Publish(full); err != nil {
		return p.failSubmission(ctx, sub, fmt.Sprintf("publish failed: %v", err))
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
	if prevSkillID != "" {
		log.Printf("[skillmarket] new version %s replaces %s (will supersede on publish)", skillID, prevSkillID)
	}

	// 发送成功通知邮件
	p.sendNotification(ctx, sub.Email, "SkillMarket: Skill Submitted",
		fmt.Sprintf("Your skill \"%s\" (v%d) has entered trial period.\nSkill ID: %s", meta.Name, versionNum, skillID))

	return nil
}

func (p *Processor) failSubmission(ctx context.Context, sub *SkillSubmission, errMsg string) error {
	_ = p.store.UpdateSubmissionStatus(ctx, sub.ID, "failed", errMsg, "")
	p.sendNotification(ctx, sub.Email, "SkillMarket: Submission Failed",
		fmt.Sprintf("Your skill submission failed.\nReason: %s", errMsg))
	return fmt.Errorf("submission %s failed: %s", sub.ID, errMsg)
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
