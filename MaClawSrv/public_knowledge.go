package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

const publicKnowledgeLibrariesKey = "public_knowledge_libraries"
const publicKnowledgeOwnerPrefix = "public:"

type publicKnowledgeLibrary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TenantID  string    `json:"tenant_id"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type publicKnowledgeLibraryView struct {
	publicKnowledgeLibrary
	SourceCount      int        `json:"source_count"`
	LatestSourceAt   *time.Time `json:"latest_source_at,omitempty"`
	DistilledSources int        `json:"distilled_sources,omitempty"`
}

func (s *knowledgeAccessService) ListPublicLibraries(ctx context.Context) ([]publicKnowledgeLibrary, error) {
	raw, err := s.kv.Get(ctx, publicKnowledgeLibrariesKey)
	if err != nil || raw == "" {
		return nil, err
	}
	var libraries []publicKnowledgeLibrary
	if err := json.Unmarshal([]byte(raw), &libraries); err != nil {
		return nil, fmt.Errorf("parse public knowledge libraries: %w", err)
	}
	return normalizePublicKnowledgeLibraries(libraries), nil
}

func (s *knowledgeAccessService) CreatePublicLibrary(ctx context.Context, tenantID, name string) (publicKnowledgeLibrary, error) {
	library, _, err := s.EnsurePublicLibrary(ctx, tenantID, name)
	return library, err
}

func (s *knowledgeAccessService) EnsurePublicLibrary(ctx context.Context, tenantID, name string) (publicKnowledgeLibrary, bool, error) {
	s.publicMu.Lock()
	defer s.publicMu.Unlock()
	return s.ensurePublicLibraryLocked(ctx, tenantID, name)
}

func (s *knowledgeAccessService) ensurePublicLibraryLocked(ctx context.Context, tenantID, name string) (publicKnowledgeLibrary, bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	name = strings.TrimSpace(name)
	if tenantID == "" || name == "" {
		return publicKnowledgeLibrary{}, false, fmt.Errorf("tenant_id and name are required")
	}
	libraries, err := s.ListPublicLibraries(ctx)
	if err != nil {
		return publicKnowledgeLibrary{}, false, err
	}
	for _, library := range libraries {
		if library.TenantID == tenantID && strings.EqualFold(library.Name, name) {
			return library, false, nil
		}
	}
	now := time.Now().UTC()
	id := knowledge.NewID("pkb")
	library := publicKnowledgeLibrary{ID: id, Name: name, TenantID: tenantID, OwnerID: publicKnowledgeOwnerPrefix + id, CreatedAt: now, UpdatedAt: now}
	libraries = append(libraries, library)
	if err := s.savePublicLibraries(ctx, libraries); err != nil {
		return publicKnowledgeLibrary{}, false, err
	}
	return library, true, nil
}

func (s *knowledgeAccessService) GetPublicLibrary(ctx context.Context, id string) (publicKnowledgeLibrary, bool, error) {
	id = strings.TrimSpace(id)
	libraries, err := s.ListPublicLibraries(ctx)
	if err != nil {
		return publicKnowledgeLibrary{}, false, err
	}
	for _, library := range libraries {
		if library.ID == id {
			return library, true, nil
		}
	}
	return publicKnowledgeLibrary{}, false, nil
}

func (s *knowledgeAccessService) DeletePublicLibrary(ctx context.Context, id string) (publicKnowledgeLibrary, bool, int, error) {
	s.publicMu.Lock()
	defer s.publicMu.Unlock()
	return s.deletePublicLibraryLocked(ctx, id)
}

func (s *knowledgeAccessService) deletePublicLibraryLocked(ctx context.Context, id string) (publicKnowledgeLibrary, bool, int, error) {
	id = strings.TrimSpace(id)
	libraries, err := s.ListPublicLibraries(ctx)
	if err != nil {
		return publicKnowledgeLibrary{}, false, 0, err
	}
	next := libraries[:0]
	var deleted publicKnowledgeLibrary
	found := false
	for _, library := range libraries {
		if library.ID == id {
			deleted = library
			found = true
			continue
		}
		next = append(next, library)
	}
	if !found {
		return publicKnowledgeLibrary{}, false, 0, nil
	}
	if err := s.savePublicLibraries(ctx, next); err != nil {
		return publicKnowledgeLibrary{}, false, 0, err
	}
	removedScopes, err := s.RemovePublicLibraryScopes(ctx, deleted)
	if err != nil {
		return publicKnowledgeLibrary{}, false, removedScopes, err
	}
	return deleted, true, removedScopes, nil
}

func (s *knowledgeAccessService) RemovePublicLibraryScopes(ctx context.Context, library publicKnowledgeLibrary) (int, error) {
	removed := 0
	for _, key := range s.kv.Keys(knowledgeAccessKeyPrefix) {
		tenantUser := strings.TrimPrefix(key, knowledgeAccessKeyPrefix)
		tenantID, userID, ok := strings.Cut(tenantUser, "/")
		if !ok || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(userID) == "" {
			continue
		}
		cfg, err := s.GetUser(ctx, tenantID, userID)
		if err != nil {
			return removed, err
		}
		if cfg == nil {
			continue
		}
		before := len(cfg.ReadScopes)
		cfg.ReadScopes = removeKnowledgeScope(cfg.ReadScopes, library.TenantID, library.OwnerID)
		removed += before - len(cfg.ReadScopes)
		if len(cfg.ReadScopes) == before {
			continue
		}
		if err := normalizeKnowledgeAccessConfig(tenantID, cfg); err != nil {
			return removed, err
		}
		if err := s.saveUserConfig(ctx, key, cfg); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (s *knowledgeAccessService) publicKnowledgeScopeRegistered(ctx context.Context, scope knowledgeScope) (bool, error) {
	if !isPublicKnowledgeScope(scope) {
		return true, nil
	}
	libraries, err := s.ListPublicLibraries(ctx)
	if err != nil {
		return false, err
	}
	tenantID := strings.TrimSpace(scope.TenantID)
	ownerID := strings.TrimSpace(scope.OwnerID)
	for _, library := range libraries {
		if library.TenantID == tenantID && library.OwnerID == ownerID {
			return true, nil
		}
	}
	return false, nil
}

func (s *knowledgeAccessService) filterRegisteredPublicKnowledgeScopes(ctx context.Context, scopes []knowledgeScope) []knowledgeScope {
	hasPublic := false
	for _, scope := range scopes {
		if isPublicKnowledgeScope(scope) {
			hasPublic = true
			break
		}
	}
	if !hasPublic {
		return scopes
	}
	libraries, err := s.ListPublicLibraries(ctx)
	if err != nil {
		return removePublicKnowledgeScopes(scopes)
	}
	registered := make(map[string]struct{}, len(libraries))
	for _, library := range libraries {
		registered[knowledgeScopeKey(library.TenantID, library.OwnerID)] = struct{}{}
	}
	filtered := make([]knowledgeScope, 0, len(scopes))
	for _, scope := range scopes {
		if !isPublicKnowledgeScope(scope) {
			filtered = append(filtered, scope)
			continue
		}
		if _, ok := registered[knowledgeScopeKey(scope.TenantID, scope.OwnerID)]; ok {
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func removePublicKnowledgeScopes(scopes []knowledgeScope) []knowledgeScope {
	filtered := make([]knowledgeScope, 0, len(scopes))
	for _, scope := range scopes {
		if !isPublicKnowledgeScope(scope) {
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func (s *knowledgeAccessService) savePublicLibraries(ctx context.Context, libraries []publicKnowledgeLibrary) error {
	data, err := json.Marshal(normalizePublicKnowledgeLibraries(libraries))
	if err != nil {
		return err
	}
	return s.kv.Set(ctx, publicKnowledgeLibrariesKey, string(data))
}

func normalizePublicKnowledgeLibraries(libraries []publicKnowledgeLibrary) []publicKnowledgeLibrary {
	out := make([]publicKnowledgeLibrary, 0, len(libraries))
	seen := make(map[string]struct{}, len(libraries))
	for _, library := range libraries {
		library.ID = strings.TrimSpace(library.ID)
		library.Name = strings.TrimSpace(library.Name)
		library.TenantID = strings.TrimSpace(library.TenantID)
		library.OwnerID = strings.TrimSpace(library.OwnerID)
		if library.ID == "" || library.Name == "" || library.TenantID == "" {
			continue
		}
		if library.OwnerID == "" {
			library.OwnerID = publicKnowledgeOwnerPrefix + library.ID
		}
		if _, ok := seen[library.ID]; ok {
			continue
		}
		seen[library.ID] = struct{}{}
		out = append(out, library)
	}
	return out
}
