package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"golang.org/x/crypto/argon2"
)

const (
	userDataMigrationPackageVersion = "maclaw-gui-user-data-migration/v1"
	userDataMigrationChunkSize      = int64(4 << 20)
	userDataMigrationAEADChunkSize  = int64(4 << 20)
	userDataMigrationMaxExpanded    = int64(4) << 30
	userDataMigrationMaxZipFiles    = 200000
	userDataMigrationMagic          = "MLMIG01"
	userDataMigrationJobRetention   = 24 * time.Hour
)

type userDataMigrationStatus struct {
	Configured          bool        `json:"configured"`
	HubURL              string      `json:"hub_url,omitempty"`
	TenantID            string      `json:"tenant_id,omitempty"`
	TenantName          string      `json:"tenant_name,omitempty"`
	UserID              string      `json:"user_id,omitempty"`
	Email               string      `json:"email,omitempty"`
	MachineID           string      `json:"machine_id,omitempty"`
	MachineName         string      `json:"machine_name,omitempty"`
	MaxCompressedBytes  int64       `json:"max_compressed_bytes,omitempty"`
	CurrentExport       interface{} `json:"current_export,omitempty"`
	ConfigurationReason string      `json:"configuration_reason,omitempty"`
}

type userDataMigrationClientConfig struct {
	HubURL       string
	ViewerToken  string
	MachineToken string
	TenantID     string
	TenantName   string
	UserID       string
	Email        string
	MachineID    string
	MachineName  string
}

type userDataMigrationManifest struct {
	Version        string                        `json:"version"`
	CreatedAt      time.Time                     `json:"created_at"`
	TenantID       string                        `json:"tenant_id,omitempty"`
	TenantName     string                        `json:"tenant_name,omitempty"`
	UserID         string                        `json:"user_id,omitempty"`
	Email          string                        `json:"email,omitempty"`
	MachineID      string                        `json:"machine_id,omitempty"`
	MachineName    string                        `json:"machine_name,omitempty"`
	MemoryEntries  int                           `json:"memory_entries"`
	KnowledgeBytes int64                         `json:"knowledge_bytes"`
	AssetBytes     int64                         `json:"asset_bytes"`
	Files          []userDataMigrationFileDigest `json:"files"`
	Meta           map[string]interface{}        `json:"meta,omitempty"`
}

type userDataMigrationFileDigest struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type userDataMigrationEncryptedHeader struct {
	Version   string `json:"version"`
	KDF       string `json:"kdf"`
	Time      uint32 `json:"time"`
	MemoryKB  uint32 `json:"memory_kb"`
	Threads   uint8  `json:"threads"`
	Salt      []byte `json:"salt"`
	Nonce     []byte `json:"nonce"`
	PlainHash string `json:"plain_sha256"`
	Stream    bool   `json:"stream,omitempty"`
	ChunkSize int64  `json:"chunk_size,omitempty"`
}

type userDataMigrationJob struct {
	ID           string                 `json:"id"`
	Kind         string                 `json:"kind"`
	Status       string                 `json:"status"`
	Progress     float64                `json:"progress"`
	ProgressText string                 `json:"progress_text,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Result       map[string]interface{} `json:"result,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
}

var (
	userDataMigrationJobs                  sync.Map
	userDataMigrationCleanupPendingExports sync.Map
	userDataMigrationJobStartMu            sync.Mutex
)

func (a *App) UserDataMigrationStatus() (userDataMigrationStatus, error) {
	cfg, loadErr := a.LoadConfig()
	status := userDataMigrationStatus{}
	if loadErr == nil {
		status.HubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
		status.TenantID = strings.TrimSpace(cfg.RemoteTenantID)
		status.TenantName = strings.TrimSpace(cfg.RemoteTenantName)
		status.UserID = strings.TrimSpace(cfg.RemoteUserID)
		status.Email = strings.TrimSpace(cfg.RemoteEmail)
		status.MachineID = strings.TrimSpace(cfg.RemoteMachineID)
		status.MachineName = firstNonEmpty(cfg.RemoteMachineName, cfg.RemoteClientID, cfg.RemoteMachineID)
	}
	if loadErr != nil {
		status.ConfigurationReason = fmt.Sprintf("load config: %v", loadErr)
		return status, nil
	}
	clientCfg, err := userDataMigrationConfigFromAppConfig(cfg)
	if err != nil {
		status.ConfigurationReason = err.Error()
		return status, nil
	}
	current, maxBytes, err := a.userDataMigrationHubGetCurrent(a.userDataMigrationContext(), clientCfg)
	status.Configured = true
	if err != nil {
		status.ConfigurationReason = err.Error()
		return status, nil
	}
	status.CurrentExport = current
	status.MaxCompressedBytes = maxBytes
	return status, nil
}

func (a *App) UserDataMigrationInstances() (map[string]interface{}, error) {
	cfg, err := a.userDataMigrationConfig()
	if err != nil {
		return nil, err
	}
	return a.userDataMigrationHubJSON(a.userDataMigrationContext(), cfg, http.MethodGet, "/api/v1/migration/instances", nil)
}

func (a *App) StartUserDataMigrationExport(password, passwordConfirm string, confirmOverwrite bool) (userDataMigrationJob, error) {
	if password == "" || password != passwordConfirm {
		return userDataMigrationJob{}, fmt.Errorf("passwords do not match")
	}
	if !confirmOverwrite {
		return userDataMigrationJob{}, fmt.Errorf("export overwrite confirmation is required")
	}
	cfg, err := a.userDataMigrationConfig()
	if err != nil {
		return userDataMigrationJob{}, err
	}
	return a.startUserDataMigrationJob("migration.export", func(ctx context.Context, progress func(float64, string)) (map[string]interface{}, error) {
		return a.runUserDataMigrationExport(ctx, cfg, password, progress)
	})
}

func (a *App) StartUserDataMigrationImport(exportID, password string) (userDataMigrationJob, error) {
	exportID = strings.TrimSpace(exportID)
	if exportID == "" || password == "" {
		return userDataMigrationJob{}, fmt.Errorf("export_id and password are required")
	}
	cfg, err := a.userDataMigrationConfig()
	if err != nil {
		return userDataMigrationJob{}, err
	}
	return a.startUserDataMigrationJob("migration.import", func(ctx context.Context, progress func(float64, string)) (map[string]interface{}, error) {
		return a.runUserDataMigrationImport(ctx, cfg, exportID, password, progress)
	})
}

func (a *App) StartUserDataMigrationCleanup(exportID string) (userDataMigrationJob, error) {
	exportID = strings.TrimSpace(exportID)
	if exportID == "" {
		return userDataMigrationJob{}, fmt.Errorf("export_id is required")
	}
	cfg, err := a.userDataMigrationConfig()
	if err != nil {
		return userDataMigrationJob{}, err
	}
	return a.startUserDataMigrationJob("migration.import.cleanup", func(ctx context.Context, progress func(float64, string)) (map[string]interface{}, error) {
		return a.runUserDataMigrationCleanup(ctx, cfg, exportID, progress)
	})
}

func (a *App) GetUserDataMigrationJob(jobID string) (userDataMigrationJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return userDataMigrationJob{}, fmt.Errorf("migration job id is required")
	}
	value, ok := userDataMigrationJobs.Load(jobID)
	if !ok {
		return userDataMigrationJob{}, fmt.Errorf("migration job %s not found", jobID)
	}
	job, ok := value.(userDataMigrationJob)
	if !ok {
		return userDataMigrationJob{}, fmt.Errorf("migration job %s has invalid state", jobID)
	}
	return job, nil
}

func (a *App) userDataMigrationContext() context.Context {
	if a != nil && a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) userDataMigrationConfig() (userDataMigrationClientConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return userDataMigrationClientConfig{}, fmt.Errorf("load config: %w", err)
	}
	return userDataMigrationConfigFromAppConfig(cfg)
}

func userDataMigrationConfigFromAppConfig(cfg corelib.AppConfig) (userDataMigrationClientConfig, error) {
	out := userDataMigrationClientConfig{
		HubURL:       strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/"),
		ViewerToken:  strings.TrimSpace(cfg.RemoteViewerToken),
		MachineToken: strings.TrimSpace(cfg.RemoteMachineToken),
		TenantID:     strings.TrimSpace(cfg.RemoteTenantID),
		TenantName:   strings.TrimSpace(cfg.RemoteTenantName),
		UserID:       strings.TrimSpace(cfg.RemoteUserID),
		Email:        strings.TrimSpace(cfg.RemoteEmail),
		MachineID:    strings.TrimSpace(cfg.RemoteMachineID),
		MachineName:  firstNonEmpty(cfg.RemoteMachineName, cfg.RemoteClientID, cfg.RemoteMachineID),
	}
	if out.HubURL == "" {
		return out, fmt.Errorf("Hub is not configured")
	}
	if out.MachineID == "" {
		return out, fmt.Errorf("Hub machine is not registered")
	}
	if out.ViewerToken == "" && out.MachineToken == "" {
		return out, fmt.Errorf("Hub login is required")
	}
	return out, nil
}

func (a *App) startUserDataMigrationJob(kind string, run func(context.Context, func(float64, string)) (map[string]interface{}, error)) (userDataMigrationJob, error) {
	userDataMigrationJobStartMu.Lock()
	defer userDataMigrationJobStartMu.Unlock()

	now := time.Now().UTC()
	pruneUserDataMigrationJobs(now)
	if userDataMigrationJobRunning() {
		return userDataMigrationJob{}, fmt.Errorf("another migration job is already running")
	}
	job := userDataMigrationJob{
		ID:        fmt.Sprintf("mig_%d", now.UnixNano()),
		Kind:      kind,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	}
	userDataMigrationJobs.Store(job.ID, job)
	go func(jobID string) {
		progress := func(v float64, text string) {
			updateUserDataMigrationJob(jobID, func(job *userDataMigrationJob) {
				if v < 0 {
					v = 0
				}
				if v > 1 {
					v = 1
				}
				job.Progress = v
				job.ProgressText = text
			})
		}
		var result map[string]interface{}
		var err error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = fmt.Errorf("migration job failed unexpectedly: %v", recovered)
				}
			}()
			result, err = run(a.userDataMigrationContext(), progress)
		}()
		updateUserDataMigrationJob(jobID, func(job *userDataMigrationJob) {
			completed := time.Now().UTC()
			job.CompletedAt = &completed
			job.UpdatedAt = completed
			if err != nil {
				job.Status = "failed"
				job.Error = err.Error()
				return
			}
			job.Status = "succeeded"
			job.Progress = 1
			if strings.TrimSpace(job.ProgressText) == "" {
				job.ProgressText = "completed"
			}
			job.Result = result
		})
	}(job.ID)
	return job, nil
}

func userDataMigrationJobRunning() bool {
	running := false
	userDataMigrationJobs.Range(func(_, value interface{}) bool {
		job, ok := value.(userDataMigrationJob)
		if ok && strings.EqualFold(job.Status, "running") {
			running = true
			return false
		}
		return true
	})
	return running
}

func pruneUserDataMigrationJobs(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-userDataMigrationJobRetention)
	userDataMigrationJobs.Range(func(key, value interface{}) bool {
		job, ok := value.(userDataMigrationJob)
		if !ok {
			userDataMigrationJobs.Delete(key)
			return true
		}
		if job.CompletedAt != nil && job.CompletedAt.Before(cutoff) {
			userDataMigrationJobs.Delete(key)
		}
		return true
	})
}

func updateUserDataMigrationJob(jobID string, mutate func(*userDataMigrationJob)) {
	value, ok := userDataMigrationJobs.Load(jobID)
	if !ok {
		return
	}
	job, ok := value.(userDataMigrationJob)
	if !ok {
		return
	}
	mutate(&job)
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = time.Now().UTC()
	} else if job.CompletedAt == nil {
		job.UpdatedAt = time.Now().UTC()
	}
	userDataMigrationJobs.Store(jobID, job)
}

func (a *App) runUserDataMigrationExport(ctx context.Context, cfg userDataMigrationClientConfig, password string, progress func(float64, string)) (map[string]interface{}, error) {
	workDir, err := a.createUserDataMigrationTempDir("migration-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	progress(0.05, "preparing migration package")
	plainPath, manifest, err := a.buildUserDataMigrationPackage(ctx, cfg, workDir)
	if err != nil {
		return nil, err
	}
	plainHash, plainSize, err := userDataMigrationFileSHA256(plainPath)
	if err != nil {
		return nil, err
	}
	_, maxBytes, _ := a.userDataMigrationHubGetCurrent(ctx, cfg)
	if maxBytes > 0 && plainSize > maxBytes {
		return nil, fmt.Errorf("compressed migration package is %s, exceeds limit %s", userDataMigrationFormatBytes(plainSize), userDataMigrationFormatBytes(maxBytes))
	}
	progress(0.22, "encrypting migration package")
	encryptedPath := filepath.Join(workDir, "migration.mlawenc")
	if err := encryptUserDataMigrationFile(plainPath, encryptedPath, password, plainHash); err != nil {
		return nil, err
	}
	encryptedHash, encryptedSize, err := userDataMigrationFileSHA256(encryptedPath)
	if err != nil {
		return nil, err
	}
	chunkCount := int((encryptedSize + userDataMigrationChunkSize - 1) / userDataMigrationChunkSize)
	createReq := map[string]interface{}{
		"compressed_size":  plainSize,
		"encrypted_size":   encryptedSize,
		"encrypted_sha256": encryptedHash,
		"plain_sha256":     plainHash,
		"chunk_size":       userDataMigrationChunkSize,
		"chunk_count":      chunkCount,
		"manifest":         manifest,
	}
	created, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/exports", createReq)
	if err != nil {
		return nil, err
	}
	exportID := strings.TrimSpace(fmt.Sprint(created["export_id"]))
	if exportID == "" {
		return nil, fmt.Errorf("Hub did not return export_id")
	}
	progress(0.30, "uploading encrypted chunks")
	if err := a.uploadUserDataMigrationChunks(ctx, cfg, exportID, encryptedPath, encryptedSize, chunkCount, progress); err != nil {
		return nil, err
	}
	completeReq := map[string]interface{}{"encrypted_sha256": encryptedHash}
	if _, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/exports/"+exportID+"/complete-upload", completeReq); err != nil {
		return nil, err
	}
	progress(1, "export completed")
	return map[string]interface{}{
		"export_id":       exportID,
		"compressed_size": plainSize,
		"encrypted_size":  encryptedSize,
		"chunk_count":     chunkCount,
		"manifest":        manifest,
	}, nil
}

func (a *App) runUserDataMigrationImport(ctx context.Context, cfg userDataMigrationClientConfig, exportID, password string, progress func(float64, string)) (result map[string]interface{}, err error) {
	workDir, err := a.createUserDataMigrationTempDir("migration-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	progress(0.05, "claiming migration export")
	claimResp, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/claim", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	claimed := true
	localRestored := false
	defer func() {
		if err != nil && claimed && !localRestored {
			abortCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = a.userDataMigrationHubJSON(abortCtx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/abort", map[string]interface{}{})
		}
	}()
	exportMap, _ := claimResp["export"].(map[string]interface{})
	claimedStatus := strings.ToLower(strings.TrimSpace(fmt.Sprint(exportMap["status"])))
	if claimedStatus == "deleted" {
		claimed = false
		userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, exportID))
		progress(1, "import cleanup already completed")
		return map[string]interface{}{"export_id": exportID, "cleanup_retried": true, "status": "deleted"}, nil
	}
	if claimedStatus == "imported" || claimedStatus == "deleting" {
		progress(0.92, "retrying Hub cleanup")
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.completeUserDataMigrationImportOnHub(cleanupCtx, cfg, exportID); err != nil {
			return nil, fmt.Errorf("Hub cleanup retry failed: %w", err)
		}
		claimed = false
		userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, exportID))
		progress(1, "import cleanup completed")
		return map[string]interface{}{"export_id": exportID, "cleanup_retried": true}, nil
	}
	chunkCount := int(userDataMigrationNumberFromMap(exportMap, "chunk_count"))
	chunkSize := int64(userDataMigrationNumberFromMap(exportMap, "chunk_size"))
	encryptedSize := int64(userDataMigrationNumberFromMap(exportMap, "encrypted_size"))
	encryptedHash := strings.TrimSpace(fmt.Sprint(exportMap["encrypted_sha256"]))
	plainHash := strings.TrimSpace(fmt.Sprint(exportMap["plain_sha256"]))
	if chunkCount <= 0 || chunkSize <= 0 || encryptedSize <= 0 || encryptedHash == "" || plainHash == "" {
		return nil, fmt.Errorf("invalid migration export metadata")
	}
	encryptedPath := filepath.Join(workDir, "migration.mlawenc")
	progress(0.12, "downloading encrypted chunks")
	if err := a.downloadUserDataMigrationChunks(ctx, cfg, exportID, encryptedPath, encryptedSize, chunkSize, chunkCount, progress); err != nil {
		return nil, err
	}
	gotHash, _, err := userDataMigrationFileSHA256(encryptedPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(gotHash, encryptedHash) {
		return nil, fmt.Errorf("encrypted package hash mismatch")
	}
	progress(0.72, "decrypting and verifying package")
	plainPath := filepath.Join(workDir, "migration.zip")
	if err := decryptUserDataMigrationFile(encryptedPath, plainPath, password, plainHash); err != nil {
		return nil, err
	}
	if gotPlain, _, err := userDataMigrationFileSHA256(plainPath); err != nil {
		return nil, err
	} else if !strings.EqualFold(gotPlain, plainHash) {
		return nil, fmt.Errorf("decrypted package hash mismatch")
	}
	progress(0.82, "restoring local memory and knowledge base")
	result, err = a.restoreUserDataMigrationPackage(ctx, plainPath, workDir)
	if err != nil {
		return nil, err
	}
	localRestored = true
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.completeUserDataMigrationImportOnHub(cleanupCtx, cfg, exportID); err != nil {
		progress(1, "local import completed; Hub cleanup can be retried")
		userDataMigrationCleanupPendingExports.Store(userDataMigrationCleanupPendingKey(cfg, exportID), time.Now().UTC())
		result["export_id"] = exportID
		result["cleanup_pending"] = true
		result["cleanup_error"] = err.Error()
		return result, nil
	}
	claimed = false
	userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, exportID))
	progress(1, "import completed")
	result["export_id"] = exportID
	return result, nil
}

func (a *App) runUserDataMigrationCleanup(ctx context.Context, cfg userDataMigrationClientConfig, exportID string, progress func(float64, string)) (map[string]interface{}, error) {
	progress(0.05, "checking migration cleanup state")
	cleanupStatus, err := a.userDataMigrationCleanupStatus(ctx, cfg, exportID)
	if err != nil {
		return nil, err
	}
	if cleanupStatus == "deleted" {
		userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, exportID))
		progress(1, "import cleanup already completed")
		return map[string]interface{}{"export_id": exportID, "cleanup_retried": true, "status": "deleted"}, nil
	}
	progress(0.15, "retrying Hub cleanup")
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := a.completeUserDataMigrationImportOnHub(cleanupCtx, cfg, exportID); err != nil {
		return nil, fmt.Errorf("Hub cleanup retry failed: %w", err)
	}
	userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, exportID))
	progress(1, "import cleanup completed")
	return map[string]interface{}{"export_id": exportID, "cleanup_retried": true}, nil
}

func (a *App) userDataMigrationCleanupStatus(ctx context.Context, cfg userDataMigrationClientConfig, exportID string) (string, error) {
	out, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodGet, "/api/v1/migration/exports/current", nil)
	if err != nil {
		return "", err
	}
	item, _ := out["export"].(map[string]interface{})
	if item == nil || strings.TrimSpace(fmt.Sprint(item["export_id"])) != exportID {
		return "", fmt.Errorf("migration export is not available for cleanup retry")
	}
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(item["status"])))
	claimedBy := strings.TrimSpace(fmt.Sprint(item["claimed_by_machine_id"]))
	if claimedBy != cfg.MachineID {
		return "", fmt.Errorf("migration export is not claimed by this machine")
	}
	switch status {
	case "imported", "deleting", "deleted":
		return status, nil
	case "importing":
		if _, ok := userDataMigrationCleanupPendingExports.Load(userDataMigrationCleanupPendingKey(cfg, exportID)); ok {
			return status, nil
		}
		return "", fmt.Errorf("migration export is not ready for cleanup retry")
	default:
		return "", fmt.Errorf("migration export is not ready for cleanup retry")
	}
}

func userDataMigrationCleanupPendingKey(cfg userDataMigrationClientConfig, exportID string) string {
	parts := []string{
		strings.TrimRight(strings.TrimSpace(cfg.HubURL), "/"),
		strings.TrimSpace(cfg.TenantID),
		strings.TrimSpace(cfg.UserID),
		strings.TrimSpace(cfg.MachineID),
		strings.TrimSpace(exportID),
	}
	return strings.Join(parts, "\x00")
}

func (a *App) completeUserDataMigrationImportOnHub(ctx context.Context, cfg userDataMigrationClientConfig, exportID string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/complete", map[string]interface{}{}); err != nil {
			lastErr = err
		} else {
			return nil
		}
		if attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 700 * time.Millisecond):
		}
	}
	return lastErr
}

func (a *App) createUserDataMigrationTempDir(pattern string) (string, error) {
	root := filepath.Join(a.GetTempDir(), "user-data-migration")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, pattern)
}

func (a *App) buildUserDataMigrationPackage(ctx context.Context, cfg userDataMigrationClientConfig, workDir string) (string, userDataMigrationManifest, error) {
	payloadDir := filepath.Join(workDir, "payload")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return "", userDataMigrationManifest{}, fmt.Errorf("memory store is not initialized")
	}
	entries := a.memoryStore.List("", "")
	memPath := filepath.Join(payloadDir, "memory_entries.json")
	if err := userDataMigrationWriteJSONFile(memPath, entries); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	knowledgePath := filepath.Join(payloadDir, "knowledge_snapshot.jsonl")
	knowledgeResult, err := a.KnowledgeExportSnapshotWithOptions(knowledge.ExportOptions{
		OutputPath:      knowledgePath,
		RedactSensitive: false,
	})
	if err != nil {
		return "", userDataMigrationManifest{}, err
	}
	assetBytes := int64(0)
	assetsDir := filepath.Join(a.GetDataDir(), "knowledge_assets")
	if st, err := os.Stat(assetsDir); err == nil {
		if !st.IsDir() {
			return "", userDataMigrationManifest{}, fmt.Errorf("knowledge_assets is not a directory")
		}
		assetBytes, err = userDataMigrationCopyDirInto(payloadDir, assetsDir, "knowledge_assets")
		if err != nil {
			return "", userDataMigrationManifest{}, err
		}
	} else if !os.IsNotExist(err) {
		return "", userDataMigrationManifest{}, fmt.Errorf("read knowledge_assets: %w", err)
	}
	manifest := userDataMigrationManifest{
		Version:        userDataMigrationPackageVersion,
		CreatedAt:      time.Now().UTC(),
		TenantID:       cfg.TenantID,
		TenantName:     cfg.TenantName,
		UserID:         cfg.UserID,
		Email:          cfg.Email,
		MachineID:      cfg.MachineID,
		MachineName:    cfg.MachineName,
		MemoryEntries:  len(entries),
		KnowledgeBytes: knowledgeResult.Bytes,
		AssetBytes:     assetBytes,
		Meta:           map[string]interface{}{"host": "gui", "contains": []string{"memory", "knowledge", "knowledge_assets"}},
	}
	files, err := userDataMigrationDigestDir(payloadDir)
	if err != nil {
		return "", userDataMigrationManifest{}, err
	}
	manifest.Files = files
	if err := userDataMigrationWriteJSONFile(filepath.Join(payloadDir, "manifest.json"), manifest); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	zipPath := filepath.Join(workDir, "migration.zip")
	if err := userDataMigrationZipDir(payloadDir, zipPath); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	_ = ctx
	return zipPath, manifest, nil
}

func (a *App) restoreUserDataMigrationPackage(ctx context.Context, zipPath, workDir string) (map[string]interface{}, error) {
	payloadDir := filepath.Join(workDir, "payload")
	if err := userDataMigrationUnzipToDir(zipPath, payloadDir, userDataMigrationMaxExpanded); err != nil {
		return nil, err
	}
	var manifest userDataMigrationManifest
	if err := userDataMigrationReadJSONFile(filepath.Join(payloadDir, "manifest.json"), &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != userDataMigrationPackageVersion {
		return nil, fmt.Errorf("unsupported migration package version %q", manifest.Version)
	}
	if err := userDataMigrationVerifyFileDigests(payloadDir, manifest.Files); err != nil {
		return nil, err
	}
	var entries []memory.Entry
	if err := userDataMigrationReadJSONFile(filepath.Join(payloadDir, "memory_entries.json"), &entries); err != nil {
		return nil, err
	}
	knowledgePath := filepath.Join(payloadDir, "knowledge_snapshot.jsonl")
	if err := a.validateUserDataMigrationKnowledgeSnapshot(knowledgePath); err != nil {
		return nil, err
	}

	memoryCount, rollbackMemory, err := a.applyUserDataMigrationMemory(entries)
	if err != nil {
		return nil, err
	}
	assetSrc := filepath.Join(payloadDir, "knowledge_assets")
	assetBytes, rollbackAssets, commitAssets, err := a.replaceUserDataMigrationKnowledgeAssets(assetSrc, workDir)
	if err != nil {
		return nil, userDataMigrationRollbackError(err, rollbackMemory)
	}
	knowledgeResult, err := a.importUserDataMigrationKnowledgeSnapshot(knowledgePath, workDir)
	if err != nil {
		return nil, userDataMigrationRollbackError(err, rollbackAssets, rollbackMemory)
	}
	if commitAssets != nil {
		commitAssets()
	}
	_ = ctx
	return map[string]interface{}{
		"memory": map[string]interface{}{
			"entries": memoryCount,
		},
		"knowledge": knowledgeResult,
		"assets": map[string]interface{}{
			"bytes": assetBytes,
		},
		"manifest": manifest,
	}, nil
}

func (a *App) restoreUserDataMigrationMemory(entries []memory.Entry) error {
	_, _, err := a.applyUserDataMigrationMemory(entries)
	return err
}

func (a *App) applyUserDataMigrationMemory(entries []memory.Entry) (int, func() error, error) {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return 0, nil, fmt.Errorf("memory store is not initialized")
	}
	cleaned, err := userDataMigrationCleanMemoryEntries(entries)
	if err != nil {
		return 0, nil, err
	}
	backup := a.memoryStore.List("", "")
	if err := a.replaceUserDataMigrationMemoryEntries(cleaned); err != nil {
		return 0, nil, userDataMigrationRollbackError(err, func() error {
			return a.replaceUserDataMigrationMemoryEntries(backup)
		})
	}
	rollback := func() error {
		return a.replaceUserDataMigrationMemoryEntries(backup)
	}
	return len(cleaned), rollback, nil
}

func (a *App) replaceUserDataMigrationMemoryEntries(entries []memory.Entry) error {
	if a.memoryStore == nil {
		return fmt.Errorf("memory store is not initialized")
	}
	incoming := map[string]struct{}{}
	for _, entry := range entries {
		incoming[entry.ID] = struct{}{}
	}
	current := a.memoryStore.List("", "")
	deleteIDs := make([]string, 0, len(current))
	for _, entry := range current {
		if _, keep := incoming[entry.ID]; !keep {
			deleteIDs = append(deleteIDs, entry.ID)
		}
	}
	if len(entries) > 0 {
		if err := a.memoryStore.UpsertEntriesByID(entries); err != nil {
			return err
		}
	}
	if len(deleteIDs) > 0 {
		if err := a.memoryStore.UpdateEntriesAndDeleteIDs(nil, deleteIDs); err != nil {
			return err
		}
	}
	return a.memoryStore.Flush()
}

func userDataMigrationCleanMemoryEntries(entries []memory.Entry) ([]memory.Entry, error) {
	cleaned := make([]memory.Entry, 0, len(entries))
	for i, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Content = strings.TrimSpace(entry.Content)
		if entry.ID == "" {
			return nil, fmt.Errorf("migration memory entry %d has empty id", i+1)
		}
		if entry.Content == "" {
			return nil, fmt.Errorf("migration memory entry %s has empty content", entry.ID)
		}
		if err := memory.ScanForInjection(entry.Content); err != nil {
			return nil, fmt.Errorf("migration memory entry %s is invalid: %w", entry.ID, err)
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned, nil
}

func (a *App) validateUserDataMigrationKnowledgeSnapshot(path string) error {
	validateDir, err := a.createUserDataMigrationTempDir("knowledge-validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(validateDir)
	store, err := knowledge.NewSQLiteStore(filepath.Join(validateDir, "knowledge.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	result, err := store.ImportSnapshot(a.knowledgeContext(), knowledge.SnapshotImportOptions{
		InputPath:        path,
		DryRun:           true,
		Overwrite:        true,
		SkipSafetyBackup: true,
	})
	if err != nil {
		return err
	}
	return userDataMigrationKnowledgeImportError(result)
}

func (a *App) importUserDataMigrationKnowledgeSnapshot(path, workDir string) (knowledge.SnapshotImportResult, error) {
	result, err := a.KnowledgeImportSnapshot(knowledge.SnapshotImportOptions{
		InputPath:        path,
		Overwrite:        true,
		ReplaceAll:       true,
		AbortOnError:     true,
		SkipSafetyBackup: true,
	})
	if err != nil {
		return result, err
	}
	if err := userDataMigrationKnowledgeImportError(result); err != nil {
		return result, err
	}
	atomic.StoreInt64(&knowledgeSourceCountCache, int64(result.Sources))
	atomic.StoreInt64(&knowledgeSourceCountTime, time.Now().Unix())
	_ = workDir
	return result, nil
}

func userDataMigrationKnowledgeImportError(result knowledge.SnapshotImportResult) error {
	if result.Failed == 0 && result.UnknownRecords == 0 && result.MissingReferences == 0 && result.Conflicts == 0 {
		return nil
	}
	parts := []string{}
	if result.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed records", result.Failed))
	}
	if result.UnknownRecords > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown records", result.UnknownRecords))
	}
	if result.MissingReferences > 0 {
		parts = append(parts, fmt.Sprintf("%d missing references", result.MissingReferences))
	}
	if result.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", result.Conflicts))
	}
	if len(result.Failures) > 0 {
		parts = append(parts, "first error: "+result.Failures[0].Error)
	}
	return fmt.Errorf("knowledge snapshot validation failed: %s", strings.Join(parts, ", "))
}

func (a *App) replaceUserDataMigrationKnowledgeAssets(assetSrc, workDir string) (int64, func() error, func(), error) {
	dataDir := a.GetDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return 0, nil, nil, err
	}
	assetDest := filepath.Join(dataDir, "knowledge_assets")
	backupDir := filepath.Join(workDir, "knowledge_assets.backup")
	hadDest := false
	if _, err := os.Stat(assetDest); err == nil {
		hadDest = true
		if err := os.RemoveAll(backupDir); err != nil {
			return 0, nil, nil, err
		}
		if err := os.Rename(assetDest, backupDir); err != nil {
			return 0, nil, nil, fmt.Errorf("backup existing knowledge assets: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return 0, nil, nil, err
	}

	rolledBack := false
	rollback := func() error {
		rolledBack = true
		var failures []string
		if err := os.RemoveAll(assetDest); err != nil {
			failures = append(failures, err.Error())
		}
		if hadDest {
			if err := os.Rename(backupDir, assetDest); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	}
	commit := func() {
		if !rolledBack && hadDest {
			_ = os.RemoveAll(backupDir)
		}
	}

	assetBytes := int64(0)
	if st, err := os.Stat(assetSrc); err == nil {
		if !st.IsDir() {
			return 0, rollback, commit, userDataMigrationRollbackError(fmt.Errorf("knowledge_assets in migration package is not a directory"), rollback)
		}
		n, err := userDataMigrationCopyDirInto(dataDir, assetSrc, "knowledge_assets")
		if err != nil {
			return 0, rollback, commit, userDataMigrationRollbackError(err, rollback)
		}
		assetBytes = n
	} else if !os.IsNotExist(err) {
		return 0, rollback, commit, userDataMigrationRollbackError(err, rollback)
	}
	return assetBytes, rollback, commit, nil
}

func userDataMigrationRollbackError(primary error, rollbacks ...func() error) error {
	if primary == nil {
		return nil
	}
	var failures []string
	for _, rollback := range rollbacks {
		if rollback == nil {
			continue
		}
		if err := rollback(); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) == 0 {
		return primary
	}
	return fmt.Errorf("%w; rollback failed: %s", primary, strings.Join(failures, "; "))
}

func (a *App) uploadUserDataMigrationChunks(ctx context.Context, cfg userDataMigrationClientConfig, exportID, path string, size int64, chunkCount int, progress func(float64, string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, userDataMigrationChunkSize)
	for i := 0; i < chunkCount; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := f.ReadAt(buf, int64(i)*userDataMigrationChunkSize)
		if err != nil && err != io.EOF {
			return err
		}
		expectedSize := userDataMigrationChunkSize
		if remaining := size - int64(i)*userDataMigrationChunkSize; remaining < expectedSize {
			expectedSize = remaining
		}
		if expectedSize <= 0 || int64(n) != expectedSize {
			return fmt.Errorf("migration chunk %d size mismatch", i)
		}
		chunk := buf[:n]
		sha := sha256.Sum256(chunk)
		shaHex := hex.EncodeToString(sha[:])
		if !a.userDataMigrationChunkUploaded(ctx, cfg, exportID, i, shaHex) {
			var uploadErr error
			for attempt := 0; attempt < 3; attempt++ {
				uploadErr = a.userDataMigrationHubRaw(ctx, cfg, http.MethodPut, fmt.Sprintf("/api/v1/migration/exports/%s/chunks/%d?sha256=%s", exportID, i, shaHex), bytes.NewReader(chunk))
				if uploadErr == nil || a.userDataMigrationChunkUploaded(ctx, cfg, exportID, i, shaHex) {
					uploadErr = nil
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
				}
			}
			if uploadErr != nil {
				return uploadErr
			}
		}
		progress(0.30+0.55*float64(i+1)/float64(chunkCount), fmt.Sprintf("uploaded %d/%d chunks", i+1, chunkCount))
	}
	return nil
}

func (a *App) userDataMigrationChunkUploaded(ctx context.Context, cfg userDataMigrationClientConfig, exportID string, index int, sha string) bool {
	status, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodGet, fmt.Sprintf("/api/v1/migration/exports/%s/chunks/%d/status", exportID, index), nil)
	if err != nil {
		return false
	}
	return status["uploaded"] == true && strings.EqualFold(fmt.Sprint(status["sha256"]), sha)
}

func (a *App) downloadUserDataMigrationChunks(ctx context.Context, cfg userDataMigrationClientConfig, exportID, path string, size, chunkSize int64, chunkCount int, progress func(float64, string)) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	for i := 0; i < chunkCount; i++ {
		data, err := a.userDataMigrationHubBytes(ctx, cfg, http.MethodGet, fmt.Sprintf("/api/v1/migration/imports/%s/chunks/%d", exportID, i), nil)
		if err != nil {
			return err
		}
		expectedSize := chunkSize
		if remaining := size - int64(i)*chunkSize; remaining < expectedSize {
			expectedSize = remaining
		}
		if expectedSize <= 0 || int64(len(data)) != expectedSize {
			return fmt.Errorf("downloaded migration chunk %d size mismatch", i)
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
		progress(0.12+0.55*float64(i+1)/float64(chunkCount), fmt.Sprintf("downloaded %d/%d chunks", i+1, chunkCount))
	}
	if st, err := out.Stat(); err != nil {
		return err
	} else if st.Size() != size {
		return fmt.Errorf("downloaded package size mismatch")
	}
	return nil
}

func encryptUserDataMigrationFile(inPath, outPath, password, plainHash string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer in.Close()
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer outFile.Close()
	salt := userDataMigrationRandomBytes(16)
	nonce := userDataMigrationRandomBytes(12)
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	header := userDataMigrationEncryptedHeader{Version: userDataMigrationPackageVersion, KDF: "argon2id", Time: 3, MemoryKB: 64 * 1024, Threads: 4, Salt: salt, Nonce: nonce, PlainHash: plainHash, Stream: true, ChunkSize: userDataMigrationAEADChunkSize}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := outFile.Write([]byte(userDataMigrationMagic)); err != nil {
		return err
	}
	if err := binary.Write(outFile, binary.BigEndian, uint32(len(headerJSON))); err != nil {
		return err
	}
	if _, err := outFile.Write(headerJSON); err != nil {
		return err
	}
	buf := make([]byte, userDataMigrationAEADChunkSize)
	for index := uint64(0); ; index++ {
		n, readErr := in.Read(buf)
		if n > 0 {
			chunkNonce, err := userDataMigrationChunkNonce(nonce, index)
			if err != nil {
				return err
			}
			ciphertext := gcm.Seal(nil, chunkNonce, buf[:n], userDataMigrationChunkAAD(headerJSON, index))
			if err := binary.Write(outFile, binary.BigEndian, uint32(len(ciphertext))); err != nil {
				return err
			}
			if _, err := outFile.Write(ciphertext); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return outFile.Close()
}

func decryptUserDataMigrationFile(inPath, outPath, password, expectedPlainHash string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer in.Close()
	magic := make([]byte, len(userDataMigrationMagic))
	if _, err := io.ReadFull(in, magic); err != nil || string(magic) != userDataMigrationMagic {
		return fmt.Errorf("invalid encrypted migration package")
	}
	var headerLen uint32
	if err := binary.Read(in, binary.BigEndian, &headerLen); err != nil {
		return fmt.Errorf("invalid encrypted migration header")
	}
	if headerLen == 0 || headerLen > 1<<20 {
		return fmt.Errorf("invalid encrypted migration header")
	}
	headerJSON := make([]byte, headerLen)
	if _, err := io.ReadFull(in, headerJSON); err != nil {
		return fmt.Errorf("invalid encrypted migration header")
	}
	var header userDataMigrationEncryptedHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return err
	}
	if err := validateUserDataMigrationEncryptedHeader(header); err != nil {
		return err
	}
	key := argon2.IDKey([]byte(password), header.Salt, header.Time, header.MemoryKB, header.Threads, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	h := sha256.New()
	if !header.Stream {
		ciphertext, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		plain, err := gcm.Open(nil, header.Nonce, ciphertext, headerJSON)
		if err != nil {
			return fmt.Errorf("migration password is incorrect or package is corrupted")
		}
		if _, err := h.Write(plain); err != nil {
			return err
		}
		if _, err := out.Write(plain); err != nil {
			return err
		}
		return finishUserDataMigrationDecryption(out, h, expectedPlainHash, header.PlainHash)
	}
	if header.ChunkSize <= 0 || header.ChunkSize > 32<<20 {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	for index := uint64(0); ; index++ {
		var frameLen uint32
		if err := binary.Read(in, binary.BigEndian, &frameLen); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("invalid encrypted migration frame")
		}
		if frameLen == 0 || int64(frameLen) > header.ChunkSize+int64(gcm.Overhead()) {
			return fmt.Errorf("invalid encrypted migration frame size")
		}
		ciphertext := make([]byte, frameLen)
		if _, err := io.ReadFull(in, ciphertext); err != nil {
			return fmt.Errorf("invalid encrypted migration frame")
		}
		chunkNonce, err := userDataMigrationChunkNonce(header.Nonce, index)
		if err != nil {
			return err
		}
		plain, err := gcm.Open(nil, chunkNonce, ciphertext, userDataMigrationChunkAAD(headerJSON, index))
		if err != nil {
			return fmt.Errorf("migration password is incorrect or package is corrupted")
		}
		if _, err := h.Write(plain); err != nil {
			return err
		}
		if _, err := out.Write(plain); err != nil {
			return err
		}
	}
	return finishUserDataMigrationDecryption(out, h, expectedPlainHash, header.PlainHash)
}

func validateUserDataMigrationEncryptedHeader(header userDataMigrationEncryptedHeader) error {
	if header.Version != userDataMigrationPackageVersion || header.KDF != "argon2id" || len(header.Salt) != 16 || len(header.Nonce) != 12 {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	if header.Time == 0 || header.Time > 10 || header.MemoryKB < 1024 || header.MemoryKB > 256*1024 || header.Threads == 0 || header.Threads > 8 {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	return nil
}

func userDataMigrationChunkNonce(base []byte, index uint64) ([]byte, error) {
	if len(base) != 12 {
		return nil, fmt.Errorf("invalid encrypted migration nonce")
	}
	nonce := append([]byte(nil), base...)
	for i := 0; i < 8; i++ {
		nonce[len(nonce)-1-i] ^= byte(index >> (8 * i))
	}
	return nonce, nil
}

func userDataMigrationChunkAAD(headerJSON []byte, index uint64) []byte {
	var idx [8]byte
	binary.BigEndian.PutUint64(idx[:], index)
	aad := make([]byte, 0, len(headerJSON)+len(idx))
	aad = append(aad, headerJSON...)
	aad = append(aad, idx[:]...)
	return aad
}

func finishUserDataMigrationDecryption(out *os.File, h interface {
	io.Writer
	Sum([]byte) []byte
}, expectedPlainHash, headerPlainHash string) error {
	got := hex.EncodeToString(h.Sum(nil))
	want := firstNonEmpty(expectedPlainHash, headerPlainHash)
	if want != "" && !strings.EqualFold(got, want) {
		return fmt.Errorf("decrypted package hash mismatch")
	}
	return out.Close()
}

func (a *App) userDataMigrationHubGetCurrent(ctx context.Context, cfg userDataMigrationClientConfig) (interface{}, int64, error) {
	out, err := a.userDataMigrationHubJSON(ctx, cfg, http.MethodGet, "/api/v1/migration/exports/current", nil)
	if err != nil {
		return nil, 0, err
	}
	return out["export"], int64(userDataMigrationNumberFromMap(out, "max_compressed_bytes")), nil
}

func (a *App) userDataMigrationHubJSON(ctx context.Context, cfg userDataMigrationClientConfig, method, path string, body interface{}) (map[string]interface{}, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	data, err := a.userDataMigrationHubBytes(ctx, cfg, method, path, reader, "application/json")
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func (a *App) userDataMigrationHubRaw(ctx context.Context, cfg userDataMigrationClientConfig, method, path string, body io.Reader) error {
	_, err := a.userDataMigrationHubBytes(ctx, cfg, method, path, body)
	return err
}

func (a *App) userDataMigrationHubBytes(ctx context.Context, cfg userDataMigrationClientConfig, method, path string, body io.Reader, contentType ...string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, cfg.HubURL+path, body)
	if err != nil {
		return nil, err
	}
	if cfg.ViewerToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ViewerToken)
		req.Header.Set("X-MaClaw-Machine-ID", cfg.MachineID)
	} else {
		req.Header.Set("Authorization", "Bearer "+cfg.MachineToken)
		req.Header.Set("X-Machine-ID", cfg.MachineID)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil && method != http.MethodGet && len(contentType) > 0 && strings.TrimSpace(contentType[0]) != "" {
		req.Header.Set("Content-Type", strings.TrimSpace(contentType[0]))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Hub migration API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func userDataMigrationFileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func userDataMigrationDigestDir(root string) ([]userDataMigrationFileDigest, error) {
	var out []userDataMigrationFileDigest
	rootManifest := filepath.Join(root, "manifest.json")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration package contains unsupported symlink: %s", path)
		}
		if d.IsDir() || path == rootManifest {
			return nil
		}
		sha, size, err := userDataMigrationFileSHA256(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, userDataMigrationFileDigest{Path: filepath.ToSlash(rel), Bytes: size, SHA256: sha})
		return nil
	})
	return out, err
}

func userDataMigrationVerifyFileDigests(root string, files []userDataMigrationFileDigest) error {
	expected := map[string]struct{}{"manifest.json": {}}
	for _, file := range files {
		cleanPath := filepath.ToSlash(strings.TrimSpace(file.Path))
		if cleanPath == "" {
			return fmt.Errorf("migration manifest contains empty file path")
		}
		expected[cleanPath] = struct{}{}
		path, err := userDataMigrationSafeJoin(root, cleanPath)
		if err != nil {
			return err
		}
		sha, size, err := userDataMigrationFileSHA256(path)
		if err != nil {
			return err
		}
		if size != file.Bytes || !strings.EqualFold(sha, file.SHA256) {
			return fmt.Errorf("migration file hash mismatch: %s", file.Path)
		}
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, ok := expected[filepath.ToSlash(rel)]; !ok {
			return fmt.Errorf("migration package contains unexpected file: %s", filepath.ToSlash(rel))
		}
		return nil
	})
}

func userDataMigrationZipDir(root, zipPath string) error {
	out, err := os.OpenFile(zipPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
	}()
	zw := zip.NewWriter(out)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration package contains unsupported symlink: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	closed = true
	return out.Close()
}

func userDataMigrationUnzipToDir(zipPath, dest string, maxExpandedBytes int64) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > userDataMigrationMaxZipFiles {
		return fmt.Errorf("migration package contains too many files")
	}
	expandedBytes := uint64(0)
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip contains unsupported symlink entry: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if maxExpandedBytes > 0 && f.UncompressedSize64 > uint64(maxExpandedBytes)-expandedBytes {
			return fmt.Errorf("migration package expands beyond %s", userDataMigrationFormatBytes(maxExpandedBytes))
		}
		outPath, err := userDataMigrationSafeJoin(dest, f.Name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			rc.Close()
			return err
		}
		limited := &io.LimitedReader{R: rc, N: int64(f.UncompressedSize64) + 1}
		n, copyErr := io.Copy(out, limited)
		closeErr := out.Close()
		rcErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if uint64(n) != f.UncompressedSize64 {
			return fmt.Errorf("zip entry size mismatch: %s", f.Name)
		}
		expandedBytes += uint64(n)
		if closeErr != nil {
			return closeErr
		}
		if rcErr != nil {
			return rcErr
		}
	}
	return nil
}

func userDataMigrationCopyDirInto(destRoot, srcRoot, destName string) (int64, error) {
	var total int64
	destBase, err := userDataMigrationSafeJoin(destRoot, destName)
	if err != nil {
		return 0, err
	}
	err = filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration package contains unsupported symlink: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		dest, err := userDataMigrationSafeJoin(destBase, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			in.Close()
			return err
		}
		n, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		inErr := in.Close()
		total += n
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return inErr
	})
	return total, err
}

func userDataMigrationSafeJoin(root, name string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	out := filepath.Join(rootAbs, filepath.FromSlash(strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/")))
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	rootCompare := rootAbs
	outCompare := outAbs
	if runtime.GOOS == "windows" {
		rootCompare = strings.ToLower(rootCompare)
		outCompare = strings.ToLower(outCompare)
	}
	if outCompare != rootCompare && !strings.HasPrefix(outCompare, rootCompare+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	return outAbs, nil
}

func userDataMigrationWriteJSONFile(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func userDataMigrationReadJSONFile(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func userDataMigrationRandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func userDataMigrationNumberFromMap(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func userDataMigrationFormatBytes(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
