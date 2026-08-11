package digitalasset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

// MergeLibrariesInput merges one or more source libraries into a target.
type MergeLibrariesInput struct {
	TenantID         string
	TargetLibraryID  string
	SourceLibraryIDs []string
	// ArchiveSources defaults to true when nil.
	ArchiveSources *bool
	Actor          string
}

// MergeLibraries copies source library knowledge into target, then optionally archives sources.
// ACL / sync flags remain those of the target library.
func (s *Service) MergeLibraries(ctx context.Context, in MergeLibrariesInput) (*store.DigitalAssetImportJob, error) {
	if err := s.requireEnabled(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if s.Host == nil || s.Repo == nil {
		return nil, fmt.Errorf("host or repo nil")
	}
	targetID := strings.TrimSpace(in.TargetLibraryID)
	if targetID == "" {
		return nil, fmt.Errorf("target_library_id required")
	}
	sources := make([]string, 0, len(in.SourceLibraryIDs))
	seen := map[string]struct{}{}
	for _, id := range in.SourceLibraryIDs {
		id = strings.TrimSpace(id)
		if id == "" || id == targetID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		sources = append(sources, id)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("source_library_ids required")
	}

	target, err := s.GetLibrary(ctx, in.TenantID, targetID)
	if err != nil {
		return nil, err
	}
	if target.Status != store.DigitalAssetStatusActive {
		return nil, fmt.Errorf("target library not active")
	}
	for _, sid := range sources {
		src, err := s.GetLibrary(ctx, in.TenantID, sid)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", sid, err)
		}
		if src.Status != store.DigitalAssetStatusActive {
			return nil, fmt.Errorf("source %s not active", sid)
		}
	}
	s.importStartMu.Lock()
	s.reclaimStaleImportJobs(ctx, in.TenantID)
	if n, err := s.Repo.CountRunningJobs(ctx, in.TenantID); err == nil && n > 0 {
		s.importStartMu.Unlock()
		return nil, fmt.Errorf("tenant already has a running import job")
	}
	now := time.Now().UTC()
	job := &store.DigitalAssetImportJob{
		ID: "daij_" + uuid.NewString(), TenantID: in.TenantID, LibraryID: targetID,
		Kind: "library_merge", Status: "running",
		ProgressJSON: string(mustJSON(map[string]any{
			"sources": sources, "phase": "merging", "percent": 20, "message": "merging libraries",
		})),
		CreatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Repo.CreateJob(ctx, job); err != nil {
		s.importStartMu.Unlock()
		return nil, err
	}
	s.importStartMu.Unlock()

	// Sequential per source: export under source lock, import under target lock.
	// Avoid nested same-host lock ordering deadlocks by not nesting WithLibraryWrite.
	err = func() error {
		for _, sid := range sources {
			tmp := filepath.Join(os.TempDir(), fmt.Sprintf("dal_merge_%s_%s.jsonl", sid, uuid.NewString()))
			if err := s.Host.WithLibraryWrite(ctx, in.TenantID, sid, func(srcStore *knowledge.SQLiteStore) error {
				_, err := srcStore.ExportSnapshot(ctx, knowledge.ExportOptions{OutputPath: tmp, Format: "jsonl"})
				return err
			}); err != nil {
				_ = os.Remove(tmp)
				return fmt.Errorf("export %s: %w", sid, err)
			}
			err := s.Host.WithLibraryWrite(ctx, in.TenantID, targetID, func(targetStore *knowledge.SQLiteStore) error {
				// Prefer package-style re-import of sources with namespaced IDs when possible.
				// Full jsonl import with Overwrite:true merges content; ID collisions overwrite.
				_, err := targetStore.ImportSnapshot(ctx, knowledge.SnapshotImportOptions{
					InputPath:        tmp,
					Overwrite:        true,
					ReplaceAll:       false,
					SkipSafetyBackup: true,
					AbortOnError:     true,
				})
				return err
			})
			_ = os.Remove(tmp)
			if err != nil {
				return fmt.Errorf("import %s: %w", sid, err)
			}
		}
		return s.Host.WithLibraryWrite(ctx, in.TenantID, targetID, func(targetStore *knowledge.SQLiteStore) error {
			target.UpdatedAt = time.Now().UTC()
			target.UpdatedBy = in.Actor
			return s.advanceContentAfterImportLocked(ctx, targetStore, target, "replace_snapshot", in.Actor)
		})
	}()
	if err != nil {
		s.failJob(job, err)
		return job, err
	}

	archive := true
	if in.ArchiveSources != nil {
		archive = *in.ArchiveSources
	}
	if archive {
		for _, sid := range sources {
			_ = s.Repo.ArchiveLibrary(ctx, in.TenantID, sid, time.Now().UTC(), in.Actor)
			src, _ := s.Repo.GetLibrary(ctx, in.TenantID, sid)
			if src != nil {
				now := time.Now().UTC()
				next := src.ContentRev + 1
				_ = s.Repo.InsertChangelog(ctx, &store.DigitalAssetChangelog{
					TenantID: in.TenantID, LibraryID: sid, Rev: next,
					Op: "tombstone_library", PackageStatus: "ready", CreatedAt: now, ReadyAt: &now,
				})
				src.ContentRev = next
				src.Status = store.DigitalAssetStatusArchived
				src.UpdatedAt = now
				src.UpdatedBy = in.Actor
				_ = s.Repo.UpdateLibrary(ctx, src)
			}
			s.Host.Evict(in.TenantID, sid)
		}
	}

	job.Status = "succeeded"
	job.ProgressJSON = string(mustJSON(map[string]any{
		"sources": sources, "phase": "done", "percent": 100, "message": "merge completed",
	}))
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(ctx, job)
	return job, nil
}
