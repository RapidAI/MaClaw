package digitalasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

// Service implements digital asset library management and sync packaging.
type Service struct {
	Repo    store.DigitalAssetRepository
	Host    *KnowledgeHost
	ACL     *Evaluator
	Limiter *SyncLimiter
	// System persists per-tenant digital_assets settings (Admin UI toggles).
	System store.SystemSettingsRepository
	// Settings holds process-level defaults (hub config / env seed).
	Settings TenantSettings
	// Enabled is the process-level seed (config digital_assets.enabled or env).
	// Runtime enablement is LoadTenantSettings(tenant).Enabled (UI can override).
	Enabled bool

	// importStartMu serializes createImportJob checks vs CreateJob (running-job gate).
	importStartMu sync.Mutex
}

// ErrFeatureDisabled is returned when digital_assets.enabled is false.
var ErrFeatureDisabled = errors.New("digital_assets feature disabled")

// ErrNotFound is returned when a library is missing.
var ErrNotFound = errors.New("digital asset library not found")

// ErrForbidden is returned for ACL denials.
var ErrForbidden = errors.New("digital asset access denied")

// ErrInvalid is returned for malformed ACL or other bad admin input.
var ErrInvalid = errors.New("invalid digital asset request")

// ErrTenantSyncDisabled when tenant-level sync is off.
var ErrTenantSyncDisabled = errors.New("tenant_sync_disabled")

// normalizeACL validates mode and canonicalizes department grants.
func normalizeACL(acl ACL) (ACL, error) {
	mode := strings.TrimSpace(acl.Mode)
	if mode == "" {
		mode = ACLModeAllMembers
	}
	if mode != ACLModeAllMembers && mode != ACLModeRestricted {
		return ACL{}, fmt.Errorf("%w: acl_mode must be all_members or restricted", ErrInvalid)
	}
	depts := normalizeACLDepartments(acl.Departments)
	if len(depts) > MaxACLDepartments {
		return ACL{}, fmt.Errorf("%w: departments exceeds %d entries", ErrInvalid, MaxACLDepartments)
	}
	if mode == ACLModeRestricted && len(depts) == 0 {
		return ACL{}, fmt.Errorf("%w: select at least one department for restricted access", ErrInvalid)
	}
	if mode == ACLModeAllMembers {
		depts = []string{}
	}
	return ACL{Mode: mode, Departments: depts}, nil
}

// maxImportJobStaleAge reclaims queued/running jobs that stopped updating
// (process crash / hung worker) so the per-tenant single-job gate cannot stick forever.
const maxImportJobStaleAge = 2 * time.Hour

// CreateLibraryInput is admin create request.
type CreateLibraryInput struct {
	TenantID           string
	Name               string
	Description        string
	ACL                ACL
	SyncEnabled        *bool
	LibraryKind        string
	AcceptsSubmissions *bool
	Actor              string
}

// CreateLibrary creates an empty library.
func (s *Service) CreateLibrary(ctx context.Context, in CreateLibraryInput) (*store.DigitalAssetLibrary, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Repo == nil {
		return nil, errors.New("digital assets repo is nil")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	// quota
	cfg := s.LoadTenantSettings(ctx, tenantID)
	if cfg.MaxLibraries > 0 {
		_, total, err := s.Repo.ListLibraries(ctx, store.DigitalAssetLibraryFilter{
			TenantID: tenantID,
			Status:   store.DigitalAssetStatusActive,
			Limit:    1,
		})
		if err != nil {
			return nil, err
		}
		if total >= cfg.MaxLibraries {
			return nil, fmt.Errorf("max_libraries exceeded (%d)", cfg.MaxLibraries)
		}
	}
	now := time.Now().UTC()
	id := "dal_" + uuid.NewString()
	acl, err := normalizeACL(in.ACL)
	if err != nil {
		return nil, err
	}
	depts, users := EncodeACLJSON(acl)
	syncOn := true
	if in.SyncEnabled != nil {
		syncOn = *in.SyncEnabled
	}
	storePath := ""
	if s.Host != nil {
		storePath = s.Host.LibraryDir(tenantID, id)
		_ = os.MkdirAll(storePath, 0o755)
		_ = os.MkdirAll(s.Host.PackagesDir(tenantID, id), 0o755)
	}
	kind, err := normalizeLibraryKind(in.LibraryKind)
	if err != nil {
		return nil, err
	}
	accepts := true
	if in.AcceptsSubmissions != nil {
		accepts = *in.AcceptsSubmissions
	}
	lib := &store.DigitalAssetLibrary{
		ID:                 id,
		TenantID:           tenantID,
		Name:               name,
		Description:        strings.TrimSpace(in.Description),
		Status:             store.DigitalAssetStatusActive,
		SyncEnabled:        syncOn,
		ACLMode:            acl.Mode,
		ACLDepartmentsJSON: depts,
		ACLUsersJSON:       users,
		StorePath:          storePath,
		LibraryKind:        kind,
		AcceptsSubmissions: accepts,
		CreatedBy:          strings.TrimSpace(in.Actor),
		UpdatedBy:          strings.TrimSpace(in.Actor),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.Repo.CreateLibrary(ctx, lib); err != nil {
		return nil, err
	}
	return lib, nil
}

// UpdateLibraryMeta updates name/description/acl/sync_enabled/kind/accepts_submissions.
func (s *Service) UpdateLibraryMeta(ctx context.Context, tenantID, libraryID string, name, description *string, acl *ACL, syncEnabled *bool, actor string, libraryKind *string, acceptsSubmissions *bool) (*store.DigitalAssetLibrary, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	lib, err := s.Repo.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil || lib.Status == store.DigitalAssetStatusDeleted {
		return nil, ErrNotFound
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return nil, fmt.Errorf("name is required")
		}
		lib.Name = n
	}
	if description != nil {
		lib.Description = strings.TrimSpace(*description)
	}
	if acl != nil {
		norm, nerr := normalizeACL(*acl)
		if nerr != nil {
			return nil, nerr
		}
		*acl = norm
		lib.ACLMode = norm.Mode
		lib.ACLDepartmentsJSON, lib.ACLUsersJSON = EncodeACLJSON(norm)
	}
	if syncEnabled != nil {
		lib.SyncEnabled = *syncEnabled
	}
	if libraryKind != nil {
		kind, kerr := normalizeLibraryKind(*libraryKind)
		if kerr != nil {
			return nil, kerr
		}
		lib.LibraryKind = kind
	}
	if acceptsSubmissions != nil {
		lib.AcceptsSubmissions = *acceptsSubmissions
	}
	lib.UpdatedBy = strings.TrimSpace(actor)
	lib.UpdatedAt = time.Now().UTC()
	if err := s.Repo.UpdateLibrary(ctx, lib); err != nil {
		return nil, err
	}
	return lib, nil
}

// SoftDeleteLibrary marks library deleted.
func (s *Service) SoftDeleteLibrary(ctx context.Context, tenantID, libraryID, actor string) error {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return err
	}
	lib, err := s.Repo.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return err
	}
	if lib == nil {
		return ErrNotFound
	}
	now := time.Now().UTC()
	nextRev := lib.ContentRev + 1
	if err := s.Repo.SoftDeleteLibrary(ctx, tenantID, libraryID, now, actor); err != nil {
		return err
	}
	// Tombstone changelog so clients drop it; keep library content_rev in sync.
	_ = s.Repo.InsertChangelog(ctx, &store.DigitalAssetChangelog{
		TenantID: tenantID, LibraryID: libraryID, Rev: nextRev,
		Op: "tombstone_library", PackageStatus: "ready", CreatedAt: now, ReadyAt: &now,
	})
	lib.ContentRev = nextRev
	lib.Status = store.DigitalAssetStatusDeleted
	lib.DeletedAt = &now
	lib.UpdatedBy = strings.TrimSpace(actor)
	lib.UpdatedAt = now
	_ = s.Repo.UpdateLibrary(ctx, lib)
	if s.Host != nil {
		s.Host.Evict(tenantID, libraryID)
	}
	return nil
}

// ListLibraries lists libraries for a tenant (admin sees all active/archived).
func (s *Service) ListLibraries(ctx context.Context, filter store.DigitalAssetLibraryFilter) ([]*store.DigitalAssetLibrary, int, error) {
	if err := s.requireEnabled(ctx, filter.TenantID); err != nil {
		return nil, 0, err
	}
	return s.Repo.ListLibraries(ctx, filter)
}

// GetLibrary returns a library by id.
func (s *Service) GetLibrary(ctx context.Context, tenantID, libraryID string) (*store.DigitalAssetLibrary, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	lib, err := s.Repo.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil || lib.Status == store.DigitalAssetStatusDeleted {
		return nil, ErrNotFound
	}
	return lib, nil
}

// SearchLibrary runs FTS on a library store.
func (s *Service) SearchLibrary(ctx context.Context, tenantID, libraryID, query string, limit int) ([]knowledge.SearchResult, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Host == nil {
		return nil, errors.New("knowledge host is nil")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var results []knowledge.SearchResult
	err := s.Host.WithLibraryRead(ctx, tenantID, libraryID, func(st *knowledge.SQLiteStore) error {
		var err error
		results, err = st.Search(ctx, knowledge.SearchOptions{Query: query, Limit: limit})
		return err
	})
	return results, err
}

// SourceView is a compact library content row for the admin content dialog.
type SourceView struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	URI          string `json:"uri,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	BatchID      string `json:"batch_id,omitempty"`
	Status       string `json:"status"`
	NodeCount    int    `json:"node_count"`
	CardCount    int    `json:"card_count"`
	FactCount    int    `json:"fact_count,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// DeleteSourcesResult is returned after admin source deletion.
type DeleteSourcesResult struct {
	Deleted   int      `json:"deleted"`
	Requested int      `json:"requested"`
	Missing   []string `json:"missing,omitempty"`
	// Error is set when some sources were deleted but the operation ended with a
	// non-nil error (e.g. mid-batch store failure after partial success).
	Error   string      `json:"error,omitempty"`
	Library LibraryView `json:"library"`
}

// JobView is the admin-facing import job payload with parsed progress.
type JobView struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	LibraryID    string         `json:"library_id"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	Progress     map[string]any `json:"progress"`
	ErrorMessage string         `json:"error,omitempty"`
	CreatedBy    string         `json:"created_by,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

// JobToView converts a store job to API view.
func JobToView(job *store.DigitalAssetImportJob) JobView {
	if job == nil {
		return JobView{}
	}
	progress := map[string]any{}
	if strings.TrimSpace(job.ProgressJSON) != "" {
		_ = json.Unmarshal([]byte(job.ProgressJSON), &progress)
	}
	if progress == nil {
		progress = map[string]any{}
	}
	return JobView{
		ID: job.ID, TenantID: job.TenantID, LibraryID: job.LibraryID,
		Kind: job.Kind, Status: job.Status, Progress: progress,
		ErrorMessage: job.ErrorMessage, CreatedBy: job.CreatedBy,
		CreatedAt: FormatTime(job.CreatedAt), UpdatedAt: FormatTime(job.UpdatedAt),
	}
}

// ListLibrarySourcesResult is a page of library sources for the admin content dialog.
type ListLibrarySourcesResult struct {
	Items   []SourceView
	Total   int
	Limit   int
	Offset  int
	HasMore bool
}

// ListLibrarySources lists imported sources currently in a library knowledge store.
// limit/offset implement server-side pagination (default limit 200, max 1000).
// total prefers library.SourceCount when unfiltered; with a query it is a lower-bound
// estimate based on the current page (offset+count, +1 when has_more).
func (s *Service) ListLibrarySources(ctx context.Context, tenantID, libraryID, query string, limit, offset int) (ListLibrarySourcesResult, error) {
	out := ListLibrarySourcesResult{}
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return out, err
	}
	lib, err := s.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return out, err
	}
	if s.Host == nil {
		return out, errors.New("knowledge host is nil")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	// Guard against abusive deep offsets (admin UI pages from the start).
	const maxOffset = 100000
	if offset > maxOffset {
		offset = maxOffset
	}
	out.Limit = limit
	out.Offset = offset
	q := strings.TrimSpace(query)
	// Fetch one extra row to detect has_more without a separate COUNT query.
	// knowledge.ListSources hard-caps Limit at 5000.
	fetchLimit := limit + 1
	const knowledgeListCap = 5000
	if fetchLimit > knowledgeListCap {
		fetchLimit = knowledgeListCap
	}
	err = s.Host.WithLibraryRead(ctx, tenantID, libraryID, func(st *knowledge.SQLiteStore) error {
		sources, lerr := st.ListSources(ctx, knowledge.ListSourcesOptions{
			Query:           q,
			Limit:           fetchLimit,
			Offset:          offset,
			IncludeDisabled: true,
		})
		if lerr != nil {
			return lerr
		}
		if len(sources) > limit {
			out.HasMore = true
			sources = sources[:limit]
		} else if fetchLimit <= limit && len(sources) >= limit {
			// Cap prevented limit+1 probe; treat a full page as potentially more.
			out.HasMore = true
		}
		out.Items = make([]SourceView, 0, len(sources))
		for _, src := range sources {
			out.Items = append(out.Items, sourceToView(src))
		}
		pageEnd := offset + len(out.Items)
		if q == "" {
			out.Total = int(lib.SourceCount)
			if out.Total < pageEnd {
				out.Total = pageEnd
			}
			if out.HasMore && out.Total <= pageEnd {
				out.Total = pageEnd + 1
			}
		} else {
			out.Total = pageEnd
			if out.HasMore {
				out.Total++
			}
		}
		return nil
	})
	return out, err
}

func sourceToView(src knowledge.Source) SourceView {
	title := strings.TrimSpace(src.Title)
	if title == "" {
		title = strings.TrimSpace(src.RelativePath)
	}
	if title == "" {
		title = strings.TrimSpace(src.URI)
	}
	if title == "" {
		title = src.ID
	}
	return SourceView{
		ID: src.ID, Title: title, Kind: src.Kind, URI: src.URI,
		RelativePath: src.RelativePath, BatchID: src.BatchID, Status: src.Status,
		NodeCount: src.NodeCount, CardCount: src.CardCount, FactCount: src.FactCount,
		ContentHash: src.ContentHash, ErrorMessage: src.ErrorMessage,
		CreatedAt: FormatTime(src.CreatedAt), UpdatedAt: FormatTime(src.UpdatedAt),
	}
}

// DeleteLibrarySource removes one knowledge source and publishes a new replace_snapshot package.
func (s *Service) DeleteLibrarySource(ctx context.Context, tenantID, libraryID, sourceID, actor string) (*DeleteSourcesResult, error) {
	return s.DeleteLibrarySources(ctx, tenantID, libraryID, []string{sourceID}, actor)
}

// DeleteLibrarySources removes one or more knowledge sources, recounts the library,
// and advances content_rev via replace_snapshot so clients drop deleted content.
func (s *Service) DeleteLibrarySources(ctx context.Context, tenantID, libraryID string, sourceIDs []string, actor string) (*DeleteSourcesResult, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Host == nil || s.Repo == nil {
		return nil, errors.New("knowledge host or repo is nil")
	}
	ids := uniqueNonEmptyStrings(sourceIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("source_id is required")
	}
	if len(ids) > 500 {
		return nil, fmt.Errorf("too many source ids (max 500)")
	}
	lib, err := s.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, ErrNotFound
	}
	if lib.Status != store.DigitalAssetStatusActive {
		return nil, fmt.Errorf("library is not active (status=%s)", lib.Status)
	}
	// Gate only the running-job check (do not hold the mutex across package export —
	// that would block unrelated imports for the whole packaging duration).
	// Store mutation vs an already-running import is serialized by WithLibraryWrite.
	s.importStartMu.Lock()
	s.reclaimStaleImportJobs(ctx, tenantID)
	if n, cerr := s.Repo.CountRunningJobs(ctx, tenantID); cerr == nil && n > 0 {
		s.importStartMu.Unlock()
		return nil, fmt.Errorf("tenant has a running import job; wait for it to finish before deleting sources")
	}
	s.importStartMu.Unlock()

	var missing []string
	deleted := 0
	var partialErr error
	err = s.Host.WithLibraryWrite(ctx, tenantID, libraryID, func(st *knowledge.SQLiteStore) error {
		// Note: a concurrent import may start after the gate above; library write locks
		// serialize store/package work per library. Do not re-check CountRunningJobs here —
		// an import on another library would spuriously block this delete.

		// Resolve existence in one list call instead of N hydrated GetSource round-trips.
		// Limit must be at least 1; knowledge defaults empty limit to 100 which could
		// truncate a large batch and mark existing ids as missing.
		existing := make(map[string]struct{}, len(ids))
		listLimit := len(ids)
		if listLimit < 1 {
			listLimit = 1
		}
		found, lerr := st.ListSources(ctx, knowledge.ListSourcesOptions{
			SourceIDs:       ids,
			Limit:           listLimit,
			IncludeDisabled: true,
		})
		if lerr != nil {
			return lerr
		}
		for _, src := range found {
			if src.ID != "" {
				existing[src.ID] = struct{}{}
			}
		}
		toDelete := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, ok := existing[id]; !ok {
				missing = append(missing, id)
				continue
			}
			toDelete = append(toDelete, id)
		}
		for _, id := range toDelete {
			if derr := st.DeleteSource(ctx, id); derr != nil {
				partialErr = derr
				break
			}
			deleted++
		}
		if deleted == 0 {
			if partialErr != nil {
				return partialErr
			}
			return ErrNotFound
		}
		// Recount. knowledge ListSources hard-caps at 5000 — fall back to arithmetic when capped.
		const listCap = 5000
		if sources, lerr := st.ListSources(ctx, knowledge.ListSourcesOptions{Limit: listCap, IncludeDisabled: true}); lerr == nil {
			if len(sources) < listCap {
				lib.SourceCount = int64(len(sources))
			} else if lib.SourceCount >= int64(deleted) {
				lib.SourceCount -= int64(deleted)
				if lib.SourceCount < int64(listCap) {
					lib.SourceCount = int64(listCap)
				}
			} else {
				lib.SourceCount = int64(listCap)
			}
		} else if lib.SourceCount >= int64(deleted) {
			lib.SourceCount -= int64(deleted)
		}
		lib.UpdatedAt = time.Now().UTC()
		lib.UpdatedBy = actor
		// Always advance package when any delete succeeded so clients cannot keep
		// serving removed sources after a mid-batch store error.
		if advErr := s.advanceContentAfterImportLocked(ctx, st, lib, "replace_snapshot", actor); advErr != nil {
			return advErr
		}
		return partialErr
	})
	// Reload library for response (content_rev may have advanced even on partialErr).
	if refreshed, gerr := s.GetLibrary(ctx, tenantID, libraryID); gerr == nil && refreshed != nil {
		lib = refreshed
	}
	res := &DeleteSourcesResult{
		Deleted:   deleted,
		Requested: len(ids),
		Missing:   missing,
		Library:   LibraryToView(lib),
	}
	if err != nil && deleted == 0 {
		return nil, err
	}
	// Partial success: return result + error so admin UI can show deleted count.
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	return res, nil
}

// isKnowledgeSourceNotFound reports whether err looks like a missing knowledge source.
// Kept for callers/tests that still classify GetSource-style errors.
func isKnowledgeSourceNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no rows")
}

func uniqueNonEmptyStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ListImportJobs returns recent import jobs for a library.
func (s *Service) ListImportJobs(ctx context.Context, tenantID, libraryID string, limit int) ([]JobView, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Repo == nil {
		return nil, errors.New("repo nil")
	}
	if _, err := s.GetLibrary(ctx, tenantID, libraryID); err != nil {
		return nil, err
	}
	jobs, err := s.Repo.ListJobs(ctx, tenantID, libraryID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]JobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, JobToView(j))
	}
	return out, nil
}

// GetImportJob returns one job for polling.
func (s *Service) GetImportJob(ctx context.Context, tenantID, jobID string) (*JobView, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Repo == nil {
		return nil, errors.New("repo nil")
	}
	job, err := s.Repo.GetJob(ctx, tenantID, jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, ErrNotFound
	}
	v := JobToView(job)
	return &v, nil
}

// ImportDirectoryIntoLibrary imports documents from a local directory synchronously
// (used by tests and internal callers). Prefer BeginImportDirectory for admin UI.
func (s *Service) ImportDirectoryIntoLibrary(ctx context.Context, tenantID, libraryID, root, actor, kind string) (*store.DigitalAssetImportJob, error) {
	job, err := s.createImportJob(ctx, tenantID, libraryID, actor, kind, root, nil)
	if err != nil {
		return nil, err
	}
	s.runImportDirectory(ctx, job, root, actor, kind, nil, "")
	updated, _ := s.Repo.GetJob(ctx, tenantID, job.ID)
	if updated != nil {
		return updated, jobErrorFromStatus(updated)
	}
	return job, nil
}

// BeginImportDirectory creates a job and imports in the background so clients can poll progress.
// cleanupDir is removed after the job finishes (success or failure); empty skips cleanup.
func (s *Service) BeginImportDirectory(ctx context.Context, tenantID, libraryID, root, actor, kind string, fileLabels []string, cleanupDir string) (*store.DigitalAssetImportJob, error) {
	job, err := s.createImportJob(ctx, tenantID, libraryID, actor, kind, root, fileLabels)
	if err != nil {
		if cleanupDir != "" {
			_ = os.RemoveAll(cleanupDir)
		}
		return nil, err
	}
	go s.runImportDirectory(context.Background(), job, root, actor, kind, fileLabels, cleanupDir)
	return job, nil
}

// BeginImportLocalDir starts an async server-directory import (no temp cleanup).
func (s *Service) BeginImportLocalDir(ctx context.Context, tenantID, libraryID, path, actor string) (*store.DigitalAssetImportJob, error) {
	return s.BeginImportDirectory(ctx, tenantID, libraryID, path, actor, "local_dir", []string{path}, "")
}

func jobErrorFromStatus(job *store.DigitalAssetImportJob) error {
	if job == nil {
		return nil
	}
	if job.Status == "failed" && strings.TrimSpace(job.ErrorMessage) != "" {
		return errors.New(job.ErrorMessage)
	}
	return nil
}

func (s *Service) createImportJob(ctx context.Context, tenantID, libraryID, actor, kind, root string, fileLabels []string) (*store.DigitalAssetImportJob, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	lib, err := s.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return nil, err
	}
	if lib != nil && lib.Status != store.DigitalAssetStatusActive {
		return nil, fmt.Errorf("library is not active (status=%s)", lib.Status)
	}
	s.importStartMu.Lock()
	defer s.importStartMu.Unlock()
	s.reclaimStaleImportJobs(ctx, tenantID)
	if n, err := s.Repo.CountRunningJobs(ctx, tenantID); err == nil && n > 0 {
		return nil, fmt.Errorf("tenant already has a running import job")
	}
	now := time.Now().UTC()
	rootLabel := filepath.Base(strings.TrimSpace(root))
	if rootLabel == "." || rootLabel == string(filepath.Separator) || rootLabel == "" {
		rootLabel = root
	}
	labels := append([]string{}, fileLabels...)
	if len(labels) > 40 {
		labels = labels[:40]
	}
	progress := map[string]any{
		"phase":        "queued",
		"percent":      0,
		"root_path":    root,
		"root_label":   rootLabel,
		"file_names":   labels,
		"file_count":   len(fileLabels),
		"total_files":  0,
		"processed":    0,
		"imported":     0,
		"failed":       0,
		"current_file": "",
		"message":      "queued",
	}
	job := &store.DigitalAssetImportJob{
		ID: "daij_" + uuid.NewString(), TenantID: tenantID, LibraryID: libraryID,
		Kind: kind, Status: "running", ProgressJSON: mustJSONString(progress),
		CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// reclaimStaleImportJobs fails long-idle queued/running jobs so the tenant gate unblocks.
func (s *Service) reclaimStaleImportJobs(ctx context.Context, tenantID string) {
	if s == nil || s.Repo == nil {
		return
	}
	cutoff := time.Now().UTC().Add(-maxImportJobStaleAge)
	_, _ = s.Repo.FailStaleRunningJobs(ctx, tenantID, cutoff, "import job timed out (no progress for 2h; reclaimed)")
}

func (s *Service) failJob(job *store.DigitalAssetImportJob, err error) {
	if job == nil {
		return
	}
	// Idempotent: do not overwrite a terminal status if a late fail races.
	if job.Status == "succeeded" || job.Status == "failed" {
		return
	}
	job.Status = "failed"
	if err != nil {
		job.ErrorMessage = err.Error()
	}
	// Merge failure into progress so poll clients / history show phase=failed.
	progress := map[string]any{}
	if strings.TrimSpace(job.ProgressJSON) != "" {
		_ = json.Unmarshal([]byte(job.ProgressJSON), &progress)
	}
	if progress == nil {
		progress = map[string]any{}
	}
	progress["phase"] = "failed"
	if err != nil {
		progress["message"] = err.Error()
		progress["error"] = err.Error()
	}
	if _, ok := progress["percent"]; !ok {
		progress["percent"] = 0
	}
	job.ProgressJSON = mustJSONString(progress)
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(context.Background(), job)
}

// progressPercent coerces progress map percent values (int / float64 / json.Number) for throttle math.
func progressPercent(p map[string]any) int {
	if p == nil {
		return 0
	}
	switch v := p["percent"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func (s *Service) runImportDirectory(ctx context.Context, job *store.DigitalAssetImportJob, root, actor, kind string, fileLabels []string, cleanupDir string) {
	if job == nil {
		return
	}
	if cleanupDir != "" {
		defer os.RemoveAll(cleanupDir)
	}
	if s.Host == nil || s.Repo == nil {
		s.failJob(job, errors.New("knowledge host or repo is nil"))
		return
	}
	tenantID := job.TenantID
	libraryID := job.LibraryID

	var progressMu sync.Mutex
	var lastWrite time.Time
	var lastPercent int
	updateProgress := func(p map[string]any, force bool) {
		if p == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		// Never clobber a terminal status with late progress callbacks.
		if job.Status == "succeeded" || job.Status == "failed" {
			return
		}
		percent := progressPercent(p)
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		p["percent"] = percent
		now := time.Now().UTC()
		// Throttle DB writes: at most ~3/sec unless forced or percent jumps by 5+.
		if !force && !lastWrite.IsZero() && now.Sub(lastWrite) < 350*time.Millisecond && percent-lastPercent < 5 {
			return
		}
		job.ProgressJSON = mustJSONString(p)
		job.UpdatedAt = now
		_ = s.Repo.UpdateJob(ctx, job)
		lastWrite = now
		lastPercent = percent
	}

	rootLabel := filepath.Base(root)
	updateProgress(map[string]any{
		"phase": "importing", "percent": 5,
		"root_path": root, "root_label": rootLabel,
		"file_names": fileLabels, "file_count": len(fileLabels),
		"message": "starting import",
	}, true)

	lib, err := s.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		s.failJob(job, err)
		return
	}

	err = s.Host.WithLibraryWrite(ctx, tenantID, libraryID, func(st *knowledge.SQLiteStore) error {
		st.SetImportProgressCallback(func(res knowledge.DirectoryImportResult) {
			total := res.TotalFiles
			if total <= 0 {
				total = res.QueuedFiles
			}
			processed := res.ProcessedFiles
			if processed <= 0 {
				processed = res.ImportedFiles + res.FailedFiles + res.SkippedFiles + res.DuplicateFiles
			}
			percent := 10
			if total > 0 {
				percent = 10 + (processed * 75 / total)
				if percent > 90 {
					percent = 90
				}
			}
			cur := res.CurrentFile
			if cur == "" {
				cur = res.LastItemPath
			}
			// Prefer finer step progress within the current file when available.
			// knowledge callbacks report processed = fully completed files while current is in-flight.
			if total > 0 && res.TotalSteps > 0 && res.CurrentStepNum > 0 {
				stepFrac := float64(res.CurrentStepNum-1) / float64(res.TotalSteps)
				if stepFrac < 0 {
					stepFrac = 0
				}
				if stepFrac > 1 {
					stepFrac = 1
				}
				partial := (float64(processed) + stepFrac) / float64(total)
				if partial > 1 {
					partial = 1
				}
				percent = 10 + int(partial*75)
				if percent < 10 {
					percent = 10
				}
				if percent > 90 {
					percent = 90
				}
			}
			updateProgress(map[string]any{
				"phase":            "importing",
				"percent":          percent,
				"root_path":        root,
				"root_label":       rootLabel,
				"file_names":       fileLabels,
				"file_count":       len(fileLabels),
				"total_files":      total,
				"processed":        processed,
				"imported":         res.ImportedFiles,
				"failed":           res.FailedFiles,
				"skipped":          res.SkippedFiles,
				"duplicates":       res.DuplicateFiles,
				"current_file":     cur,
				"current_step":     res.CurrentStep,
				"current_step_num": res.CurrentStepNum,
				"total_steps":      res.TotalSteps,
				"batch_id":         res.BatchID,
				"last_item_path":   res.LastItemPath,
				"last_item_status": res.LastItemStatus,
				"message":          "importing files",
			}, false)
		})
		defer st.SetImportProgressCallback(nil)

		updateProgress(map[string]any{
			"phase": "importing", "percent": 12,
			"root_path": root, "root_label": rootLabel,
			"file_names": fileLabels, "message": "scanning and importing files",
		}, true)
		res, err := st.ImportDirectory(ctx, knowledge.DirectoryImportRequest{
			RootPath:  root,
			Recursive: true,
		})
		if err != nil {
			return err
		}
		// Progress callback schedules linking/embedding in background; wait before packaging.
		st.WaitBackground()

		// Refresh source_count from store so re-imports / skips do not drift.
		if sources, lerr := st.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 100000, IncludeDisabled: true}); lerr == nil {
			lib.SourceCount = int64(len(sources))
		} else if res.ImportedFiles > 0 {
			lib.SourceCount += int64(res.ImportedFiles)
		}
		lib.UpdatedAt = time.Now().UTC()
		lib.UpdatedBy = actor
		updateProgress(map[string]any{
			"phase": "packaging", "percent": 92,
			"root_path": root, "root_label": rootLabel,
			"file_names": fileLabels, "file_count": len(fileLabels),
			"total_files": res.TotalFiles, "imported": res.ImportedFiles,
			"failed": res.FailedFiles, "skipped": res.SkippedFiles,
			"duplicates": res.DuplicateFiles, "batch_id": res.BatchID,
			"message": "building sync package",
		}, true)
		if err := s.advanceContentAfterImportLocked(ctx, st, lib, "replace_snapshot", actor); err != nil {
			return err
		}
		// Capture a readable file sample for import history.
		names := append([]string{}, fileLabels...)
		if len(names) == 0 && res.LastItemPath != "" {
			names = []string{res.LastItemPath}
		}
		if len(names) > 40 {
			names = names[:40]
		}
		updateProgress(map[string]any{
			"phase": "done", "percent": 100,
			"root_path": root, "root_label": rootLabel,
			"file_names": names, "file_count": maxInt(len(fileLabels), res.ImportedFiles),
			"total_files": res.TotalFiles, "imported": res.ImportedFiles,
			"failed": res.FailedFiles, "skipped": res.SkippedFiles,
			"duplicates": res.DuplicateFiles, "batch_id": res.BatchID,
			"content_rev": lib.ContentRev,
			"kind":        kind,
		}, true)
		return nil
	})
	if err != nil {
		s.failJob(job, err)
		return
	}
	progressMu.Lock()
	job.Status = "succeeded"
	job.UpdatedAt = time.Now().UTC()
	// Ensure final progress snapshot is terminal even if last updateProgress was throttled.
	if strings.TrimSpace(job.ProgressJSON) != "" {
		final := map[string]any{}
		if json.Unmarshal([]byte(job.ProgressJSON), &final) == nil && final != nil {
			final["phase"] = "done"
			final["percent"] = 100
			job.ProgressJSON = mustJSONString(final)
		}
	}
	_ = s.Repo.UpdateJob(ctx, job)
	progressMu.Unlock()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mustJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ImportUploadFiles saves uploaded files into a temp tree and starts an async import job.
// kind is typically "upload" or "browser_dir".
func (s *Service) ImportUploadFiles(ctx context.Context, tenantID, libraryID, actor string, files map[string][]byte, kind string) (*store.DigitalAssetImportJob, error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return nil, err
	}
	if s.Host == nil {
		return nil, errors.New("knowledge host is nil")
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "upload"
	}
	jobID := "daij_" + uuid.NewString()
	tmp := s.Host.TmpDir(tenantID, jobID)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	tmpAbs, err := filepath.Abs(tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	labels := make([]string, 0, len(files))
	for rel, data := range files {
		dest, err := safeJoinUnderRoot(tmpAbs, rel)
		if err != nil {
			_ = os.RemoveAll(tmp)
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			_ = os.RemoveAll(tmp)
			return nil, err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			_ = os.RemoveAll(tmp)
			return nil, err
		}
		labels = append(labels, filepath.ToSlash(rel))
	}
	return s.BeginImportDirectory(ctx, tenantID, libraryID, tmp, actor, kind, labels, tmp)
}

// safeJoinUnderRoot joins root and a relative path, rejecting absolute paths and ".." escapes.
func safeJoinUnderRoot(rootAbs, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty relative path")
	}
	// Reject absolute / drive / UNC forms before normalizing.
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") ||
		strings.HasPrefix(rel, "//") || len(rel) >= 2 && rel[1] == ':' {
		return "", fmt.Errorf("absolute path not allowed %q", rel)
	}
	// Normalize to slash form then clean as OS path.
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(rel, "/") || rel == "" || rel == "." {
		return "", fmt.Errorf("invalid relative path")
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path %q", rel)
	}
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute path not allowed %q", rel)
	}
	dest := filepath.Join(rootAbs, cleaned)
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if destAbs != rootAbs && !strings.HasPrefix(destAbs, rootAbs+sep) {
		return "", fmt.Errorf("path escapes root: %q", rel)
	}
	return destAbs, nil
}

// advanceContentAfterImportLocked builds a ready snapshot package and bumps content_rev.
// Caller must already hold the library write lock (via WithLibraryWrite).
func (s *Service) advanceContentAfterImportLocked(ctx context.Context, st *knowledge.SQLiteStore, lib *store.DigitalAssetLibrary, op, actor string) error {
	if s.Host == nil || s.Repo == nil || st == nil {
		return errors.New("host, repo, or store nil")
	}
	nextRev := lib.ContentRev + 1
	now := time.Now().UTC()
	if err := s.Repo.InsertChangelog(ctx, &store.DigitalAssetChangelog{
		TenantID: lib.TenantID, LibraryID: lib.ID, Rev: nextRev, Op: op,
		PackageStatus: "pending", CreatedAt: now,
	}); err != nil {
		return err
	}
	pkgName := fmt.Sprintf("rev_%d.jsonl", nextRev)
	pkgPath := filepath.Join(s.Host.PackagesDir(lib.TenantID, lib.ID), pkgName)
	if _, err := st.ExportSnapshot(ctx, knowledge.ExportOptions{
		OutputPath: pkgPath,
		Format:     "jsonl",
	}); err != nil {
		_ = s.Repo.UpdateChangelogPackage(ctx, lib.TenantID, lib.ID, nextRev, "failed", "", "", 0, "", err.Error(), nil)
		return err
	}
	contentHash, pkgBytes, err := fileSHA256(pkgPath)
	if err != nil {
		_ = s.Repo.UpdateChangelogPackage(ctx, lib.TenantID, lib.ID, nextRev, "failed", "", "", 0, "", err.Error(), nil)
		return err
	}
	readyAt := time.Now().UTC()
	pkgRef := filepath.ToSlash(filepath.Join("packages", pkgName))
	if err := s.Repo.UpdateChangelogPackage(ctx, lib.TenantID, lib.ID, nextRev, "ready",
		pkgRef, contentHash, pkgBytes, contentHash, "", &readyAt); err != nil {
		return err
	}
	lib.ContentRev = nextRev
	lib.ContentHash = contentHash
	lib.UpdatedAt = readyAt
	lib.UpdatedBy = actor
	return s.Repo.UpdateLibrary(ctx, lib)
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

func normalizeLibraryKind(kind string) (string, error) {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		return LibraryKindBusiness, nil
	}
	if kind != LibraryKindBusiness && kind != LibraryKindTechnical {
		return "", fmt.Errorf("%w: library_kind must be business or technical", ErrInvalid)
	}
	return kind, nil
}

func (s *Service) requireEnabled(ctx context.Context, tenantID string) error {
	if s == nil {
		return ErrFeatureDisabled
	}
	if !s.IsFeatureEnabled(ctx, tenantID) {
		return ErrFeatureDisabled
	}
	return nil
}

// LibraryToView converts store model to API view.
func LibraryToView(lib *store.DigitalAssetLibrary) LibraryView {
	if lib == nil {
		return LibraryView{}
	}
	acl := ParseACL(lib.ACLMode, lib.ACLDepartmentsJSON, lib.ACLUsersJSON)
	kind := strings.TrimSpace(lib.LibraryKind)
	if kind == "" {
		kind = LibraryKindBusiness
	}
	return LibraryView{
		ID: lib.ID, TenantID: lib.TenantID, Name: lib.Name, Description: lib.Description,
		Status: lib.Status, SyncEnabled: lib.SyncEnabled, ACLMode: acl.Mode,
		Departments: acl.Departments, ACLFingerprint: acl.Fingerprint(),
		ContentRev: lib.ContentRev, ContentHash: lib.ContentHash,
		SourceCount: lib.SourceCount, CardCount: lib.CardCount, ByteSize: lib.ByteSize,
		LibraryKind: kind, AcceptsSubmissions: lib.AcceptsSubmissions,
		CreatedBy: lib.CreatedBy, UpdatedBy: lib.UpdatedBy,
		CreatedAt: FormatTime(lib.CreatedAt), UpdatedAt: FormatTime(lib.UpdatedAt),
	}
}

// ManifestLibrary is one entry in sync manifest.
type ManifestLibrary struct {
	LibraryID      string `json:"library_id"`
	Name           string `json:"name"`
	ContentRev     int64  `json:"content_rev"`
	ContentHash    string `json:"content_hash"`
	ACLFingerprint string `json:"acl_fingerprint"`
	SyncEnabled    bool   `json:"sync_enabled"`
}

// BuildManifest returns libraries the user may sync.
func (s *Service) BuildManifest(ctx context.Context, tenantID, email string) (tenantSync bool, libs []ManifestLibrary, err error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return false, nil, err
	}
	cfg := s.LoadTenantSettings(ctx, tenantID)
	if !cfg.SyncEnabled {
		return false, nil, nil
	}
	items, _, err := s.Repo.ListLibraries(ctx, store.DigitalAssetLibraryFilter{
		TenantID: tenantID,
		Status:   store.DigitalAssetStatusActive,
		Limit:    200,
	})
	if err != nil {
		return false, nil, err
	}
	out := make([]ManifestLibrary, 0, len(items))
	for _, lib := range items {
		if lib == nil || !lib.SyncEnabled {
			continue
		}
		if s.ACL != nil {
			ok, aerr := s.ACL.CanAccessLibrary(ctx, lib, email)
			if aerr != nil {
				return false, nil, aerr
			}
			if !ok {
				continue
			}
		}
		acl := ParseACL(lib.ACLMode, lib.ACLDepartmentsJSON, lib.ACLUsersJSON)
		out = append(out, ManifestLibrary{
			LibraryID: lib.ID, Name: lib.Name, ContentRev: lib.ContentRev,
			ContentHash: lib.ContentHash, ACLFingerprint: acl.Fingerprint(),
			SyncEnabled: lib.SyncEnabled,
		})
	}
	return true, out, nil
}

// PullOp is one sync operation returned to client.
type PullOp struct {
	Rev            int64  `json:"rev"`
	Op             string `json:"op"`
	PackageURL     string `json:"package_url"`
	PackageSHA256  string `json:"package_sha256"`
	PackageBytes   int64  `json:"package_bytes"`
	ContentHash    string `json:"content_hash"`
	PackageFormat  string `json:"package_format"`
	ACLFingerprint string `json:"acl_fingerprint,omitempty"`
}

// Pull returns ready changelog ops after sinceRev for an authorized user.
func (s *Service) Pull(ctx context.Context, tenantID, libraryID, email, userID, deviceID string, sinceRev int64) (reason string, ops []PullOp, err error) {
	if err := s.requireEnabled(ctx, tenantID); err != nil {
		return "", nil, err
	}
	cfg := s.LoadTenantSettings(ctx, tenantID)
	if !cfg.SyncEnabled {
		return "tenant_sync_disabled", nil, nil
	}
	lib, err := s.GetLibrary(ctx, tenantID, libraryID)
	if err != nil {
		return "", nil, err
	}
	if s.ACL != nil {
		ok, aerr := s.ACL.CanAccessLibrary(ctx, lib, email)
		if aerr != nil {
			return "", nil, aerr
		}
		if !ok {
			return "", nil, ErrForbidden
		}
	}
	if !lib.SyncEnabled {
		return "library_sync_disabled", nil, nil
	}
	if s.Limiter != nil {
		if ok, retry := s.Limiter.AllowPull(tenantID, userID); !ok {
			return fmt.Sprintf("rate_limited:%d", retry), nil, nil
		}
		release, aerr := s.Limiter.AcquireSlot(tenantID)
		if aerr != nil {
			return "tenant_busy", nil, nil
		}
		defer release()
	}
	rows, err := s.Repo.ListChangelogSince(ctx, tenantID, libraryID, sinceRev, true, 50)
	if err != nil {
		return "", nil, err
	}
	// Gap detection: client skipped too far (changelog GC). Force full replace at tip.
	if sinceRev > 0 && len(rows) == 0 && lib.ContentRev > sinceRev {
		// No ready revs after sinceRev but tip is ahead — orphaned cursor; return tip package as replace.
		if tip, gerr := s.Repo.GetChangelog(ctx, tenantID, libraryID, lib.ContentRev); gerr == nil && tip != nil && tip.PackageStatus == "ready" {
			rows = []*store.DigitalAssetChangelog{tip}
		}
	} else if sinceRev > 0 && len(rows) > 0 && rows[0].Rev > sinceRev+1 {
		// Missing intermediate revs — bootstrap with latest replace_snapshot if available.
		if tip, gerr := s.Repo.GetChangelog(ctx, tenantID, libraryID, lib.ContentRev); gerr == nil && tip != nil && tip.PackageStatus == "ready" {
			tip.Op = "replace_snapshot"
			rows = []*store.DigitalAssetChangelog{tip}
		}
	}
	acl := ParseACL(lib.ACLMode, lib.ACLDepartmentsJSON, lib.ACLUsersJSON)
	fp := acl.Fingerprint()
	ops = make([]PullOp, 0, len(rows))
	var maxOpRev int64
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.Rev > maxOpRev {
			maxOpRev = row.Rev
		}
		// Always emit authenticated package URL by rev (do not expose raw storage paths).
		pkgURL := fmt.Sprintf("/api/digital-assets/libraries/%s/sync/packages/%d", libraryID, row.Rev)
		ops = append(ops, PullOp{
			Rev: row.Rev, Op: row.Op,
			PackageURL:    pkgURL,
			PackageSHA256: row.PackageSHA256, PackageBytes: row.PackageBytes,
			ContentHash: row.ContentHash, PackageFormat: PackageFormatJSONL,
			ACLFingerprint: fp,
		})
	}
	if deviceID != "" && userID != "" {
		// Telemetry only: record highest rev returned this pull (not assumed applied).
		cursorRev := sinceRev
		if maxOpRev > cursorRev {
			cursorRev = maxOpRev
		}
		_ = s.Repo.UpsertSyncCursor(ctx, &store.DigitalAssetSyncCursor{
			TenantID: tenantID, LibraryID: libraryID, UserID: userID, DeviceID: deviceID,
			LastRev: cursorRev, LastSyncAt: time.Now().UTC(), LastStatus: "pull",
		})
	}
	return "", ops, nil
}

// ResolvePackagePath maps package_ref to absolute path if under library packages dir.
func (s *Service) ResolvePackagePath(tenantID, libraryID, packageRef string) (string, error) {
	if s.Host == nil {
		return "", errors.New("host nil")
	}
	ref := strings.TrimSpace(packageRef)
	ref = strings.TrimPrefix(ref, "packages/")
	ref = strings.TrimPrefix(ref, string(filepath.Separator))
	ref = filepath.Clean(ref)
	if ref == "." || ref == ".." || strings.HasPrefix(ref, "..") {
		return "", fmt.Errorf("invalid package ref")
	}
	base := s.Host.PackagesDir(tenantID, libraryID)
	full := filepath.Join(base, ref)
	baseAbs, _ := filepath.Abs(base)
	fullAbs, _ := filepath.Abs(full)
	if !strings.HasPrefix(fullAbs, baseAbs+string(filepath.Separator)) && fullAbs != baseAbs {
		return "", fmt.Errorf("invalid package path")
	}
	return full, nil
}

// PackagePathForRev returns absolute path for a ready rev package file.
func (s *Service) PackagePathForRev(ctx context.Context, tenantID, libraryID string, rev int64) (string, error) {
	row, err := s.Repo.GetChangelog(ctx, tenantID, libraryID, rev)
	if err != nil {
		return "", err
	}
	if row == nil || row.PackageStatus != "ready" {
		return "", fmt.Errorf("package not ready")
	}
	return s.ResolvePackagePath(tenantID, libraryID, row.PackageRef)
}

// DumpSettingsJSON is helper for tests.
func DumpSettingsJSON(s TenantSettings) string {
	b, _ := json.Marshal(s)
	return string(b)
}
