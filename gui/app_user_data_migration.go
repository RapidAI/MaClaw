package main

import (
	"archive/zip"
	"bufio"
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
	"log"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/gui/petpack"
	"golang.org/x/crypto/argon2"
)

const (
	userDataMigrationPackageVersion       = "maclaw-gui-user-data-migration/v2"
	userDataMigrationLegacyVersion        = "maclaw-gui-user-data-migration/v1"
	userDataMigrationConfigSchema         = "corelib.AppConfig/v1"
	userDataMigrationChunkSize            = int64(4 << 20)
	userDataMigrationAEADChunkSize        = int64(4 << 20)
	userDataMigrationMaxExpanded          = int64(4) << 30
	userDataMigrationMaxZipFiles          = 200000
	userDataMigrationLocalRestoreAttempts = 4
	userDataMigrationMaxManifest          = 16 << 20
	userDataMigrationMaxConfigJSON        = int64(64 << 20)
	userDataMigrationMaxMemoryJSON        = int64(256 << 20)
	userDataMigrationMaxDownload          = int64(2) << 30
	userDataMigrationMinChunkSize         = int64(256 << 10)
	userDataMigrationMaxChunkSize         = int64(8 << 20)
	userDataMigrationMaxChunks            = 8192
	userDataMigrationMaxJSONDepth         = 128
	userDataMigrationMagic                = "MLMIG01"
	userDataMigrationJobRetention         = 24 * time.Hour
	userDataMigrationMinPasswordLen       = 12
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
	Version          string                        `json:"version"`
	CreatedAt        time.Time                     `json:"created_at"`
	TenantID         string                        `json:"tenant_id,omitempty"`
	TenantName       string                        `json:"tenant_name,omitempty"`
	UserID           string                        `json:"user_id,omitempty"`
	Email            string                        `json:"email,omitempty"`
	MachineID        string                        `json:"machine_id,omitempty"`
	MachineName      string                        `json:"machine_name,omitempty"`
	MemoryEntries    int                           `json:"memory_entries"`
	KnowledgeBytes   int64                         `json:"knowledge_bytes"`
	AssetBytes       int64                         `json:"asset_bytes"`
	PetPackBytes     int64                         `json:"pet_pack_bytes"`
	PetPacksIncluded bool                          `json:"pet_packs_included,omitempty"`
	ExpertBytes      int64                         `json:"expert_bytes"`
	ExpertsIncluded  bool                          `json:"experts_included,omitempty"`
	ConfigSchema     string                        `json:"config_schema_version,omitempty"`
	ConfigSections   int                           `json:"config_section_count,omitempty"`
	SecretCount      int                           `json:"secret_count,omitempty"`
	ExcludedConfig   []string                      `json:"excluded_config_paths,omitempty"`
	Files            []userDataMigrationFileDigest `json:"files"`
	Meta             map[string]interface{}        `json:"meta,omitempty"`
}

type userDataMigrationConfigPolicy struct {
	SchemaVersion  string   `json:"schema_version"`
	Restore        string   `json:"restore"`
	PreserveTarget []string `json:"preserve_target"`
	RewriteTarget  []string `json:"rewrite_for_target"`
	SkipRuntime    []string `json:"skip_runtime"`
}

type userDataMigrationSecretInventory struct {
	SchemaVersion string   `json:"schema_version"`
	Paths         []string `json:"paths"`
}

var userDataMigrationPreserveTargetConfigPaths = []string{
	"remote_hub_id", "remote_hub_url", "remote_hubcenter_url", "remote_hubcenter_urls",
	"remote_enabled", "remote_email", "remote_mobile", "remote_sn", "remote_user_id", "remote_tenant_id",
	"remote_tenant_name", "remote_machine_id", "remote_machine_name", "remote_machine_token",
	"remote_viewer_token", "skill_market_session_token", "remote_nickname", "remote_client_id",
	"hub_security_centralized",
	// These values identify local projects, devices, permissions or model files
	// and cannot safely be reused on another machine.
	"projects", "current_project", "external_skill_dirs", "audio_input_device_id", "audio_output_device_id",
	"noise_floor_calibrated", "speech_level_calibrated", "local_needle_model_path", "ve_allowed_directories",
	"lansenger_group_allow_all_directories", "lansenger_group_allowed_directories",
}

var userDataMigrationRewriteTargetConfigPaths = []string{
	"data_dir", "working_directory",
}

var userDataMigrationSkipRuntimeConfigPaths = []string{
	"env_check_done", "last_env_check_time", "onboarding_done", "llm_token_usage",
	"floating_btn_x", "floating_btn_y", "floating_btn_position_set",
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
	userDataMigrationConfigRestoreApps     sync.Map
	userDataMigrationJobStartMu            sync.Mutex
)

func userDataMigrationIsConfigRestore(app *App) bool {
	_, ok := userDataMigrationConfigRestoreApps.Load(app)
	return ok
}

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
	if err := validateUserDataMigrationPassword(password); err != nil {
		return userDataMigrationJob{}, err
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

func validateUserDataMigrationPassword(password string) error {
	if len([]rune(password)) < userDataMigrationMinPasswordLen {
		return fmt.Errorf("migration password must be at least %d characters", userDataMigrationMinPasswordLen)
	}
	var hasLetter, hasNumber bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsNumber(r):
			hasNumber = true
		}
	}
	if !hasLetter || !hasNumber {
		return fmt.Errorf("migration password must contain letters and numbers")
	}
	return nil
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
	log.Printf("[onboarding-migration] job_started job_id=%s kind=%s", job.ID, kind)
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
			log.Printf("[onboarding-migration] job_progress job_id=%s kind=%s progress=%.2f stage=%q", jobID, kind, v, text)
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
		if err != nil {
			log.Printf("[onboarding-migration] job_failed job_id=%s kind=%s elapsed=%s err=%v", jobID, kind, time.Since(now), err)
		} else {
			log.Printf("[onboarding-migration] job_succeeded job_id=%s kind=%s elapsed=%s cleanup_pending=%t", jobID, kind, time.Since(now), result != nil && result["cleanup_pending"] == true)
		}
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
	startedAt := time.Now()
	log.Printf("[onboarding-migration] import_begin export_id=%s tenant_id=%s user_id=%s machine_id=%s", exportID, cfg.TenantID, cfg.UserID, cfg.MachineID)
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
			log.Printf("[onboarding-migration] import_abort_claim export_id=%s elapsed=%s err=%v", exportID, time.Since(startedAt), err)
			abortCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = a.userDataMigrationHubJSON(abortCtx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/abort", map[string]interface{}{})
		}
	}()
	exportMap, _ := claimResp["export"].(map[string]interface{})
	claimedStatus := strings.ToLower(strings.TrimSpace(fmt.Sprint(exportMap["status"])))
	log.Printf("[onboarding-migration] import_claimed export_id=%s status=%s", exportID, claimedStatus)
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
			claimed = false // This package was already restored; never abort it on cleanup failure.
			userDataMigrationCleanupPendingExports.Store(userDataMigrationCleanupPendingKey(cfg, exportID), time.Now().UTC())
			return nil, fmt.Errorf("Hub cleanup retry failed: %w", err)
		}
		claimed = false
		userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, exportID))
		progress(1, "import cleanup completed")
		return map[string]interface{}{"export_id": exportID, "cleanup_retried": true}, nil
	}
	chunkCount, chunkSize, encryptedSize, compressedSize, encryptedHash, plainHash, err := userDataMigrationDownloadMetadata(exportMap)
	if err != nil {
		return nil, err
	}
	log.Printf("[onboarding-migration] import_metadata export_id=%s chunks=%d chunk_size=%d encrypted_size=%d compressed_size=%d", exportID, chunkCount, chunkSize, encryptedSize, compressedSize)
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
	if err := decryptUserDataMigrationFile(encryptedPath, plainPath, password, plainHash, compressedSize); err != nil {
		return nil, err
	}
	if gotPlain, _, err := userDataMigrationFileSHA256(plainPath); err != nil {
		return nil, err
	} else if !strings.EqualFold(gotPlain, plainHash) {
		return nil, fmt.Errorf("decrypted package hash mismatch")
	}
	progress(0.82, "restoring local memory and knowledge base")
	result, err = a.restoreUserDataMigrationPackageWithRetry(ctx, plainPath, workDir, progress)
	if err != nil {
		// Keep the restore phase in the job error. The UI can then distinguish a
		// local restore failure from a transfer failure and show the actionable
		// underlying cause (for example a locked knowledge database or no disk
		// space) instead of suggesting that the network is at fault.
		return nil, fmt.Errorf("restore local migration data: %w", err)
	}
	localRestored = true
	log.Printf("[onboarding-migration] import_local_restore_committed export_id=%s elapsed=%s", exportID, time.Since(startedAt))
	progress(0.90, "validating restored LLM providers")
	result["llm_validation"] = userDataMigrationSafeLLMValidation(a.validateUserDataMigrationLLMProviders)
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.completeUserDataMigrationImportOnHub(cleanupCtx, cfg, exportID); err != nil {
		log.Printf("[onboarding-migration] import_cleanup_pending export_id=%s elapsed=%s err=%v", exportID, time.Since(startedAt), err)
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
	log.Printf("[onboarding-migration] import_complete export_id=%s elapsed=%s", exportID, time.Since(startedAt))
	return result, nil
}

// dryRunUserDataMigrationImport verifies a Hub migration package end-to-end
// without touching the target machine's configuration, memory, knowledge base,
// or attachments. Hub currently requires an import claim before encrypted chunks
// can be downloaded, so this method always releases that claim before returning.
func (a *App) dryRunUserDataMigrationImport(ctx context.Context, cfg userDataMigrationClientConfig, exportID, password string, progress func(float64, string)) (result map[string]interface{}, err error) {
	workDir, err := a.createUserDataMigrationTempDir("migration-import-dry-run-*")
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
	defer func() {
		if !claimed {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, abortErr := a.userDataMigrationHubJSON(abortCtx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/abort", map[string]interface{}{}); abortErr != nil {
			if err == nil {
				err = fmt.Errorf("migration dry-run succeeded but could not release Hub claim: %w", abortErr)
			} else {
				err = fmt.Errorf("%w; could not release Hub claim: %v", err, abortErr)
			}
		}
	}()

	exportMap, _ := claimResp["export"].(map[string]interface{})
	chunkCount, chunkSize, encryptedSize, compressedSize, encryptedHash, plainHash, err := userDataMigrationDownloadMetadata(exportMap)
	if err != nil {
		return nil, err
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
	if err := decryptUserDataMigrationFile(encryptedPath, plainPath, password, plainHash, compressedSize); err != nil {
		return nil, err
	}
	if gotPlain, _, err := userDataMigrationFileSHA256(plainPath); err != nil {
		return nil, err
	} else if !strings.EqualFold(gotPlain, plainHash) {
		return nil, fmt.Errorf("decrypted package hash mismatch")
	}

	progress(0.82, "validating migration package without restoring")
	return a.validateUserDataMigrationPackageForDryRun(ctx, plainPath, workDir)
}

func (a *App) validateUserDataMigrationPackageForDryRun(ctx context.Context, zipPath, workDir string) (map[string]interface{}, error) {
	payloadDir := filepath.Join(workDir, "dry-run-payload")
	if err := userDataMigrationUnzipToDir(zipPath, payloadDir, userDataMigrationMaxExpanded); err != nil {
		return nil, err
	}
	var manifest userDataMigrationManifest
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "manifest.json"), &manifest, userDataMigrationMaxManifest); err != nil {
		return nil, err
	}
	if manifest.Version != userDataMigrationPackageVersion && manifest.Version != userDataMigrationLegacyVersion {
		return nil, fmt.Errorf("unsupported migration package version %q", manifest.Version)
	}
	if manifest.MemoryEntries < 0 || manifest.KnowledgeBytes < 0 || manifest.AssetBytes < 0 || manifest.PetPackBytes < 0 || manifest.ExpertBytes < 0 {
		return nil, fmt.Errorf("migration manifest contains invalid counts")
	}
	if err := userDataMigrationVerifyFileDigests(payloadDir, manifest.Files); err != nil {
		return nil, err
	}
	if err := validateUserDataMigrationManifestFileStats(manifest); err != nil {
		return nil, err
	}
	if manifest.ExpertsIncluded {
		if err := userDataMigrationValidateExperts(filepath.Join(payloadDir, "experts")); err != nil {
			return nil, err
		}
	}
	var entries []memory.Entry
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "memory_entries.json"), &entries, userDataMigrationMaxMemoryJSON); err != nil {
		return nil, err
	}
	if len(entries) != manifest.MemoryEntries {
		return nil, fmt.Errorf("migration memory entry count mismatch")
	}
	if _, err := userDataMigrationCleanMemoryEntries(entries); err != nil {
		return nil, err
	}
	knowledgePath := filepath.Join(payloadDir, "knowledge_snapshot.jsonl")
	repairedKnowledgePath, knowledgeRepair, repairErr := userDataMigrationRepairKnowledgeSnapshot(knowledgePath, filepath.Join(workDir, "knowledge_snapshot.repaired.jsonl"))
	if repairErr != nil {
		return nil, repairErr
	}
	knowledgePath = repairedKnowledgePath
	if err := a.validateUserDataMigrationKnowledgeSnapshot(knowledgePath); err != nil {
		return nil, err
	}
	configSections := 0
	secretCount := 0
	if manifest.Version == userDataMigrationPackageVersion {
		if manifest.ConfigSchema != userDataMigrationConfigSchema {
			return nil, fmt.Errorf("unsupported migration config schema %q", manifest.ConfigSchema)
		}
		if manifest.ConfigSections < 0 || manifest.SecretCount < 0 {
			return nil, fmt.Errorf("migration configuration metadata is invalid")
		}
		var incomingConfig map[string]interface{}
		if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "config", "app_config.json"), &incomingConfig, userDataMigrationMaxConfigJSON); err != nil {
			return nil, fmt.Errorf("read migration system configuration: %w", err)
		}
		if len(incomingConfig) != manifest.ConfigSections {
			return nil, fmt.Errorf("migration configuration section count mismatch")
		}
		if err := validateUserDataMigrationConfigMetadata(payloadDir, manifest, incomingConfig); err != nil {
			return nil, err
		}
		if err := userDataMigrationValidateConfigKeys(incomingConfig); err != nil {
			return nil, err
		}
		configSections = len(incomingConfig)
		secretCount = len(userDataMigrationSecretPaths(incomingConfig, ""))
	}
	_ = ctx
	return map[string]interface{}{
		"dry_run":          true,
		"memory":           map[string]interface{}{"entries": len(entries)},
		"knowledge":        map[string]interface{}{"validated": true, "bytes": manifest.KnowledgeBytes},
		"knowledge_repair": knowledgeRepair,
		"assets":           map[string]interface{}{"bytes": manifest.AssetBytes},
		"pet_packs":        map[string]interface{}{"included": manifest.PetPacksIncluded, "bytes": manifest.PetPackBytes},
		"experts":          map[string]interface{}{"included": manifest.ExpertsIncluded, "bytes": manifest.ExpertBytes},
		"config":           map[string]interface{}{"sections": configSections, "secrets": secretCount},
		"manifest":         manifest,
	}, nil
}

func (a *App) restoreUserDataMigrationPackageWithRetry(ctx context.Context, zipPath, workDir string, progress func(float64, string)) (map[string]interface{}, error) {
	var lastErr error
	for attempt := 1; attempt <= userDataMigrationLocalRestoreAttempts; attempt++ {
		result, err := a.restoreUserDataMigrationPackage(ctx, zipPath, workDir)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt == userDataMigrationLocalRestoreAttempts || !userDataMigrationRetryableLocalRestoreError(err) {
			break
		}

		delay := time.Duration(attempt) * time.Second
		if progress != nil {
			progress(0.82, fmt.Sprintf("local restore temporarily busy; retrying (%d/%d)", attempt+1, userDataMigrationLocalRestoreAttempts))
		}
		log.Printf("[onboarding-migration] local_restore_retry attempt=%d/%d delay=%s err=%v", attempt, userDataMigrationLocalRestoreAttempts, delay, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func userDataMigrationRetryableLocalRestoreError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked",
		"database table is locked",
		"database is busy",
		"resource temporarily unavailable",
		"sharing violation",
		"being used by another process",
		"file is being used by another process",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func userDataMigrationSafeLLMValidation(validate func() map[string]interface{}) (result map[string]interface{}) {
	defer func() {
		if recover() != nil {
			// Connectivity validation is diagnostic. A provider implementation must
			// not turn an already committed local restore into a failed import or
			// expose panic details that may contain request metadata.
			result = map[string]interface{}{
				"tested": 0, "passed": 0, "failed": 0,
				"providers": []map[string]interface{}{}, "status": "validation_unavailable",
			}
		}
	}()
	if validate == nil {
		return map[string]interface{}{
			"tested": 0, "passed": 0, "failed": 0,
			"providers": []map[string]interface{}{}, "status": "validation_unavailable",
		}
	}
	return validate()
}

func (a *App) validateUserDataMigrationLLMProviders() map[string]interface{} {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]interface{}{"tested": 0, "passed": 0, "failed": 0, "providers": []map[string]interface{}{}, "status": "config_unavailable"}
	}
	providers := append([]corelib.MaclawLLMProvider(nil), cfg.MaclawLLMProviders...)
	if len(providers) == 0 && strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != "" {
		providers = append(providers, corelib.MaclawLLMProvider{
			Name: cfg.MaclawLLMCurrentProvider, URL: cfg.MaclawLLMUrl, Key: cfg.MaclawLLMKey,
			Model: cfg.MaclawLLMModel, Protocol: cfg.MaclawLLMProtocol, ContextLength: cfg.MaclawLLMContextLength,
			TimeoutSec: cfg.MaclawLLMTimeoutSec,
		})
	}
	items := make([]map[string]interface{}, 0, len(providers))
	passed := 0
	for i, provider := range providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = fmt.Sprintf("Provider %d", i+1)
		}
		item := map[string]interface{}{"name": name, "ok": false}
		if strings.TrimSpace(provider.URL) == "" || strings.TrimSpace(provider.Model) == "" {
			item["status"] = "incomplete"
		} else if _, err := a.TestMaclawLLM(corelib.MaclawLLMConfig{
			URL: provider.URL, Key: provider.Key, Model: provider.Model, Protocol: provider.Protocol,
			ContextLength: provider.ContextLength, TimeoutSec: provider.TimeoutSec, MaxOutputTokens: provider.MaxOutputTokens,
			SupportsVision: provider.SupportsVision, AgentType: provider.AgentType, WireAPI: provider.WireAPI,
			ProviderName: name, AuthType: provider.AuthType,
		}); err != nil {
			// Do not expose provider errors here: upstream failures may contain request
			// metadata. The settings test action remains available for diagnostics.
			item["status"] = "connection_failed"
		} else {
			item["ok"] = true
			item["status"] = "passed"
			passed++
		}
		items = append(items, item)
	}
	return map[string]interface{}{
		"tested": len(items), "passed": passed, "failed": len(items) - passed, "providers": items,
	}
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
	if err := os.MkdirAll(payloadDir, 0o700); err != nil {
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
	appConfig, err := a.LoadConfig()
	if err != nil {
		return "", userDataMigrationManifest{}, fmt.Errorf("load system configuration: %w", err)
	}
	migrationConfig, secretPaths, err := userDataMigrationExportableConfig(appConfig)
	if err != nil {
		return "", userDataMigrationManifest{}, err
	}
	configDir := filepath.Join(payloadDir, "config")
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "app_config.json"), migrationConfig); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	policy := userDataMigrationConfigPolicy{
		SchemaVersion:  userDataMigrationConfigSchema,
		Restore:        "all_fields_except_explicit_exclusions",
		PreserveTarget: append([]string(nil), userDataMigrationPreserveTargetConfigPaths...),
		RewriteTarget:  append([]string(nil), userDataMigrationRewriteTargetConfigPaths...),
		SkipRuntime:    append([]string(nil), userDataMigrationSkipRuntimeConfigPaths...),
	}
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "migration_policy.json"), policy); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "secret_inventory.json"), userDataMigrationSecretInventory{
		SchemaVersion: userDataMigrationConfigSchema,
		Paths:         secretPaths,
	}); err != nil {
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
	petPackBytes := int64(0)
	petPacksDir := a.userDataMigrationPetPacksDir()
	if st, err := os.Stat(petPacksDir); err == nil {
		if !st.IsDir() {
			return "", userDataMigrationManifest{}, fmt.Errorf("pet-packs is not a directory")
		}
		petPackBytes, err = userDataMigrationCopyDirInto(payloadDir, petPacksDir, "pet_packs")
		if err != nil {
			return "", userDataMigrationManifest{}, err
		}
	} else if !os.IsNotExist(err) {
		return "", userDataMigrationManifest{}, fmt.Errorf("read pet-packs: %w", err)
	}
	// Built-in experts ship with every app installation and must not be part of
	// a migration package. Export only persisted user-created, overridden, or
	// installed definitions and their relevant local sync/install state.
	expertPath := filepath.Join(payloadDir, "experts", "experts.json")
	experts, err := userDataMigrationExportableExperts(filepath.Join(a.userDataMigrationExpertsDir(), "experts.json"))
	if err != nil {
		return "", userDataMigrationManifest{}, err
	}
	if err := userDataMigrationWriteJSONFile(expertPath, experts); err != nil {
		return "", userDataMigrationManifest{}, err
	}
	_, expertBytes, err := userDataMigrationFileSHA256(expertPath)
	if err != nil {
		return "", userDataMigrationManifest{}, err
	}
	manifest := userDataMigrationManifest{
		Version:          userDataMigrationPackageVersion,
		CreatedAt:        time.Now().UTC(),
		TenantID:         cfg.TenantID,
		TenantName:       cfg.TenantName,
		UserID:           cfg.UserID,
		Email:            cfg.Email,
		MachineID:        cfg.MachineID,
		MachineName:      cfg.MachineName,
		MemoryEntries:    len(entries),
		KnowledgeBytes:   knowledgeResult.Bytes,
		AssetBytes:       assetBytes,
		PetPackBytes:     petPackBytes,
		PetPacksIncluded: true,
		ExpertBytes:      expertBytes,
		ExpertsIncluded:  true,
		ConfigSchema:     userDataMigrationConfigSchema,
		ConfigSections:   len(migrationConfig),
		SecretCount:      len(secretPaths),
		ExcludedConfig:   userDataMigrationExcludedConfigPaths(),
		Meta:             map[string]interface{}{"host": "gui", "contains": []string{"config", "memory", "knowledge", "knowledge_assets", "pet_packs", "experts"}},
	}
	if manifest.KnowledgeBytes < 0 || manifest.AssetBytes < 0 || manifest.PetPackBytes < 0 || manifest.ExpertBytes < 0 {
		return "", userDataMigrationManifest{}, fmt.Errorf("migration data size is invalid")
	}
	files, err := userDataMigrationDigestDir(payloadDir)
	if err != nil {
		return "", userDataMigrationManifest{}, err
	}
	manifest.Files = files
	if encoded, err := json.Marshal(manifest); err != nil {
		return "", userDataMigrationManifest{}, err
	} else if len(encoded) > userDataMigrationMaxManifest {
		return "", userDataMigrationManifest{}, fmt.Errorf("migration manifest exceeds %s", userDataMigrationFormatBytes(userDataMigrationMaxManifest))
	}
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
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "manifest.json"), &manifest, userDataMigrationMaxManifest); err != nil {
		return nil, err
	}
	if manifest.Version != userDataMigrationPackageVersion && manifest.Version != userDataMigrationLegacyVersion {
		return nil, fmt.Errorf("unsupported migration package version %q", manifest.Version)
	}
	if manifest.MemoryEntries < 0 || manifest.KnowledgeBytes < 0 || manifest.AssetBytes < 0 || manifest.PetPackBytes < 0 || manifest.ExpertBytes < 0 {
		return nil, fmt.Errorf("migration manifest contains invalid counts")
	}
	if err := userDataMigrationVerifyFileDigests(payloadDir, manifest.Files); err != nil {
		return nil, err
	}
	if err := validateUserDataMigrationManifestFileStats(manifest); err != nil {
		return nil, err
	}
	var entries []memory.Entry
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "memory_entries.json"), &entries, userDataMigrationMaxMemoryJSON); err != nil {
		return nil, err
	}
	if len(entries) != manifest.MemoryEntries {
		return nil, fmt.Errorf("migration memory entry count mismatch")
	}
	knowledgePath := filepath.Join(payloadDir, "knowledge_snapshot.jsonl")
	knowledgePath, knowledgeRepair, err := userDataMigrationRepairKnowledgeSnapshot(knowledgePath, filepath.Join(workDir, "knowledge_snapshot.repaired.jsonl"))
	if err != nil {
		return nil, err
	}
	if err := a.validateUserDataMigrationKnowledgeSnapshot(knowledgePath); err != nil {
		return nil, err
	}
	var incomingConfig map[string]interface{}
	if manifest.Version == userDataMigrationPackageVersion {
		if manifest.ConfigSchema != userDataMigrationConfigSchema {
			return nil, fmt.Errorf("unsupported migration config schema %q", manifest.ConfigSchema)
		}
		if manifest.ConfigSections < 0 || manifest.SecretCount < 0 {
			return nil, fmt.Errorf("migration configuration metadata is invalid")
		}
		if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "config", "app_config.json"), &incomingConfig, userDataMigrationMaxConfigJSON); err != nil {
			return nil, fmt.Errorf("read migration system configuration: %w", err)
		}
		if len(incomingConfig) != manifest.ConfigSections {
			return nil, fmt.Errorf("migration configuration section count mismatch")
		}
		if err := validateUserDataMigrationConfigMetadata(payloadDir, manifest, incomingConfig); err != nil {
			return nil, err
		}
	}
	rollbackKnowledge, err := a.prepareUserDataMigrationKnowledgeRollback(workDir)
	if err != nil {
		return nil, fmt.Errorf("backup target knowledge base: %w", err)
	}

	configSections, secretCount, rollbackConfig, err := a.applyUserDataMigrationConfig(incomingConfig)
	if err != nil {
		return nil, err
	}
	memoryCount, rollbackMemory, err := a.applyUserDataMigrationMemory(entries)
	if err != nil {
		return nil, userDataMigrationRollbackError(err, rollbackConfig)
	}
	assetSrc := filepath.Join(payloadDir, "knowledge_assets")
	assetBytes, rollbackAssets, commitAssets, err := a.replaceUserDataMigrationKnowledgeAssets(assetSrc, workDir)
	if err != nil {
		return nil, userDataMigrationRollbackError(err, rollbackMemory, rollbackConfig)
	}
	petPackBytes := int64(0)
	var rollbackPetPacks func() error
	var commitPetPacks func()
	if manifest.PetPacksIncluded {
		petPackSrc := filepath.Join(payloadDir, "pet_packs")
		petPackBytes, rollbackPetPacks, commitPetPacks, err = a.replaceUserDataMigrationPetPacks(petPackSrc, workDir)
		if err != nil {
			return nil, userDataMigrationRollbackError(err, rollbackAssets, rollbackMemory, rollbackConfig)
		}
	}
	expertBytes := int64(0)
	var rollbackExperts func() error
	var commitExperts func()
	if manifest.ExpertsIncluded {
		expertSrc := filepath.Join(payloadDir, "experts")
		if err := userDataMigrationValidateExperts(expertSrc); err != nil {
			return nil, userDataMigrationRollbackError(err, rollbackPetPacks, rollbackAssets, rollbackMemory, rollbackConfig)
		}
		expertBytes, rollbackExperts, commitExperts, err = a.restoreUserDataMigrationExperts(expertSrc, workDir)
		if err != nil {
			return nil, userDataMigrationRollbackError(err, rollbackPetPacks, rollbackAssets, rollbackMemory, rollbackConfig)
		}
	}
	knowledgeResult, err := a.importUserDataMigrationKnowledgeSnapshot(knowledgePath, workDir)
	if err != nil {
		return nil, userDataMigrationRollbackError(err, rollbackKnowledge, rollbackExperts, rollbackPetPacks, rollbackAssets, rollbackMemory, rollbackConfig)
	}
	if commitAssets != nil {
		commitAssets()
	}
	if commitPetPacks != nil {
		commitPetPacks()
	}
	if commitExperts != nil {
		commitExperts()
	}
	if manifest.ExpertsIncluded {
		invalidateUserDataMigrationExpertCache()
	}
	if userDataMigrationSamePath(petpack.UserPacksDir(), a.userDataMigrationPetPacksDir()) {
		_ = petpack.EnsureGlobal().Scan()
	}
	_ = ctx
	return map[string]interface{}{
		"memory": map[string]interface{}{
			"entries": memoryCount,
		},
		"knowledge":        knowledgeResult,
		"knowledge_repair": knowledgeRepair,
		"assets": map[string]interface{}{
			"bytes": assetBytes,
		},
		"pet_packs": map[string]interface{}{
			"included": manifest.PetPacksIncluded,
			"bytes":    petPackBytes,
		},
		"experts": map[string]interface{}{
			"included": manifest.ExpertsIncluded,
			"bytes":    expertBytes,
		},
		"config": map[string]interface{}{
			"sections": configSections,
			"secrets":  secretCount,
			"schema":   manifest.ConfigSchema,
		},
		"manifest": manifest,
	}, nil
}

// userDataMigrationRepairKnowledgeSnapshot removes orphaned cards and facts
// from an otherwise valid snapshot. Older knowledge-store versions could leave
// cards pointing to document nodes already removed during source maintenance.
// Those records cannot be restored faithfully and previously made the whole
// migration fail at 82%. Valid records are preserved verbatim.
func userDataMigrationRepairKnowledgeSnapshot(inputPath, outputPath string) (string, map[string]int, error) {
	type record struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	type nodeRef struct {
		ID string `json:"id"`
	}
	type cardRef struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
	}
	type factRef struct {
		CardID string `json:"card_id"`
	}

	// A migration snapshot can be multiple gigabytes. Do not retain every JSONL
	// record while repairing it: collect the two small reference sets in separate
	// passes, then stream the repaired output in a third pass.
	forEachRecord := func(fn func(record, string) error) error {
		in, err := os.Open(inputPath)
		if err != nil {
			return err
		}
		defer in.Close()
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 1024*1024), 128*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var item record
			if err := json.Unmarshal([]byte(line), &item); err != nil {
				return fmt.Errorf("read migration knowledge snapshot: %w", err)
			}
			if err := fn(item, line); err != nil {
				return err
			}
		}
		return scanner.Err()
	}

	nodeIDs := make(map[string]struct{})
	if err := forEachRecord(func(item record, _ string) error {
		if item.Type == "node" {
			var node nodeRef
			if err := json.Unmarshal(item.Data, &node); err != nil {
				return fmt.Errorf("read migration knowledge node: %w", err)
			}
			if node.ID = strings.TrimSpace(node.ID); node.ID != "" {
				nodeIDs[node.ID] = struct{}{}
			}
		}
		return nil
	}); err != nil {
		return "", nil, err
	}

	validCards := make(map[string]struct{})
	if err := forEachRecord(func(item record, _ string) error {
		if item.Type != "card" {
			return nil
		}
		var card cardRef
		if err := json.Unmarshal(item.Data, &card); err != nil {
			return fmt.Errorf("read migration knowledge card: %w", err)
		}
		if card.ID = strings.TrimSpace(card.ID); card.ID == "" {
			return nil
		}
		if nodeID := strings.TrimSpace(card.NodeID); nodeID == "" {
			validCards[card.ID] = struct{}{}
			return nil
		} else if _, ok := nodeIDs[nodeID]; ok {
			validCards[card.ID] = struct{}{}
		}
		return nil
	}); err != nil {
		return "", nil, err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return "", nil, err
	}
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, err
	}
	completed := false
	defer func() {
		if !completed {
			_ = out.Close()
			_ = os.Remove(outputPath)
		}
	}()
	writer := bufio.NewWriterSize(out, 1024*1024)
	repair := map[string]int{"orphaned_cards": 0, "orphaned_facts": 0}
	if err := forEachRecord(func(item record, line string) error {
		skip := false
		switch item.Type {
		case "card":
			var card cardRef
			if err := json.Unmarshal(item.Data, &card); err != nil {
				return fmt.Errorf("read migration knowledge card: %w", err)
			}
			_, keep := validCards[strings.TrimSpace(card.ID)]
			skip = !keep
			if skip {
				repair["orphaned_cards"]++
			}
		case "fact":
			var fact factRef
			if err := json.Unmarshal(item.Data, &fact); err != nil {
				return fmt.Errorf("read migration knowledge fact: %w", err)
			}
			_, keep := validCards[strings.TrimSpace(fact.CardID)]
			skip = !keep
			if skip {
				repair["orphaned_facts"]++
			}
		}
		if skip {
			return nil
		}
		if _, err := writer.WriteString(line); err != nil {
			return fmt.Errorf("write repaired migration knowledge snapshot: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write repaired migration knowledge snapshot: %w", err)
		}
		return nil
	}); err != nil {
		return "", nil, err
	}
	if err := writer.Flush(); err != nil {
		return "", nil, fmt.Errorf("flush repaired migration knowledge snapshot: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", nil, fmt.Errorf("close repaired migration knowledge snapshot: %w", err)
	}
	completed = true
	return outputPath, repair, nil
}

func (a *App) restoreUserDataMigrationMemory(entries []memory.Entry) error {
	_, _, err := a.applyUserDataMigrationMemory(entries)
	return err
}

func userDataMigrationExportableConfig(cfg corelib.AppConfig) (map[string]interface{}, []string, error) {
	out, err := userDataMigrationCompleteConfigMap(cfg)
	if err != nil {
		return nil, nil, err
	}
	for _, path := range userDataMigrationExcludedConfigPaths() {
		delete(out, path)
	}
	secretPaths := userDataMigrationSecretPaths(out, "")
	return out, secretPaths, nil
}

// userDataMigrationCompleteConfigMap serializes every top-level AppConfig field,
// including zero, empty and nil values hidden by omitempty. Migration is a full
// replacement for portable settings, so an empty source value must be able to
// clear a non-empty value on the target machine.
func userDataMigrationCompleteConfigMap(cfg corelib.AppConfig) (map[string]interface{}, error) {
	typeOfConfig := reflect.TypeOf(cfg)
	valueOfConfig := reflect.ValueOf(cfg)
	out := make(map[string]interface{}, typeOfConfig.NumField())
	for i := 0; i < typeOfConfig.NumField(); i++ {
		field := typeOfConfig.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			name = field.Name
		}
		if name == "-" {
			continue
		}
		raw, err := json.Marshal(valueOfConfig.Field(i).Interface())
		if err != nil {
			return nil, fmt.Errorf("marshal system configuration field %s: %w", name, err)
		}
		var normalized interface{}
		if err := json.Unmarshal(raw, &normalized); err != nil {
			return nil, fmt.Errorf("normalize system configuration field %s: %w", name, err)
		}
		out[name] = normalized
	}
	return out, nil
}

func userDataMigrationValidateConfigKeys(incoming map[string]interface{}) error {
	known, err := userDataMigrationCompleteConfigMap(corelib.AppConfig{})
	if err != nil {
		return err
	}
	for key := range incoming {
		if _, ok := known[key]; !ok {
			return fmt.Errorf("migration configuration contains unsupported field %q", key)
		}
	}
	return nil
}

func userDataMigrationSecretPaths(value interface{}, prefix string) []string {
	var out []string
	switch current := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if userDataMigrationSecretField(key) && userDataMigrationHasSecretValue(current[key]) {
				out = append(out, path)
				continue
			}
			out = append(out, userDataMigrationSecretPaths(current[key], path)...)
		}
	case []interface{}:
		for i, item := range current {
			out = append(out, userDataMigrationSecretPaths(item, fmt.Sprintf("%s[%d]", prefix, i))...)
		}
	}
	return out
}

func userDataMigrationSecretField(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("-", "_", " ", "_").Replace(key)
	compactKey := strings.ReplaceAll(key, "_", "")
	for _, marker := range []string{
		"api_key", "apikey", "password", "secret", "passphrase", "authorization", "auth_header",
		"access_key", "private_key", "credential", "cookie", "bearer",
	} {
		if strings.Contains(key, marker) || strings.Contains(compactKey, strings.ReplaceAll(marker, "_", "")) {
			return true
		}
	}
	if strings.HasSuffix(compactKey, "token") && !strings.HasSuffix(compactKey, "tokenbudget") {
		return true
	}
	return key == "key" || strings.HasSuffix(key, "_key")
}

func userDataMigrationHasSecretValue(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []interface{}:
		return len(v) > 0
	case map[string]interface{}:
		return len(v) > 0
	default:
		return value != nil
	}
}

func validateUserDataMigrationConfigMetadata(payloadDir string, manifest userDataMigrationManifest, incomingConfig map[string]interface{}) error {
	var policy userDataMigrationConfigPolicy
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "config", "migration_policy.json"), &policy, userDataMigrationMaxManifest); err != nil {
		return fmt.Errorf("read migration configuration policy: %w", err)
	}
	if policy.SchemaVersion != userDataMigrationConfigSchema ||
		policy.Restore != "all_fields_except_explicit_exclusions" ||
		!userDataMigrationStringSlicesEqual(policy.PreserveTarget, userDataMigrationPreserveTargetConfigPaths) ||
		!userDataMigrationStringSlicesEqual(policy.RewriteTarget, userDataMigrationRewriteTargetConfigPaths) ||
		!userDataMigrationStringSlicesEqual(policy.SkipRuntime, userDataMigrationSkipRuntimeConfigPaths) {
		return fmt.Errorf("migration configuration policy is incompatible with this version")
	}

	var inventory userDataMigrationSecretInventory
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(payloadDir, "config", "secret_inventory.json"), &inventory, userDataMigrationMaxManifest); err != nil {
		return fmt.Errorf("read migration secret inventory: %w", err)
	}
	actualSecretPaths := userDataMigrationSecretPaths(incomingConfig, "")
	actualExcludedPaths := userDataMigrationExcludedConfigPaths()
	if inventory.SchemaVersion != userDataMigrationConfigSchema ||
		len(inventory.Paths) != manifest.SecretCount ||
		!userDataMigrationStringSlicesEqual(inventory.Paths, actualSecretPaths) {
		return fmt.Errorf("migration secret inventory is inconsistent")
	}
	if !userDataMigrationStringSlicesEqual(manifest.ExcludedConfig, actualExcludedPaths) {
		return fmt.Errorf("migration excluded configuration metadata is inconsistent")
	}
	return nil
}

func userDataMigrationStringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (a *App) applyUserDataMigrationConfig(incoming map[string]interface{}) (int, int, func() error, error) {
	if len(incoming) == 0 {
		return 0, 0, nil, nil
	}
	if err := userDataMigrationValidateConfigKeys(incoming); err != nil {
		return 0, 0, nil, err
	}
	current, err := a.LoadConfig()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("load target system configuration: %w", err)
	}
	currentRaw, err := json.Marshal(current)
	if err != nil {
		return 0, 0, nil, err
	}
	var merged map[string]interface{}
	if err := json.Unmarshal(currentRaw, &merged); err != nil {
		return 0, 0, nil, err
	}
	for key, value := range incoming {
		if userDataMigrationContainsPath(userDataMigrationExcludedConfigPaths(), key) {
			continue
		}
		merged[key] = value
	}
	mergedRaw, err := json.Marshal(merged)
	if err != nil {
		return 0, 0, nil, err
	}
	var restored corelib.AppConfig
	if err := json.Unmarshal(mergedRaw, &restored); err != nil {
		return 0, 0, nil, fmt.Errorf("validate migrated system configuration: %w", err)
	}
	secretCount := len(userDataMigrationSecretPaths(incoming, ""))
	if err := a.saveUserDataMigrationConfig(restored); err != nil {
		return 0, 0, nil, fmt.Errorf("restore system configuration: %w", err)
	}
	rollback := func() error { return a.saveUserDataMigrationConfig(current) }
	return len(incoming), secretCount, rollback, nil
}

func (a *App) saveUserDataMigrationConfig(cfg corelib.AppConfig) error {
	userDataMigrationConfigRestoreApps.Store(a, struct{}{})
	defer userDataMigrationConfigRestoreApps.Delete(a)
	return a.SaveConfig(cfg)
}

func userDataMigrationContainsPath(paths []string, value string) bool {
	for _, item := range paths {
		if item == value {
			return true
		}
	}
	return false
}

func userDataMigrationExcludedConfigPaths() []string {
	paths := make([]string, 0, len(userDataMigrationPreserveTargetConfigPaths)+len(userDataMigrationRewriteTargetConfigPaths)+len(userDataMigrationSkipRuntimeConfigPaths))
	paths = append(paths, userDataMigrationPreserveTargetConfigPaths...)
	paths = append(paths, userDataMigrationRewriteTargetConfigPaths...)
	paths = append(paths, userDataMigrationSkipRuntimeConfigPaths...)
	return paths
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

func (a *App) prepareUserDataMigrationKnowledgeRollback(workDir string) (func() error, error) {
	backupPath := filepath.Join(workDir, "knowledge_snapshot.backup.jsonl")
	if _, err := a.KnowledgeExportSnapshotWithOptions(knowledge.ExportOptions{
		OutputPath:      backupPath,
		RedactSensitive: false,
	}); err != nil {
		return nil, err
	}
	return func() error {
		result, err := a.KnowledgeImportSnapshot(knowledge.SnapshotImportOptions{
			InputPath:        backupPath,
			Overwrite:        true,
			ReplaceAll:       true,
			AbortOnError:     true,
			SkipSafetyBackup: true,
		})
		if err != nil {
			return err
		}
		if err := userDataMigrationKnowledgeImportError(result); err != nil {
			return err
		}
		atomic.StoreInt64(&knowledgeSourceCountCache, int64(result.Sources))
		atomic.StoreInt64(&knowledgeSourceCountTime, time.Now().Unix())
		return nil
	}, nil
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
	return userDataMigrationReplaceDirectory(
		assetSrc,
		filepath.Join(a.GetDataDir(), "knowledge_assets"),
		workDir,
		"knowledge_assets.backup",
		"knowledge_assets",
	)
}

// userDataMigrationExpertsDir resolves the expert store directory from this
// App's effective data root so source and target migration roots stay isolated.
func (a *App) userDataMigrationExpertsDir() string {
	return filepath.Join(a.GetDataDir(), "experts")
}

func (a *App) restoreUserDataMigrationExperts(source, workDir string) (int64, func() error, func(), error) {
	currentPath := filepath.Join(a.userDataMigrationExpertsDir(), "experts.json")
	incomingPath := filepath.Join(source, "experts.json")
	var incoming expertStoreFile
	if err := userDataMigrationReadJSONFileLimited(incomingPath, &incoming, userDataMigrationMaxConfigJSON); err != nil {
		return 0, nil, nil, fmt.Errorf("read migration experts store: %w", err)
	}
	current, err := userDataMigrationReadExpertStore(currentPath)
	if err != nil {
		return 0, nil, nil, err
	}
	merged := userDataMigrationMergeExpertStores(current, incoming)
	stagingDir := filepath.Join(workDir, "experts")
	stagingPath := filepath.Join(stagingDir, "experts.json")
	if err := userDataMigrationWriteJSONFile(stagingPath, merged); err != nil {
		return 0, nil, nil, err
	}
	return userDataMigrationReplaceDirectory(stagingDir, a.userDataMigrationExpertsDir(), workDir, "experts.backup", "experts")
}

func userDataMigrationReadExpertStore(path string) (expertStoreFile, error) {
	var store expertStoreFile
	if err := userDataMigrationReadJSONFileLimited(path, &store, userDataMigrationMaxConfigJSON); err != nil {
		if os.IsNotExist(err) {
			return expertStoreFile{}, nil
		}
		return expertStoreFile{}, fmt.Errorf("read target experts store: %w", err)
	}
	return store, nil
}

func userDataMigrationMergeExpertStores(current, incoming expertStoreFile) expertStoreFile {
	merged := expertStoreFile{
		Experts:           append([]ExpertDefinition(nil), incoming.Experts...),
		DeletedIDs:        userDataMigrationMergeExpertTimestamps(current.DeletedIDs, incoming.DeletedIDs),
		PendingHubUploads: mergeUserDataMigrationExpertFlags(current.PendingHubUploads, incoming.PendingHubUploads),
		PendingHubDeletes: userDataMigrationMergeExpertTimestamps(current.PendingHubDeletes, incoming.PendingHubDeletes),
		LocalOnlyIDs:      mergeUserDataMigrationExpertFlags(current.LocalOnlyIDs, incoming.LocalOnlyIDs),
		MarketInstallIDs:  mergeUserDataMigrationExpertFlags(current.MarketInstallIDs, incoming.MarketInstallIDs),
	}
	incomingIDs := make(map[string]struct{}, len(incoming.Experts))
	for _, expert := range incoming.Experts {
		incomingIDs[expert.ID] = struct{}{}
	}
	for _, expert := range current.Experts {
		if builtinExpertByID(expert.ID) != nil {
			if _, replaced := incomingIDs[expert.ID]; replaced {
				continue
			}
			merged.Experts = append(merged.Experts, expert)
		}
	}
	return merged
}

func userDataMigrationMergeExpertTimestamps(current, incoming map[string]string) map[string]string {
	if len(current) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]string, len(current)+len(incoming))
	for id, timestamp := range incoming {
		merged[id] = timestamp
	}
	for id, timestamp := range current {
		if builtinExpertByID(id) != nil {
			merged[id] = timestamp
		}
	}
	return merged
}

func mergeUserDataMigrationExpertFlags(current, incoming map[string]bool) map[string]bool {
	if len(current) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]bool, len(current)+len(incoming))
	for id, enabled := range incoming {
		if enabled {
			merged[id] = true
		}
	}
	for id, enabled := range current {
		if enabled && builtinExpertByID(id) != nil {
			merged[id] = true
		}
	}
	return merged
}

func userDataMigrationExportableExperts(path string) (expertStoreFile, error) {
	out := expertStoreFile{Experts: []ExpertDefinition{}}
	var source expertStoreFile
	if err := userDataMigrationReadJSONFileLimited(path, &source, userDataMigrationMaxConfigJSON); err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("read experts for migration: %w", err)
	}
	for _, expert := range source.Experts {
		if !expert.Builtin {
			out.Experts = append(out.Experts, expert)
		}
	}
	// Built-ins are not persisted by the normal expert store. Keep metadata for
	// stored records (including a user override of a builtin expert) because it
	// is part of the user's local customization and synchronization state, but
	// never carry state entries for an app-provided expert definition.
	out.DeletedIDs = userDataMigrationFilterSystemExpertTimestamps(source.DeletedIDs)
	out.PendingHubUploads = userDataMigrationFilterSystemExpertFlags(source.PendingHubUploads)
	out.PendingHubDeletes = userDataMigrationFilterSystemExpertTimestamps(source.PendingHubDeletes)
	out.LocalOnlyIDs = userDataMigrationFilterSystemExpertFlags(source.LocalOnlyIDs)
	out.MarketInstallIDs = userDataMigrationFilterSystemExpertFlags(source.MarketInstallIDs)
	return out, nil
}

func userDataMigrationFilterSystemExpertTimestamps(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for id, timestamp := range source {
		if builtinExpertByID(id) == nil {
			out[id] = timestamp
		}
	}
	return out
}

func userDataMigrationFilterSystemExpertFlags(source map[string]bool) map[string]bool {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]bool, len(source))
	for id, enabled := range source {
		if enabled && builtinExpertByID(id) == nil {
			out[id] = true
		}
	}
	return out
}

func userDataMigrationValidateExperts(source string) error {
	path := filepath.Join(source, "experts.json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read migration experts store: %w", err)
	}
	var data expertStoreFile
	if err := userDataMigrationReadJSONFileLimited(path, &data, userDataMigrationMaxConfigJSON); err != nil {
		return fmt.Errorf("read migration experts store: %w", err)
	}
	seen := make(map[string]struct{}, len(data.Experts))
	for _, expert := range data.Experts {
		id := strings.TrimSpace(expert.ID)
		if id == "" || !expertIDPattern.MatchString(id) {
			return fmt.Errorf("migration expert has invalid id %q", expert.ID)
		}
		if expert.Builtin {
			return fmt.Errorf("migration package must not contain system expert %q", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("migration experts contain duplicate id %q", id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(expert.Name) == "" {
			return fmt.Errorf("migration expert %q has empty name", id)
		}
	}
	return nil
}

func invalidateUserDataMigrationExpertCache() {
	expertDefCache.Range(func(key, _ interface{}) bool {
		expertDefCache.Delete(key)
		return true
	})
}

// userDataMigrationPetPacksDir resolves the pack directory from this App's
// effective data root. This keeps a migration independent from the process
// global pack registry, which may point at a different data root in tests.
func (a *App) userDataMigrationPetPacksDir() string {
	if env := strings.TrimSpace(os.Getenv("MACLAW_PET_PACKS_DIR")); env != "" {
		return filepath.Clean(env)
	}
	return filepath.Join(a.getMaclawBaseDir(), "pet-packs")
}

func userDataMigrationSamePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func (a *App) replaceUserDataMigrationPetPacks(source, workDir string) (int64, func() error, func(), error) {
	return userDataMigrationReplaceDirectory(source, a.userDataMigrationPetPacksDir(), workDir, "pet_packs.backup", "pet_packs")
}

func userDataMigrationReplaceDirectory(source, destination, workDir, backupName, packageName string) (int64, func() error, func(), error) {
	resolvedSource, err := filepath.Abs(source)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("resolve %s source: %w", packageName, err)
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("resolve %s destination: %w", packageName, err)
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("resolve migration work directory: %w", err)
	}
	backupDir, err := userDataMigrationSafeJoin(workDir, backupName)
	if err != nil {
		return 0, nil, nil, err
	}
	if userDataMigrationSamePath(destination, backupDir) || userDataMigrationPathContains(destination, backupDir) || userDataMigrationPathContains(backupDir, destination) {
		return 0, nil, nil, fmt.Errorf("unsafe %s backup location", packageName)
	}
	if userDataMigrationSamePath(resolvedSource, destination) || userDataMigrationPathContains(resolvedSource, destination) || userDataMigrationPathContains(destination, resolvedSource) ||
		userDataMigrationSamePath(resolvedSource, backupDir) || userDataMigrationPathContains(resolvedSource, backupDir) || userDataMigrationPathContains(backupDir, resolvedSource) {
		return 0, nil, nil, fmt.Errorf("unsafe %s source location", packageName)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return 0, nil, nil, err
	}
	hadDestination := false
	if _, err := os.Stat(destination); err == nil {
		hadDestination = true
		if err := os.RemoveAll(backupDir); err != nil {
			return 0, nil, nil, err
		}
		if err := os.Rename(destination, backupDir); err != nil {
			return 0, nil, nil, fmt.Errorf("backup existing %s: %w", packageName, err)
		}
	} else if !os.IsNotExist(err) {
		return 0, nil, nil, err
	}

	rolledBack := false
	rollback := func() error {
		rolledBack = true
		var failures []string
		if err := os.RemoveAll(destination); err != nil {
			failures = append(failures, err.Error())
		}
		if hadDestination {
			if err := os.Rename(backupDir, destination); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	}
	commit := func() {
		if !rolledBack && hadDestination {
			_ = os.RemoveAll(backupDir)
		}
	}

	if st, err := os.Stat(resolvedSource); err == nil {
		if !st.IsDir() {
			return 0, rollback, commit, userDataMigrationRollbackError(fmt.Errorf("%s in migration package is not a directory", packageName), rollback)
		}
		bytes, err := userDataMigrationCopyDirInto(filepath.Dir(destination), resolvedSource, filepath.Base(destination))
		if err != nil {
			return 0, rollback, commit, userDataMigrationRollbackError(err, rollback)
		}
		return bytes, rollback, commit, nil
	} else if !os.IsNotExist(err) {
		return 0, rollback, commit, userDataMigrationRollbackError(err, rollback)
	}
	return 0, rollback, commit, nil
}

func userDataMigrationPathContains(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if runtime.GOOS == "windows" {
		parent = strings.ToLower(parent)
		child = strings.ToLower(child)
	}
	return strings.HasPrefix(child, parent+string(os.PathSeparator))
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
	completed := false
	defer func() {
		_ = out.Close()
		if !completed {
			_ = os.Remove(path)
		}
	}()
	for i := 0; i < chunkCount; i++ {
		expectedSize := chunkSize
		if remaining := size - int64(i)*chunkSize; remaining < expectedSize {
			expectedSize = remaining
		}
		if expectedSize <= 0 {
			return fmt.Errorf("downloaded migration chunk %d size mismatch", i)
		}
		var data []byte
		for attempt := 0; attempt < 3; attempt++ {
			data, err = a.userDataMigrationHubBytesLimited(ctx, cfg, http.MethodGet, fmt.Sprintf("/api/v1/migration/imports/%s/chunks/%d", exportID, i), nil, expectedSize, "")
			if err == nil {
				break
			}
			if attempt == 2 || !userDataMigrationRetryableTransferError(err) {
				return err
			}
			if progress != nil {
				progress(0.12+0.55*float64(i)/float64(chunkCount), fmt.Sprintf("download temporarily failed; retrying chunk %d/%d (%d/3)", i+1, chunkCount, attempt+2))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
			}
		}
		if int64(len(data)) != expectedSize {
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
	if err := out.Close(); err != nil {
		return err
	}
	completed = true
	return nil
}

func userDataMigrationRetryableTransferError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"context deadline exceeded", "connection reset", "connection refused", "broken pipe",
		"unexpected eof", "transport is closing", "temporarily unavailable", "returned 429",
		"returned 500", "returned 502", "returned 503", "returned 504",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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

func decryptUserDataMigrationFile(inPath, outPath, password, expectedPlainHash string, expectedPlainSize ...int64) error {
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
	if err := userDataMigrationDecodeStrictJSON(headerJSON, &header); err != nil {
		return fmt.Errorf("invalid encrypted migration header: %w", err)
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
		if len(expectedPlainSize) > 0 && expectedPlainSize[0] > 0 && int64(len(plain)) != expectedPlainSize[0] {
			return fmt.Errorf("decrypted package size mismatch")
		}
		if _, err := out.Write(plain); err != nil {
			return err
		}
		return finishUserDataMigrationDecryption(out, h, expectedPlainHash, header.PlainHash)
	}
	if header.ChunkSize <= 0 || header.ChunkSize > 32<<20 {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	var plainBytes int64
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
		plainBytes += int64(len(plain))
		if len(expectedPlainSize) > 0 && expectedPlainSize[0] > 0 && plainBytes > expectedPlainSize[0] {
			return fmt.Errorf("decrypted package size mismatch")
		}
		if _, err := h.Write(plain); err != nil {
			return err
		}
		if _, err := out.Write(plain); err != nil {
			return err
		}
	}
	if len(expectedPlainSize) > 0 && expectedPlainSize[0] > 0 && plainBytes != expectedPlainSize[0] {
		return fmt.Errorf("decrypted package size mismatch")
	}
	return finishUserDataMigrationDecryption(out, h, expectedPlainHash, header.PlainHash)
}

func validateUserDataMigrationEncryptedHeader(header userDataMigrationEncryptedHeader) error {
	if (header.Version != userDataMigrationPackageVersion && header.Version != userDataMigrationLegacyVersion) || header.KDF != "argon2id" || len(header.Salt) != 16 || len(header.Nonce) != 12 {
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
		if err := userDataMigrationDecodeStrictJSON(data, &out); err != nil {
			return nil, fmt.Errorf("invalid Hub migration JSON response: %w", err)
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
	value := ""
	if len(contentType) > 0 {
		value = contentType[0]
	}
	return a.userDataMigrationHubBytesLimited(ctx, cfg, method, path, body, 32<<20, value)
}

func (a *App) userDataMigrationHubBytesLimited(ctx context.Context, cfg userDataMigrationClientConfig, method, path string, body io.Reader, maxResponseBytes int64, contentType string) ([]byte, error) {
	if maxResponseBytes <= 0 {
		return nil, fmt.Errorf("invalid Hub migration response size limit")
	}
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
	if body != nil && method != http.MethodGet && strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", strings.TrimSpace(contentType))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("read Hub migration response: %w", readErr)
	}
	if int64(len(data)) > maxResponseBytes {
		return nil, fmt.Errorf("Hub migration API %s %s response exceeds %s", method, path, userDataMigrationFormatBytes(maxResponseBytes))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Hub migration API %s %s returned %d: %s", method, path, resp.StatusCode, userDataMigrationHubErrorMessage(data))
	}
	return data, nil
}

func userDataMigrationHubErrorMessage(data []byte) string {
	const fallback = "request failed"
	if len(data) == 0 {
		return fallback
	}
	var payload map[string]interface{}
	if err := userDataMigrationDecodeStrictJSON(data, &payload); err != nil {
		return fallback
	}
	for _, value := range []interface{}{payload["message"], payload["code"]} {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			return userDataMigrationTruncateErrorText(text, 512)
		}
	}
	if nested, ok := payload["error"].(map[string]interface{}); ok {
		for _, value := range []interface{}{nested["message"], nested["code"]} {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
				return userDataMigrationTruncateErrorText(text, 512)
			}
		}
	}
	return fallback
}

func userDataMigrationTruncateErrorText(value string, maxRunes int) string {
	runes := []rune(value)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return value
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
	if err == nil {
		sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	}
	return out, err
}

func userDataMigrationVerifyFileDigests(root string, files []userDataMigrationFileDigest) error {
	expected := map[string]struct{}{"manifest.json": {}}
	for _, file := range files {
		cleanPath, key, err := userDataMigrationCanonicalRelativePath(file.Path)
		if err != nil {
			return fmt.Errorf("migration manifest contains invalid file path %q: %w", file.Path, err)
		}
		if key == "manifest.json" {
			return fmt.Errorf("migration manifest must not list itself")
		}
		if _, exists := expected[key]; exists {
			return fmt.Errorf("migration manifest contains duplicate file path: %s", cleanPath)
		}
		if file.Bytes < 0 || !userDataMigrationValidSHA256(file.SHA256) {
			return fmt.Errorf("migration manifest contains invalid file metadata: %s", cleanPath)
		}
		expected[key] = struct{}{}
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
		_, key, err := userDataMigrationCanonicalRelativePath(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("migration package contains unexpected file: %s", filepath.ToSlash(rel))
		}
		return nil
	})
}

func validateUserDataMigrationManifestFileStats(manifest userDataMigrationManifest) error {
	files := make(map[string]userDataMigrationFileDigest, len(manifest.Files))
	for _, file := range manifest.Files {
		clean, _, err := userDataMigrationCanonicalRelativePath(file.Path)
		if err != nil {
			return err
		}
		files[clean] = file
	}
	required := []string{"memory_entries.json", "knowledge_snapshot.jsonl"}
	if manifest.Version == userDataMigrationPackageVersion {
		required = append(required, "config/app_config.json", "config/migration_policy.json", "config/secret_inventory.json")
	}
	for _, name := range required {
		if _, ok := files[name]; !ok {
			return fmt.Errorf("migration manifest is missing required file: %s", name)
		}
	}
	if files["knowledge_snapshot.jsonl"].Bytes != manifest.KnowledgeBytes {
		return fmt.Errorf("migration knowledge byte count mismatch")
	}
	// pet_packs is a namespace directory, never a payload file. Rejecting a
	// file at its root lets dry-run fail before restore attempts to replace the
	// user's installed pack tree with an invalid path.
	if _, exists := files["pet_packs"]; exists {
		return fmt.Errorf("migration pet-pack root must be a directory")
	}
	// experts follows the same namespace convention as pet packs. It stores the
	// local expert definitions and Hub reconciliation metadata together.
	if _, exists := files["experts"]; exists {
		return fmt.Errorf("migration experts root must be a directory")
	}
	var assetBytes int64
	for name, file := range files {
		if strings.HasPrefix(name, "knowledge_assets/") {
			if file.Bytes > int64(^uint64(0)>>1)-assetBytes {
				return fmt.Errorf("migration asset byte count overflow")
			}
			assetBytes += file.Bytes
		}
	}
	if assetBytes != manifest.AssetBytes {
		return fmt.Errorf("migration asset byte count mismatch")
	}
	var petPackBytes int64
	petPackFileCount := 0
	for name, file := range files {
		if strings.HasPrefix(name, "pet_packs/") {
			if file.Bytes > int64(^uint64(0)>>1)-petPackBytes {
				return fmt.Errorf("migration pet-pack byte count overflow")
			}
			petPackBytes += file.Bytes
			petPackFileCount++
		}
	}
	if petPackBytes != manifest.PetPackBytes {
		return fmt.Errorf("migration pet-pack byte count mismatch")
	}
	// Packages created before pet packs joined the migration scope do not have
	// PetPacksIncluded set. Keep them compatible, but do not silently accept
	// unclaimed pet-pack payload files that would be ignored during restore.
	if !manifest.PetPacksIncluded && (petPackFileCount != 0 || manifest.PetPackBytes != 0) {
		return fmt.Errorf("migration package contains pet-pack data without declaring it")
	}
	var expertBytes int64
	expertFileCount := 0
	for name, file := range files {
		if strings.HasPrefix(name, "experts/") {
			if file.Bytes > int64(^uint64(0)>>1)-expertBytes {
				return fmt.Errorf("migration expert byte count overflow")
			}
			expertBytes += file.Bytes
			expertFileCount++
		}
	}
	if expertBytes != manifest.ExpertBytes {
		return fmt.Errorf("migration expert byte count mismatch")
	}
	if !manifest.ExpertsIncluded && (expertFileCount != 0 || manifest.ExpertBytes != 0) {
		return fmt.Errorf("migration package contains expert data without declaring it")
	}
	return nil
}

func userDataMigrationCanonicalRelativePath(name string) (string, string, error) {
	if name == "" || strings.TrimSpace(name) != name || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", "", fmt.Errorf("path must be a canonical relative slash path")
	}
	clean := pathpkg.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", "", fmt.Errorf("path must be a canonical relative slash path")
	}
	if !userDataMigrationPortablePathSegments(clean) {
		return "", "", fmt.Errorf("path contains a segment unsupported on Windows")
	}
	// Migration packages are portable. Use a case-insensitive identity on every
	// platform so a package accepted on Linux cannot become ambiguous on Windows.
	key := strings.ToLower(clean)
	return clean, key, nil
}

func userDataMigrationPortablePathSegments(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || strings.Contains(segment, ":") || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return false
		}
		for _, r := range segment {
			if r < 0x20 {
				return false
			}
		}
		base := strings.ToUpper(strings.SplitN(segment, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

func userDataMigrationValidSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
	seenFiles := make(map[string]struct{}, len(zr.File))
	seenDirs := make(map[string]struct{})
	declaredDirs := make(map[string]struct{})
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip contains unsupported symlink entry: %s", f.Name)
		}
		entryName := strings.TrimSuffix(f.Name, "/")
		cleanName, key, err := userDataMigrationCanonicalRelativePath(entryName)
		if err != nil {
			return fmt.Errorf("zip contains invalid entry path %q: %w", f.Name, err)
		}
		if f.FileInfo().IsDir() {
			if _, fileExists := seenFiles[key]; fileExists {
				return fmt.Errorf("zip entry path collides with a file: %s", f.Name)
			}
			if _, exists := declaredDirs[key]; exists {
				return fmt.Errorf("zip contains duplicate directory entry: %s", f.Name)
			}
			declaredDirs[key] = struct{}{}
			seenDirs[key] = struct{}{}
			continue
		}
		if _, exists := seenFiles[key]; exists {
			return fmt.Errorf("zip contains duplicate file entry: %s", f.Name)
		}
		if _, exists := seenDirs[key]; exists {
			return fmt.Errorf("zip entry path collides with a directory: %s", f.Name)
		}
		for parent := pathpkg.Dir(key); parent != "."; parent = pathpkg.Dir(parent) {
			if _, exists := seenFiles[parent]; exists {
				return fmt.Errorf("zip entry path has file parent: %s", f.Name)
			}
			seenDirs[parent] = struct{}{}
		}
		seenFiles[key] = struct{}{}
		if maxExpandedBytes > 0 && f.UncompressedSize64 > uint64(maxExpandedBytes)-expandedBytes {
			return fmt.Errorf("migration package expands beyond %s", userDataMigrationFormatBytes(maxExpandedBytes))
		}
		outPath, err := userDataMigrationSafeJoin(dest, cleanName)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
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
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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
	return userDataMigrationDecodeStrictJSON(data, v)
}

func userDataMigrationReadJSONFileLimited(path string, v interface{}, maxBytes int64) error {
	if maxBytes <= 0 {
		return fmt.Errorf("invalid JSON size limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("migration JSON file exceeds %s", userDataMigrationFormatBytes(maxBytes))
	}
	return userDataMigrationDecodeStrictJSON(data, v)
}

func userDataMigrationDecodeStrictJSON(data []byte, v interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := userDataMigrationWalkJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("migration JSON file contains trailing data")
		}
		return fmt.Errorf("invalid migration JSON: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("invalid migration JSON: %w", err)
	}
	return nil
}

func userDataMigrationWalkJSONValue(decoder *json.Decoder, depth int) error {
	if depth > userDataMigrationMaxJSONDepth {
		return fmt.Errorf("migration JSON nesting exceeds %d levels", userDataMigrationMaxJSONDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid migration JSON: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("invalid migration JSON: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid migration JSON object key")
			}
			identity := strings.ToLower(key)
			if _, exists := seen[identity]; exists {
				return fmt.Errorf("migration JSON file contains duplicate field %q", key)
			}
			seen[identity] = struct{}{}
			if err := userDataMigrationWalkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := userDataMigrationWalkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid migration JSON delimiter")
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid migration JSON: %w", err)
	}
	want := json.Delim('}')
	if delim == '[' {
		want = ']'
	}
	if closing != want {
		return fmt.Errorf("invalid migration JSON delimiter")
	}
	return nil
}

func userDataMigrationDownloadMetadata(m map[string]interface{}) (int, int64, int64, int64, string, string, error) {
	chunkCount64, ok := userDataMigrationStrictPositiveInt64(m, "chunk_count")
	if !ok || chunkCount64 > userDataMigrationMaxChunks {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	chunkSize, ok := userDataMigrationStrictPositiveInt64(m, "chunk_size")
	if !ok || chunkSize < userDataMigrationMinChunkSize || chunkSize > userDataMigrationMaxChunkSize {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	encryptedSize, ok := userDataMigrationStrictPositiveInt64(m, "encrypted_size")
	if !ok || encryptedSize > userDataMigrationMaxDownload {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	compressedSize, ok := userDataMigrationStrictPositiveInt64(m, "compressed_size")
	if !ok || compressedSize > userDataMigrationMaxExpanded || compressedSize > encryptedSize {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	maxOverhead := int64(1<<20) + chunkCount64*64
	if encryptedSize > compressedSize+maxOverhead {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	expectedChunks := (encryptedSize + chunkSize - 1) / chunkSize
	if expectedChunks != chunkCount64 {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	encryptedHash := strings.TrimSpace(fmt.Sprint(m["encrypted_sha256"]))
	plainHash := strings.TrimSpace(fmt.Sprint(m["plain_sha256"]))
	if !userDataMigrationValidSHA256(encryptedHash) || !userDataMigrationValidSHA256(plainHash) {
		return 0, 0, 0, 0, "", "", fmt.Errorf("invalid migration export metadata")
	}
	return int(chunkCount64), chunkSize, encryptedSize, compressedSize, strings.ToLower(encryptedHash), strings.ToLower(plainHash), nil
}

func userDataMigrationStrictPositiveInt64(m map[string]interface{}, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	switch value := m[key].(type) {
	case float64:
		if value <= 0 || value > float64(int64(^uint64(0)>>1)) || value != float64(int64(value)) {
			return 0, false
		}
		return int64(value), true
	case json.Number:
		v, err := value.Int64()
		return v, err == nil && v > 0
	case int:
		return int64(value), value > 0
	case int64:
		return value, value > 0
	case int32:
		return int64(value), value > 0
	default:
		return 0, false
	}
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
