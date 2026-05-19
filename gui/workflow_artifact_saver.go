package main

import (
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
// through corelib/memory's generated task-artifact upsert path.
//
// Deduplication and metadata repair are owned by Store.UpsertTaskArtifact:
// phase-level identity comes from the workflow/phase tags, while source refs,
// owner scope, and project boundary stay in the shared corelib memory layer.
type workflowArtifactSaver struct {
	store *memory.Store
}

// SaveArtifact persists a workflow phase output summary as a task_artifact
// memory entry. Re-saves of the same phase are upserted by corelib/memory.
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

	identityCount := len(tags)
	if phaseTag != "" {
		identityCount = 2
	}
	result, err := s.store.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
		Title:            title,
		Content:          summary,
		Tags:             tags,
		IdentityTagCount: identityCount,
		SourceType:       sourceType,
		SourceURL:        sourceURL,
		OwnerID:          ownerID,
	})
	if err != nil {
		return fmt.Errorf("artifact_saver: %w", err)
	}

	log.Printf("[artifact_saver] saved task_artifact for phase %q (%d runes, entry=%s created=%v updated=%v touched=%v)", phaseTag, len([]rune(summary)), result.EntryID, result.Created, result.Updated, result.Touched)
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
