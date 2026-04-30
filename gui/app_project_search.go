package main

import (
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ProjectSearchResult is the frontend-facing search result type.
// Exported as a Wails binding return type.
type ProjectSearchResult struct {
	ID           string   `json:"id"`             // ProjectPath as stable ID
	Name         string   `json:"name"`           // Human-readable project name
	ProjectPath  string   `json:"project_path"`   // Canonical absolute path
	WorkflowType string   `json:"workflow_type"`  // e.g. "coding", "product_design"
	Preview      string   `json:"preview"`        // Short content preview (~150 chars)
	Tags         []string `json:"tags"`           // Union of all entry tags
	LastActivity string   `json:"last_activity"`  // RFC3339 formatted timestamp
	EntryCount   int      `json:"entry_count"`    // Number of memory entries
	Pinned       bool     `json:"pinned"`         // Whether the task is pinned to top
}

// SearchProjects searches the project index for projects matching the query.
// Returns up to `limit` results sorted by relevance (or recency if query is empty).
// This is a Wails binding method called from the frontend search box.
func (a *App) SearchProjects(query string, limit int) []ProjectSearchResult {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return nil
	}

	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return nil
	}

	if limit <= 0 {
		limit = 10
	}

	var records []memory.ProjectRecord
	if query == "" {
		records = pi.ListRecent(limit)
	} else {
		records = pi.Search(query, limit)
	}

	results := make([]ProjectSearchResult, 0, len(records))
	for _, rec := range records {
		r := ProjectSearchResult{
			ID:           rec.ProjectPath,
			Name:         rec.Name,
			ProjectPath:  rec.ProjectPath,
			WorkflowType: rec.WorkflowType,
			Preview:      rec.Preview,
			Tags:         rec.Tags,
			LastActivity: rec.LastActivity.Format(time.RFC3339),
			EntryCount:   rec.EntryCount,
			Pinned:       pi.IsPinned(rec.ProjectPath),
		}

		// Generate a human-readable name when the index couldn't extract one.
		// Priority: user custom name > extracted name > preview-based summary > directory name.
		if custom := pi.CustomName(rec.ProjectPath); custom != "" {
			r.Name = custom
		} else if r.Name == "" {
			r.Name = deriveTaskName(rec)
		}

		results = append(results, r)
	}

	return results
}

// ResumeProject switches the current context to the specified project.
// It clears the current conversation and cancels any active workflow,
// preparing a clean slate for the target project.
//
// Project context injection happens naturally through the existing proactive
// recall mechanism (appendProactiveRecall in im_system_prompt.go): when the
// user sends the next message, RecallDynamic will find the project's
// project_knowledge and task_artifact entries and inject them into the
// system prompt. No explicit seed entry is needed.
//
// Returns a human-readable summary for the frontend to display.
func (a *App) ResumeProject(projectPath string) string {
	if projectPath == "" {
		return ""
	}

	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return "记忆系统未初始化"
	}

	userID := "desktop-user"

	// 0. Update config.CurrentProject so that GetCurrentProjectPath() returns
	//    the target project. Without this, all downstream consumers
	//    (appendProactiveRecall → RecallDynamic, workflow adapter, tool
	//    execution, etc.) would still resolve to the OLD project path,
	//    causing cross-project context contamination.
	//
	// Also notify frontend of the config change so its cached
	// config.current_project stays in sync. Without this, a subsequent
	// frontend SaveConfig would overwrite the backend's correct
	// current_project with the stale value (same race pattern as #11/#23).
	if updatedCfg := a.switchCurrentProjectByPath(projectPath); updatedCfg != nil && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "config-changed", *updatedCfg)
	}

	// 1. Cancel any active workflow (user is switching projects).
	//    Do this BEFORE clearing conversation — CancelWorkflow may
	//    reference conversation state.
	if a.workflowEngine != nil {
		ws := a.workflowEngine.GetActiveWorkflow(userID)
		if ws != nil {
			_ = a.workflowEngine.CancelWorkflow(userID)
		}
	}

	// 2. Clear conversation memory AND per-user session state (drift detector,
	//    orchestrator, steering context, etc.). This mirrors what /new does.
	//    We do this on the backend so the frontend's subsequent clearHistory()
	//    is a no-op (idempotent Clear on already-empty memory).
	a.ensureInteractionInfra()
	hubClient := a.hubClient()
	if hubClient != nil {
		handler := hubClient.ensureIMHandler()
		if handler != nil {
			handler.memory.Clear(userID)
			handler.clearPerUserSessionState(userID)
		}
	}

	// 3. Resolve project name for the display message.
	//    GetDisplayName returns the custom name if the user renamed the task,
	//    otherwise the auto-generated name from entry content.
	pi := a.memoryStore.ProjectIndex()
	var projectName string
	if pi != nil {
		projectName = pi.GetDisplayName(projectPath)
	}
	if projectName == "" {
		projectName = lastPathComponent(projectPath)
	}

	log.Printf("[project_search] ResumeProject: path=%q, name=%q", projectPath, projectName)

	// Note: the frontend also calls clearHistory() which invokes
	// ClearAIAssistantHistory. That's a second Clear on already-empty memory —
	// idempotent, no harm done.

	msg := "🔖 已切换到任务：" + projectName
	// Only show the path line if it looks like a real absolute path.
	// Some tasks have inferred paths like "\path.dirname" from tag fragments
	// — showing those is confusing rather than helpful.
	if memory.LooksLikeFilePath(projectPath) {
		msg += "\n📁 " + projectPath
	}
	return msg
}

// RenameTask sets a user-defined display name for a task in the recent tasks list.
// Pass empty name to revert to the auto-generated name.
// This is a Wails binding method called from the frontend inline editor.
func (a *App) RenameTask(projectPath, newName string) string {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return ""
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return ""
	}
	pi.SetCustomName(projectPath, newName)
	return pi.GetDisplayName(projectPath)
}

// PinTask pins or unpins a task in the recent tasks list.
// Pinned tasks appear at the top of the list.
func (a *App) PinTask(projectPath string, pinned bool) {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return
	}
	pi.SetPinned(projectPath, pinned)
}

// HideTask removes a task from the recent tasks list (soft delete).
// The underlying memory entries are preserved — only the list visibility is affected.
func (a *App) HideTask(projectPath string) {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil {
		return
	}
	pi.SetHidden(projectPath, true)
}

// switchCurrentProjectByPath updates config.CurrentProject to match the
// given project path. If the path matches an existing ProjectConfig entry,
// that entry's ID becomes CurrentProject. If no match is found (e.g. the
// project was discovered from memory but not in the config list), the
// config is not modified — GetCurrentProjectPath() will fall back to the
// first project in the list, which is acceptable.
//
// Returns the updated config on success (for event emission), or nil if
// no change was needed or an error occurred.
//
// This uses LoadConfig → merge → SaveConfig to avoid overwriting concurrent
// config changes (same pattern as #11 / #23 config race fix).
func (a *App) switchCurrentProjectByPath(projectPath string) *corelib.AppConfig {
	if projectPath == "" {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[project_search] switchCurrentProjectByPath: LoadConfig failed: %v", err)
		return nil
	}

	cleanTarget := filepath.Clean(projectPath)
	for _, p := range cfg.Projects {
		if strings.EqualFold(filepath.Clean(p.Path), cleanTarget) {
			if cfg.CurrentProject == p.Id {
				return nil // already current, no-op
			}
			cfg.CurrentProject = p.Id
			if err := a.SaveConfig(cfg); err != nil {
				log.Printf("[project_search] switchCurrentProjectByPath: SaveConfig failed: %v", err)
				return nil
			}
			log.Printf("[project_search] switchCurrentProjectByPath: switched to project %q (id=%s)", p.Name, p.Id)
			return &cfg
		}
	}
	log.Printf("[project_search] switchCurrentProjectByPath: no matching project config for path %q", projectPath)
	return nil
}

// lastPathComponent returns the last directory component of a path.
func lastPathComponent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			if i < len(p)-1 {
				return p[i+1:]
			}
		}
	}
	return p
}

// deriveTaskName generates a human-readable task name from a ProjectRecord
// when extractTitle couldn't find a good name from entry content.
// Uses preview text + workflow type + directory name as fallback layers.
func deriveTaskName(rec memory.ProjectRecord) string {
	// Layer 1: use preview if it's meaningful (not just a path or metadata).
	if rec.Preview != "" && !memory.LooksLikeFilePath(rec.Preview) {
		// Truncate to a reasonable display length.
		name := rec.Preview
		if runes := []rune(name); len(runes) > 40 {
			name = string(runes[:40]) + "..."
		}
		return name
	}

	// Layer 2: workflow type + directory name (e.g. "编码: steave2").
	dir := lastPathComponent(rec.ProjectPath)
	if rec.WorkflowType != "" {
		label := workflowTypeLabel(rec.WorkflowType)
		if label != "" {
			return label + ": " + dir
		}
	}

	// Layer 3: bare directory name.
	return dir
}

// workflowTypeLabel returns a short Chinese label for a workflow type.
func workflowTypeLabel(wfType string) string {
	switch wfType {
	case "coding":
		return "编码"
	case "product_design":
		return "产品设计"
	case "presentation_design":
		return "PPT 设计"
	case "innovation":
		return "创新"
	case "business_plan":
		return "商业计划"
	case "testing":
		return "测试"
	case "literature_review":
		return "文献综述"
	case "research_report":
		return "研究报告"
	case "experiment_design":
		return "实验设计"
	case "grant_proposal":
		return "基金申请"
	case "paper_writing":
		return "论文写作"
	case "project_proposal":
		return "项目提案"
	case "event_planning":
		return "活动策划"
	case "competitive_analysis":
		return "竞品分析"
	case "bid_response":
		return "招投标"
	case "contract_review":
		return "合同审查"
	case "due_diligence":
		return "尽职调查"
	case "compliance_audit":
		return "合规审计"
	case "patent_analysis":
		return "专利分析"
	default:
		return wfType
	}
}
