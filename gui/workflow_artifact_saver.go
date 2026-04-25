package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// workflowArtifactSaver adapts memory.Store to the workflow.ArtifactSaver
// interface, allowing the WorkflowEngine to persist phase output summaries
// to long-term memory without importing corelib/memory directly.
//
// Deduplication strategy:
//   - Content hash dedup is handled by Store.Save (existing mechanism).
//   - Phase-level dedup (one entry per phaseID) is handled by tracking
//     saved phase IDs and calling Store.Update for subsequent saves.
//     This avoids manual lock/unlock on Store internals.
type workflowArtifactSaver struct {
	store *memory.Store

	mu       sync.Mutex
	phaseIDs map[string]string // phaseTag → entry ID (tracks saved artifacts)
}

// SaveArtifact persists a workflow phase output summary as a task_artifact
// memory entry. Uses Store.Save for new entries and Store.Update for
// re-saves of the same phase (e.g. user modifies and re-confirms).
func (s *workflowArtifactSaver) SaveArtifact(content string, tags []string, sourceURL string) error {
	if s.store == nil || strings.TrimSpace(content) == "" {
		return nil
	}

	// Extract phaseTag: the second tag (after "workflow") that isn't a path.
	// Convention: tags = ["workflow", phaseID, workflowType, ...projectPath]
	phaseTag := extractPhaseTag(tags)

	// Phase-level dedup: if we already saved an artifact for this phase,
	// update the existing entry instead of creating a new one.
	if phaseTag != "" {
		s.mu.Lock()
		existingID := s.phaseIDs[phaseTag]
		s.mu.Unlock()

		if existingID != "" {
			if err := s.store.Update(existingID, content, memory.CategoryTaskArtifact, tags); err != nil {
				log.Printf("[artifact_saver] failed to update task_artifact %s: %v", existingID, err)
				// Fall through to save as new entry.
			} else {
				log.Printf("[artifact_saver] updated task_artifact %s for phase %q", existingID, phaseTag)
				return nil
			}
		}
	}

	entry := memory.Entry{
		Content:    content,
		Category:   memory.CategoryTaskArtifact,
		Tags:       tags,
		Scope:      memory.ScopeProject,
		SourceType: "workflow_output",
		SourceURL:  sourceURL,
	}
	// Store.Save handles content hash dedup internally.
	if err := s.store.Save(entry); err != nil {
		return fmt.Errorf("artifact_saver: %w", err)
	}

	// Track the saved entry ID for future phase-level dedup.
	if phaseTag != "" {
		// Find the entry by content hash (unique, stable identifier).
		// Do Store read BEFORE acquiring s.mu to avoid nested locking.
		h := sha256.Sum256([]byte(content))
		contentHash := hex.EncodeToString(h[:])
		var entryID string
		s.store.RLock()
		for _, e := range s.store.Entries() {
			if e.ContentHash == contentHash {
				entryID = e.ID
				break
			}
		}
		s.store.RUnlock()

		if entryID != "" {
			s.mu.Lock()
			if s.phaseIDs == nil {
				s.phaseIDs = make(map[string]string)
			}
			s.phaseIDs[phaseTag] = entryID
			s.mu.Unlock()
		}
	}

	log.Printf("[artifact_saver] saved task_artifact for phase %q (%d runes)", phaseTag, len([]rune(content)))
	return nil
}

// extractPhaseTag extracts the phase ID from the tags slice.
// Convention: tags = ["workflow", phaseID, workflowType, ...optional projectPath]
// Returns the second element if it exists and isn't "workflow".
func extractPhaseTag(tags []string) string {
	if len(tags) >= 2 {
		return tags[1] // tags[0] is "workflow", tags[1] is phaseID
	}
	return ""
}

// deferredArtifactSaver lazily resolves the memory store on first use.
// Thread-safe: uses sync.Once for initialization.
type deferredArtifactSaver struct {
	app  *App
	once sync.Once
	inner *workflowArtifactSaver
}

func (d *deferredArtifactSaver) SaveArtifact(content string, tags []string, sourceURL string) error {
	d.once.Do(func() {
		d.app.ensureMemoryStore()
		if d.app.memoryStore != nil {
			d.inner = &workflowArtifactSaver{store: d.app.memoryStore}
		}
	})
	if d.inner == nil {
		return nil
	}
	return d.inner.SaveArtifact(content, tags, sourceURL)
}
