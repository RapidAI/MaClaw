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
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"golang.org/x/crypto/argon2"
)

const (
	migrationPackageVersion = "maclaw-user-data-migration/v1"
	migrationChunkSize      = int64(4 << 20)
	migrationAEADChunkSize  = int64(4 << 20)
	migrationMagic          = "MLMIG01"
	migrationHubResolveWait = 8 * time.Second
	migrationMaxDownload    = int64(2) << 30
	migrationMaxExpanded    = int64(4) << 30
	migrationMaxZipFiles    = 200000
	migrationMaxManifest    = int64(16) << 20
	migrationMaxMemoryJSON  = int64(256) << 20
)

type migrationStatusResponse struct {
	Configured          bool        `json:"configured"`
	HubURL              string      `json:"hub_url,omitempty"`
	TenantID            string      `json:"tenant_id,omitempty"`
	UserID              string      `json:"user_id,omitempty"`
	MachineID           string      `json:"machine_id,omitempty"`
	MachineName         string      `json:"machine_name,omitempty"`
	MaxPackageBytes     int64       `json:"max_package_bytes,omitempty"`
	CurrentExport       interface{} `json:"current_export,omitempty"`
	ConfigurationReason string      `json:"configuration_reason,omitempty"`
}

type migrationManifest struct {
	Version        string                 `json:"version"`
	CreatedAt      time.Time              `json:"created_at"`
	TenantID       string                 `json:"tenant_id"`
	UserID         string                 `json:"user_id"`
	MachineID      string                 `json:"machine_id"`
	MachineName    string                 `json:"machine_name"`
	MemoryEntries  int                    `json:"memory_entries"`
	KnowledgeBytes int64                  `json:"knowledge_bytes"`
	AssetBytes     int64                  `json:"asset_bytes"`
	Files          []migrationFileDigest  `json:"files"`
	Meta           map[string]interface{} `json:"meta,omitempty"`
}

type migrationFileDigest struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type encryptedMigrationHeader struct {
	Version   string `json:"version"`
	KDF       string `json:"kdf"`
	Time      uint32 `json:"time"`
	MemoryKB  uint32 `json:"memory_kb"`
	Threads   uint8  `json:"threads"`
	Salt      []byte `json:"salt"`
	Nonce     []byte `json:"nonce"`
	PlainHash string `json:"plain_sha256"`
	PlainSize int64  `json:"plain_size"`
	Stream    bool   `json:"stream,omitempty"`
	ChunkSize int64  `json:"chunk_size,omitempty"`
}

func (s *HTTPServer) handleMigrationStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg, err := s.migrationConfig(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusOK, migrationStatusResponse{Configured: false, HubURL: cfg.HubURL, TenantID: p.TenantID, UserID: p.UserID, MachineID: cfg.MachineID, MachineName: cfg.MachineName, ConfigurationReason: err.Error()})
		return
	}
	current, maxBytes, err := s.migrationHubGetCurrent(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, migrationStatusResponse{Configured: true, HubURL: cfg.HubURL, TenantID: p.TenantID, UserID: p.UserID, MachineID: cfg.MachineID, MachineName: cfg.MachineName, ConfigurationReason: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, migrationStatusResponse{Configured: true, HubURL: cfg.HubURL, TenantID: p.TenantID, UserID: p.UserID, MachineID: cfg.MachineID, MachineName: cfg.MachineName, MaxPackageBytes: maxBytes, CurrentExport: current})
}

func (s *HTTPServer) handleMigrationInstances(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg, err := s.migrationConfig(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.migrationHubJSON(r.Context(), cfg, http.MethodGet, "/api/v1/migration/instances", nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleMigrationExport(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var req struct {
		Password         string `json:"password"`
		PasswordConfirm  string `json:"password_confirm"`
		ConfirmOverwrite bool   `json:"confirm_overwrite"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid migration export body"})
		return
	}
	if req.Password == "" || req.Password != req.PasswordConfirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "passwords do not match"})
		return
	}
	if !req.ConfirmOverwrite {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "export overwrite confirmation is required"})
		return
	}
	cfg, err := s.migrationConfig(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var job *asyncJobRecord
	progress := func(v float64, text string) {
		if job != nil {
			s.jobs.updateProgress(job.ID, v, text)
		}
	}
	job = s.jobs.createUserJob("migration.export", p, func(ctx context.Context) (any, error) {
		return s.runMigrationExport(ctx, p, cfg, req.Password, progress)
	})
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"job_id": job.ID, "status": string(job.Status)})
}

func (s *HTTPServer) handleMigrationImport(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var req struct {
		ExportID     string `json:"export_id"`
		Password     string `json:"password"`
		CleanupRetry bool   `json:"cleanup_retry"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid migration import body"})
		return
	}
	if strings.TrimSpace(req.ExportID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "export_id is required"})
		return
	}
	if req.Password == "" && !req.CleanupRetry {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "migration password is required"})
		return
	}
	cfg, err := s.migrationConfig(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var job *asyncJobRecord
	progress := func(v float64, text string) {
		if job != nil {
			s.jobs.updateProgress(job.ID, v, text)
		}
	}
	job = s.jobs.createUserJob("migration.import", p, func(ctx context.Context) (any, error) {
		return s.runMigrationImport(ctx, p, cfg, strings.TrimSpace(req.ExportID), req.Password, progress)
	})
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"job_id": job.ID, "status": string(job.Status)})
}

type migrationClientConfig struct {
	HubURL       string
	ViewerToken  string
	MachineToken string
	TenantID     string
	MachineID    string
	MachineName  string
}

func (s *HTTPServer) migrationConfig(ctx context.Context, p agentservice.Principal) (migrationClientConfig, error) {
	raw, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil {
		return migrationClientConfig{}, err
	}
	cfg := raw.AppConfig
	hubURL, resolveErr := s.migrationHubURL(ctx, cfg)
	out := migrationClientConfig{
		HubURL:       hubURL,
		ViewerToken:  strings.TrimSpace(cfg.RemoteViewerToken),
		MachineToken: strings.TrimSpace(cfg.RemoteMachineToken),
		TenantID:     strings.TrimSpace(cfg.RemoteTenantID),
		MachineID:    strings.TrimSpace(cfg.RemoteMachineID),
		MachineName:  firstMigrationNonEmpty(cfg.RemoteMachineName, cfg.RemoteClientID, cfg.RemoteMachineID),
	}
	if resolveErr != nil {
		return out, resolveErr
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

func (s *HTTPServer) migrationHubURL(ctx context.Context, cfg corelib.AppConfig) (string, error) {
	if hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/"); hubURL != "" {
		return hubURL, nil
	}
	if strings.TrimSpace(cfg.RemoteHubCenterURL) == "" && len(cfg.RemoteHubCenterURLs) == 0 {
		return "", nil
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	if email == "" {
		return "", nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, migrationHubResolveWait)
	defer cancel()
	result, _, _, err := remote.NewEnrollmentClient().ResolveHubs(resolveCtx, email, "", cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs)
	if err != nil {
		return "", fmt.Errorf("Hub resolution failed: %w", err)
	}
	if result == nil || len(result.Hubs) == 0 {
		return "", fmt.Errorf("Hub resolution failed: no Hub found for %s", email)
	}
	for _, hub := range result.Hubs {
		if strings.TrimSpace(result.DefaultHubID) != "" && hub.HubID == result.DefaultHubID {
			return strings.TrimRight(strings.TrimSpace(hub.BaseURL), "/"), nil
		}
	}
	return strings.TrimRight(strings.TrimSpace(result.Hubs[0].BaseURL), "/"), nil
}

func (s *HTTPServer) runMigrationExport(ctx context.Context, p agentservice.Principal, cfg migrationClientConfig, password string, progress func(float64, string)) (map[string]interface{}, error) {
	workDir, err := s.createMigrationTempDir("migration-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	progress(0.05, "preparing migration package")
	plainPath, _, err := s.buildMigrationPackage(ctx, p, cfg, workDir)
	if err != nil {
		return nil, err
	}
	plainHash, plainSize, err := fileSHA256(plainPath)
	if err != nil {
		return nil, err
	}
	current, maxBytes, _ := s.migrationHubGetCurrent(ctx, cfg)
	_ = current
	if maxBytes > 0 && plainSize > maxBytes {
		return nil, fmt.Errorf("compressed migration package is %s, exceeds limit %s", formatBytes(plainSize), formatBytes(maxBytes))
	}
	progress(0.22, "encrypting migration package")
	encryptedPath := filepath.Join(workDir, "migration.mlawenc")
	if err := encryptMigrationFile(plainPath, encryptedPath, password, plainHash); err != nil {
		return nil, err
	}
	encryptedHash, encryptedSize, err := fileSHA256(encryptedPath)
	if err != nil {
		return nil, err
	}
	chunkCount := int((encryptedSize + migrationChunkSize - 1) / migrationChunkSize)
	createReq := map[string]interface{}{
		"encrypted_size":   encryptedSize,
		"encrypted_sha256": encryptedHash,
		"chunk_size":       migrationChunkSize,
		"chunk_count":      chunkCount,
	}
	created, err := s.migrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/exports", createReq)
	if err != nil {
		return nil, err
	}
	exportID := strings.TrimSpace(fmt.Sprint(created["export_id"]))
	if exportID == "" {
		return nil, fmt.Errorf("Hub did not return export_id")
	}
	progress(0.30, "uploading encrypted chunks")
	if err := s.uploadMigrationChunks(ctx, cfg, exportID, encryptedPath, encryptedSize, chunkCount, progress); err != nil {
		return nil, err
	}
	completeReq := map[string]interface{}{"encrypted_sha256": encryptedHash}
	if _, err := s.migrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/exports/"+exportID+"/complete-upload", completeReq); err != nil {
		return nil, err
	}
	progress(1, "export completed")
	return map[string]interface{}{"export_id": exportID, "encrypted_size": encryptedSize, "chunk_count": chunkCount}, nil
}

func (s *HTTPServer) runMigrationImport(ctx context.Context, p agentservice.Principal, cfg migrationClientConfig, exportID, password string, progress func(float64, string)) (result map[string]interface{}, err error) {
	workDir, err := s.createMigrationTempDir("migration-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	progress(0.05, "claiming migration export")
	claimResp, err := s.migrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/claim", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	claimed := true
	localRestored := false
	defer func() {
		if err != nil && claimed && !localRestored {
			abortCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = s.migrationHubJSON(abortCtx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/abort", map[string]interface{}{})
		}
	}()
	exportMap, _ := claimResp["export"].(map[string]interface{})
	claimedStatus := strings.ToLower(strings.TrimSpace(fmt.Sprint(exportMap["status"])))
	if claimedStatus == "deleted" {
		claimed = false
		progress(1, "import cleanup already completed")
		return map[string]interface{}{"export_id": exportID, "cleanup_retried": true, "status": "deleted"}, nil
	}
	if claimedStatus == "imported" || claimedStatus == "deleting" {
		progress(0.92, "retrying Hub cleanup")
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.completeMigrationImportOnHub(cleanupCtx, cfg, exportID); err != nil {
			return nil, fmt.Errorf("Hub cleanup retry failed: %w", err)
		}
		claimed = false
		progress(1, "import cleanup completed")
		return map[string]interface{}{"export_id": exportID, "cleanup_retried": true}, nil
	}
	if password == "" {
		return nil, fmt.Errorf("migration password is required")
	}
	chunkCount := int(numberFromMap(exportMap, "chunk_count"))
	chunkSize := int64(numberFromMap(exportMap, "chunk_size"))
	encryptedSize := int64(numberFromMap(exportMap, "encrypted_size"))
	encryptedHash := strings.TrimSpace(fmt.Sprint(exportMap["encrypted_sha256"]))
	if chunkCount <= 0 || chunkCount > 8192 || chunkSize < 256<<10 || chunkSize > 8<<20 || encryptedSize <= 0 || encryptedSize > migrationMaxDownload || int64(chunkCount) != (encryptedSize+chunkSize-1)/chunkSize || !validMigrationSHA256(encryptedHash) {
		return nil, fmt.Errorf("invalid migration export metadata")
	}
	encryptedPath := filepath.Join(workDir, "migration.mlawenc")
	progress(0.12, "downloading encrypted chunks")
	if err := s.downloadMigrationChunks(ctx, cfg, exportID, encryptedPath, encryptedSize, chunkSize, chunkCount, progress); err != nil {
		return nil, err
	}
	gotHash, _, err := fileSHA256(encryptedPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(gotHash, encryptedHash) {
		return nil, fmt.Errorf("encrypted package hash mismatch")
	}
	progress(0.72, "decrypting and verifying package")
	plainPath := filepath.Join(workDir, "migration.zip")
	if err := decryptMigrationFile(encryptedPath, plainPath, password, ""); err != nil {
		return nil, err
	}
	progress(0.82, "restoring local memory and knowledge base")
	result, err = s.restoreMigrationPackage(ctx, p, plainPath, workDir)
	if err != nil {
		return nil, err
	}
	localRestored = true
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.completeMigrationImportOnHub(cleanupCtx, cfg, exportID); err != nil {
		return nil, fmt.Errorf("local import succeeded, but Hub cleanup failed: %w", err)
	}
	claimed = false
	progress(1, "import completed")
	result["export_id"] = exportID
	return result, nil
}

func (s *HTTPServer) completeMigrationImportOnHub(ctx context.Context, cfg migrationClientConfig, exportID string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := s.migrationHubJSON(ctx, cfg, http.MethodPost, "/api/v1/migration/imports/"+exportID+"/complete", map[string]interface{}{}); err != nil {
			lastErr = err
		} else {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 700 * time.Millisecond):
		}
	}
	return lastErr
}

func (s *HTTPServer) createMigrationTempDir(pattern string) (string, error) {
	root := filepath.Join(s.svc.DataRoot(), "tmp")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, pattern)
}

func (s *HTTPServer) buildMigrationPackage(ctx context.Context, p agentservice.Principal, cfg migrationClientConfig, workDir string) (string, migrationManifest, error) {
	payloadDir := filepath.Join(workDir, "payload")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		return "", migrationManifest{}, err
	}
	mem, err := s.svc.ExportUserMemorySnapshot(ctx, p)
	if err != nil {
		return "", migrationManifest{}, err
	}
	memPath := filepath.Join(payloadDir, "memory_snapshot.json")
	if err := writeJSONFile(memPath, mem); err != nil {
		return "", migrationManifest{}, err
	}
	knowledgePath := filepath.Join(payloadDir, "knowledge_snapshot.jsonl")
	var knowledgeBytes int64
	var knowledgeSourceIDs []string
	if s.knowledgeMgr != nil && s.knowledgeMgr.Store() != nil {
		res, err := s.knowledgeMgr.Store().ExportSnapshot(ctx, knowledge.ExportOptions{
			OutputPath:      knowledgePath,
			RedactSensitive: false,
			TenantID:        p.TenantID,
			OwnerID:         p.UserID,
		})
		if err != nil {
			return "", migrationManifest{}, err
		}
		knowledgeBytes = res.Bytes
		knowledgeSourceIDs = res.SourceIDs
	} else if err := os.WriteFile(knowledgePath, nil, 0o600); err != nil {
		return "", migrationManifest{}, err
	}
	manifest := migrationManifest{
		Version:        migrationPackageVersion,
		CreatedAt:      time.Now().UTC(),
		TenantID:       p.TenantID,
		UserID:         p.UserID,
		MachineID:      cfg.MachineID,
		MachineName:    cfg.MachineName,
		MemoryEntries:  len(mem.Entries),
		KnowledgeBytes: knowledgeBytes,
		Meta:           map[string]interface{}{"contains": []string{"memory", "knowledge", "knowledge_assets"}},
	}
	assetsDir := filepath.Join(s.svc.DataRoot(), "knowledge_assets")
	if st, err := os.Stat(assetsDir); err == nil && st.IsDir() {
		assetBytes, err := copyKnowledgeAssetsInto(payloadDir, assetsDir, knowledgeSourceIDs)
		if err != nil {
			return "", migrationManifest{}, err
		}
		manifest.AssetBytes = assetBytes
	}
	files, err := digestDir(payloadDir)
	if err != nil {
		return "", migrationManifest{}, err
	}
	manifest.Files = files
	if err := writeJSONFile(filepath.Join(payloadDir, "manifest.json"), manifest); err != nil {
		return "", migrationManifest{}, err
	}
	zipPath := filepath.Join(workDir, "migration.zip")
	if err := zipDir(payloadDir, zipPath); err != nil {
		return "", migrationManifest{}, err
	}
	return zipPath, manifest, nil
}

func (s *HTTPServer) restoreMigrationPackage(ctx context.Context, p agentservice.Principal, zipPath, workDir string) (map[string]interface{}, error) {
	payloadDir := filepath.Join(workDir, "payload")
	if err := unzipToDir(zipPath, payloadDir); err != nil {
		return nil, err
	}
	var manifest migrationManifest
	if err := readJSONFileLimited(filepath.Join(payloadDir, "manifest.json"), &manifest, migrationMaxManifest); err != nil {
		return nil, err
	}
	if manifest.Version != migrationPackageVersion {
		return nil, fmt.Errorf("unsupported migration package version %q", manifest.Version)
	}
	if err := verifyFileDigests(payloadDir, manifest.Files); err != nil {
		return nil, err
	}
	var mem agentservice.UserMemorySnapshot
	if err := readJSONFileLimited(filepath.Join(payloadDir, "memory_snapshot.json"), &mem, migrationMaxMemoryJSON); err != nil {
		return nil, err
	}
	memResult, err := s.svc.ImportUserMemorySnapshot(ctx, p, mem)
	if err != nil {
		return nil, err
	}
	assetSrc := filepath.Join(payloadDir, "knowledge_assets")
	if st, err := os.Stat(assetSrc); err == nil && st.IsDir() {
		if _, err := copyDirInto(s.svc.DataRoot(), assetSrc, "knowledge_assets"); err != nil {
			return nil, err
		}
	}
	knowledgeResult := knowledge.SnapshotImportResult{}
	knowledgePath := filepath.Join(payloadDir, "knowledge_snapshot.jsonl")
	if s.knowledgeMgr != nil && s.knowledgeMgr.Store() != nil {
		knowledgeResult, err = s.knowledgeMgr.Store().ImportSnapshot(ctx, knowledge.SnapshotImportOptions{InputPath: knowledgePath, Overwrite: true})
		if err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{
		"memory":    memResult,
		"knowledge": knowledgeResult,
		"manifest":  manifest,
	}, nil
}

func (s *HTTPServer) uploadMigrationChunks(ctx context.Context, cfg migrationClientConfig, exportID, path string, size int64, chunkCount int, progress func(float64, string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, migrationChunkSize)
	for i := 0; i < chunkCount; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := f.ReadAt(buf, int64(i)*migrationChunkSize)
		if err != nil && err != io.EOF {
			return err
		}
		expectedSize := migrationChunkSize
		if remaining := size - int64(i)*migrationChunkSize; remaining < expectedSize {
			expectedSize = remaining
		}
		if expectedSize <= 0 || int64(n) != expectedSize {
			return fmt.Errorf("migration chunk %d size mismatch", i)
		}
		chunk := buf[:n]
		sha := sha256.Sum256(chunk)
		shaHex := hex.EncodeToString(sha[:])
		if !s.migrationChunkUploaded(ctx, cfg, exportID, i, shaHex) {
			var uploadErr error
			for attempt := 0; attempt < 3; attempt++ {
				uploadErr = s.migrationHubRaw(ctx, cfg, http.MethodPut, fmt.Sprintf("/api/v1/migration/exports/%s/chunks/%d?sha256=%s", exportID, i, shaHex), bytes.NewReader(chunk), "application/octet-stream")
				if uploadErr == nil || s.migrationChunkUploaded(ctx, cfg, exportID, i, shaHex) {
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

func (s *HTTPServer) migrationChunkUploaded(ctx context.Context, cfg migrationClientConfig, exportID string, index int, sha string) bool {
	status, err := s.migrationHubJSON(ctx, cfg, http.MethodGet, fmt.Sprintf("/api/v1/migration/exports/%s/chunks/%d/status", exportID, index), nil)
	if err != nil {
		return false
	}
	return status["uploaded"] == true && strings.EqualFold(fmt.Sprint(status["sha256"]), sha)
}

func (s *HTTPServer) downloadMigrationChunks(ctx context.Context, cfg migrationClientConfig, exportID, path string, size, chunkSize int64, chunkCount int, progress func(float64, string)) error {
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
		data, err := s.migrationHubBytesLimited(ctx, cfg, http.MethodGet, fmt.Sprintf("/api/v1/migration/imports/%s/chunks/%d", exportID, i), nil, expectedSize)
		if err != nil {
			return err
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

func encryptMigrationFile(inPath, outPath, password, plainHash string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer in.Close()
	plainInfo, err := in.Stat()
	if err != nil || plainInfo.Size() <= 0 || plainInfo.Size() > migrationMaxExpanded {
		return fmt.Errorf("invalid migration package size")
	}
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer outFile.Close()
	salt := randomBytes(16)
	nonce := randomBytes(12)
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	header := encryptedMigrationHeader{Version: migrationPackageVersion, KDF: "argon2id", Time: 3, MemoryKB: 64 * 1024, Threads: 4, Salt: salt, Nonce: nonce, PlainHash: plainHash, PlainSize: plainInfo.Size(), Stream: true, ChunkSize: migrationAEADChunkSize}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := outFile.Write([]byte(migrationMagic)); err != nil {
		return err
	}
	if err := binary.Write(outFile, binary.BigEndian, uint32(len(headerJSON))); err != nil {
		return err
	}
	if _, err := outFile.Write(headerJSON); err != nil {
		return err
	}
	buf := make([]byte, migrationAEADChunkSize)
	for index := uint64(0); ; index++ {
		n, readErr := in.Read(buf)
		if n > 0 {
			chunkNonce, err := migrationChunkNonce(nonce, index)
			if err != nil {
				return err
			}
			ciphertext := gcm.Seal(nil, chunkNonce, buf[:n], migrationChunkAAD(headerJSON, index))
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

func decryptMigrationFile(inPath, outPath, password, expectedPlainHash string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer in.Close()
	magic := make([]byte, len(migrationMagic))
	if _, err := io.ReadFull(in, magic); err != nil || string(magic) != migrationMagic {
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
	var header encryptedMigrationHeader
	if err := decodeMigrationStrictJSON(headerJSON, &header); err != nil {
		return err
	}
	if err := validateEncryptedMigrationHeader(header); err != nil {
		return err
	}
	expectedPlainSize := header.PlainSize
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
	completed := false
	defer func() {
		_ = out.Close()
		if !completed {
			_ = os.Remove(outPath)
		}
	}()
	h := sha256.New()
	var written int64
	if !header.Stream {
		ciphertext, err := io.ReadAll(io.LimitReader(in, expectedPlainSize+int64(gcm.Overhead())+1))
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
		written = int64(len(plain))
		if written != expectedPlainSize {
			return fmt.Errorf("decrypted package size mismatch")
		}
		if err := finishDecryptedMigration(out, h, expectedPlainHash, header.PlainHash); err != nil {
			return err
		}
		completed = true
		return nil
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
		chunkNonce, err := migrationChunkNonce(header.Nonce, index)
		if err != nil {
			return err
		}
		plain, err := gcm.Open(nil, chunkNonce, ciphertext, migrationChunkAAD(headerJSON, index))
		if err != nil {
			return fmt.Errorf("migration password is incorrect or package is corrupted")
		}
		if _, err := h.Write(plain); err != nil {
			return err
		}
		if _, err := out.Write(plain); err != nil {
			return err
		}
		written += int64(len(plain))
		if written > expectedPlainSize {
			return fmt.Errorf("decrypted package size mismatch")
		}
	}
	if written != expectedPlainSize {
		return fmt.Errorf("decrypted package size mismatch")
	}
	if err := finishDecryptedMigration(out, h, expectedPlainHash, header.PlainHash); err != nil {
		return err
	}
	completed = true
	return nil
}

func validateEncryptedMigrationHeader(header encryptedMigrationHeader) error {
	if header.Version != migrationPackageVersion || header.KDF != "argon2id" || len(header.Salt) != 16 || len(header.Nonce) != 12 {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	if header.Time == 0 || header.Time > 10 || header.MemoryKB < 1024 || header.MemoryKB > 256*1024 || header.Threads == 0 || header.Threads > 8 {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	if !validMigrationSHA256(header.PlainHash) || header.PlainSize <= 0 || header.PlainSize > migrationMaxExpanded {
		return fmt.Errorf("unsupported encrypted migration package")
	}
	return nil
}

func migrationChunkNonce(base []byte, index uint64) ([]byte, error) {
	if len(base) != 12 {
		return nil, fmt.Errorf("invalid encrypted migration nonce")
	}
	nonce := append([]byte(nil), base...)
	for i := 0; i < 8; i++ {
		nonce[len(nonce)-1-i] ^= byte(index >> (8 * i))
	}
	return nonce, nil
}

func migrationChunkAAD(headerJSON []byte, index uint64) []byte {
	var idx [8]byte
	binary.BigEndian.PutUint64(idx[:], index)
	aad := make([]byte, 0, len(headerJSON)+len(idx))
	aad = append(aad, headerJSON...)
	aad = append(aad, idx[:]...)
	return aad
}

func finishDecryptedMigration(out *os.File, h hashWriter, expectedPlainHash, headerPlainHash string) error {
	got := hex.EncodeToString(h.Sum(nil))
	want := firstMigrationNonEmpty(expectedPlainHash, headerPlainHash)
	if want != "" && !strings.EqualFold(got, want) {
		return fmt.Errorf("decrypted package hash mismatch")
	}
	return out.Close()
}

type hashWriter interface {
	io.Writer
	Sum([]byte) []byte
}

func (s *HTTPServer) migrationHubGetCurrent(ctx context.Context, cfg migrationClientConfig) (interface{}, int64, error) {
	out, err := s.migrationHubJSON(ctx, cfg, http.MethodGet, "/api/v1/migration/exports/current", nil)
	if err != nil {
		return nil, 0, err
	}
	return out["export"], int64(numberFromMap(out, "max_package_bytes")), nil
}

func (s *HTTPServer) migrationHubJSON(ctx context.Context, cfg migrationClientConfig, method, path string, body interface{}) (map[string]interface{}, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	data, err := s.migrationHubBytes(ctx, cfg, method, path, reader, "application/json")
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if len(data) > 0 {
		if err := decodeMigrationJSON(data, &out); err != nil {
			return nil, fmt.Errorf("invalid Hub migration JSON response: %w", err)
		}
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func decodeMigrationJSON(data []byte, value interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("response must contain one JSON value")
		}
		return err
	}
	return nil
}

func decodeMigrationStrictJSON(data []byte, value interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkMigrationJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("migration JSON contains trailing data")
		}
		return err
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("migration JSON must contain one value")
	}
	return nil
}

func walkMigrationJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("migration JSON nesting is too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
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
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("migration JSON object key must be a string")
			}
			folded := strings.ToLower(key)
			if _, exists := seen[folded]; exists {
				return fmt.Errorf("migration JSON contains duplicate field %q", key)
			}
			seen[folded] = struct{}{}
			if err := walkMigrationJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkMigrationJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("invalid migration JSON delimiter")
	}
}

func (s *HTTPServer) migrationHubRaw(ctx context.Context, cfg migrationClientConfig, method, path string, body io.Reader, contentType ...string) error {
	_, err := s.migrationHubBytes(ctx, cfg, method, path, body, contentType...)
	return err
}

func (s *HTTPServer) migrationHubBytes(ctx context.Context, cfg migrationClientConfig, method, path string, body io.Reader, contentType ...string) ([]byte, error) {
	return s.migrationHubBytesLimited(ctx, cfg, method, path, body, 32<<20, contentType...)
}

func (s *HTTPServer) migrationHubBytesLimited(ctx context.Context, cfg migrationClientConfig, method, path string, body io.Reader, maxBytes int64, contentType ...string) ([]byte, error) {
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
	if body != nil && method != http.MethodGet && len(contentType) > 0 && strings.TrimSpace(contentType[0]) != "" {
		req.Header.Set("Content-Type", strings.TrimSpace(contentType[0]))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if maxBytes < 1 {
		maxBytes = 1
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Hub migration API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("Hub migration API %s %s response exceeds %d bytes", method, path, maxBytes)
	}
	return data, nil
}

func fileSHA256(path string) (string, int64, error) {
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

func digestDir(root string) ([]migrationFileDigest, error) {
	var out []migrationFileDigest
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration package contains unsupported symlink: %s", path)
		}
		sha, size, err := fileSHA256(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		out = append(out, migrationFileDigest{Path: rel, Bytes: size, SHA256: sha})
		return nil
	})
	return out, err
}

func verifyFileDigests(root string, files []migrationFileDigest) error {
	if len(files) == 0 || len(files) > migrationMaxZipFiles {
		return fmt.Errorf("invalid migration manifest file list")
	}
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Bytes < 0 || !validMigrationSHA256(file.SHA256) {
			return fmt.Errorf("invalid migration manifest file: %s", file.Path)
		}
		path, err := safeJoin(root, file.Path)
		if err != nil {
			return err
		}
		key := strings.ToLower(filepath.Clean(path))
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate migration manifest file: %s", file.Path)
		}
		seen[key] = struct{}{}
		sha, size, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if size != file.Bytes || !strings.EqualFold(sha, file.SHA256) {
			return fmt.Errorf("migration file hash mismatch: %s", file.Path)
		}
	}
	actual := make(map[string]string, len(files))
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
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		if !canonicalMigrationRelativePath(rel) {
			return fmt.Errorf("invalid migration package file: %s", rel)
		}
		key := strings.ToLower(filepath.Clean(path))
		actual[key] = rel
		return nil
	}); err != nil {
		return err
	}
	if len(actual) != len(seen) {
		return fmt.Errorf("migration manifest file set does not match package contents")
	}
	for key, rel := range actual {
		if _, ok := seen[key]; !ok {
			return fmt.Errorf("migration package contains undeclared file: %s", rel)
		}
	}
	return nil
}

func validMigrationSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func zipDir(root, zipPath string) error {
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
		if err != nil || d.IsDir() {
			return err
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

func unzipToDir(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > migrationMaxZipFiles {
		return fmt.Errorf("migration package contains too many files")
	}
	var expandedBytes uint64
	seen := make(map[string]struct{}, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip contains unsupported symlink entry: %s", f.Name)
		}
		cleanName := filepath.ToSlash(filepath.Clean(strings.TrimSuffix(f.Name, "/")))
		if !canonicalMigrationRelativePath(cleanName) {
			return fmt.Errorf("zip contains invalid entry path: %s", f.Name)
		}
		key := strings.ToLower(cleanName)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("zip contains duplicate entry: %s", f.Name)
		}
		seen[key] = struct{}{}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > uint64(migrationMaxExpanded)-expandedBytes {
			return fmt.Errorf("migration package expands beyond %d bytes", migrationMaxExpanded)
		}
		outPath, err := safeJoin(dest, cleanName)
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

func copyDirInto(destRoot, srcRoot, destName string) (int64, error) {
	var total int64
	destBase, err := safeJoin(destRoot, destName)
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
		dest, err := safeJoin(destBase, rel)
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

func copyKnowledgeAssetsInto(destRoot, srcRoot string, sourceIDs []string) (int64, error) {
	sourceSet := map[string]struct{}{}
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			sourceSet[id] = struct{}{}
		}
	}
	if len(sourceSet) == 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || !knowledgeAssetDirSelected(entry.Name(), sourceSet) {
			continue
		}
		n, err := copyDirInto(destRoot, filepath.Join(srcRoot, entry.Name()), filepath.ToSlash(filepath.Join("knowledge_assets", entry.Name())))
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func knowledgeAssetDirSelected(name string, sourceSet map[string]struct{}) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if _, ok := sourceSet[name]; ok {
		return true
	}
	for sourceID := range sourceSet {
		if strings.HasPrefix(name, sourceID+"_") {
			return true
		}
	}
	return false
}

func safeJoin(root, name string) (string, error) {
	if !canonicalMigrationRelativePath(filepath.ToSlash(name)) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	out := filepath.Join(rootAbs, filepath.FromSlash(name))
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	if outAbs != rootAbs && !strings.HasPrefix(strings.ToLower(outAbs), strings.ToLower(rootAbs)+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	return outAbs, nil
}

func canonicalMigrationRelativePath(name string) bool {
	if name == "" || strings.TrimSpace(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || filepath.IsAbs(filepath.FromSlash(name)) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean == "." || clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, segment := range strings.Split(clean, "/") {
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

func writeJSONFile(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readJSONFile(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func readJSONFileLimited(path string, v interface{}, maxBytes int64) error {
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
		return fmt.Errorf("migration JSON exceeds %d bytes: %s", maxBytes, filepath.Base(path))
	}
	if err := decodeMigrationStrictJSON(data, v); err != nil {
		return fmt.Errorf("invalid migration JSON %s: %w", filepath.Base(path), err)
	}
	return nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func numberFromMap(m map[string]interface{}, key string) float64 {
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

func firstMigrationNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatBytes(n int64) string {
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
