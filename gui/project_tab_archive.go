package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ---------------------------------------------------------------------------
// ArchiveService — extracts experience from a project's memory entries via
// LLM summarization and saves the result as a globally-recallable
// project_knowledge entry. Marks the project as archived in ProjectIndex.
//
// Graceful degradation:
//   - LLM unavailable → only marks archived, skips experience extraction
//   - Concurrent archive → detects TaskPref.Archived=true and returns early
//   - LLM timeout -> 30s context deadline
// ---------------------------------------------------------------------------

// ArchiveRequest holds the parameters for an archive operation.
type ArchiveRequest struct {
	// ProjectPath is the canonical project path to archive.
	ProjectPath string

	// ProjectName is the human-readable project name (for the LLM prompt).
	// If empty, derived from ProjectIndex or the path's last segment.
	ProjectName string
}

// ArchiveResult holds the outcome of an archive operation.
type ArchiveResult struct {
	// Archived indicates whether the project was successfully marked as archived.
	Archived bool `json:"archived"`

	// ExperienceExtracted indicates whether LLM experience extraction succeeded.
	ExperienceExtracted bool `json:"experience_extracted"`

	// ExperienceSummary is the extracted experience text (empty if extraction skipped/failed).
	ExperienceSummary string `json:"experience_summary,omitempty"`

	// Message is a human-readable status message.
	Message string `json:"message"`
}

// ArchiveService handles project archival with LLM experience extraction.
type ArchiveService struct {
	memoryStore *memory.Store
	llmCaller   archiveLLMCaller
	projIndex   *memory.ProjectIndex
	ownerID     string // multi-tenant isolation; empty for desktop single-user
}

// archiveLLMCaller abstracts the LLM call for testability.
// In production, this is satisfied by IMMessageHandler.LLMClassify.
type archiveLLMCaller interface {
	LLMClassify(ctx context.Context, req LLMClassifyRequest) (*LLMClassifyResult, error)
}

// NewArchiveService creates an ArchiveService with the given dependencies.
func NewArchiveService(store *memory.Store, caller archiveLLMCaller, index *memory.ProjectIndex) *ArchiveService {
	return &ArchiveService{
		memoryStore: store,
		llmCaller:   caller,
		projIndex:   index,
	}
}

// Archive performs the full archive operation:
//  1. Check if already archived (concurrent archive guard)
//  2. Collect project entries from memory store
//  3. Call LLM to extract experience summary (30s timeout)
//  4. Save experience as project_knowledge (ScopeGlobal, tag: archived_experience)
//  5. Mark TaskPref.Archived = true in ProjectIndex
//
// If LLM is unavailable or fails, only step 5 is performed (graceful degradation).
func (s *ArchiveService) Archive(ctx context.Context, req ArchiveRequest) (*ArchiveResult, error) {
	if req.ProjectPath == "" {
		return nil, fmt.Errorf("archive: project path is required")
	}

	// --- Step 0: Concurrent archive guard ---
	if s.projIndex != nil && s.projIndex.IsArchived(req.ProjectPath) {
		return &ArchiveResult{
			Archived:            true,
			ExperienceExtracted: false,
			Message:             "项目已归档",
		}, nil
	}

	// Resolve project name.
	projectName := req.ProjectName
	if projectName == "" && s.projIndex != nil {
		projectName = s.projIndex.GetDisplayName(req.ProjectPath)
	}
	if projectName == "" {
		// Fallback: use last path segment.
		parts := strings.Split(strings.ReplaceAll(req.ProjectPath, "\\", "/"), "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				projectName = parts[i]
				break
			}
		}
	}

	// --- Step 1: Collect project entries ---
	entries := s.collectProjectEntries(req.ProjectPath)

	// --- Step 2: Attempt LLM experience extraction ---
	var experienceSummary string
	var experienceExtracted bool

	if s.llmCaller != nil && len(entries) > 0 {
		summary, err := s.extractExperience(ctx, projectName, req.ProjectPath, entries)
		if err != nil {
			log.Printf("[archive] LLM experience extraction failed for %s: %v", req.ProjectPath, err)
			// Graceful degradation: continue to mark archived without experience.
		} else if summary != "" {
			experienceSummary = summary
			experienceExtracted = true
		}
	}

	// --- Step 3: Save experience entry (if extracted) ---
	if experienceExtracted {
		if err := s.saveExperienceEntry(projectName, req.ProjectPath, experienceSummary); err != nil {
			log.Printf("[archive] failed to save experience entry for %s: %v", req.ProjectPath, err)
			// Non-fatal: still mark as archived.
			experienceExtracted = false
		}
	}

	// --- Step 4: Mark archived in ProjectIndex ---
	if s.projIndex != nil {
		s.projIndex.SetArchived(req.ProjectPath, true)
	}

	// Build result message.
	msg := "项目已归档"
	if experienceExtracted {
		msg = "项目已归档，经验已提取并保存"
	} else if len(entries) == 0 {
		msg = "项目已归档（无项目记录可提取经验）"
	} else if s.llmCaller == nil {
		msg = "项目已归档（LLM 不可用，跳过经验提取）"
	}

	log.Printf("[archive] completed: path=%s name=%q entries=%d extracted=%v",
		req.ProjectPath, projectName, len(entries), experienceExtracted)

	return &ArchiveResult{
		Archived:            true,
		ExperienceExtracted: experienceExtracted,
		ExperienceSummary:   experienceSummary,
		Message:             msg,
	}, nil
}

// collectProjectEntries gathers all task_artifact and project_knowledge entries
// whose tags contain the given project path.
func (s *ArchiveService) collectProjectEntries(projectPath string) []memory.Entry {
	if s.memoryStore == nil {
		return nil
	}

	s.memoryStore.RLock()
	defer s.memoryStore.RUnlock()

	allEntries := s.memoryStore.Entries()
	var result []memory.Entry

	// Normalize project path for comparison (handle both slash styles).
	normalizedPath := strings.ToLower(strings.ReplaceAll(projectPath, "\\", "/"))

	for i := range allEntries {
		e := &allEntries[i]
		// Only collect task_artifact and project_knowledge categories.
		cat := memory.MapToCanonical(e.Category)
		if cat != memory.CategoryTaskArtifact && cat != memory.CategoryProjectKnowledge {
			continue
		}
		// Check if any tag matches the project path.
		if !entryBelongsToProject(e, normalizedPath) {
			continue
		}
		result = append(result, *e)
	}

	return result
}

// entryBelongsToProject checks if an entry's tags contain the given project path.
func entryBelongsToProject(e *memory.Entry, normalizedProjectPath string) bool {
	for _, tag := range e.Tags {
		normalizedTag := strings.ToLower(strings.ReplaceAll(tag, "\\", "/"))
		if normalizedTag == normalizedProjectPath {
			return true
		}
		// Also match if the tag starts with the project path (subdirectory).
		if strings.HasPrefix(normalizedTag, normalizedProjectPath+"/") {
			return true
		}
	}
	return false
}

// extractExperience calls the LLM to generate a structured experience summary
// from the project's memory entries.
func (s *ArchiveService) extractExperience(ctx context.Context, projectName, projectPath string, entries []memory.Entry) (string, error) {
	// Build entries content for the prompt.
	var entriesContent strings.Builder
	for i, e := range entries {
		if i > 0 {
			entriesContent.WriteString("\n---\n")
		}
		segment := fmt.Sprintf("[%s] %s", e.Category, e.Content)
		// Check BEFORE writing — truncate at entry boundary, never mid-character.
		if entriesContent.Len()+len(segment) > 8000 {
			entriesContent.WriteString("\n...(更多记录已省略)")
			break
		}
		entriesContent.WriteString(segment)
	}

	systemPrompt := `你是一个项目经验提取专家。请从以下项目记录中提取关键经验，生成结构化摘要。

请按以下格式输出经验摘要：

## 任务目标
（一句话描述这个项目/任务要做什么）

## 技术方案
（使用了什么技术栈、架构、工具）

## 关键决策
（做了哪些重要的技术/设计决策，为什么）

## 踩坑与解决
（遇到了什么问题，如何解决的）

## 产出物
（最终产出了什么文件/成果）

## 可复用经验
（未来类似项目可以直接复用的经验/模式/代码片段）`

	userMessage := fmt.Sprintf("项目名称：%s\n项目路径：%s\n\n项目记录：\n%s",
		projectName, projectPath, entriesContent.String())

	// Use 30s timeout for the LLM call; remote routing jitter should not drop
	// archive extraction on otherwise healthy providers.
	llmCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := s.llmCaller.LLMClassify(llmCtx, LLMClassifyRequest{
		SystemPrompt: systemPrompt,
		UserMessage:  userMessage,
		TimeoutSec:   30,
		Tag:          "archive-experience",
	})
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	if result == nil || strings.TrimSpace(result.Text) == "" {
		return "", fmt.Errorf("LLM returned empty response")
	}

	log.Printf("[archive] experience extracted: input=%d output=%d latency=%.1fs",
		result.InputTokens, result.OutputTokens, result.Latency.Seconds())

	return strings.TrimSpace(result.Text), nil
}

// saveExperienceEntry saves the extracted experience as a new project_knowledge
// entry with ScopeGlobal and "archived_experience" tag.
func (s *ArchiveService) saveExperienceEntry(projectName, projectPath, summary string) error {
	if s.memoryStore == nil {
		return fmt.Errorf("memory store not available")
	}

	_, err := s.memoryStore.UpsertProjectKnowledge(memory.ProjectKnowledgeUpsertOptions{
		Content:          summary,
		Title:            fmt.Sprintf("归档经验：%s", projectName),
		Scope:            memory.ScopeGlobal,
		Tags:             []string{"archived_experience", projectPath},
		IdentityTagCount: 2,
		SourceType:       "archived_experience",
		OwnerID:          s.ownerID,
	})
	return err
}
