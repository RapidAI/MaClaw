package workflow

import "fmt"

// PhaseContract is the static, template-derived capability contract for one
// phase. Runtime gates such as "waiting for form" belong in PhaseRuntimeGate.
type PhaseContract struct {
	PhaseID                  string
	Kind                     PhaseKind
	ToolPolicy               ToolFilterPolicy
	ExpectsDocument          bool
	RequiresReview           bool
	RequiresStructuredForm   bool
	MutationScope            MutationScope
	AllowsRepoInspection     bool
	AllowsProjectMutation    bool
	AllowsDelegation         bool
	UsesSystemDocPersistence bool
	ActivatesOrchestrator    bool
}

// PhaseRuntimeGate combines the static phase contract with the current
// WorkflowState gates that decide whether the agent loop may run now.
type PhaseRuntimeGate struct {
	Contract                PhaseContract
	WaitingForWorkflowInput bool
	WaitingForPhaseForm     bool
	AwaitingReview          bool
	BlocksAgentLoop         bool
}

// DerivePhaseContract converts the existing template fields into one
// authoritative phase capability contract. New template fields may override the
// derived kind/scope, but the final booleans are always computed here.
func DerivePhaseContract(tmpl *WorkflowTemplate, phase PhaseTemplate) PhaseContract {
	kind, scope := derivePhaseKindAndScope(tmpl, phase)
	contract := PhaseContract{
		PhaseID:                phase.ID,
		Kind:                   kind,
		ToolPolicy:             phase.ToolPolicy,
		RequiresReview:         phase.NeedsConfirm,
		RequiresStructuredForm: phase.InputSchema != nil,
		MutationScope:          scope,
	}
	contract.ExpectsDocument = contract.RequiresReview || (phase.ToolPolicy != ToolFilterFull && phase.ToolPolicy != ToolFilterOpsControlled)
	contract.UsesSystemDocPersistence = contract.ExpectsDocument || scope == MutationScopeWorkflowDoc
	contract.AllowsRepoInspection = IsToolAllowedByPolicy(phase.ToolPolicy, "read_file") || IsToolAllowedByPolicy(phase.ToolPolicy, "list_directory")
	contract.AllowsProjectMutation = scope == MutationScopeProject
	contract.AllowsDelegation = scope == MutationScopeProject && phase.ToolPolicy == ToolFilterFull
	contract.ActivatesOrchestrator = kind == PhaseKindExecution &&
		scope == MutationScopeProject &&
		phase.ToolPolicy == ToolFilterFull &&
		!phase.NeedsConfirm &&
		!phase.DisableOrchestrator
	return contract
}

// DeriveWorkflowContracts returns one contract per template phase.
func DeriveWorkflowContracts(tmpl *WorkflowTemplate) []PhaseContract {
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}
	contracts := make([]PhaseContract, len(tmpl.Phases))
	for i, phase := range tmpl.Phases {
		contracts[i] = DerivePhaseContract(tmpl, phase)
	}
	return contracts
}

// DerivePhaseRuntimeGate computes runtime gates for the current active phase.
// Corrupt or missing state blocks the agent loop defensively.
func DerivePhaseRuntimeGate(tmpl *WorkflowTemplate, ws *WorkflowState) PhaseRuntimeGate {
	var gate PhaseRuntimeGate
	if tmpl == nil || ws == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		gate.BlocksAgentLoop = true
		return gate
	}
	phase := tmpl.Phases[ws.PhaseIndex]
	gate.Contract = DerivePhaseContract(tmpl, phase)
	if phase.ID != ws.CurrentPhase || ws.Status != WorkflowActive {
		gate.BlocksAgentLoop = true
		return gate
	}
	gate.WaitingForWorkflowInput = ws.IsWaitingForInput(tmpl)
	gate.WaitingForPhaseForm = phase.InputSchema != nil && !ws.phaseFormGateSatisfied()
	gate.AwaitingReview = ws.PendingReviewPhaseID != ""
	gate.BlocksAgentLoop = gate.WaitingForWorkflowInput || gate.WaitingForPhaseForm || gate.AwaitingReview
	return gate
}

func derivePhaseKindAndScope(tmpl *WorkflowTemplate, phase PhaseTemplate) (PhaseKind, MutationScope) {
	kind := phase.Kind
	scope := phase.MutationScope
	if kind == PhaseKindUnknown || scope == MutationScopeUnknown {
		derivedKind, derivedScope := deriveCompatiblePhaseKindAndScope(tmpl, phase)
		if kind == PhaseKindUnknown {
			kind = derivedKind
		}
		if scope == MutationScopeUnknown {
			scope = derivedScope
		}
	}
	if kind == PhaseKindUnknown {
		if phase.ToolPolicy != ToolFilterFull || phase.NeedsConfirm {
			kind = PhaseKindDocumentPlanning
		}
	}
	if scope == MutationScopeUnknown {
		if phase.NeedsConfirm {
			scope = MutationScopeWorkflowDoc
		} else {
			scope = MutationScopeNone
		}
	}
	return kind, scope
}

func deriveCompatiblePhaseKindAndScope(tmpl *WorkflowTemplate, phase PhaseTemplate) (PhaseKind, MutationScope) {
	switch phase.ToolPolicy {
	case ToolFilterPlanning:
		return PhaseKindCodePlanning, MutationScopeWorkflowDoc
	case ToolFilterOpsControlled:
		return PhaseKindOpsExecution, MutationScopeOps
	case ToolFilterFull:
		return deriveFullPhaseKindAndScope(tmpl, phase)
	default:
		if phase.NeedsConfirm {
			return deriveReviewablePhaseKind(tmpl, phase), MutationScopeWorkflowDoc
		}
		return PhaseKindDocumentPlanning, MutationScopeWorkflowDoc
	}
}

func deriveFullPhaseKindAndScope(tmpl *WorkflowTemplate, phase PhaseTemplate) (PhaseKind, MutationScope) {
	if phase.NeedsConfirm {
		return PhaseKindDocumentPlanning, MutationScopeWorkflowDoc
	}
	if tmpl == nil {
		// Compatibility for legacy callers that only pass a PhaseTemplate.
		if phase.DisableOrchestrator {
			return PhaseKindArtifactGeneration, MutationScopeArtifact
		}
		return PhaseKindExecution, MutationScopeProject
	}
	switch tmpl.Type {
	case WorkflowCoding:
		if phase.ID == PhaseCodingImplementation {
			return PhaseKindExecution, MutationScopeProject
		}
	case WorkflowTesting:
		if phase.ID == "test_execution" {
			return PhaseKindExecution, MutationScopeProject
		}
	case WorkflowBusinessPlan:
		if phase.ID == "bp_doc_generation" {
			return PhaseKindArtifactGeneration, MutationScopeArtifact
		}
	case WorkflowPresentationDesign:
		if phase.ID == "ppt_generation" {
			return PhaseKindArtifactGeneration, MutationScopeArtifact
		}
	}
	if phase.DisableOrchestrator {
		return PhaseKindArtifactGeneration, MutationScopeArtifact
	}
	// Fail closed for unknown full-tool template phases. They can still run the
	// normal phase prompt with full tools, but are not project mutation phases
	// and must not auto-activate the coding orchestrator.
	return PhaseKindUnknown, MutationScopeNone
}

func deriveReviewablePhaseKind(tmpl *WorkflowTemplate, phase PhaseTemplate) PhaseKind {
	if tmpl != nil && tmpl.Type == WorkflowOpsMaintenance && phase.ID == "risk_policy" {
		return PhaseKindOpsRiskPolicy
	}
	if tmpl != nil && tmpl.Type == WorkflowCoding && phase.ID == PhaseCodingReview {
		return PhaseKindReview
	}
	return PhaseKindDocumentPlanning
}

// ValidateWorkflowTemplateContract reports phase-contract conflicts that would
// blur planning, artifact generation, project mutation, or ops execution.
// It is non-mutating so callers can use it as CI/admission before deciding
// whether to reject a dynamically registered template.
func ValidateWorkflowTemplateContract(tmpl *WorkflowTemplate) []error {
	if tmpl == nil {
		return []error{fmt.Errorf("workflow template is nil")}
	}
	var errs []error
	for _, phase := range tmpl.Phases {
		c := DerivePhaseContract(tmpl, phase)
		prefix := fmt.Sprintf("%s/%s", tmpl.Type, phase.ID)
		if c.RequiresReview && c.MutationScope == MutationScopeProject {
			errs = append(errs, fmt.Errorf("%s: reviewable phase cannot mutate project state", prefix))
		}
		if c.RequiresReview && phase.ToolPolicy == ToolFilterFull {
			errs = append(errs, fmt.Errorf("%s: reviewable phase cannot use full tool policy", prefix))
		}
		if phase.ToolPolicy == ToolFilterDocOnly && c.MutationScope == MutationScopeProject {
			errs = append(errs, fmt.Errorf("%s: doc_only phase cannot use project mutation scope", prefix))
		}
		if phase.ToolPolicy == ToolFilterFull {
			switch c.MutationScope {
			case MutationScopeProject, MutationScopeArtifact:
			default:
				errs = append(errs, fmt.Errorf("%s: full tool policy requires project or artifact mutation scope, got %s", prefix, c.MutationScope))
			}
			if c.Kind == PhaseKindUnknown {
				errs = append(errs, fmt.Errorf("%s: full tool policy requires known phase kind", prefix))
			}
		}
		if phase.ToolPolicy == ToolFilterPlanning {
			switch c.MutationScope {
			case MutationScopeProject, MutationScopeArtifact, MutationScopeOps:
				errs = append(errs, fmt.Errorf("%s: planning phase cannot use mutation scope %s", prefix, c.MutationScope))
			}
		}
		if c.Kind == PhaseKindArtifactGeneration && c.MutationScope != MutationScopeArtifact {
			errs = append(errs, fmt.Errorf("%s: artifact_generation phase must use artifact mutation scope, got %s", prefix, c.MutationScope))
		}
		if c.Kind == PhaseKindOpsExecution {
			if phase.ToolPolicy != ToolFilterOpsControlled {
				errs = append(errs, fmt.Errorf("%s: ops_execution phase must use ops_controlled tool policy, got %s", prefix, phase.ToolPolicy))
			}
			if c.MutationScope != MutationScopeOps {
				errs = append(errs, fmt.Errorf("%s: ops_execution phase must use ops mutation scope, got %s", prefix, c.MutationScope))
			}
		}
		if c.Kind == PhaseKindExecution && c.MutationScope != MutationScopeProject {
			errs = append(errs, fmt.Errorf("%s: execution phase must use project mutation scope, got %s", prefix, c.MutationScope))
		}
		if c.Kind == PhaseKindExecution && phase.ToolPolicy != ToolFilterFull {
			errs = append(errs, fmt.Errorf("%s: execution phase must use full tool policy, got %s", prefix, phase.ToolPolicy))
		}
		if c.ActivatesOrchestrator && (c.Kind != PhaseKindExecution || c.MutationScope != MutationScopeProject || phase.ToolPolicy != ToolFilterFull || phase.NeedsConfirm || phase.DisableOrchestrator) {
			errs = append(errs, fmt.Errorf("%s: activates_orchestrator has inconsistent contract: kind=%s scope=%s policy=%s needs_confirm=%t disable_orchestrator=%t",
				prefix, c.Kind, c.MutationScope, phase.ToolPolicy, phase.NeedsConfirm, phase.DisableOrchestrator))
		}
	}
	return errs
}
