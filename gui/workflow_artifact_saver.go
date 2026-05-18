package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
func (s *workflowArtifactSaver) SaveArtifact(title, content string, tags []string, sourceURL string) error {
	return s.SaveArtifactForUser(title, content, tags, sourceURL, "")
}

// SaveArtifactForUser is like SaveArtifact but sets OwnerID for multi-tenant isolation.
func (s *workflowArtifactSaver) SaveArtifactForUser(title, content string, tags []string, sourceURL string, ownerID string) error {
	return s.SaveArtifactFullForUser(title, content, content, tags, sourceURL, ownerID)
}

// SaveArtifactFull persists a compact workflow phase summary while preserving
// the full phase output behind SourceURL when the caller has no source file.
func (s *workflowArtifactSaver) SaveArtifactFull(title, summary, fullContent string, tags []string, sourceURL string) error {
	return s.SaveArtifactFullForUser(title, summary, fullContent, tags, sourceURL, "")
}

func (s *workflowArtifactSaver) SaveArtifactFullForUser(title, summary, fullContent string, tags []string, sourceURL string, ownerID string) error {
	if s.store == nil || strings.TrimSpace(summary) == "" {
		return nil
	}
	summary = memoryRefPreview(summary)

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
			if err := s.store.Delete(existingID); err != nil {
				log.Printf("[artifact_saver] failed to replace task_artifact %s: %v", existingID, err)
			} else {
				log.Printf("[artifact_saver] replacing task_artifact %s for phase %q", existingID, phaseTag)
			}
		}
	}

	sourceType := "workflow_output"
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		if refPath, err := writeMemoryRefFile(s.store.Path(), ownerID, "workflow_output", fullContent, time.Now()); err != nil {
			log.Printf("[artifact_saver] failed to write workflow ref for owner=%s phase=%q: %v", ownerID, phaseTag, err)
		} else {
			sourceURL = refPath
			sourceType = "workflow_output_ref"
			tags = append(append([]string{}, tags...), "source_ref")
		}
	}

	entry := memory.Entry{
		Content:    summary,
		Title:      title,
		Category:   memory.CategoryTaskArtifact,
		Tags:       tags,
		Scope:      memory.ScopeProject,
		SourceType: sourceType,
		SourceURL:  sourceURL,
		OwnerID:    ownerID, // multi-tenant: associate with the user who ran this workflow
	}
	// Store.Save handles content hash dedup internally.
	if err := s.store.Save(entry); err != nil {
		return fmt.Errorf("artifact_saver: %w", err)
	}

	// Track the saved entry ID for future phase-level dedup.
	if phaseTag != "" {
		// Find the entry by content hash (unique, stable identifier).
		// Do Store read BEFORE acquiring s.mu to avoid nested locking.
		h := sha256.Sum256([]byte(summary))
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

	log.Printf("[artifact_saver] saved task_artifact for phase %q (%d runes)", phaseTag, len([]rune(summary)))
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
	app   *App
	once  sync.Once
	inner *workflowArtifactSaver

	// currentUserID is set by the agent loop caller before SavePhaseOutput.
	// Must be set before each workflow phase save because runAgentLoop's
	// defer clears lastUserID before the post-loop doc capture runs.
	currentUserID atomic.Value // stores string
}

func (d *deferredArtifactSaver) SetCurrentUserID(userID string) {
	d.currentUserID.Store(userID)
}

func (d *deferredArtifactSaver) SaveArtifact(title, content string, tags []string, sourceURL string) error {
	return d.SaveArtifactFull(title, content, content, tags, sourceURL)
}

func (d *deferredArtifactSaver) SaveArtifactFull(title, summary, fullContent string, tags []string, sourceURL string) error {
	d.once.Do(func() {
		d.app.ensureMemoryStore()
		if d.app.memoryStore != nil {
			d.inner = &workflowArtifactSaver{store: d.app.memoryStore}
		}
	})
	if d.inner == nil {
		return nil
	}
	ownerID, _ := d.currentUserID.Load().(string)
	if err := d.inner.SaveArtifactFullForUser(title, summary, fullContent, tags, sourceURL, ownerID); err != nil {
		return err
	}
	d.app.triggerMemoryPipelineSoon(45 * time.Second)
	return nil
}
