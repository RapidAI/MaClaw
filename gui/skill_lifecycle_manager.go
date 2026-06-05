package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

const skillUploadLeaseTimeout = 30 * time.Minute
const skillUploadQueueProcessInterval = 10 * time.Minute
const skillUploadQueueProcessLimit = 3

type SkillUploadQueueItem struct {
	ID              string            `json:"id"`
	SkillName       string            `json:"skill_name"`
	SkillDir        string            `json:"skill_dir,omitempty"`
	LocalHash       string            `json:"local_hash,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Status          skillUploadStatus `json:"status"`
	Attempts        int               `json:"attempts"`
	LastError       string            `json:"last_error,omitempty"`
	NextAttemptAt   string            `json:"next_attempt_at,omitempty"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
	SubmissionID    string            `json:"submission_id,omitempty"`
	UploadedTargets map[string]string `json:"uploaded_targets,omitempty"`
	QualityScore    int               `json:"quality_score,omitempty"`
	RequireRuntime  bool              `json:"require_runtime_proof"`
}

type skillUploadQueueFile struct {
	Items []SkillUploadQueueItem `json:"items"`
}

type SkillLifecycleManager struct {
	app          *App
	queuePath    string
	mu           sync.Mutex
	processMu    sync.Mutex
	workerMu     sync.Mutex
	workerStop   chan struct{}
	workerDone   chan struct{}
	workerCancel context.CancelFunc
}

type skillUploadBlockedError struct {
	Message string
	Score   int
}

func (e *skillUploadBlockedError) Error() string { return e.Message }

func NewSkillLifecycleManager(app *App) *SkillLifecycleManager {
	return &SkillLifecycleManager{
		app:       app,
		queuePath: filepath.Join(app.GetDataDir(), "skill_upload_queue.json"),
	}
}

func (m *SkillLifecycleManager) StartBackgroundProcessing(ctx context.Context, interval time.Duration) {
	if m == nil || m.app == nil {
		return
	}
	if interval <= 0 {
		interval = skillUploadQueueProcessInterval
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	m.workerMu.Lock()
	if m.workerStop != nil {
		cancel()
		m.workerMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	m.workerStop = stop
	m.workerDone = done
	m.workerCancel = cancel
	m.workerMu.Unlock()
	go func() {
		defer close(done)
		m.processUploadQueueInBackground(workerCtx, "startup")
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.processUploadQueueInBackground(workerCtx, "interval")
			case <-workerCtx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
}

func (m *SkillLifecycleManager) StopBackgroundProcessing() {
	if m == nil {
		return
	}
	m.workerMu.Lock()
	stop := m.workerStop
	done := m.workerDone
	cancel := m.workerCancel
	m.workerStop = nil
	m.workerDone = nil
	m.workerCancel = nil
	m.workerMu.Unlock()
	if stop == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	close(stop)
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			log.Printf("[skill-lifecycle] upload queue worker did not stop within timeout")
		}
	}
}

func (m *SkillLifecycleManager) processUploadQueueInBackground(ctx context.Context, reason string) {
	if err := m.ProcessPendingUploads(ctx, skillUploadQueueProcessLimit); err != nil {
		log.Printf("[skill-lifecycle] background upload queue process failed reason=%s: %v", reason, err)
	}
}

func (m *SkillLifecycleManager) HasRunnableUploadItems(now time.Time) (bool, error) {
	if m == nil {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return false, err
	}
	for _, item := range q.Items {
		switch item.Status {
		case skillUploadStatusPending:
			if uploadQueueItemDue(item, now) {
				return true, nil
			}
		case skillUploadStatusFailed:
			if uploadQueueItemDue(item, now) {
				return true, nil
			}
		case skillUploadStatusUploading:
			updatedAt, err := time.Parse(time.RFC3339, item.UpdatedAt)
			if err != nil || now.Sub(updatedAt) >= skillUploadLeaseTimeout {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *SkillLifecycleManager) NormalizeInstalled(entry *corelib.NLSkillEntry) *corelib.NLSkillEntry {
	if m == nil || m.app == nil {
		return entry
	}
	return normalizeInstalledSkillEntry(entry, m.app)
}

func (m *SkillLifecycleManager) EnqueueUpload(ctx context.Context, skillName, skillDir, reason string, requireRuntimeProof bool, processNow bool) (*SkillUploadQueueItem, error) {
	if m == nil || m.app == nil {
		return nil, fmt.Errorf("skill lifecycle manager not initialized")
	}
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return nil, fmt.Errorf("skill name is empty")
	}
	entry, err := m.resolveSkillEntry(skillName, skillDir)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		if canonicalName := strings.TrimSpace(entry.Name); canonicalName != "" {
			skillName = canonicalName
		}
	}
	if strings.TrimSpace(skillDir) == "" && entry != nil {
		skillDir = entry.SkillDir
	}
	localHash := ""
	if strings.TrimSpace(skillDir) != "" {
		localHash = skillDirHash(skillDir)
		if uploaded, ok, lookupErr := m.findUploadedQueueItem(skillName, localHash); lookupErr != nil {
			return nil, lookupErr
		} else if ok {
			return uploaded, nil
		}
	}
	var quality skillQualityReport
	if strings.TrimSpace(skillDir) != "" {
		_, report, prepErr := prepareSkillDirForMarket(skillDir, true, m.app)
		if prepErr != nil {
			item := m.recordBlocked(skillName, skillDir, reason, localHash, requireRuntimeProof, "portability preparation failed: "+prepErr.Error(), 0)
			return item, prepErr
		}
		canonicalHash := skillDirHash(skillDir)
		if canonicalHash != "" {
			localHash = canonicalHash
		}
		if uploaded, ok, lookupErr := m.findUploadedQueueItem(skillName, localHash); lookupErr != nil {
			return nil, lookupErr
		} else if ok {
			return uploaded, nil
		}
		if reloaded, loadErr := loadMarketPackageSkillEntry(skillDir, entry); loadErr == nil {
			if strings.TrimSpace(reloaded.Name) == "" {
				reloaded.Name = skillName
			}
			reloaded.SkillDir = skillDir
			entry = m.mergeRegisteredRuntimeStats(reloaded)
		}
		quality = evaluateSkillQuality(entry, report, requireRuntimeProof)
		writeSkillQualityStatus(skillDir, entry, quality, reasonStage(reason), requireRuntimeProof)
		if !quality.MarketReady {
			msg := fmt.Sprintf("quality gate blocked upload: score=%d reasons=%s", quality.Score, strings.Join(quality.Reasons, "; "))
			item := m.recordBlocked(skillName, skillDir, reason, localHash, requireRuntimeProof, msg, quality.Score)
			return item, errors.New(msg)
		}
	}

	item, err := m.upsertPending(skillName, skillDir, localHash, reason, requireRuntimeProof, quality.Score)
	if err != nil {
		return nil, err
	}
	if processNow {
		if err := m.ProcessPendingUploads(ctx, 1); err != nil {
			log.Printf("[skill-lifecycle] process upload queue failed: %v", err)
		}
	}
	return item, nil
}

func (m *SkillLifecycleManager) UploadNow(ctx context.Context, skillName, reason string, requireRuntimeProof bool) (string, error) {
	return m.UploadNowWithCompletedTargets(ctx, skillName, reason, requireRuntimeProof, nil)
}

func (m *SkillLifecycleManager) UploadNowWithCompletedTargets(ctx context.Context, skillName, reason string, requireRuntimeProof bool, completedTargets map[string]string) (string, error) {
	if m == nil || m.app == nil {
		return "", fmt.Errorf("skill lifecycle manager not initialized")
	}
	m.app.ensureInteractionInfra()
	if m.app.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}
	m.app.ensureSkillMarketClient()
	if m.app.skillMarketClient == nil {
		return "", fmt.Errorf("skill market client not initialized")
	}

	zipPath, tmpDir, err := m.app.packageSkillForMarketWithDirForOutbound(skillName)
	if err != nil {
		return "", fmt.Errorf("package skill: %w", err)
	}
	defer os.Remove(zipPath)
	defer os.RemoveAll(tmpDir)

	report, err := skill.ValidateSkillPortability(tmpDir)
	if err != nil {
		return "", fmt.Errorf("portability validation failed: %w", err)
	}
	target := m.findRegisteredSkill(skillName)
	qualityEntry, loadErr := loadMarketPackageSkillEntry(tmpDir, target)
	if loadErr != nil {
		return "", fmt.Errorf("load packaged skill entry: %w", loadErr)
	}
	quality := evaluateSkillQualityForDir(qualityEntry, report, requireRuntimeProof, tmpDir)
	if target != nil && strings.TrimSpace(target.SkillDir) != "" {
		writeSkillQualityStatus(target.SkillDir, qualityEntry, quality, reasonStage(reason), requireRuntimeProof)
	}
	if !quality.MarketReady {
		msg := fmt.Sprintf("upload blocked by quality gate: score=%d reasons=%s", quality.Score, strings.Join(quality.Reasons, "; "))
		return "", &skillUploadBlockedError{Message: msg, Score: quality.Score}
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	if email == "" {
		return "", fmt.Errorf("remote_email is not configured")
	}
	submissionID, err := m.app.skillMarketClient.SubmitSkillToConfiguredTargetsWithCompleted(ctx, zipPath, email, completedTargets)
	if err != nil {
		return "", fmt.Errorf("submit skill: %w", err)
	}
	_ = m.app.skillExecutor.MarkUploaded(skillName, submissionID)
	if target != nil && strings.TrimSpace(target.SkillDir) != "" {
		status := map[string]string{"submission_id": submissionID}
		if data, marshalErr := json.Marshal(status); marshalErr == nil {
			_ = os.WriteFile(filepath.Join(target.SkillDir, "upload_status.json"), data, 0o644)
		}
		if m.app.autoUploadTrigger != nil {
			m.app.autoUploadTrigger.MarkUploadedHash(skillName, skillDirHash(target.SkillDir))
		}
	}
	return submissionID, nil
}

func (m *SkillLifecycleManager) uploadQueueItem(ctx context.Context, item SkillUploadQueueItem) (string, error) {
	if strings.TrimSpace(item.SkillDir) == "" {
		return m.UploadNowWithCompletedTargets(ctx, item.SkillName, item.Reason, item.RequireRuntime, item.UploadedTargets)
	}
	return m.UploadDirNowWithCompletedTargets(ctx, item.SkillName, item.SkillDir, item.Reason, item.RequireRuntime, item.UploadedTargets)
}

func (m *SkillLifecycleManager) UploadDirNow(ctx context.Context, skillName, skillDir, reason string, requireRuntimeProof bool) (string, error) {
	return m.UploadDirNowWithCompletedTargets(ctx, skillName, skillDir, reason, requireRuntimeProof, nil)
}

func (m *SkillLifecycleManager) UploadDirNowWithCompletedTargets(ctx context.Context, skillName, skillDir, reason string, requireRuntimeProof bool, completedTargets map[string]string) (string, error) {
	if m == nil || m.app == nil {
		return "", fmt.Errorf("skill lifecycle manager not initialized")
	}
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return "", fmt.Errorf("skill directory is empty")
	}
	m.app.ensureSkillMarketClient()
	if m.app.skillMarketClient == nil {
		return "", fmt.Errorf("skill market client not initialized")
	}

	_, report, err := prepareSkillDirForMarket(skillDir, true, m.app)
	if err != nil {
		return "", fmt.Errorf("prepare skill dir: %w", err)
	}
	entry, err := loadMarketPackageSkillEntry(skillDir, nil)
	if err != nil {
		return "", fmt.Errorf("load skill entry: %w", err)
	}
	if strings.TrimSpace(entry.Name) == "" {
		entry.Name = skillName
	}
	entry.SkillDir = skillDir
	if registered := m.findRegisteredSkill(entry.Name); registered != nil {
		mergeSkillPackagingRuntimeFields(entry, registered)
	}
	quality := evaluateSkillQuality(entry, report, requireRuntimeProof)
	writeSkillQualityStatus(skillDir, entry, quality, reasonStage(reason), requireRuntimeProof)
	if !quality.MarketReady {
		msg := fmt.Sprintf("upload blocked by quality gate: score=%d reasons=%s", quality.Score, strings.Join(quality.Reasons, "; "))
		return "", &skillUploadBlockedError{Message: msg, Score: quality.Score}
	}
	cfg, err := m.app.LoadConfig()

	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	if email == "" {
		return "", fmt.Errorf("remote_email is not configured")
	}
	tmpDir, err := os.MkdirTemp("", "skill-package-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	if err := copyDirContents(skillDir, tmpDir); err != nil {
		return "", fmt.Errorf("copy skill dir for package: %w", err)
	}
	if err := writePackageViewSkillYAML(tmpDir, entry); err != nil {
		return "", err
	}
	tmpReport, err := skill.ValidateSkillPortability(tmpDir)
	if err != nil {
		return "", fmt.Errorf("validate skill package: %w", err)
	}
	packageQuality := evaluateSkillQualityForDir(entry, tmpReport, requireRuntimeProof, tmpDir)
	if !packageQuality.MarketReady {
		msg := fmt.Sprintf("upload blocked by package quality gate: score=%d reasons=%s", packageQuality.Score, strings.Join(packageQuality.Reasons, "; "))
		return "", &skillUploadBlockedError{Message: msg, Score: packageQuality.Score}
	}
	if err := scanSkillDirForOutboundPackage(tmpDir, m.app); err != nil {
		return "", err
	}
	if err := writeSkillPackageManifest(tmpDir, entry, packageQuality, reasonStage(reason), requireRuntimeProof); err != nil {
		return "", fmt.Errorf("write skill package manifest: %w", err)
	}
	zipPath := filepath.Join(m.app.GetTempDir(), fmt.Sprintf("skill-%s-%d.zip", toKebabCase(entry.Name), time.Now().UnixMilli()))
	if err := zipDirectory(tmpDir, zipPath); err != nil {
		return "", fmt.Errorf("package skill dir: %w", err)
	}
	defer os.Remove(zipPath)

	submissionID, err := m.app.skillMarketClient.SubmitSkillToConfiguredTargetsWithCompleted(ctx, zipPath, email, completedTargets)
	if err != nil {
		return "", fmt.Errorf("submit skill: %w", err)
	}
	if m.app.skillExecutor != nil {
		_ = m.app.skillExecutor.MarkUploaded(entry.Name, submissionID)
	}
	status := map[string]string{"submission_id": submissionID}
	if data, marshalErr := json.Marshal(status); marshalErr == nil {
		_ = os.WriteFile(filepath.Join(skillDir, "upload_status.json"), data, 0o644)
	}
	if m.app.autoUploadTrigger != nil {
		m.app.autoUploadTrigger.MarkUploadedHash(entry.Name, skillDirHash(skillDir))
	}
	return submissionID, nil
}

func (m *SkillLifecycleManager) ProcessPendingUploads(ctx context.Context, limit int) error {
	return m.processPendingUploads(ctx, limit, "", false)
}

func (m *SkillLifecycleManager) processPendingUploads(ctx context.Context, limit int, skillName string, forceReady bool) error {
	if m == nil || m.app == nil {
		return fmt.Errorf("skill lifecycle manager not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.processMu.Lock()
	defer m.processMu.Unlock()
	processed := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if limit > 0 && processed >= limit {
			return nil
		}
		item, ok, err := m.nextPendingItemMatching(time.Now(), skillName, forceReady)
		if err != nil || !ok {
			return err
		}
		processed++
		submissionID, uploadErr := m.uploadQueueItem(ctx, item)
		if uploadErr != nil {
			m.markUploadFailed(item.ID, uploadErr)
			continue
		}
		m.markUploaded(item.ID, submissionID)
	}
}

func (m *SkillLifecycleManager) EvaluateInstalledSkills(requireRuntimeProof bool) ([]SkillQualityStatus, error) {
	if m == nil || m.app == nil {
		return nil, fmt.Errorf("skill lifecycle manager not initialized")
	}
	m.app.ensureInteractionInfra()
	if m.app.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	m.app.skillExecutor.mu.RLock()
	skills := append([]corelib.NLSkillEntry(nil), m.app.skillExecutor.loadSkills()...)
	m.app.skillExecutor.mu.RUnlock()

	statuses := make([]SkillQualityStatus, 0, len(skills))
	for i := range skills {
		entry := &skills[i]
		if strings.TrimSpace(entry.SkillDir) == "" {
			quality := evaluateSkillQuality(entry, nil, requireRuntimeProof)
			statuses = append(statuses, buildSkillQualityStatus("", entry, quality, "audit", requireRuntimeProof))
			continue
		}
		_, report, err := prepareSkillDirForMarket(entry.SkillDir, true, m.app)
		if err != nil {
			quality := skillQualityReport{Score: 0, MarketReady: false, Reasons: []string{"portability preparation failed: " + err.Error()}}
			writeSkillQualityStatus(entry.SkillDir, entry, quality, "audit", requireRuntimeProof)
			statuses = append(statuses, buildSkillQualityStatus(entry.SkillDir, entry, quality, "audit", requireRuntimeProof))
			continue
		}
		qualityEntry := entry
		if reloaded, err := loadMarketPackageSkillEntry(entry.SkillDir, entry); err == nil {
			qualityEntry = reloaded
		}
		quality := evaluateSkillQuality(qualityEntry, report, requireRuntimeProof)
		writeSkillQualityStatus(entry.SkillDir, qualityEntry, quality, "audit", requireRuntimeProof)
		statuses = append(statuses, buildSkillQualityStatus(entry.SkillDir, qualityEntry, quality, "audit", requireRuntimeProof))
	}
	return statuses, nil
}

func (m *SkillLifecycleManager) RetryBlockedAndProcess(ctx context.Context, skillName string, limit int) error {
	if err := m.RetryBlocked(skillName); err != nil {
		return err
	}
	return m.processPendingUploads(ctx, limit, skillName, strings.TrimSpace(skillName) != "")
}

func (m *SkillLifecycleManager) RetryBlocked(skillName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	changed := false
	for i := range q.Items {
		if q.Items[i].Status != skillUploadStatusBlocked {
			continue
		}
		if strings.TrimSpace(skillName) != "" && !strings.EqualFold(q.Items[i].SkillName, skillName) {
			continue
		}
		if strings.TrimSpace(q.Items[i].SkillDir) != "" {
			item, ready := m.reevaluateBlockedUploadItem(q.Items[i], now)
			q.Items[i] = item
			if !ready {
				changed = true
				continue
			}
		} else {
			q.Items[i].Status = skillUploadStatusPending
			q.Items[i].LastError = ""
			q.Items[i].NextAttemptAt = ""
			q.Items[i].UpdatedAt = now
		}
		changed = true
	}
	if !changed {
		return nil
	}
	q.Items = dedupeSkillUploadQueueItems(q.Items)
	return m.saveQueueLocked(q)
}

func (m *SkillLifecycleManager) reevaluateBlockedUploadItem(item SkillUploadQueueItem, now string) (SkillUploadQueueItem, bool) {
	_, report, err := prepareSkillDirForMarket(item.SkillDir, true, m.app)
	if err != nil {
		item.LastError = "portability preparation failed: " + err.Error()
		item.QualityScore = 0
		item.UpdatedAt = now
		return item, false
	}
	entry, err := loadMarketPackageSkillEntry(item.SkillDir, nil)
	if err != nil {
		item.LastError = "load skill entry failed: " + err.Error()
		item.QualityScore = 0
		item.UpdatedAt = now
		return item, false
	}
	if strings.TrimSpace(entry.Name) == "" {
		entry.Name = item.SkillName
	}
	entry.SkillDir = item.SkillDir
	entry = m.mergeRegisteredRuntimeStats(entry)
	quality := evaluateSkillQuality(entry, report, item.RequireRuntime)
	writeSkillQualityStatus(item.SkillDir, entry, quality, reasonStage(item.Reason), item.RequireRuntime)
	localHash := skillDirHash(item.SkillDir)
	item.SkillName = entry.Name
	item.LocalHash = localHash
	item.ID = skillUploadQueueID(entry.Name, localHash)
	item.QualityScore = quality.Score
	item.UpdatedAt = now
	if !quality.MarketReady {
		item.Status = skillUploadStatusBlocked
		item.LastError = fmt.Sprintf("quality gate blocked upload: score=%d reasons=%s", quality.Score, strings.Join(quality.Reasons, "; "))
		return item, false
	}
	item.Status = skillUploadStatusPending
	item.LastError = ""
	item.NextAttemptAt = ""
	return item, true
}

func dedupeSkillUploadQueueItems(items []SkillUploadQueueItem) []SkillUploadQueueItem {
	seen := make(map[string]int, len(items))
	out := make([]SkillUploadQueueItem, 0, len(items))
	for _, item := range items {
		if idx, ok := seen[item.ID]; ok {
			if uploadQueueItemRank(item) >= uploadQueueItemRank(out[idx]) {
				out[idx] = item
			}
			continue
		}
		seen[item.ID] = len(out)
		out = append(out, item)
	}
	return out
}

func uploadQueueItemRank(item SkillUploadQueueItem) int {
	switch item.Status {
	case skillUploadStatusUploaded:
		return 5
	case skillUploadStatusUploading:
		return 4
	case skillUploadStatusPending:
		return 3
	case skillUploadStatusFailed:
		return 2
	case skillUploadStatusBlocked:
		return 1
	default:
		return 0
	}
}

func (m *SkillLifecycleManager) ListUploadQueue() ([]SkillUploadQueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return nil, err
	}
	return append([]SkillUploadQueueItem(nil), q.Items...), nil
}

func (m *SkillLifecycleManager) mergeRegisteredRuntimeStats(entry *corelib.NLSkillEntry) *corelib.NLSkillEntry {
	if entry == nil {
		return nil
	}
	registered := m.findRegisteredSkill(entry.Name)
	if registered == nil && strings.TrimSpace(entry.SkillDir) != "" {
		registered = m.findRegisteredSkill(filepath.Base(entry.SkillDir))
	}
	if registered == nil {
		return entry
	}
	entry.UsageCount = registered.UsageCount
	entry.SuccessCount = registered.SuccessCount
	entry.FailureCount = registered.FailureCount
	entry.WorkaroundCount = registered.WorkaroundCount
	entry.LastUsedAt = registered.LastUsedAt
	entry.LastError = registered.LastError
	entry.RepairAttemptCount = registered.RepairAttemptCount
	entry.LastRepairAt = registered.LastRepairAt
	entry.RepairHistory = append([]corelib.SkillRepairRecord(nil), registered.RepairHistory...)
	return entry
}

func (m *SkillLifecycleManager) resolveSkillEntry(skillName, skillDir string) (*corelib.NLSkillEntry, error) {
	if strings.TrimSpace(skillDir) != "" {
		entry, err := loadImportedSkillEntry(skillDir)
		if err == nil {
			if strings.TrimSpace(entry.Name) == "" {
				entry.Name = skillName
			}
			entry.SkillDir = skillDir
			return entry, nil
		}
	}
	if entry := m.findRegisteredSkill(skillName); entry != nil {
		return entry, nil
	}
	return nil, fmt.Errorf("skill %q not found", skillName)
}

func (m *SkillLifecycleManager) findRegisteredSkill(skillName string) *corelib.NLSkillEntry {
	if m.app == nil {
		return nil
	}
	if m.app.skillExecutor == nil {
		m.app.ensureRemoteInfra()
	}
	if m.app.skillExecutor == nil {
		return nil
	}
	m.app.skillExecutor.mu.RLock()
	defer m.app.skillExecutor.mu.RUnlock()
	for _, s := range m.app.skillExecutor.loadSkills() {
		if s.MatchesName(skillName) {
			cp := s
			return &cp
		}
	}
	return nil
}

func (m *SkillLifecycleManager) recordBlocked(skillName, skillDir, reason, localHash string, requireRuntimeProof bool, msg string, score int) *SkillUploadQueueItem {
	item := SkillUploadQueueItem{
		ID:             skillUploadQueueID(skillName, localHash),
		SkillName:      skillName,
		SkillDir:       skillDir,
		LocalHash:      localHash,
		Reason:         reason,
		Status:         skillUploadStatusBlocked,
		LastError:      msg,
		QualityScore:   score,
		RequireRuntime: requireRuntimeProof,
		CreatedAt:      time.Now().Format(time.RFC3339),
		UpdatedAt:      time.Now().Format(time.RFC3339),
	}
	if err := m.saveOrReplace(item); err != nil {
		log.Printf("[skill-lifecycle] persist blocked queue item failed: %v", err)
	}
	return &item
}

func (m *SkillLifecycleManager) findUploadedQueueItem(skillName, localHash string) (*SkillUploadQueueItem, bool, error) {
	if strings.TrimSpace(localHash) == "" {
		return nil, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return nil, false, err
	}
	id := skillUploadQueueID(skillName, localHash)
	for i := range q.Items {
		if q.Items[i].ID == id && q.Items[i].Status == skillUploadStatusUploaded {
			item := q.Items[i]
			return &item, true, nil
		}
	}
	return nil, false, nil
}

func (m *SkillLifecycleManager) upsertPending(skillName, skillDir, localHash, reason string, requireRuntimeProof bool, score int) (*SkillUploadQueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().Format(time.RFC3339)
	id := skillUploadQueueID(skillName, localHash)
	for i := range q.Items {
		if q.Items[i].ID == id {
			if q.Items[i].Status == skillUploadStatusUploaded {
				return &q.Items[i], nil
			}
			q.Items[i].SkillDir = skillDir
			q.Items[i].Reason = reason
			q.Items[i].Status = skillUploadStatusPending
			q.Items[i].LastError = ""
			q.Items[i].NextAttemptAt = ""
			q.Items[i].QualityScore = score
			q.Items[i].RequireRuntime = requireRuntimeProof
			q.Items[i].UpdatedAt = now
			if err := m.saveQueueLocked(q); err != nil {
				return nil, err
			}
			return &q.Items[i], nil
		}
	}
	item := SkillUploadQueueItem{
		ID:             id,
		SkillName:      skillName,
		SkillDir:       skillDir,
		LocalHash:      localHash,
		Reason:         reason,
		Status:         skillUploadStatusPending,
		QualityScore:   score,
		RequireRuntime: requireRuntimeProof,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	q.Items = append(q.Items, item)
	if err := m.saveQueueLocked(q); err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *SkillLifecycleManager) nextPendingItem(now time.Time) (SkillUploadQueueItem, bool, error) {
	return m.nextPendingItemMatching(now, "", false)
}

func (m *SkillLifecycleManager) nextPendingItemMatching(now time.Time, skillName string, forceReady bool) (SkillUploadQueueItem, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return SkillUploadQueueItem{}, false, err
	}
	filter := strings.TrimSpace(skillName)
	forceNamedRetry := forceReady && filter != ""
	for i := range q.Items {
		if filter != "" && !strings.EqualFold(q.Items[i].SkillName, filter) {
			continue
		}
		status := q.Items[i].Status
		if status == skillUploadStatusUploading {
			updatedAt, err := time.Parse(time.RFC3339, q.Items[i].UpdatedAt)
			if err == nil && now.Sub(updatedAt) < skillUploadLeaseTimeout {
				continue
			}
			q.Items[i].Status = skillUploadStatusFailed
			q.Items[i].LastError = "upload lease expired; retrying"
			q.Items[i].NextAttemptAt = ""
		} else if status != skillUploadStatusPending && status != skillUploadStatusFailed {
			continue
		}
		if !forceNamedRetry && !uploadQueueItemDue(q.Items[i], now) {
			continue
		}
		q.Items[i].Status = skillUploadStatusUploading
		q.Items[i].UpdatedAt = now.Format(time.RFC3339)
		item := q.Items[i]
		if err := m.saveQueueLocked(q); err != nil {
			return SkillUploadQueueItem{}, false, err
		}
		return item, true, nil
	}
	return SkillUploadQueueItem{}, false, nil
}

func uploadQueueItemDue(item SkillUploadQueueItem, now time.Time) bool {
	if strings.TrimSpace(item.NextAttemptAt) == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, item.NextAttemptAt)
	return err != nil || !now.Before(t)
}

func (m *SkillLifecycleManager) markUploadFailed(id string, uploadErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		log.Printf("[skill-lifecycle] load queue after failure failed: %v", err)
		return
	}
	now := time.Now()
	var blocked *skillUploadBlockedError
	var partial *skillSubmitPartialError
	for i := range q.Items {
		if q.Items[i].ID != id {
			continue
		}
		if errors.As(uploadErr, &blocked) {
			q.Items[i].Status = skillUploadStatusBlocked
			q.Items[i].LastError = blocked.Error()
			q.Items[i].QualityScore = blocked.Score
			q.Items[i].NextAttemptAt = ""
			q.Items[i].UpdatedAt = now.Format(time.RFC3339)
			break
		}
		if errors.As(uploadErr, &partial) && len(partial.Completed) > 0 {
			if q.Items[i].UploadedTargets == nil {
				q.Items[i].UploadedTargets = map[string]string{}
			}
			for target, submissionID := range normalizedSkillSubmitTargetResults(partial.Completed) {
				q.Items[i].UploadedTargets[target] = submissionID
			}
		}
		q.Items[i].Attempts++
		q.Items[i].Status = skillUploadStatusFailed
		q.Items[i].LastError = uploadErr.Error()
		q.Items[i].UpdatedAt = now.Format(time.RFC3339)
		q.Items[i].NextAttemptAt = now.Add(skillUploadBackoff(q.Items[i].Attempts)).Format(time.RFC3339)
		break
	}
	if err := m.saveQueueLocked(q); err != nil {
		log.Printf("[skill-lifecycle] save queue after failure failed: %v", err)
	}
}

func (m *SkillLifecycleManager) markUploaded(id, submissionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		log.Printf("[skill-lifecycle] load queue after upload failed: %v", err)
		return
	}
	now := time.Now().Format(time.RFC3339)
	for i := range q.Items {
		if q.Items[i].ID != id {
			continue
		}
		q.Items[i].Status = skillUploadStatusUploaded
		q.Items[i].SubmissionID = submissionID
		q.Items[i].UploadedTargets = map[string]string{}
		for target, targetSubmissionID := range m.parseUploadedTargetResults(submissionID) {
			q.Items[i].UploadedTargets[target] = targetSubmissionID
		}
		q.Items[i].LastError = ""
		q.Items[i].NextAttemptAt = ""
		q.Items[i].UpdatedAt = now
		break
	}
	if err := m.saveQueueLocked(q); err != nil {
		log.Printf("[skill-lifecycle] save queue after upload failed: %v", err)
	}
}

func (m *SkillLifecycleManager) parseUploadedTargetResults(submissionID string) map[string]string {
	if strings.Contains(submissionID, "=") || strings.Contains(submissionID, ";") {
		return parseSkillSubmitTargetResults(submissionID)
	}
	if m == nil || m.app == nil {
		return parseSkillSubmitTargetResults(submissionID)
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return parseSkillSubmitTargetResults(submissionID)
	}
	hasEnterpriseHub := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
	targets := cfg.CapabilityMarketPolicy.UploadTargets(hasEnterpriseHub)
	if len(targets) == 1 && strings.TrimSpace(submissionID) != "" {
		return map[string]string{targets[0]: strings.TrimSpace(submissionID)}
	}
	return parseSkillSubmitTargetResults(submissionID)
}

func (m *SkillLifecycleManager) saveOrReplace(item SkillUploadQueueItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, err := m.loadQueueLocked()
	if err != nil {
		return err
	}
	for i := range q.Items {
		if q.Items[i].ID == item.ID {
			q.Items[i] = item
			return m.saveQueueLocked(q)
		}
	}
	q.Items = append(q.Items, item)
	return m.saveQueueLocked(q)
}

func (m *SkillLifecycleManager) loadQueueLocked() (skillUploadQueueFile, error) {
	var q skillUploadQueueFile
	data, err := os.ReadFile(m.queuePath)
	if os.IsNotExist(err) {
		return q, nil
	}
	if err != nil {
		return q, err
	}
	if len(data) == 0 {
		return q, nil
	}
	if err := json.Unmarshal(data, &q); err != nil {
		return q, err
	}
	return q, nil
}

func (m *SkillLifecycleManager) saveQueueLocked(q skillUploadQueueFile) error {
	if err := os.MkdirAll(filepath.Dir(m.queuePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.queuePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.queuePath)
}

func skillUploadQueueID(skillName, localHash string) string {
	key := strings.ToLower(strings.TrimSpace(skillName)) + "\x00" + strings.TrimSpace(localHash)
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("suq_%x", sum[:8])
}

func skillUploadBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(1<<uint(attempts-1)) * time.Minute
}

func reasonStage(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "upload_queue"
	}
	return reason
}
