package digitalasset

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

const BackupFormatV1 = "digital_asset_backup_v1"

// BackupManifest is the root manifest inside backup.zip.
type BackupManifest struct {
	Format     string               `json:"format"`
	TenantID   string               `json:"tenant_id"`
	ExportedAt string               `json:"exported_at"`
	Libraries  []BackupLibraryEntry `json:"libraries"`
}

// BackupLibraryEntry describes one library in the backup.
type BackupLibraryEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ContentRev int64  `json:"content_rev"`
	SHA256     string `json:"sha256"`
}

// libraryMetaFile is stored as libraries/{id}/meta.json
type libraryMetaFile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ACLMode     string   `json:"acl_mode"`
	Departments []string `json:"departments"`
	SyncEnabled bool     `json:"sync_enabled"`
	ContentRev  int64    `json:"content_rev"`
	SourceCount int64    `json:"source_count"`
}

// ExportBackupInput selects libraries to export.
type ExportBackupInput struct {
	TenantID   string
	LibraryIDs []string // empty + All = all active
	All        bool
	Actor      string
}

// ExportBackupResult is the completed export job + download path.
type ExportBackupResult struct {
	JobID        string
	Status       string
	DownloadPath string // absolute path on server
	Error        string
}

// ExportBackup builds a backup.zip under Host tmp / backups.
func (s *Service) ExportBackup(ctx context.Context, in ExportBackupInput) (*ExportBackupResult, error) {
	if err := s.requireEnabled(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if s.Host == nil || s.Repo == nil {
		return nil, fmt.Errorf("host or repo nil")
	}
	ids := append([]string{}, in.LibraryIDs...)
	if in.All || len(ids) == 0 {
		items, _, err := s.Repo.ListLibraries(ctx, store.DigitalAssetLibraryFilter{
			TenantID: in.TenantID,
			Status:   store.DigitalAssetStatusActive,
			Limit:    200,
		})
		if err != nil {
			return nil, err
		}
		ids = ids[:0]
		for _, lib := range items {
			if lib != nil {
				ids = append(ids, lib.ID)
			}
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no libraries to export")
	}

	now := time.Now().UTC()
	jobID := "daij_" + uuid.NewString()
	job := &store.DigitalAssetImportJob{
		ID: jobID, TenantID: in.TenantID, LibraryID: "",
		Kind: "export_backup", Status: "running",
		ProgressJSON: `{"phase":"exporting"}`,
		CreatedBy:    in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	outDir := filepath.Join(s.Host.Root(), "backups", in.TenantID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	zipPath := filepath.Join(outDir, jobID+".zip")

	zf, err := os.Create(zipPath)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(zf)

	manifest := BackupManifest{
		Format:     BackupFormatV1,
		TenantID:   in.TenantID,
		ExportedAt: now.Format(time.RFC3339),
	}

	fail := func(e error) (*ExportBackupResult, error) {
		_ = zw.Close()
		_ = zf.Close()
		_ = os.Remove(zipPath)
		job.Status = "failed"
		job.ErrorMessage = e.Error()
		job.UpdatedAt = time.Now().UTC()
		_ = s.Repo.UpdateJob(ctx, job)
		return &ExportBackupResult{JobID: jobID, Status: "failed", Error: e.Error()}, e
	}

	for _, id := range ids {
		lib, err := s.GetLibrary(ctx, in.TenantID, id)
		if err != nil {
			return fail(err)
		}
		tmpJSONL := filepath.Join(os.TempDir(), "dal_bak_"+id+"_"+uuid.NewString()+".jsonl")
		if err := s.Host.WithLibraryRead(ctx, in.TenantID, id, func(st *knowledge.SQLiteStore) error {
			_, err := st.ExportSnapshot(ctx, knowledge.ExportOptions{OutputPath: tmpJSONL, Format: "jsonl"})
			return err
		}); err != nil {
			_ = os.Remove(tmpJSONL)
			return fail(err)
		}
		sum, _, err := fileSHA256(tmpJSONL)
		if err != nil {
			_ = os.Remove(tmpJSONL)
			return fail(err)
		}
		acl := ParseACL(lib.ACLMode, lib.ACLDepartmentsJSON, lib.ACLUsersJSON)
		meta := libraryMetaFile{
			ID: lib.ID, Name: lib.Name, Description: lib.Description,
			ACLMode: acl.Mode, Departments: acl.Departments,
			SyncEnabled: lib.SyncEnabled, ContentRev: lib.ContentRev, SourceCount: lib.SourceCount,
		}
		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		if err := writeZipFile(zw, filepath.ToSlash(filepath.Join("libraries", id, "meta.json")), metaBytes); err != nil {
			_ = os.Remove(tmpJSONL)
			return fail(err)
		}
		if err := copyFileToZip(zw, filepath.ToSlash(filepath.Join("libraries", id, "knowledge.jsonl")), tmpJSONL); err != nil {
			_ = os.Remove(tmpJSONL)
			return fail(err)
		}
		_ = os.Remove(tmpJSONL)
		manifest.Libraries = append(manifest.Libraries, BackupLibraryEntry{
			ID: id, Name: lib.Name, ContentRev: lib.ContentRev, SHA256: sum,
		})
	}
	manBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeZipFile(zw, "manifest.json", manBytes); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		_ = zf.Close()
		return fail(err)
	}
	if err := zf.Close(); err != nil {
		return fail(err)
	}

	job.Status = "succeeded"
	job.ProgressJSON = string(mustJSON(map[string]any{
		"phase": "done", "download": filepath.Base(zipPath), "libraries": len(manifest.Libraries),
	}))
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(ctx, job)
	return &ExportBackupResult{JobID: jobID, Status: "succeeded", DownloadPath: zipPath}, nil
}

// ImportBackupInput restores a backup zip.
type ImportBackupInput struct {
	TenantID         string
	ZipPath          string
	Mode             string // new_libraries | into_library | replace_library
	TargetLibraryID  string
	RestoreACL       bool
	IDPolicy         string // new_ids | preserve_ids
	Actor            string
	AllowCrossTenant bool
}

// ImportBackup restores libraries from a backup zip.
func (s *Service) ImportBackup(ctx context.Context, in ImportBackupInput) (*store.DigitalAssetImportJob, error) {
	if err := s.requireEnabled(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if s.Host == nil {
		return nil, fmt.Errorf("host nil")
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = "new_libraries"
	}
	idPolicy := strings.TrimSpace(in.IDPolicy)
	if idPolicy == "" {
		idPolicy = "new_ids"
	}

	zr, err := zip.OpenReader(in.ZipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var man BackupManifest
	if err := readZipJSON(&zr.Reader, "manifest.json", &man); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if man.Format != BackupFormatV1 {
		return nil, fmt.Errorf("unsupported backup format")
	}
	if !in.AllowCrossTenant && store.NormalizeTenantID(man.TenantID) != store.NormalizeTenantID(in.TenantID) {
		return nil, fmt.Errorf("cross_tenant_restore_denied")
	}
	if len(man.Libraries) == 0 {
		return nil, fmt.Errorf("backup has no libraries")
	}

	now := time.Now().UTC()
	job := &store.DigitalAssetImportJob{
		ID: "daij_" + uuid.NewString(), TenantID: in.TenantID, LibraryID: in.TargetLibraryID,
		Kind: "backup", Status: "running", ProgressJSON: `{"phase":"restoring"}`,
		CreatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	for _, entry := range man.Libraries {
		metaPath := filepath.ToSlash(filepath.Join("libraries", entry.ID, "meta.json"))
		jsonlPath := filepath.ToSlash(filepath.Join("libraries", entry.ID, "knowledge.jsonl"))
		var meta libraryMetaFile
		if err := readZipJSON(&zr.Reader, metaPath, &meta); err != nil {
			// fallback minimal meta
			meta = libraryMetaFile{ID: entry.ID, Name: entry.Name, ACLMode: ACLModeAllMembers, SyncEnabled: true}
		}
		tmpJSONL := filepath.Join(os.TempDir(), "dal_restore_"+uuid.NewString()+".jsonl")
		if err := extractZipFile(&zr.Reader, jsonlPath, tmpJSONL); err != nil {
			job.Status = "failed"
			job.ErrorMessage = err.Error()
			_ = s.Repo.UpdateJob(ctx, job)
			return job, err
		}

		var lib *store.DigitalAssetLibrary
		switch mode {
		case "into_library", "replace_library":
			lib, err = s.GetLibrary(ctx, in.TenantID, in.TargetLibraryID)
			if err != nil {
				_ = os.Remove(tmpJSONL)
				job.Status = "failed"
				job.ErrorMessage = err.Error()
				_ = s.Repo.UpdateJob(ctx, job)
				return job, err
			}
			if mode == "replace_library" {
				_ = s.Host.WithLibraryWrite(ctx, in.TenantID, lib.ID, func(st *knowledge.SQLiteStore) error {
					sources, err := st.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 100000, IncludeDisabled: true})
					if err != nil {
						return err
					}
					for _, src := range sources {
						_ = st.DeleteSource(ctx, src.ID)
					}
					return nil
				})
			}
		default: // new_libraries
			aclMode := meta.ACLMode
			if !in.RestoreACL {
				aclMode = ACLModeAllMembers
				meta.Departments = nil
			}
			name := meta.Name
			if name == "" {
				name = entry.Name
			}
			if name == "" {
				name = "Restored library"
			}
			lib, err = s.CreateLibrary(ctx, CreateLibraryInput{
				TenantID: in.TenantID, Name: name, Description: meta.Description,
				ACL:         ACL{Mode: aclMode, Departments: meta.Departments},
				SyncEnabled: &meta.SyncEnabled, Actor: in.Actor,
			})
			if err != nil {
				_ = os.Remove(tmpJSONL)
				job.Status = "failed"
				job.ErrorMessage = err.Error()
				_ = s.Repo.UpdateJob(ctx, job)
				return job, err
			}
			if idPolicy == "preserve_ids" {
				// v1: always new ids (CreateLibrary generates new). preserve is best-effort no-op.
				_ = idPolicy
			}
		}

		err = s.Host.WithLibraryWrite(ctx, in.TenantID, lib.ID, func(st *knowledge.SQLiteStore) error {
			_, err := st.ImportSnapshot(ctx, knowledge.SnapshotImportOptions{
				InputPath: tmpJSONL, Overwrite: true, ReplaceAll: false,
				SkipSafetyBackup: true, AbortOnError: true,
			})
			if err != nil {
				return err
			}
			lib.UpdatedAt = time.Now().UTC()
			lib.UpdatedBy = in.Actor
			return s.advanceContentAfterImportLocked(ctx, st, lib, "replace_snapshot", in.Actor)
		})
		_ = os.Remove(tmpJSONL)
		if err != nil {
			job.Status = "failed"
			job.ErrorMessage = err.Error()
			_ = s.Repo.UpdateJob(ctx, job)
			return job, err
		}
	}

	job.Status = "succeeded"
	job.ProgressJSON = string(mustJSON(map[string]any{"phase": "done", "libraries": len(man.Libraries)}))
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(ctx, job)
	return job, nil
}

// BackupDownloadPath returns path for a succeeded export job if file exists.
func (s *Service) BackupDownloadPath(tenantID, jobID string) (string, error) {
	if s.Host == nil {
		return "", fmt.Errorf("host nil")
	}
	path := filepath.Join(s.Host.Root(), "backups", tenantID, jobID+".zip")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("backup file not found")
	}
	return path, nil
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func copyFileToZip(zw *zip.Writer, name, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func readZipJSON(zr *zip.Reader, name string, dest any) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return json.NewDecoder(rc).Decode(dest)
	}
	return fmt.Errorf("%s not found in zip", name)
}

func extractZipFile(zr *zip.Reader, name, dest string) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, rc)
		_ = out.Close()
		return err
	}
	return fmt.Errorf("%s not found in zip", name)
}
