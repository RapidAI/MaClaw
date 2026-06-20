package agentservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

type UserMemorySnapshot struct {
	Format      string         `json:"format"`
	SourceOwner string         `json:"source_owner"`
	Entries     []memory.Entry `json:"entries"`
}

type UserMemorySnapshotImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

func (s *Service) ExportUserMemorySnapshot(ctx context.Context, p Principal) (UserMemorySnapshot, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return UserMemorySnapshot{}, err
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return UserMemorySnapshot{}, err
	}
	defer store.Stop()
	ownerID := memoryOwnerIDForPrincipal(p)
	entries := store.List("", "")
	out := make([]memory.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.OwnerID != "" && entry.OwnerID != ownerID {
			continue
		}
		out = append(out, entry)
	}
	return UserMemorySnapshot{Format: "maclaw-memory-snapshot/v1", SourceOwner: ownerID, Entries: out}, nil
}

func (s *Service) ImportUserMemorySnapshot(ctx context.Context, p Principal, snap UserMemorySnapshot) (UserMemorySnapshotImportResult, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return UserMemorySnapshotImportResult{}, err
	}
	if strings.TrimSpace(snap.Format) != "" && snap.Format != "maclaw-memory-snapshot/v1" {
		return UserMemorySnapshotImportResult{}, fmt.Errorf("unsupported memory snapshot format %q", snap.Format)
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return UserMemorySnapshotImportResult{}, err
	}
	defer store.Stop()
	targetOwner := memoryOwnerIDForPrincipal(p)
	entries := make([]memory.Entry, 0, len(snap.Entries))
	result := UserMemorySnapshotImportResult{}
	for _, entry := range snap.Entries {
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Content = strings.TrimSpace(entry.Content)
		if entry.ID == "" || entry.Content == "" {
			result.Skipped++
			continue
		}
		entry.OwnerID = targetOwner
		if entry.Boundary != nil {
			entry.Boundary.OwnerID = targetOwner
		}
		entries = append(entries, entry)
	}
	if len(entries) > 0 {
		if err := store.UpsertEntriesByID(entries); err != nil {
			return result, err
		}
	}
	result.Imported = len(entries)
	return result, nil
}
