package guiapp

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// TaskLLMChoice is what one task is using. FollowsAssignment is true when the
// pair is the live 模型分配 selection. An explicit pin sets it false. A pin
// whose provider is gone is reported as following, and the loop falls back.
type TaskLLMChoice struct {
	ProviderID        string `json:"providerId"`
	ProviderName      string `json:"providerName"`
	Model             string `json:"model"`
	FollowsAssignment bool   `json:"followsAssignment"`
}

// GetTaskLLMChoice returns the model this task will call. An empty path is the
// assistant assignment. A stored pin is used only while that provider still exists.
func (a *App) GetTaskLLMChoice(projectPath string) (TaskLLMChoice, error) {
	if a == nil {
		return TaskLLMChoice{FollowsAssignment: true}, fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if id, model, pinned := a.readTaskLLMPin(projectPath); pinned {
		if choice, ok := a.describeTaskLLMChoice(id, model, false); ok {
			return choice, nil
		}
	}
	return a.followingTaskLLMChoice(projectPath), nil
}

// SetTaskLLMChoice pins this task, or clears the pin when either side is empty.
// The pair must name a saved provider. The model id is not checked against a
// catalog. A pair that is already the live assignment is stored as "follow".
func (a *App) SetTaskLLMChoice(projectPath, providerID, model string) error {
	return a.writeTaskLLMChoice(projectPath, providerID, model, "")
}

func (a *App) validateTaskLLMPair(providerID, model string) error {
	providerID = strings.TrimSpace(providerID)
	model = strings.TrimSpace(model)
	if providerID == "" && model == "" {
		return nil
	}
	if providerID == "" || model == "" {
		return fmt.Errorf("服务商和模型需要一起指定")
	}
	if _, ok := a.lookupTaskLLMProvider(providerID); !ok {
		return fmt.Errorf("找不到这个服务商")
	}
	return nil
}

// persistCreatedTaskLLM stores a pin after the task record exists. A store
// failure is a warning: the task itself was created. An empty pair stores nothing.
func (a *App) persistCreatedTaskLLM(projectPath string, opts TaskCreateOptions) string {
	providerID := strings.TrimSpace(opts.LLMProviderID)
	model := strings.TrimSpace(opts.LLMModel)
	if providerID == "" && model == "" {
		return ""
	}
	if err := a.writeTaskLLMChoice(projectPath, providerID, model, opts.Mode); err != nil {
		log.Printf("[task-llm] store pin failed path=%q: %v", projectPath, err)
		return "任务已创建，但没能记住这个任务单独的模型。"
	}
	return ""
}

func (a *App) writeTaskLLMChoice(projectPath, providerID, model, modeHint string) error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return fmt.Errorf("任务路径为空")
	}
	providerID = strings.TrimSpace(providerID)
	model = strings.TrimSpace(model)
	if err := a.validateTaskLLMPair(providerID, model); err != nil {
		return err
	}
	if providerID != "" && a.taskLLMMatchesAssignment(projectPath, modeHint, providerID, model) {
		providerID, model = "", ""
	}
	pi, err := a.taskPrefsIndex()
	if err != nil {
		return err
	}
	pi.SetTaskLLM(projectPath, providerID, model)
	return nil
}

func (a *App) readTaskLLMPin(projectPath string) (providerID, model string, pinned bool) {
	pi, err := a.taskPrefsIndex()
	if err != nil {
		return "", "", false
	}
	providerID, model = pi.TaskLLM(projectPath)
	return providerID, model, providerID != "" && model != ""
}

// readTaskLLMPinIfStoreOpen reads the pin from an already-open store only.
// Status/prefill polls call this on every tab focus; cold-initializing the
// memory store here (ensureMemoryStore → LoadConfig) can re-enter path-bound
// locks during startup and turns a cheap read into a model load.
func (a *App) readTaskLLMPinIfStoreOpen(projectPath string) (providerID, model string, pinned bool) {
	if a == nil || a.memoryStore == nil || a.memoryStore.ProjectIndex() == nil {
		return "", "", false
	}
	providerID, model = a.memoryStore.ProjectIndex().TaskLLM(projectPath)
	return providerID, model, providerID != "" && model != ""
}

func (a *App) taskPrefsIndex() (*memory.ProjectIndex, error) {
	if a == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	a.ensureMemoryStore()
	if a.memoryStore == nil || a.memoryStore.ProjectIndex() == nil {
		return nil, fmt.Errorf("任务偏好不可用")
	}
	return a.memoryStore.ProjectIndex(), nil
}

// taskAssignmentProfile is the 模型分配 row a task follows when it has no pin.
// Coding and remote coding follow the coding row (already effective when it
// inherits the assistant). Everything else follows the assistant row.
func (a *App) taskAssignmentProfile(projectPath, modeHint string) string {
	switch strings.ToLower(strings.TrimSpace(modeHint)) {
	case "coding_dev", "remote_coding_dev":
		return maclawLLMProfileCoding
	case "chat", "cloud":
		return maclawLLMProfileAssistant
	}
	mode := ""
	if a != nil {
		mode = a.projectRecordCodingMode(projectPath)
	}
	if mode == taskCodingDevTag || mode == taskRemoteCodingDevTag {
		return maclawLLMProfileCoding
	}
	return maclawLLMProfileAssistant
}

func (a *App) taskLLMMatchesAssignment(projectPath, modeHint, providerID, model string) bool {
	assignedID, assignedModel, ok := a.assignmentPair(a.taskAssignmentProfile(projectPath, modeHint))
	if !ok {
		return false
	}
	if strings.TrimSpace(providerID) != assignedID {
		return false
	}
	return sameDesktopRoleModel(model, assignedModel)
}

func (a *App) assignmentPair(profile string) (providerID, model string, ok bool) {
	if a == nil {
		return "", "", false
	}
	cfg, err := a.ResolveMaclawLLMProfile(profile)
	if err != nil {
		return "", "", false
	}
	providerID = strings.TrimSpace(cfg.ProviderID)
	model = strings.TrimSpace(cfg.Model)
	return providerID, model, providerID != "" && model != ""
}

func (a *App) followingTaskLLMChoice(projectPath string) TaskLLMChoice {
	id, model, ok := a.assignmentPair(a.taskAssignmentProfile(projectPath, ""))
	if !ok {
		return TaskLLMChoice{FollowsAssignment: true}
	}
	choice, described := a.describeTaskLLMChoice(id, model, true)
	if !described {
		return TaskLLMChoice{ProviderID: id, Model: model, FollowsAssignment: true}
	}
	return choice
}

func (a *App) describeTaskLLMChoice(providerID, model string, follows bool) (TaskLLMChoice, bool) {
	provider, ok := a.lookupTaskLLMProvider(providerID)
	if !ok {
		return TaskLLMChoice{}, false
	}
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = providerID
	}
	return TaskLLMChoice{
		ProviderID:        strings.TrimSpace(providerID),
		ProviderName:      name,
		Model:             strings.TrimSpace(model),
		FollowsAssignment: follows,
	}, strings.TrimSpace(model) != ""
}

func (a *App) lookupTaskLLMProvider(providerID string) (corelib.MaclawLLMProvider, bool) {
	if a == nil || strings.TrimSpace(providerID) == "" {
		return corelib.MaclawLLMProvider{}, false
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMProvider{}, false
	}
	state := a.maclawLLMProvidersFromConfig(&cfg)
	return resolveMaclawLLMProviderByID(state.Providers, providerID)
}

// taskBoundLLMConfig is the pinned model for this task, with Profile set to
// the loop role so usage stays on that role. A missing provider or an empty
// path returns false and the loop keeps resolving the role.
//
// The pin covers every role that belongs to the task, the desktop helper
// included: desktop computer use runs text-primary (OmniParser + OCR), so a
// pin without a vision probe is still usable. Refusing it only made the helper
// call a provider the composer did not show.
func (a *App) taskBoundLLMConfig(projectPath, role string) (corelib.MaclawLLMConfig, bool) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	providerID, model, pinned := a.readTaskLLMPin(projectPath)
	return a.taskBoundLLMConfigFromPin(role, providerID, model, pinned)
}

// taskBoundLLMConfigNoInit is taskBoundLLMConfig for read-only status/prefill
// paths: it never cold-initializes the memory store. When the store is closed
// (e.g. during startup or in bare-App tests) the pin is unavailable and the
// caller falls back to the assignment.
func (a *App) taskBoundLLMConfigNoInit(projectPath, role string) (corelib.MaclawLLMConfig, bool) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	providerID, model, pinned := a.readTaskLLMPinIfStoreOpen(projectPath)
	return a.taskBoundLLMConfigFromPin(role, providerID, model, pinned)
}

func (a *App) taskBoundLLMConfigFromPin(role, providerID, model string, pinned bool) (corelib.MaclawLLMConfig, bool) {
	if !pinned {
		return corelib.MaclawLLMConfig{}, false
	}
	provider, ok := a.lookupTaskLLMProvider(providerID)
	if !ok {
		log.Printf("[LLM] task pin provider %q unavailable, following assignment", providerID)
		return corelib.MaclawLLMConfig{}, false
	}
	cfg := a.materializeMaclawLLMSelection(role, provider, model)
	if strings.TrimSpace(cfg.Model) == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	return cfg, true
}
