package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	colleagueSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/service"
	roleSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/workflow"
)

type Service struct {
	mu         sync.Mutex
	path       string
	state      persistedState
	roles      *roleSvc.RoleService
	colleagues *colleagueSvc.ColleagueService
	workflows  *workflow.Service
}

type persistedState struct {
	Plans map[string]Plan  `json:"plans"`
	Runs  map[string][]Run `json:"runs"`
}

func NewService(path string) *Service {
	if strings.TrimSpace(path) == "" {
		path = defaultStorePath()
	}
	s := &Service{path: path, state: persistedState{Plans: map[string]Plan{}, Runs: map[string][]Run{}}}
	_ = s.load()
	return s
}

func (s *Service) SetOrganizationProvisioner(roles *roleSvc.RoleService, colleagues *colleagueSvc.ColleagueService) {
	s.roles = roles
	s.colleagues = colleagues
}

func (s *Service) SetWorkflowProvisioner(workflows *workflow.Service) {
	s.workflows = workflows
}

func (s *Service) Status(tenantID string) Status {
	tenantID = normalizeTenantID(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.state.Plans[tenantID]
	issues := ValidatePlan(plan)
	runs := s.state.Runs[tenantID]
	var lastRun *Run
	if len(runs) > 0 {
		last := runs[len(runs)-1]
		lastRun = &last
	}
	var planPtr *Plan
	if ok {
		copy := plan
		planPtr = &copy
	}
	return Status{
		TenantID:           tenantID,
		HasPlan:            ok,
		ReadyToStart:       ok && noBlockingIssues(issues),
		Plan:               planPtr,
		ValidationIssues:   issues,
		LastRun:            lastRun,
		SuggestedFirstWave: BuildFirstWave(plan),
	}
}

func (s *Service) DraftPlan(tenantID string, input Plan) (Plan, []ValidationIssue, error) {
	tenantID = normalizeTenantID(tenantID)
	plan := NormalizePlan(tenantID, input)
	issues := ValidatePlan(plan)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Plans[tenantID] = plan
	return plan, issues, s.saveLocked()
}

func (s *Service) Validate(input Plan) []ValidationIssue {
	return ValidatePlan(NormalizePlan(input.TenantID, input))
}

func (s *Service) ApplyPlan(tenantID string, input Plan) (Plan, []ValidationIssue, []AppliedAsset, error) {
	tenantID = normalizeTenantID(tenantID)
	plan := NormalizePlan(tenantID, input)
	issues := ValidatePlan(plan)
	if !noBlockingIssues(issues) {
		return plan, issues, nil, fmt.Errorf("bootstrap plan has blocking validation issues")
	}
	assets, err := s.provisionOrganizationAssets(tenantID, plan)
	if err != nil {
		return plan, issues, assets, err
	}
	workflowAssets, err := s.provisionWorkflowTemplates(tenantID)
	assets = append(assets, workflowAssets...)
	if err != nil {
		return plan, issues, assets, err
	}
	plan.Status = "applied"
	plan.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Plans[tenantID] = plan
	return plan, issues, assets, s.saveLocked()
}

func (s *Service) StartFirstWave(tenantID string) (Run, error) {
	tenantID = normalizeTenantID(tenantID)
	s.mu.Lock()
	plan, ok := s.state.Plans[tenantID]
	s.mu.Unlock()
	if !ok {
		plan = NormalizePlan(tenantID, Plan{})
	}
	issues := ValidatePlan(plan)
	if !noBlockingIssues(issues) {
		return Run{}, fmt.Errorf("bootstrap plan is not ready")
	}
	assets, err := s.startWorkflowInstances(tenantID, plan)
	if err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run := Run{ID: fmt.Sprintf("boot_%d", now.UnixNano()), TenantID: tenantID, Status: "first_wave_started", Plan: plan, Tasks: BuildFirstWave(plan), AppliedAssets: assets, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Runs[tenantID] = append(s.state.Runs[tenantID], run)
	return run, s.saveLocked()
}

func (s *Service) GetRun(tenantID, runID string) (Run, bool) {
	tenantID = normalizeTenantID(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.state.Runs[tenantID] {
		if run.ID == runID {
			return run, true
		}
	}
	return Run{}, false
}

func (s *Service) provisionOrganizationAssets(tenantID string, plan Plan) ([]AppliedAsset, error) {
	if s.roles == nil || s.colleagues == nil {
		return nil, nil
	}
	assets := make([]AppliedAsset, 0, len(plan.VirtualDepartments)+len(plan.InitialIWorkers))
	roleIDs := map[string]string{}
	for index, department := range plan.VirtualDepartments {
		code := roleCodeFromName(department)
		role, err := s.roles.GetByCode(tenantID, code)
		status := "existing"
		if err != nil || role == nil {
			role, err = s.roles.Create(tenantID, roleSvc.CreateRequest{Name: department, Code: code, Description: "Bootstrap virtual department for AI-native organization execution.", DefaultStrengths: []string{department, "goal execution", "memory reuse"}, ApplicableTasks: []string{"goal execution", "recurring task", "exception handling"}, SortOrder: 100 + index})
			status = "created"
		}
		if err != nil {
			return assets, fmt.Errorf("provision role %s: %w", department, err)
		}
		roleIDs[code] = role.ID
		assets = append(assets, AppliedAsset{Kind: "role", ID: role.ID, Name: role.Name, Status: status})
	}

	existingColleagues, err := s.colleagues.List(tenantID)
	if err != nil {
		return assets, fmt.Errorf("list colleagues: %w", err)
	}
	byName := map[string]bool{}
	for _, colleague := range existingColleagues {
		byName[strings.ToLower(strings.TrimSpace(colleague.Name))] = true
	}
	for _, worker := range plan.InitialIWorkers {
		key := strings.ToLower(strings.TrimSpace(worker))
		if byName[key] {
			assets = append(assets, AppliedAsset{Kind: "iworker", Name: worker, Status: "existing"})
			continue
		}
		roleID := roleIDs[workerRoleCode(worker)]
		if roleID == "" {
			roleID = firstRoleID(roleIDs)
		}
		if roleID == "" {
			return assets, fmt.Errorf("no role available for worker %s", worker)
		}
		created, err := s.colleagues.Create(tenantID, colleagueSvc.CreateRequest{Name: worker, Avatar: workerAvatar(worker), RoleID: roleID, Description: "Bootstrap-created iWorker bound to iWorkerCenter memory and organization goals.", Strengths: workerStrengths(worker), Tasks: workerTasks(worker)})
		if err != nil {
			return assets, fmt.Errorf("provision iWorker %s: %w", worker, err)
		}
		assets = append(assets, AppliedAsset{Kind: "iworker", ID: created.ID, Name: created.Name, Status: "created"})
	}
	return assets, nil
}

func (s *Service) provisionWorkflowTemplates(tenantID string) ([]AppliedAsset, error) {
	if s.workflows == nil {
		return nil, nil
	}
	existing, err := s.workflows.ListDefinitions(tenantID)
	if err != nil {
		return nil, fmt.Errorf("list workflow definitions: %w", err)
	}
	byName := map[string]*workflow.Definition{}
	for _, def := range existing {
		byName[strings.ToLower(strings.TrimSpace(def.Name))] = def
	}

	templates := bootstrapWorkflowTemplates()
	assets := make([]AppliedAsset, 0, len(templates))
	for _, tpl := range templates {
		key := strings.ToLower(strings.TrimSpace(tpl.Name))
		if def := byName[key]; def != nil {
			status := "existing"
			if def.Status != workflow.DefStatusPublished {
				if err := s.workflows.PublishDefinition(tenantID, def.ID); err != nil {
					return assets, fmt.Errorf("publish workflow template %s: %w", tpl.Name, err)
				}
				status = "published"
			}
			assets = append(assets, AppliedAsset{Kind: "workflow", ID: def.ID, Name: def.Name, Status: status})
			continue
		}

		def, err := s.workflows.CreateDefinition(tenantID, tpl)
		if err != nil {
			return assets, fmt.Errorf("create workflow template %s: %w", tpl.Name, err)
		}
		if err := s.workflows.PublishDefinition(tenantID, def.ID); err != nil {
			return assets, fmt.Errorf("publish workflow template %s: %w", tpl.Name, err)
		}
		assets = append(assets, AppliedAsset{Kind: "workflow", ID: def.ID, Name: def.Name, Status: "created_published"})
	}
	return assets, nil
}

func (s *Service) startWorkflowInstances(tenantID string, plan Plan) ([]AppliedAsset, error) {
	if s.workflows == nil {
		return nil, nil
	}
	definitions, err := s.workflows.ListDefinitions(tenantID)
	if err != nil {
		return nil, fmt.Errorf("list workflow definitions: %w", err)
	}
	byName := map[string]*workflow.Definition{}
	for _, def := range definitions {
		byName[strings.ToLower(strings.TrimSpace(def.Name))] = def
	}

	initiatorID := s.pickInitiatorID(tenantID)
	assets := make([]AppliedAsset, 0, len(BuildFirstWave(plan)))
	for _, task := range BuildFirstWave(plan) {
		def := byName[strings.ToLower(strings.TrimSpace(task.Title))]
		if def == nil {
			return assets, fmt.Errorf("workflow template %s is missing; apply bootstrap plan first", task.Title)
		}
		if def.Status != workflow.DefStatusPublished {
			if err := s.workflows.PublishDefinition(tenantID, def.ID); err != nil {
				return assets, fmt.Errorf("publish workflow template %s: %w", def.Name, err)
			}
		}
		inst, err := s.workflows.StartInstance(tenantID, workflow.StartInstanceRequest{DefinitionID: def.ID, Title: task.Title, InitiatorID: initiatorID, InputData: firstWaveInputData(plan, task)})
		if err != nil {
			return assets, fmt.Errorf("start first-wave workflow %s: %w", task.Title, err)
		}
		assets = append(assets, AppliedAsset{Kind: "workflow_instance", ID: inst.ID, Name: task.Title, Status: inst.Status})
	}
	return assets, nil
}

func bootstrapWorkflowTemplates() []workflow.CreateDefinitionRequest {
	return []workflow.CreateDefinitionRequest{
		{
			Name:        "Daily operating brief",
			Description: "Generate a daily operating brief from company, department, and personal memory. Escalate only material risks to executives.",
			TriggerType: "scheduled",
			Steps: []workflow.CreateStepDefRequest{
				{StepCode: "collect_signals", StepName: "Collect operating signals", StepType: "processing", AssigneeRoleCode: "operations", TimeoutMinutes: 30},
				{StepCode: "peer_review", StepName: "Peer review brief risks", StepType: "review", AssigneeRoleCode: "quality", TimeoutMinutes: 20},
			},
		},
		{
			Name:        "Customer exception scan",
			Description: "Continuously scan customer-facing exceptions and prepare response drafts without making external commitment changes autonomously.",
			TriggerType: "scheduled",
			Steps: []workflow.CreateStepDefRequest{
				{StepCode: "scan_exceptions", StepName: "Scan customer exceptions", StepType: "processing", AssigneeRoleCode: "office", TimeoutMinutes: 20},
				{StepCode: "classify_actions", StepName: "Classify response and handoff", StepType: "processing", AssigneeRoleCode: "operations", TimeoutMinutes: 20},
			},
		},
		{
			Name:        "Policy memory check",
			Description: "Check whether company policy memory has enough evidence for current operation decisions and surface gaps before execution risk grows.",
			TriggerType: "event",
			Steps: []workflow.CreateStepDefRequest{
				{StepCode: "check_policy_memory", StepName: "Check policy memory coverage", StepType: "review", AssigneeRoleCode: "quality", TimeoutMinutes: 30},
			},
		},
		{
			Name:        "Open issue classification",
			Description: "Classify open issues into owner, severity, next action, memory scope, and escalation boundary for autonomous follow-through.",
			TriggerType: "manual",
			Steps: []workflow.CreateStepDefRequest{
				{StepCode: "classify_issue", StepName: "Classify open issue", StepType: "processing", AssigneeRoleCode: "data", TimeoutMinutes: 30},
				{StepCode: "route_issue", StepName: "Route owner and next action", StepType: "processing", AssigneeRoleCode: "operations", TimeoutMinutes: 20},
			},
		},
	}
}

func (s *Service) pickInitiatorID(tenantID string) string {
	if s.colleagues == nil {
		return "bootstrap"
	}
	colleagues, err := s.colleagues.ListActive(tenantID)
	if err != nil || len(colleagues) == 0 {
		return "bootstrap"
	}
	for _, colleague := range colleagues {
		if strings.Contains(strings.ToLower(colleague.Name), "office") {
			return colleague.ID
		}
	}
	return colleagues[0].ID
}

func firstWaveInputData(plan Plan, task FirstWaveTask) string {
	payload := map[string]any{
		"source":               "enterprise_bootstrap",
		"company_name":         plan.CompanyName,
		"business_summary":     plan.BusinessSummary,
		"priority":             plan.Priority,
		"task_id":              task.ID,
		"title":                task.Title,
		"owner_iworker":        task.OwnerIWorker,
		"expected_output":      task.ExpectedOutput,
		"memory_scope":         task.MemoryScope,
		"escalation_threshold": task.EscalationThreshold,
		"requires_peer_review": task.RequiresPeerReview,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return task.Title
	}
	return string(data)
}

func NormalizePlan(tenantID string, input Plan) Plan {
	now := time.Now().UTC()
	plan := input
	plan.TenantID = normalizeTenantID(firstNonEmpty(input.TenantID, tenantID))
	plan.CompanyName = strings.TrimSpace(plan.CompanyName)
	if plan.CompanyName == "" {
		plan.CompanyName = "New customer company"
	}
	plan.BusinessSummary = strings.TrimSpace(plan.BusinessSummary)
	if plan.BusinessSummary == "" {
		plan.BusinessSummary = "To be collected during executive intake."
	}
	plan.Priority = strings.TrimSpace(plan.Priority)
	if plan.Priority == "" {
		plan.Priority = "Stabilize daily operations and customer delivery."
	}
	plan.VirtualDepartments = uniqueOrDefault(plan.VirtualDepartments, []string{"Sales", "Operations", "Customer Success", "Finance", "Quality", "Office", "Data"})
	plan.InitialIWorkers = uniqueOrDefault(plan.InitialIWorkers, []string{"Office iWorker", "Ops iWorker", "Data iWorker", "Quality iWorker"})
	plan.MemoryScopes = uniqueOrDefault(plan.MemoryScopes, []string{"company", "department", "personal"})
	plan.RecurringTasks = uniqueOrDefault(plan.RecurringTasks, []string{"Daily operating brief", "Customer exception scan", "Weekly decision summary", "Policy memory review"})
	plan.RequiresExecutiveConfirmation = uniqueOrDefault(plan.RequiresExecutiveConfirmation, []string{"business priorities", "risk boundaries", "external communication rules"})
	if plan.WatcherPolicy.MaxRunSeconds <= 0 {
		plan.WatcherPolicy.MaxRunSeconds = 120
	}
	plan.WatcherPolicy.Enabled = true
	plan.WatcherPolicy.SingleFlight = true
	plan.WatcherPolicy.ScaleByWorkerCount = true
	if plan.Status == "" {
		plan.Status = "draft"
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = now
	}
	return plan
}

func ValidatePlan(plan Plan) []ValidationIssue {
	var issues []ValidationIssue
	if strings.TrimSpace(plan.CompanyName) == "" || plan.CompanyName == "New customer company" {
		issues = append(issues, ValidationIssue{Field: "company_name", Level: "warning", Message: "company name should be confirmed before production launch"})
	}
	if len(plan.VirtualDepartments) < 3 {
		issues = append(issues, ValidationIssue{Field: "virtual_departments", Level: "error", Message: "at least three virtual departments are recommended"})
	}
	if len(plan.InitialIWorkers) < 2 {
		issues = append(issues, ValidationIssue{Field: "initial_iworkers", Level: "error", Message: "at least two iWorkers are required for parallel execution"})
	}
	if !contains(plan.MemoryScopes, "company") || !contains(plan.MemoryScopes, "department") || !contains(plan.MemoryScopes, "personal") {
		issues = append(issues, ValidationIssue{Field: "memory_scopes", Level: "error", Message: "company, department, and personal memory scopes are required"})
	}
	if !plan.WatcherPolicy.Enabled || !plan.WatcherPolicy.SingleFlight {
		issues = append(issues, ValidationIssue{Field: "watcher_policy", Level: "error", Message: "GoalWatcher must be enabled with single-flight protection"})
	}
	return issues
}

func BuildFirstWave(plan Plan) []FirstWaveTask {
	plan = NormalizePlan(plan.TenantID, plan)
	return []FirstWaveTask{
		{ID: "first_daily_brief", Title: "Daily operating brief", OwnerIWorker: pickWorker(plan, "Ops iWorker"), ExpectedOutput: "executive brief", MemoryScope: "company", EscalationThreshold: "major delivery or cash-risk decision", RequiresPeerReview: true, RecommendedTrigger: "daily 09:00"},
		{ID: "first_customer_exception_scan", Title: "Customer exception scan", OwnerIWorker: pickWorker(plan, "Office iWorker"), ExpectedOutput: "exception list and response draft", MemoryScope: "department", EscalationThreshold: "external commitment change", RequiresPeerReview: true, RecommendedTrigger: "every 30 minutes during business hours"},
		{ID: "first_policy_memory_check", Title: "Policy memory check", OwnerIWorker: pickWorker(plan, "Quality iWorker"), ExpectedOutput: "memory gaps and risky assumptions", MemoryScope: "company", EscalationThreshold: "missing compliance boundary", RequiresPeerReview: false, RecommendedTrigger: "after memory import"},
		{ID: "first_open_issue_classification", Title: "Current open issue classification", OwnerIWorker: pickWorker(plan, "Data iWorker"), ExpectedOutput: "table", MemoryScope: "department", EscalationThreshold: "unowned high-priority issue", RequiresPeerReview: false, RecommendedTrigger: "first bootstrap run"},
	}
}

func (s *Service) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Plans == nil {
		state.Plans = map[string]Plan{}
	}
	if state.Runs == nil {
		state.Runs = map[string][]Run{}
	}
	s.state = state
	return nil
}

func (s *Service) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func defaultStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "bootstrap_state.json"
	}
	return filepath.Join(home, ".iworkercenter", "bootstrap_state.json")
}
func normalizeTenantID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "default"
	}
	return id
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func uniqueOrDefault(values, defaults []string) []string {
	if len(values) == 0 {
		values = defaults
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return append([]string(nil), defaults...)
	}
	return out
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}
func noBlockingIssues(issues []ValidationIssue) bool {
	for _, issue := range issues {
		if issue.Level == "error" {
			return false
		}
	}
	return true
}
func pickWorker(plan Plan, preferred string) string {
	for _, worker := range plan.InitialIWorkers {
		if strings.EqualFold(worker, preferred) {
			return worker
		}
	}
	if len(plan.InitialIWorkers) > 0 {
		return plan.InitialIWorkers[0]
	}
	return preferred
}

var nonCodeChars = regexp.MustCompile(`[^a-z0-9]+`)

func roleCodeFromName(name string) string {
	code := strings.ToLower(strings.TrimSpace(name))
	code = nonCodeChars.ReplaceAllString(code, "_")
	code = strings.Trim(code, "_")
	if code == "" {
		return "bootstrap_role"
	}
	return code
}
func workerRoleCode(worker string) string {
	lower := strings.ToLower(worker)
	switch {
	case strings.Contains(lower, "office"):
		return "office"
	case strings.Contains(lower, "ops") || strings.Contains(lower, "operation"):
		return "operations"
	case strings.Contains(lower, "data"):
		return "data"
	case strings.Contains(lower, "quality"):
		return "quality"
	default:
		return "office"
	}
}
func firstRoleID(roleIDs map[string]string) string {
	for _, id := range roleIDs {
		return id
	}
	return ""
}
func workerAvatar(worker string) string {
	for _, r := range strings.TrimSpace(worker) {
		return strings.ToUpper(string(r))
	}
	return "i"
}
func workerStrengths(worker string) []string {
	lower := strings.ToLower(worker)
	switch {
	case strings.Contains(lower, "data"):
		return []string{"structured data", "validation", "table cleanup"}
	case strings.Contains(lower, "quality"):
		return []string{"quality review", "root cause", "policy memory"}
	case strings.Contains(lower, "ops"):
		return []string{"operations", "handoff", "goal execution"}
	default:
		return []string{"communication", "document", "stakeholder-ready output"}
	}
}
func workerTasks(worker string) []string {
	lower := strings.ToLower(worker)
	switch {
	case strings.Contains(lower, "data"):
		return []string{"Current open issue classification", "Data table cleanup"}
	case strings.Contains(lower, "quality"):
		return []string{"Policy memory check", "Risk review"}
	case strings.Contains(lower, "ops"):
		return []string{"Daily operating brief", "Exception scan"}
	default:
		return []string{"Customer exception scan", "Weekly decision summary"}
	}
}
