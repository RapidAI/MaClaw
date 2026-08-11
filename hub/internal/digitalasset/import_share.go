package digitalasset

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

// SharePackageLoader resolves a knowledge share package file path on this Hub.
type SharePackageLoader interface {
	Get(ctx context.Context, knowledgeID string) (*store.KnowledgeShare, error)
	// ResolvePackagePath returns absolute path to package JSON if stored locally.
	ResolvePackagePath(storageRef string) (string, bool)
	IncrementImport(ctx context.Context, knowledgeID string) error
}

// KnowledgeShareFileLoader is the default disk + repo loader.
type KnowledgeShareFileLoader struct {
	Repo       store.KnowledgeShareRepository
	PackageDir string
}

func (l KnowledgeShareFileLoader) Get(ctx context.Context, knowledgeID string) (*store.KnowledgeShare, error) {
	if l.Repo == nil {
		return nil, fmt.Errorf("knowledge share repo nil")
	}
	return l.Repo.Get(ctx, knowledgeID)
}

func (l KnowledgeShareFileLoader) ResolvePackagePath(storageRef string) (string, bool) {
	storageRef = strings.TrimSpace(storageRef)
	packageDir := strings.TrimSpace(l.PackageDir)
	if !strings.HasPrefix(storageRef, "local:knowledge-packages/") || packageDir == "" {
		return "", false
	}
	name := strings.TrimPrefix(storageRef, "local:knowledge-packages/")
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	path := filepath.Join(packageDir, name)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

func (l KnowledgeShareFileLoader) IncrementImport(ctx context.Context, knowledgeID string) error {
	if l.Repo == nil {
		return nil
	}
	return l.Repo.IncrementCounters(ctx, knowledgeID, 0, 1, time.Now().UTC())
}

// ParseKnowledgeShareRef extracts knowledge_id from URL, path, or raw id.
func ParseKnowledgeShareRef(ref string) (knowledgeID string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("share_ref is required")
	}
	// Pure id
	if !strings.Contains(ref, "/") && !strings.Contains(ref, "?") && !strings.Contains(ref, ":") {
		return ref, nil
	}
	// URL or path
	if u, perr := url.Parse(ref); perr == nil && (u.Scheme != "" || strings.HasPrefix(ref, "/")) {
		path := u.Path
		if path == "" {
			path = ref
		}
		// /api/knowledge/shares/{id}[/package]
		// /hub/knowledge/shares/{id}
		// /k/{id}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		for i, p := range parts {
			if p == "shares" && i+1 < len(parts) {
				id := parts[i+1]
				if id != "package" && id != "mine" {
					return id, nil
				}
			}
			if p == "k" && i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
		// last segment if looks like id
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if last != "package" && last != "shares" && last != "" {
				return last, nil
			}
		}
	}
	// Fallback: take last path segment
	ref = strings.TrimRight(ref, "/")
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		cand := ref[i+1:]
		if j := strings.Index(cand, "?"); j >= 0 {
			cand = cand[:j]
		}
		if cand != "" && cand != "package" {
			return cand, nil
		}
	}
	return "", fmt.Errorf("invalid_share_ref")
}

type sharePackageFile struct {
	Manifest struct {
		Format    string `json:"format"`
		Title     string `json:"title"`
		PackageID string `json:"package_id"`
	} `json:"manifest"`
	Sources []struct {
		ID               string   `json:"id"`
		Kind             string   `json:"kind"`
		URI              string   `json:"uri"`
		CanonicalURI     string   `json:"canonical_uri"`
		Title            string   `json:"title"`
		TopicHint        string   `json:"topic_hint"`
		Labels           []string `json:"labels"`
		Content          string   `json:"content"`
		ContentTruncated bool     `json:"content_truncated"`
	} `json:"sources"`
}

// ImportKnowledgeShareInput is admin share→enterprise import.
type ImportKnowledgeShareInput struct {
	TenantID   string
	LibraryID  string
	ShareRef   string
	ImportMode string // merge_namespace (default) | merge | replace_library
	Actor      string
	ActorEmail string // for private share visibility
	// AllowAdminImportPrivate when true allows private/users shares for tenant admins.
	AllowAdminImportPrivate bool
}

// ImportKnowledgeShare copies a Hub knowledge-share package into a digital asset library.
func (s *Service) ImportKnowledgeShare(ctx context.Context, loader SharePackageLoader, in ImportKnowledgeShareInput) (*store.DigitalAssetImportJob, error) {
	if err := s.requireEnabled(ctx, in.TenantID); err != nil {
		return nil, err
	}
	if loader == nil {
		return nil, fmt.Errorf("share loader nil")
	}
	if s.Host == nil {
		return nil, fmt.Errorf("knowledge host is nil")
	}
	kid, err := ParseKnowledgeShareRef(in.ShareRef)
	if err != nil {
		return nil, err
	}
	share, err := loader.Get(ctx, kid)
	if err != nil {
		return nil, err
	}
	if share == nil || strings.EqualFold(share.Status, "deleted") {
		return nil, fmt.Errorf("share_unavailable")
	}
	if share.ExpiresAt != nil && !share.ExpiresAt.IsZero() && time.Now().UTC().After(share.ExpiresAt.UTC()) {
		return nil, fmt.Errorf("share_unavailable")
	}
	// Same tenant only
	if store.NormalizeTenantID(share.TenantID) != store.NormalizeTenantID(in.TenantID) {
		return nil, fmt.Errorf("cross_tenant_share")
	}
	// Visibility for admin import
	if !shareAllowedForAdminImport(share, in.ActorEmail, in.AllowAdminImportPrivate) {
		return nil, fmt.Errorf("share_forbidden")
	}
	pkgPath, ok := loader.ResolvePackagePath(share.StorageRef)
	if !ok {
		return nil, fmt.Errorf("package missing")
	}
	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, err
	}
	var pkg sharePackageFile
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("invalid package json: %w", err)
	}
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return nil, fmt.Errorf("unsupported package format")
	}
	if len(pkg.Sources) == 0 {
		return nil, fmt.Errorf("package has no sources")
	}

	mode := strings.TrimSpace(in.ImportMode)
	if mode == "" {
		mode = "merge_namespace"
	}

	lib, err := s.GetLibrary(ctx, in.TenantID, in.LibraryID)
	if err != nil {
		return nil, err
	}
	s.importStartMu.Lock()
	s.reclaimStaleImportJobs(ctx, in.TenantID)
	if n, err := s.Repo.CountRunningJobs(ctx, in.TenantID); err == nil && n > 0 {
		s.importStartMu.Unlock()
		return nil, fmt.Errorf("tenant already has a running import job")
	}
	now := time.Now().UTC()
	progress, _ := json.Marshal(map[string]any{
		"share_ref": in.ShareRef, "knowledge_id": kid, "import_mode": mode,
		"phase": "importing", "percent": 15, "message": "importing knowledge share",
	})
	job := &store.DigitalAssetImportJob{
		ID: "daij_" + uuid.NewString(), TenantID: in.TenantID, LibraryID: in.LibraryID,
		Kind: "knowledge_share", Status: "running", ProgressJSON: string(progress),
		CreatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Repo.CreateJob(ctx, job); err != nil {
		s.importStartMu.Unlock()
		return nil, err
	}
	s.importStartMu.Unlock()

	prefix := ""
	if mode == "merge_namespace" {
		prefix = "ks_" + kid + "_"
	}

	err = s.Host.WithLibraryWrite(ctx, in.TenantID, in.LibraryID, func(st *knowledge.SQLiteStore) error {
		if mode == "replace_library" {
			// Export empty replace: delete all by re-import after purge via replace_snapshot path.
			// Soft approach: ImportPackageSources only adds; for replace we wipe via export empty?
			// Use ListSources + DeleteSource for all.
			sources, lerr := st.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 100000, IncludeDisabled: true})
			if lerr != nil {
				return lerr
			}
			for _, src := range sources {
				_ = st.DeleteSource(ctx, src.ID)
			}
		}
		ps := make([]knowledge.PackageSource, 0, len(pkg.Sources))
		for _, src := range pkg.Sources {
			id := strings.TrimSpace(src.ID)
			if prefix != "" && id != "" && !strings.HasPrefix(id, prefix) {
				id = prefix + id
			} else if prefix != "" && id == "" {
				id = prefix + uuid.NewString()
			}
			labels := append([]string{}, src.Labels...)
			labels = append(labels, "enterprise_import_kind=knowledge_share", "source_knowledge_id="+kid)
			ps = append(ps, knowledge.PackageSource{
				ID: id, Kind: src.Kind, URI: src.URI, CanonicalURI: src.CanonicalURI,
				Title: src.Title, TopicHint: firstNonEmpty(src.TopicHint, pkg.Manifest.Title),
				Labels: labels, Content: src.Content, ContentTruncated: src.ContentTruncated,
			})
		}
		res := knowledge.ImportPackageSources(ctx, st, ps, knowledge.PackageImportOptions{
			TenantID:  in.TenantID,
			TopicHint: pkg.Manifest.Title,
			RootPath:  "share://" + kid,
		})
		if res.Failed > 0 && res.Imported == 0 {
			return fmt.Errorf("import failed: %v", res.Warnings)
		}
		lib.SourceCount += int64(res.Imported)
		lib.UpdatedAt = time.Now().UTC()
		lib.UpdatedBy = in.Actor
		return s.advanceContentAfterImportLocked(ctx, st, lib, "upsert_sources", in.Actor)
	})
	if err != nil {
		s.failJob(job, err)
		return job, err
	}
	_ = loader.IncrementImport(ctx, kid)
	job.Status = "succeeded"
	job.ProgressJSON = string(mustJSON(map[string]any{
		"knowledge_id": kid, "import_mode": mode, "phase": "done", "percent": 100,
		"message": "import completed",
	}))
	job.UpdatedAt = time.Now().UTC()
	_ = s.Repo.UpdateJob(ctx, job)
	return job, nil
}

func shareAllowedForAdminImport(share *store.KnowledgeShare, actorEmail string, allowPrivate bool) bool {
	if share == nil {
		return false
	}
	scope := strings.ToLower(strings.TrimSpace(share.VisibilityScope))
	// Normalize common aliases
	switch scope {
	case "global":
		scope = "public"
	case "selected_users":
		scope = "users"
	}
	switch scope {
	case "public", "hub", "tenant", "":
		return true
	case "private", "users":
		if allowPrivate {
			return true
		}
		email := strings.ToLower(strings.TrimSpace(actorEmail))
		if email != "" && strings.EqualFold(share.OwnerUserEmail, email) {
			return true
		}
		// selected users list
		var users []string
		_ = json.Unmarshal([]byte(share.VisibilityUsersJSON), &users)
		for _, u := range users {
			if strings.ToLower(strings.TrimSpace(u)) == email {
				return true
			}
		}
		return false
	default:
		return allowPrivate
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
