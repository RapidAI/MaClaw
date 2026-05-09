package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/experience"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// Service handles workflow definition and runtime logic.
type Service struct {
	repo         *Repo
	dbProvider   *db.Provider
	collabRepo   *collaboration.Repo
	colleagueRp  *colleagueRepo.ColleagueRepo
	expExtractor *experience.Extractor // optional, may be nil
	capResolver  CapabilityAssigneeResolver
	capRecorder  CapabilityUsageRecorder
	memRecorder  CapabilityExecutionMemoryRecorder
}

// NewService creates a Service.
func NewService(repo *Repo, dbProvider *db.Provider, collabRepo *collaboration.Repo, colleagueRp *colleagueRepo.ColleagueRepo) *Service {
	return &Service{repo: repo, dbProvider: dbProvider, collabRepo: collabRepo, colleagueRp: colleagueRp}
}

// SetExperienceExtractor sets the optional experience extractor for auto-learning.
func (s *Service) SetExperienceExtractor(ext *experience.Extractor) {
	s.expExtractor = ext
}

type CapabilityAssigneeResolver interface {
	SelectWorkerForTask(ctx context.Context, tenantID, roleCode, taskText string) (string, bool, error)
}

func (s *Service) SetCapabilityAssigneeResolver(resolver CapabilityAssigneeResolver) {
	if s != nil {
		s.capResolver = resolver
	}
}

// CapabilityUsageRecorder receives best-effort execution feedback from workflow runtime.
type CapabilityUsageRecorder interface {
	RecordCapabilityUsage(ctx context.Context, tenantID, capabilityID, colleagueID, workflowInstanceID, workflowStepInstanceID, status, resultSummary, errorMessage string, latencyMs int64, qualityScore int, qualityReason string) error
}

func (s *Service) SetCapabilityUsageRecorder(recorder CapabilityUsageRecorder) {
	if s != nil {
		s.capRecorder = recorder
	}
}

// CapabilityExecutionMemoryRecorder persists workflow execution facts into Center-owned iWorker memory.
type CapabilityExecutionMemoryRecorder interface {
	RecordCapabilityExecutionMemory(ctx context.Context, tenantID, workerID, orgUnitID, capabilityID, taskTitle, workflowName, status, resultSummary, errorMessage string) error
}

func (s *Service) SetCapabilityExecutionMemoryRecorder(recorder CapabilityExecutionMemoryRecorder) {
	if s != nil {
		s.memRecorder = recorder
	}
}

// CompleteStepInput carries a runtime completion result plus optional capability feedback.
type CompleteStepInput struct {
	ActorID             string
	Result              string
	CapabilityID        string
	CapabilityStatus    string
	CapabilityError     string
	CapabilityLatencyMs int64
	QualityScore        int
	QualityReason       string
}

var ErrStepActorForbidden = errors.New("workflow step actor is not assigned")

// --- Definition management ---

// CreateDefinitionRequest holds fields for creating a workflow definition.
type CreateDefinitionRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	TriggerType string                 `json:"trigger_type"`
	Steps       []CreateStepDefRequest `json:"steps"`
}

// CreateStepDefRequest holds fields for a step within a definition.
type CreateStepDefRequest struct {
	StepCode            string `json:"step_code"`
	StepName            string `json:"step_name"`
	StepType            string `json:"step_type"`
	AssigneeMode        string `json:"assignee_mode"`
	AssigneeRoleCode    string `json:"assignee_role_code"`
	AssigneeColleagueID string `json:"assignee_colleague_id"`
	TimeoutMinutes      int    `json:"timeout_minutes"`
	RejectRule          string `json:"reject_rule"`
}

// CreateDefinition creates a workflow definition with its steps atomically.
func (s *Service) CreateDefinition(tenantID string, req CreateDefinitionRequest) (*Definition, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(req.Steps) == 0 {
		return nil, fmt.Errorf("at least one step is required")
	}

	now := time.Now()
	def := &Definition{
		ID:          idgen.New("wfdef"),
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		TriggerType: defaultStr(req.TriggerType, "manual"),
		Status:      DefStatusDraft,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	var steps []*StepDefinition
	for i, stepReq := range req.Steps {
		step := &StepDefinition{
			ID:                  idgen.New("wfstp"),
			WorkflowID:          def.ID,
			StepCode:            defaultStr(stepReq.StepCode, fmt.Sprintf("step_%d", i+1)),
			StepName:            strings.TrimSpace(stepReq.StepName),
			StepType:            defaultStr(stepReq.StepType, "processing"),
			AssigneeMode:        defaultStr(stepReq.AssigneeMode, "by_role"),
			AssigneeRoleCode:    strings.TrimSpace(stepReq.AssigneeRoleCode),
			AssigneeColleagueID: strings.TrimSpace(stepReq.AssigneeColleagueID),
			TimeoutMinutes:      stepReq.TimeoutMinutes,
			RejectRule:          defaultStr(stepReq.RejectRule, "end_process"),
			SortOrder:           i,
		}
		if step.StepName == "" {
			step.StepName = step.StepCode
		}
		steps = append(steps, step)
	}

	if err := s.dbProvider.RunInTx(func(tx *sql.Tx) error {
		if err := s.repo.InsertDefinitionTx(tenantID, tx, def); err != nil {
			return fmt.Errorf("create definition: %w", err)
		}
		for i, step := range steps {
			if err := s.repo.InsertStepDefinitionTx(tenantID, tx, step); err != nil {
				return fmt.Errorf("create step %d: %w", i, err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return def, nil
}

// PublishDefinition sets a definition to published status.
func (s *Service) PublishDefinition(tenantID string, id string) error {
	def, err := s.repo.GetDefinition(tenantID, id)
	if err != nil {
		return fmt.Errorf("definition not found: %w", err)
	}
	def.Status = DefStatusPublished
	def.UpdatedAt = time.Now()
	return s.repo.UpdateDefinition(tenantID, def)
}

// GetDefinition returns a definition by ID.
func (s *Service) GetDefinition(tenantID string, id string) (*Definition, error) {
	return s.repo.GetDefinition(tenantID, id)
}

// ListDefinitions returns all definitions.
func (s *Service) ListDefinitions(tenantID string) ([]*Definition, error) {
	return s.repo.ListDefinitions(tenantID)
}

// ListStepDefinitions returns steps for a definition.
func (s *Service) ListStepDefinitions(tenantID string, workflowID string) ([]*StepDefinition, error) {
	return s.repo.ListStepDefinitions(tenantID, workflowID)
}

// --- Instance lifecycle ---

// StartInstanceRequest holds fields for starting a workflow instance.
type StartInstanceRequest struct {
	DefinitionID string `json:"definition_id"`
	Title        string `json:"title"`
	InitiatorID  string `json:"initiator_id"`
	InputData    string `json:"input_data"`
}

// StartInstance creates a workflow instance and its first step + collaboration task atomically.
func (s *Service) StartInstance(tenantID string, req StartInstanceRequest) (*Instance, error) {
	def, err := s.repo.GetDefinition(tenantID, req.DefinitionID)
	if err != nil {
		return nil, fmt.Errorf("definition not found: %w", err)
	}
	if def.Status != DefStatusPublished {
		return nil, fmt.Errorf("definition is not published (status=%s)", def.Status)
	}

	steps, err := s.repo.ListStepDefinitions(tenantID, def.ID)
	if err != nil || len(steps) == 0 {
		return nil, fmt.Errorf("no steps defined for workflow %s", def.ID)
	}

	firstStep := steps[0]
	assigneeID, err := s.resolveAssignee(tenantID, firstStep)
	if err != nil {
		return nil, fmt.Errorf("cannot assign first step: %w", err)
	}

	now := time.Now()
	inst := &Instance{
		ID:           idgen.New("wfinst"),
		DefinitionID: def.ID,
		Title:        defaultStr(req.Title, def.Name),
		InitiatorID:  strings.TrimSpace(req.InitiatorID),
		Status:       InstStatusRunning,
		InputData:    req.InputData,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	stepInst := &StepInstance{
		ID:                  idgen.New("wfsi"),
		InstanceID:          inst.ID,
		StepDefinitionID:    firstStep.ID,
		AssigneeColleagueID: assigneeID,
		Status:              StepPending,
		SortOrder:           firstStep.SortOrder,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	collabTask := &collaboration.Task{
		ID:                     idgen.New("collab"),
		Title:                  fmt.Sprintf("[%s] %s", inst.Title, firstStep.StepName),
		Description:            fmt.Sprintf("Workflow step is ready for processing: %s", firstStep.StepName),
		FromColleagueID:        inst.InitiatorID,
		ToColleagueID:          assigneeID,
		ToRoleCode:             firstStep.AssigneeRoleCode,
		Status:                 collaboration.StatusPending,
		WorkflowStepInstanceID: stepInst.ID,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	stepInst.CollaborationTaskID = collabTask.ID
	inst.CurrentStepID = stepInst.ID

	// All-or-nothing transaction
	if err := s.dbProvider.RunInTx(func(tx *sql.Tx) error {
		if err := s.repo.InsertInstanceTx(tenantID, tx, inst); err != nil {
			return fmt.Errorf("insert instance: %w", err)
		}
		if err := s.repo.InsertStepInstanceTx(tenantID, tx, stepInst); err != nil {
			return fmt.Errorf("insert step instance: %w", err)
		}
		if err := s.collabRepo.InsertTaskTx(tenantID, tx, collabTask); err != nil {
			return fmt.Errorf("insert collab task: %w", err)
		}
		if err := s.collabRepo.InsertEventTx(tenantID, tx, &collaboration.TaskEvent{
			ID: idgen.New("cevt"), TaskID: collabTask.ID,
			Event: "created", ActorID: inst.InitiatorID, Note: "workflow auto-created", CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("insert collab event: %w", err)
		}
		if err := s.repo.InsertEventTx(tenantID, tx, &InstanceEvent{
			ID: idgen.New("wfevt"), InstanceID: inst.ID, StepID: stepInst.ID,
			Event: "instance_started", ActorID: inst.InitiatorID, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("insert workflow event: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return inst, nil
}

// CompleteStep marks a step as completed and advances to the next step atomically.
func (s *Service) CompleteStep(tenantID string, stepInstanceID, actorID, result string) error {
	return s.CompleteStepWithInput(tenantID, stepInstanceID, CompleteStepInput{ActorID: actorID, Result: result})
}

// StartOrResumeStep marks a non-terminal workflow step as actively executing.
// It is intentionally conservative: it never reassigns or advances the workflow.
func (s *Service) StartOrResumeStep(tenantID string, stepInstanceID, actorID, note string) error {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return fmt.Errorf("actor_id is required")
	}
	stepInst, err := s.repo.GetStepInstance(tenantID, stepInstanceID)
	if err != nil {
		return fmt.Errorf("step instance not found: %w", err)
	}
	if StepTerminal(stepInst.Status) {
		return fmt.Errorf("step %s is already in terminal status %s", stepInstanceID, stepInst.Status)
	}
	inst, err := s.repo.GetInstance(tenantID, stepInst.InstanceID)
	if err != nil {
		return fmt.Errorf("workflow instance not found: %w", err)
	}
	if inst.Status != InstStatusRunning {
		return fmt.Errorf("workflow instance is not running (status=%s)", inst.Status)
	}
	if err := ensureStepActorAuthorized(stepInst, actorID); err != nil {
		return err
	}
	var collabTask *collaboration.Task
	if strings.TrimSpace(stepInst.CollaborationTaskID) != "" {
		collabTask, err = s.collabRepo.GetByID(tenantID, stepInst.CollaborationTaskID)
		if err != nil {
			return fmt.Errorf("collaboration task not found: %w", err)
		}
		if collabTask.IsTerminal() {
			return fmt.Errorf("collaboration task %s is in terminal status %s", collabTask.ID, collabTask.Status)
		}
	}
	now := time.Now()
	eventName := "step_started"
	if stepInst.Status == StepInProgress || (collabTask != nil && collabTask.Status == collaboration.StatusInProgress) {
		eventName = "step_resumed"
	}
	return s.dbProvider.RunInTx(func(tx *sql.Tx) error {
		stepInst.Status = StepInProgress
		stepInst.UpdatedAt = now
		if err := s.repo.UpdateStepInstanceTx(tenantID, tx, stepInst); err != nil {
			return fmt.Errorf("update step: %w", err)
		}
		if collabTask != nil {
			if err := s.collabRepo.UpdateStatusTx(tenantID, tx, collabTask.ID, collaboration.StatusInProgress, collabTask.Result); err != nil {
				return fmt.Errorf("update collaboration task: %w", err)
			}
			if err := s.collabRepo.InsertEventTx(tenantID, tx, &collaboration.TaskEvent{ID: idgen.New("cevt"), TaskID: collabTask.ID, Event: collaboration.StatusInProgress, ActorID: actorID, Note: firstNonEmpty(note, eventName), CreatedAt: now}); err != nil {
				return fmt.Errorf("record collaboration start event: %w", err)
			}
		}
		return s.repo.InsertEventTx(tenantID, tx, &InstanceEvent{ID: idgen.New("wfevt"), InstanceID: inst.ID, StepID: stepInst.ID, Event: eventName, ActorID: actorID, Note: strings.TrimSpace(note), CreatedAt: now})
	})
}

// CompleteStepWithInput marks a step completed and records optional capability execution feedback.
func (s *Service) CompleteStepWithInput(tenantID string, stepInstanceID string, input CompleteStepInput) error {
	actorID := strings.TrimSpace(input.ActorID)
	if actorID == "" {
		return fmt.Errorf("actor_id is required")
	}
	result := input.Result
	stepInst, err := s.repo.GetStepInstance(tenantID, stepInstanceID)
	if err != nil {
		return fmt.Errorf("step instance not found: %w", err)
	}
	if StepTerminal(stepInst.Status) {
		return fmt.Errorf("step %s is already in terminal status %s", stepInstanceID, stepInst.Status)
	}

	inst, err := s.repo.GetInstance(tenantID, stepInst.InstanceID)
	if err != nil {
		return fmt.Errorf("workflow instance not found: %w", err)
	}
	if inst.Status != InstStatusRunning {
		return fmt.Errorf("workflow instance is not running (status=%s)", inst.Status)
	}
	if err := ensureStepActorAuthorized(stepInst, actorID); err != nil {
		return err
	}

	stepDef, err := s.repo.GetStepDefinition(tenantID, stepInst.StepDefinitionID)
	if err != nil {
		return fmt.Errorf("step definition not found: %w", err)
	}

	allStepDefs, err := s.repo.ListStepDefinitions(tenantID, inst.DefinitionID)
	if err != nil {
		return fmt.Errorf("list step definitions: %w", err)
	}

	// Find next step definition
	var nextStepDef *StepDefinition
	for i, sd := range allStepDefs {
		if sd.ID == stepDef.ID && i+1 < len(allStepDefs) {
			nextStepDef = allStepDefs[i+1]
			break
		}
	}

	now := time.Now()

	err = s.dbProvider.RunInTx(func(tx *sql.Tx) error {
		// 1. Mark current step completed
		stepInst.Status = StepCompleted
		stepInst.Result = result
		stepInst.UpdatedAt = now
		if err := s.repo.UpdateStepInstanceTx(tenantID, tx, stepInst); err != nil {
			return fmt.Errorf("update step: %w", err)
		}

		// 2. Mark collaboration task completed
		if stepInst.CollaborationTaskID != "" {
			if err := s.collabRepo.UpdateStatusTx(tenantID, tx, stepInst.CollaborationTaskID, collaboration.StatusCompleted, result); err != nil {
				return fmt.Errorf("complete collab task: %w", err)
			}
			if err := s.collabRepo.InsertEventTx(tenantID, tx, &collaboration.TaskEvent{
				ID: idgen.New("cevt"), TaskID: stepInst.CollaborationTaskID,
				Event: "completed", ActorID: actorID, Note: "workflow step completed", CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("record collab complete event: %w", err)
			}
		}
		if err := s.repo.InsertEventTx(tenantID, tx, &InstanceEvent{
			ID: idgen.New("wfevt"), InstanceID: inst.ID, StepID: stepInst.ID,
			Event: "step_completed", ActorID: actorID, CreatedAt: now,
		}); err != nil {
			return err
		}

		if nextStepDef == nil {
			// No more steps 闁?workflow completed
			inst.Status = InstStatusCompleted
			inst.UpdatedAt = now
			if err := s.repo.UpdateInstanceTx(tenantID, tx, inst); err != nil {
				return fmt.Errorf("complete instance: %w", err)
			}
			return s.repo.InsertEventTx(tenantID, tx, &InstanceEvent{
				ID: idgen.New("wfevt"), InstanceID: inst.ID, StepID: stepInst.ID,
				Event: "instance_completed", ActorID: actorID, CreatedAt: now,
			})
		}

		// 3. Resolve assignee for next step
		nextAssignee, err := s.resolveAssignee(tenantID, nextStepDef)
		if err != nil {
			return fmt.Errorf("resolve next assignee: %w", err)
		}

		// 4. Create next step instance + collaboration task
		nextStepInst := &StepInstance{
			ID:                  idgen.New("wfsi"),
			InstanceID:          inst.ID,
			StepDefinitionID:    nextStepDef.ID,
			AssigneeColleagueID: nextAssignee,
			Status:              StepPending,
			SortOrder:           nextStepDef.SortOrder,
			CreatedAt:           now,
			UpdatedAt:           now,
		}

		nextCollab := &collaboration.Task{
			ID:                     idgen.New("collab"),
			Title:                  fmt.Sprintf("[%s] %s", inst.Title, nextStepDef.StepName),
			Description:            fmt.Sprintf("Workflow step is ready for processing: %s", nextStepDef.StepName),
			FromColleagueID:        stepInst.AssigneeColleagueID,
			ToColleagueID:          nextAssignee,
			ToRoleCode:             nextStepDef.AssigneeRoleCode,
			Status:                 collaboration.StatusPending,
			WorkflowStepInstanceID: nextStepInst.ID,
			CreatedAt:              now,
			UpdatedAt:              now,
		}
		nextStepInst.CollaborationTaskID = nextCollab.ID

		if err := s.repo.InsertStepInstanceTx(tenantID, tx, nextStepInst); err != nil {
			return fmt.Errorf("insert next step: %w", err)
		}
		if err := s.collabRepo.InsertTaskTx(tenantID, tx, nextCollab); err != nil {
			return fmt.Errorf("insert next collab: %w", err)
		}
		if err := s.collabRepo.InsertEventTx(tenantID, tx, &collaboration.TaskEvent{
			ID: idgen.New("cevt"), TaskID: nextCollab.ID,
			Event: "created", ActorID: actorID, Note: "workflow auto-advanced", CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("record next collab event: %w", err)
		}

		// 5. Update instance current step
		inst.CurrentStepID = nextStepInst.ID
		inst.UpdatedAt = now
		if err := s.repo.UpdateInstanceTx(tenantID, tx, inst); err != nil {
			return fmt.Errorf("update instance step: %w", err)
		}

		// 6. Write event
		return s.repo.InsertEventTx(tenantID, tx, &InstanceEvent{
			ID: idgen.New("wfevt"), InstanceID: inst.ID, StepID: nextStepInst.ID,
			Event: "step_advanced", ActorID: actorID, Note: nextStepDef.StepName, CreatedAt: now,
		})
	})
	if err != nil {
		return err
	}

	capabilityID := strings.TrimSpace(input.CapabilityID)
	if capabilityID != "" {
		status := normalizeCapabilityUsageStatus(input.CapabilityStatus, input.CapabilityError)
		var recorderErrs []error
		if s.capRecorder != nil {
			if err := s.capRecorder.RecordCapabilityUsage(context.Background(), tenantID, capabilityID, actorID, inst.ID, stepInst.ID, status, result, input.CapabilityError, input.CapabilityLatencyMs, input.QualityScore, input.QualityReason); err != nil {
				recorderErrs = append(recorderErrs, fmt.Errorf("record capability usage: %w", err))
			}
		}
		if s.memRecorder != nil {
			if err := s.memRecorder.RecordCapabilityExecutionMemory(context.Background(), tenantID, actorID, stepDef.AssigneeRoleCode, capabilityID, stepDef.StepName, inst.Title, status, result, input.CapabilityError); err != nil {
				recorderErrs = append(recorderErrs, fmt.Errorf("record capability execution memory: %w", err))
			}
		}
		if len(recorderErrs) > 0 {
			return errors.Join(recorderErrs...)
		}
	}

	// Trigger experience extraction asynchronously (best-effort, after commit)
	if s.expExtractor != nil && result != "" {
		go s.expExtractor.Extract(tenantID, experience.ExtractionInput{
			TaskTitle:     stepDef.StepName,
			TaskResult:    result,
			RoleCode:      stepDef.AssigneeRoleCode,
			ColleagueName: actorID,
			WorkflowName:  inst.Title,
		})
	}

	return nil
}

// RejectStep rejects a step and terminates the workflow.
func (s *Service) RejectStep(tenantID string, stepInstanceID, actorID, note string) error {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return fmt.Errorf("actor_id is required")
	}
	stepInst, err := s.repo.GetStepInstance(tenantID, stepInstanceID)
	if err != nil {
		return fmt.Errorf("step instance not found: %w", err)
	}
	if StepTerminal(stepInst.Status) {
		return fmt.Errorf("step %s is already in terminal status %s", stepInstanceID, stepInst.Status)
	}

	inst, err := s.repo.GetInstance(tenantID, stepInst.InstanceID)
	if err != nil {
		return fmt.Errorf("workflow instance not found: %w", err)
	}
	if inst.Status != InstStatusRunning {
		return fmt.Errorf("workflow instance is not running (status=%s)", inst.Status)
	}
	if err := ensureStepActorAuthorized(stepInst, actorID); err != nil {
		return err
	}

	now := time.Now()

	return s.dbProvider.RunInTx(func(tx *sql.Tx) error {
		// 1. Reject step
		stepInst.Status = StepRejected
		stepInst.UpdatedAt = now
		if err := s.repo.UpdateStepInstanceTx(tenantID, tx, stepInst); err != nil {
			return err
		}

		// 2. Reject collaboration task
		if stepInst.CollaborationTaskID != "" {
			if err := s.collabRepo.UpdateStatusTx(tenantID, tx, stepInst.CollaborationTaskID, collaboration.StatusRejected, ""); err != nil {
				return fmt.Errorf("reject collab task: %w", err)
			}
			if err := s.collabRepo.InsertEventTx(tenantID, tx, &collaboration.TaskEvent{
				ID: idgen.New("cevt"), TaskID: stepInst.CollaborationTaskID,
				Event: "rejected", ActorID: actorID, Note: note, CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("record collab reject event: %w", err)
			}
		}

		// 3. Reject workflow instance
		inst.Status = InstStatusRejected
		inst.UpdatedAt = now
		if err := s.repo.UpdateInstanceTx(tenantID, tx, inst); err != nil {
			return err
		}

		// 4. Write event
		return s.repo.InsertEventTx(tenantID, tx, &InstanceEvent{
			ID: idgen.New("wfevt"), InstanceID: inst.ID, StepID: stepInst.ID,
			Event: "instance_rejected", ActorID: actorID, Note: note, CreatedAt: now,
		})
	})
}

// GetInstance returns an instance by ID.
func (s *Service) GetInstance(tenantID string, id string) (*Instance, error) {
	return s.repo.GetInstance(tenantID, id)
}

// GetStepInstance returns a workflow step instance by ID.
func (s *Service) GetStepInstance(tenantID string, id string) (*StepInstance, error) {
	return s.repo.GetStepInstance(tenantID, id)
}

// ListInstances returns all instances.
func (s *Service) ListInstances(tenantID string) ([]*Instance, error) {
	return s.repo.ListInstances(tenantID)
}

// ListInstancesForColleague returns workflow instances visible to one iWorker.
func (s *Service) ListInstancesForColleague(tenantID string, colleagueID string) ([]*Instance, error) {
	colleagueID = strings.TrimSpace(colleagueID)
	if colleagueID == "" {
		return s.ListInstances(tenantID)
	}
	return s.repo.ListInstancesForColleague(tenantID, colleagueID)
}

// ListStepInstances returns step instances for a workflow instance.
func (s *Service) ListStepInstances(tenantID string, instanceID string) ([]*StepInstance, error) {
	return s.repo.ListStepInstances(tenantID, instanceID)
}

// ListEvents returns events for a workflow instance.
func (s *Service) ListEvents(tenantID string, instanceID string) ([]*InstanceEvent, error) {
	return s.repo.ListEvents(tenantID, instanceID)
}

func ensureStepActorAuthorized(stepInst *StepInstance, actorID string) error {
	if stepInst == nil {
		return fmt.Errorf("workflow step is required")
	}
	assigneeID := strings.TrimSpace(stepInst.AssigneeColleagueID)
	if assigneeID == "" {
		return fmt.Errorf("workflow step %s has no assigned colleague", stepInst.ID)
	}
	if strings.TrimSpace(actorID) != assigneeID {
		return fmt.Errorf("%w: actor %s cannot operate workflow step %s assigned to %s", ErrStepActorForbidden, actorID, stepInst.ID, assigneeID)
	}
	return nil
}

// resolveAssignee finds the colleague to assign a step to.
func (s *Service) resolveAssignee(tenantID string, stepDef *StepDefinition) (string, error) {
	if stepDef.AssigneeMode == "fixed_colleague" && stepDef.AssigneeColleagueID != "" {
		return stepDef.AssigneeColleagueID, nil
	}
	if s.capResolver != nil {
		taskText := strings.Join([]string{stepDef.StepName, stepDef.StepCode, stepDef.StepType, stepDef.AssigneeRoleCode}, " ")
		if selected, ok, err := s.capResolver.SelectWorkerForTask(context.Background(), tenantID, stepDef.AssigneeRoleCode, taskText); err != nil {
			return "", err
		} else if ok {
			return selected, nil
		}
	}
	if stepDef.AssigneeRoleCode != "" {
		selected, _, err := collaboration.ResolveRoleAssignee(s.collabRepo, s.colleagueRp, tenantID, stepDef.AssigneeRoleCode)
		if err == nil && selected != nil {
			return selected.ID, nil
		}
	}
	return "", fmt.Errorf("no assignee found for step %s (role=%s)", stepDef.StepCode, stepDef.AssigneeRoleCode)
}

func normalizeCapabilityUsageStatus(status string, errText string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "success", "succeeded", "ok", "completed":
		return "success"
	case "failure", "failed", "error":
		return "failure"
	}
	if strings.TrimSpace(errText) != "" {
		return "failure"
	}
	return "success"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultStr(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return strings.TrimSpace(val)
}
