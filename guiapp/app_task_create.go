package guiapp

import (
	"fmt"
	"log"
	"strings"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// TaskCreateOptions is the single entry point for creating a task from the
// new-task wizard. All fields are optional except Name; the zero-value options
// (only Name set) must behave exactly like the legacy CreateTask(name, "").
//
// Mode maps the wizard's "type × location" choice:
// chat | coding_dev | remote_coding_dev | cloud.
type TaskCreateOptions struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	WorkingDir string `json:"workingDir"`
	// Remote carries SSH connection parameters when Mode is remote_coding_dev.
	Remote *RemoteTarget `json:"remote,omitempty"`
	// CloudWorkspaceID selects a cloud workspace when Mode is cloud.
	CloudWorkspaceID string `json:"cloudWorkspaceId"`
	// ExpertID selects a named AI expert (empty = generic assistant).
	ExpertID string `json:"expertId"`
	// ExpertName is the display fallback for ExpertID.
	ExpertName string `json:"expertName"`
	// WorkflowTemplateID pins a workflow template. Empty never starts a
	// workflow at the creation layer; the wizard's 「无」 and 「自动判断」 both
	// map to empty here ('auto' is collapsed to "" by the frontend), and the
	// two are distinguished only by the first message's
	// no_workflow_interception transport flag, which「无」sets and
	// 「自动判断」/zero-config leaves unset. Mutually exclusive with ExpertID.
	WorkflowTemplateID string `json:"workflowTemplateId"`
	// Params fills the pinned template's first-phase parameter slots.
	Params map[string]string `json:"params,omitempty"`
}

// RemoteTarget carries SSH connection parameters for a remote task. Password
// is only used to connect/prepare the environment and is never persisted.
type RemoteTarget struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	WorkDir  string `json:"workDir"`
	// Safety "diagnosis" forces the read-only remote ops diagnosis path.
	Safety string `json:"safety"`
}

// WorkflowTemplateSummary is the wizard-facing view of a v2 workflow
// template. SemanticOnly templates are returned as-is; the frontend filters
// them out of the explicit picker per the design.
type WorkflowTemplateSummary struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Category           string `json:"category"`
	PhaseCount         int    `json:"phaseCount"`
	RequiresWorkingDir bool   `json:"requiresWorkingDir"`
	HasParamSlots      bool   `json:"hasParamSlots"`
	SemanticOnly       bool   `json:"semanticOnly"`
}

// UnifiedTaskCreateResult is the CreateTaskUnified return value. A non-empty
// Warning means the task record was created and is openable, but a secondary
// step failed (workflow start, remote env prepare) and the task degraded.
// The warning travels inside the result — not as the error return — because
// the Wails JS binding rejects the whole promise on a non-nil error and the
// project path would be lost to the GUI.
type UnifiedTaskCreateResult struct {
	ProjectPath string `json:"projectPath"`
	Warning     string `json:"warning,omitempty"`
}

// CreateTaskUnified creates a task record (and, where applicable, arms its
// execution environment) through one entry point. It returns the task's
// project path. Legacy bindings keep their behavior; this method only
// composes them, so zero-value options are strictly equivalent to
// CreateTask(name, "").
//
// Hard failures (validation, record creation) return a zero result plus err.
// Partial failures return the created path plus Warning and a nil err, so the
// GUI can still open the task and surface the degradation.
func (a *App) CreateTaskUnified(opts TaskCreateOptions) (UnifiedTaskCreateResult, error) {
	zero := UnifiedTaskCreateResult{}
	if a == nil {
		return zero, fmt.Errorf("app unavailable")
	}
	name := normalizeRecentTaskName(opts.Name)
	if name == "" {
		return zero, fmt.Errorf("task name is required")
	}
	opts.Name = name
	opts.Mode = strings.ToLower(strings.TrimSpace(opts.Mode))
	opts.ExpertID = strings.TrimSpace(opts.ExpertID)
	opts.WorkflowTemplateID = strings.TrimSpace(opts.WorkflowTemplateID)
	opts.CloudWorkspaceID = strings.TrimSpace(opts.CloudWorkspaceID)

	if opts.ExpertID != "" && opts.WorkflowTemplateID != "" {
		return zero, fmt.Errorf("expert and workflow template are mutually exclusive")
	}

	switch opts.Mode {
	case "", "chat":
	case "coding_dev":
		// WorkingDir is optional: CreateTaskWithMode inherits the current
		// top-bar directory when empty, same as the legacy coding entry point.
	case "remote_coding_dev":
		if opts.Remote == nil {
			return zero, fmt.Errorf("remote target is required for remote coding tasks")
		}
		if strings.TrimSpace(opts.Remote.Host) == "" || strings.TrimSpace(opts.Remote.User) == "" || strings.TrimSpace(opts.Remote.WorkDir) == "" {
			return zero, fmt.Errorf("remote host, user and work dir are required")
		}
	case "cloud":
		if opts.CloudWorkspaceID == "" {
			return zero, fmt.Errorf("cloud workspace id is required")
		}
	default:
		return zero, fmt.Errorf("unknown task mode: %s", opts.Mode)
	}

	// Expert branch: one durable, resumable task record per expert. The task
	// title follows the existing expert launcher convention (expert name, not
	// the user's prompt text), so Name is intentionally not used here.
	//
	// Contract with the wizard frontend: after this call returns, open the
	// expert tab for ExpertID and send Name as the first user message through
	// the existing expert chat channel (same as ensureExpertTask +
	// setPendingExpertOpen in App.tsx). The backend routes expert messages by
	// expert session id (expert_session_policy.go), so no project-path link is
	// needed; this record is only the sidebar entry.
	if opts.ExpertID != "" {
		result := a.CreateExpertTask(opts.ExpertID, opts.ExpertName)
		if strings.TrimSpace(result.ProjectPath) == "" {
			return zero, fmt.Errorf("expert task creation failed")
		}
		return UnifiedTaskCreateResult{ProjectPath: result.ProjectPath}, nil
	}

	// Workflow branch: validate the pinned template BEFORE creating any record
	// so a typo does not leave an orphan task in the sidebar. Then create the
	// task record (equivalent to a chat task) and arm the pinned template
	// directly, skipping the semantic interception path.
	if opts.WorkflowTemplateID != "" {
		if err := a.validateUnifiedWorkflowTemplate(opts.WorkflowTemplateID); err != nil {
			return zero, err
		}
		// A pinned workflow on a cloud workspace must still produce a cloud
		// workspace task: CreateTaskWithMode does not understand mode "cloud",
		// so route the record creation through the cloud branch.
		projectPath := ""
		if opts.Mode == "cloud" {
			result, err := a.CreateTaskWithCloudWorkspace(opts.Name, opts.WorkingDir, "", opts.CloudWorkspaceID)
			if err != nil {
				return zero, err
			}
			projectPath = result.ProjectPath
		} else {
			result := a.CreateTaskWithMode(opts.Name, opts.WorkingDir, opts.Mode)
			projectPath = result.ProjectPath
		}
		if strings.TrimSpace(projectPath) == "" {
			return zero, fmt.Errorf("task record creation failed")
		}
		if err := a.startUnifiedWorkflow(opts, projectPath); err != nil {
			// The record already exists; carry the path alongside the
			// degradation as a warning (same contract as the remote branch)
			// so the GUI can still open the task. It degrades to an ordinary
			// chat task.
			return UnifiedTaskCreateResult{
				ProjectPath: projectPath,
				Warning:     err.Error(),
			}, nil
		}
		return UnifiedTaskCreateResult{ProjectPath: projectPath}, nil
	}

	switch opts.Mode {
	case "", "chat":
		result := a.CreateTask(opts.Name, opts.WorkingDir)
		if strings.TrimSpace(result.ProjectPath) == "" {
			return zero, fmt.Errorf("task record creation failed")
		}
		return UnifiedTaskCreateResult{ProjectPath: result.ProjectPath}, nil
	case "coding_dev":
		result := a.CreateTaskWithMode(opts.Name, opts.WorkingDir, opts.Mode)
		if strings.TrimSpace(result.ProjectPath) == "" {
			return zero, fmt.Errorf("task record creation failed")
		}
		return UnifiedTaskCreateResult{ProjectPath: result.ProjectPath}, nil
	case "remote_coding_dev":
		return a.createUnifiedRemoteTask(opts)
	case "cloud":
		result, err := a.CreateTaskWithCloudWorkspace(opts.Name, opts.WorkingDir, "", opts.CloudWorkspaceID)
		if err != nil {
			return zero, err
		}
		if strings.TrimSpace(result.ProjectPath) == "" {
			return zero, fmt.Errorf("cloud workspace task creation failed")
		}
		return UnifiedTaskCreateResult{ProjectPath: result.ProjectPath}, nil
	}
	return zero, fmt.Errorf("unknown task mode: %s", opts.Mode)
}

// createUnifiedRemoteTask creates the remote task record then arms the SSH
// environment with the one-shot password. On prepare failure the task record
// still exists, so the path is returned with the error as a Warning and the
// GUI reconnect flow takes over after opening the task.
func (a *App) createUnifiedRemoteTask(opts TaskCreateOptions) (UnifiedTaskCreateResult, error) {
	remote := opts.Remote
	port := remote.Port
	if port <= 0 || port >= 65536 {
		port = 22
	}
	diagnosis := strings.EqualFold(strings.TrimSpace(remote.Safety), "diagnosis")
	var result ProjectSearchResult
	if diagnosis {
		result = a.CreateRemoteOpsDiagnosisTask(opts.Name, remote.Host, remote.User, remote.WorkDir, port)
	} else {
		result = a.CreateRemoteCodingTask(opts.Name, remote.Host, remote.User, remote.WorkDir, port)
	}
	if strings.TrimSpace(result.ProjectPath) == "" {
		return UnifiedTaskCreateResult{}, fmt.Errorf("remote task creation failed")
	}
	var err error
	if diagnosis {
		err = a.PrepareRemoteOpsDiagnosisEnvironment(result.ProjectPath, remote.Host, remote.User, remote.Password, remote.WorkDir, port)
	} else {
		err = a.PrepareRemoteCodingEnvironment(result.ProjectPath, remote.Host, remote.User, remote.Password, remote.WorkDir, port)
	}
	if err != nil {
		log.Printf("[task-create] remote env prepare failed project=%s host=%s:%d err=%v", result.ProjectPath, remote.Host, port, err)
		return UnifiedTaskCreateResult{
			ProjectPath: result.ProjectPath,
			Warning:     err.Error(),
		}, nil
	}
	return UnifiedTaskCreateResult{ProjectPath: result.ProjectPath}, nil
}

// validateUnifiedWorkflowTemplate rejects unknown template ids before the
// workflow branch creates a task record, so a bad id cannot leave an orphan
// sidebar entry. The legacy-engine fallback validates inside
// StartWorkflowWithOptions and is test-only.
func (a *App) validateUnifiedWorkflowTemplate(templateID string) error {
	if a == nil || a.workflowV2 == nil || a.workflowV2.machine == nil {
		return nil
	}
	if reg := a.workflowV2.machine.GetRegistry(); reg != nil && reg.Get(templateID) == nil {
		return fmt.Errorf("unknown workflow template: %s", templateID)
	}
	return nil
}

// startUnifiedWorkflow starts the pinned template for a task created through
// CreateTaskUnified. Production goes through the V2 StateMachine (the legacy
// WorkflowEngine is nil at runtime and kept only for test infrastructure).
//
// First-phase contract: unlike the IM interception paths, no phase runs
// inside this call (there is no in-flight user message and a Wails binding
// must not block on an agent loop). The workflow stays active with the first
// phase pending; the frontend opens the returned task and sends Name as the
// first message, and routeWithWorkflowV2 then drives HandleInput — running
// the phase when FormData is complete, or showing the AG UI form when param
// slots are still missing. A failed SubmitForm below therefore cannot
// deadlock the workflow: it only defers the form to that first message.
func (a *App) startUnifiedWorkflow(opts TaskCreateOptions, projectPath string) error {
	userID := projectSessionOwnerID(projectPath)
	wf := a.workflowV2
	if wf != nil && wf.machine != nil {
		state, err := wf.machine.Create(userID, opts.WorkflowTemplateID, projectPath, opts.Name)
		if err != nil {
			log.Printf("[task-create] workflow create failed user=%s type=%s err=%v", userID, opts.WorkflowTemplateID, err)
			return err
		}
		if len(opts.Params) > 0 {
			formData := make(map[string]interface{}, len(opts.Params))
			for key, value := range opts.Params {
				formData[key] = value
			}
			if err := wf.machine.SubmitForm(userID, formData); err != nil {
				// Missing required slots are a UI concern; keep the workflow
				// active so the form dialog can collect the remaining fields.
				log.Printf("[task-create] workflow param submit failed user=%s type=%s err=%v", userID, opts.WorkflowTemplateID, err)
			}
		}
		log.Printf("[task-create] workflow started: user=%s type=%s project=%s id=%s", userID, state.Type, state.ProjectPath, state.ID)
		return nil
	}
	// Test fallback: exercise the legacy engine path used by 50+ test files.
	if engine := a.workflowEngine; engine != nil {
		_, err := engine.StartWorkflowWithOptions(userID, v2.StructuredIntent{
			Category: v2.WorkflowType(opts.WorkflowTemplateID),
			Summary:  opts.Name,
		}, v2.WorkflowStartOptions{ProjectPath: projectPath})
		return err
	}
	return fmt.Errorf("workflow engine unavailable")
}

// ListWorkflowTemplateSummaries enumerates the v2 template registry for the
// wizard's workflow picker. SemanticOnly templates are included untouched;
// the frontend hides them from the explicit list.
func (a *App) ListWorkflowTemplateSummaries() []WorkflowTemplateSummary {
	registry := a.workflowTemplateRegistry()
	if registry == nil {
		return nil
	}
	types := registry.AllTypes()
	summaries := make([]WorkflowTemplateSummary, 0, len(types))
	for _, typ := range types {
		tmpl := registry.Get(typ)
		if tmpl == nil {
			continue
		}
		summaries = append(summaries, WorkflowTemplateSummary{
			ID:                 tmpl.Type,
			Title:              tmpl.Name,
			Category:           tmpl.Type,
			PhaseCount:         len(tmpl.Phases),
			RequiresWorkingDir: !tmpl.NoWorkingDirRequired,
			HasParamSlots:      workflowTemplateHasParamSlots(tmpl),
			SemanticOnly:       tmpl.SemanticOnly,
		})
	}
	return summaries
}

// workflowTemplateRegistry prefers the live App registry and falls back to a
// builtin registry for tests and pre-init callers.
func (a *App) workflowTemplateRegistry() *v2.TemplateRegistry {
	if a != nil && a.workflowV2 != nil && a.workflowV2.registry != nil {
		return a.workflowV2.registry
	}
	registry := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(registry)
	return registry
}

// workflowTemplateHasParamSlots reports whether any phase declares an AG UI
// input schema (the wizard's parameter slots).
func workflowTemplateHasParamSlots(tmpl *v2.WorkflowTemplate) bool {
	if tmpl == nil {
		return false
	}
	for _, phase := range tmpl.Phases {
		if phase.InputSchema != nil {
			return true
		}
	}
	return false
}
