package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingStaticWorkspaceBinding is the host-issued handle for one local Coding
// workspace.  WorkspaceHandle is deliberately opaque: project paths are
// execution data resolved by a future Coding adapter, not provider identity,
// invocation identity, or a model-controlled parameter.
//
// This is intentionally separate from trustedCodingInvocationIdentity.  The
// latter proves who owns a semantic turn; it cannot prove which project that
// turn is permitted to inspect.
type codingStaticWorkspaceBinding struct {
	WorkspaceHandle string
	HostKind        string // local only in S1-A; remote receives its own S2 binding.
}

func (b codingStaticWorkspaceBinding) complete() bool {
	return strings.TrimSpace(b.WorkspaceHandle) != "" && strings.TrimSpace(b.HostKind) == "local"
}

// codingStaticExecutionEnvelope is the host-owned input to the Coding static
// shadow planner.  It deliberately contains neither task wording nor tool
// names.  A later S1-B adapter may resolve WorkspaceHandle to an actual
// directory, but that resolution must be made by the host and revalidated at
// execution; the planner only sees this immutable binding identity.
type codingStaticExecutionEnvelope struct {
	Identity  *trustedCodingInvocationIdentity
	Workspace codingStaticWorkspaceBinding
	Posture   codingRequestKind
	Role      codingSubAgentRole
}

func (e codingStaticExecutionEnvelope) complete() bool {
	return e.Identity != nil && e.Identity.complete() && e.Workspace.complete()
}

const (
	codingStaticReadAdapter        = "coding_static_workspace_read"
	codingStaticReadImplementation = "coding-static-workspace-read-v1"
	codingStaticRepoAdapter        = "coding_static_workspace_repo_inspect"
	codingStaticRepoImplementation = "coding-static-workspace-repo-inspect-v1"

	codingStaticCatalogNeedEvidence = "host:coding-static-capability-policy:v1"
)

// codingStaticPlanPreparation is intentionally below materialization.  It is
// safe to produce while the legacy static belt remains executable because it
// carries no definition aliases, grants, or dispatcher binding.  S1-C is the
// only phase allowed to turn its selections into an executable surface.
type codingStaticPlanPreparation struct {
	Catalog tool.ToolCatalogSnapshot
	Plan    tool.ToolPlan
}

// codingStaticWorkspaceExecutor is the narrow S1-B adapter seam.  It accepts
// a host-issued workspace binding and an already-planned selection; it does
// not accept a project path, provider name, or model function name.  This
// keeps any later dispatcher from using the convenience IM principal adapter
// to select a Coding workspace.
type codingStaticWorkspaceExecutor struct {
	ownerID string
	resolve func(string, codingStaticWorkspaceBinding) (string, bool)
	binding codingStaticWorkspaceBinding
}

func newCodingStaticWorkspaceExecutor(app *App, ownerID string, binding codingStaticWorkspaceBinding) *codingStaticWorkspaceExecutor {
	if app == nil || !binding.complete() || strings.TrimSpace(ownerID) == "" {
		return nil
	}
	return &codingStaticWorkspaceExecutor{ownerID: strings.TrimSpace(ownerID), binding: binding, resolve: app.resolveDesktopCodingStaticWorkspace}
}

// ExecuteReadOnlySelection is deliberately not a model-call entry point: S1-C
// has not supplied response/tool-call correlation, grants, or admission yet.
// It exists so S1-B can prove the fixed local adapter and its canonical
// parameter boundary without reopening name-based routing.  Only selections
// from the Coding static read-only catalog are accepted.
func (e *codingStaticWorkspaceExecutor) ExecuteReadOnlySelection(selection tool.PlannedSelection, argsJSON string) (string, error) {
	if e == nil || e.resolve == nil || !e.binding.complete() {
		return "", fmt.Errorf("coding_static_workspace_unavailable")
	}
	workspace, ok := e.resolve(e.ownerID, e.binding)
	if !ok || strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("coding_static_workspace_unavailable")
	}
	switch selection.AdapterName {
	case codingStaticReadAdapter:
		if selection.Provider.ImplementationID != codingStaticReadImplementation || selection.Provider.ProviderID != "coding-workspace:"+e.binding.WorkspaceHandle || selection.FitProof.MatchedCapability != tool.CapabilityFSReadLocal {
			return "", fmt.Errorf("coding_static_selection_binding_rejected")
		}
		canonical, err := tool.CanonicalizeAuthorizedInvocationArguments(argsJSON, semanticTrustedFileReadInvocationSchema(), selection.ParameterAuthorization)
		if err != nil {
			return "", err
		}
		path, query, filePattern, err := semanticTrustedFileReadArgsAllowed(canonical.Values)
		if err != nil {
			return "", err
		}
		return readTrustedCodingWorkspaceFile(workspace, path, query, filePattern)
	case codingStaticRepoAdapter:
		if selection.Provider.ImplementationID != codingStaticRepoImplementation || selection.Provider.ProviderID != "coding-workspace:"+e.binding.WorkspaceHandle || selection.FitProof.MatchedCapability != tool.CapabilityRepoInspectVCS {
			return "", fmt.Errorf("coding_static_selection_binding_rejected")
		}
		if _, err := tool.CanonicalizeAuthorizedInvocationArguments(argsJSON, semanticTrustedRepoInspectInvocationSchema(), selection.ParameterAuthorization); err != nil {
			return "", err
		}
		return inspectTrustedRepoWorkspace(nil, workspace)
	default:
		return "", fmt.Errorf("coding_static_selection_binding_rejected")
	}
}

// readTrustedCodingWorkspaceFile shares the reviewed file operation, but it
// takes a workspace directory obtained only from Coding's fixed adapter.  Do
// not replace it with IMMessageHandler.readTrustedFile: that API resolves a
// workspace from an IM principal and therefore has a different host binding.
func readTrustedCodingWorkspaceFile(workspace, path, query, filePattern string) (string, error) {
	path, query, filePattern = strings.TrimSpace(path), strings.TrimSpace(query), strings.TrimSpace(filePattern)
	absPath, err := trustedFileReadResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	ctx, cancel := trustedFileReadContext(query, filePattern)
	defer cancel()
	if query != "" {
		raw := tool.SearchFilesInProjectCtx(ctx, absPath, query, filePattern)
		if ctx.Err() != nil {
			return "", fmt.Errorf("trusted_file_read_search_incomplete")
		}
		return trustedFileReadRewriteWorkspace(workspace, raw), nil
	}
	if filePattern != "" {
		located, err := trustedFileReadLocated(agent.ToolGlobDetailedCtx(ctx, map[string]interface{}{"pattern": filePattern, "path": absPath}))
		if err != nil {
			return "", err
		}
		return trustedFileReadRewriteWorkspace(workspace, located), nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("trusted_file_read_not_found")
	}
	display := trustedFileWriteDisplayPath(workspace, absPath, path)
	if info.IsDir() {
		return trustedFileReadList(absPath, display)
	}
	if trustedFileReadUsesDocumentReader(absPath) {
		out := agent.ToolReadDocumentWithOfficeReadConfigAndContext(map[string]interface{}{"file_path": absPath}, agent.OfficeReadConfig{}, 0)
		if class, failed := agent.DocumentReadFailure(out); failed {
			return "", fmt.Errorf("trusted_document_read_failed_%s", class)
		}
		return trustedFileReadRewriteWorkspace(workspace, semanticDocumentReadResultProjection(out)), nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return trustedFileReadPage(string(data), trustedFileReadUsesTailDefault(absPath)), nil
}

// codingStaticReadOnlyCapabilityNeeds is a reviewed, host policy.  It is not
// a translation of codingSubAgentToolOrder and must never infer needs from the
// task text.  S1-A deliberately covers only project-scoped read-only work;
// write, build, shell, web, remote and control-plane capabilities stay out of
// this catalog until their separate contracts exist.
func codingStaticReadOnlyCapabilityNeeds() []tool.CapabilityNeed {
	return []tool.CapabilityNeed{
		{
			ID:          "need:coding-static:fs.read.local:0001",
			Capability:  tool.CapabilityFSReadLocal,
			Polarity:    tool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{codingStaticCatalogNeedEvidence},
		},
		{
			ID:          "need:coding-static:repo.inspect.vcs:0001",
			Capability:  tool.CapabilityRepoInspectVCS,
			Polarity:    tool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{codingStaticCatalogNeedEvidence},
		},
	}
}

// codingStaticPostureConstraints records the host posture as planner input.
// The S1-A inventory exposes no mutation provider at all; retaining these
// denials makes that decision explicit and prevents a later provider append
// from silently making an inquiry/operational turn writable or shell-capable.
func codingStaticPostureConstraints(posture codingRequestKind) []tool.RoutingConstraint {
	switch posture {
	case codingRequestInquiry, codingRequestOperational:
		return []tool.RoutingConstraint{
			{ID: "coding-static:deny-write", Capability: tool.CapabilityFSWriteLocal, Effect: "deny", Authority: tool.AuthorityPolicy},
			{ID: "coding-static:deny-build", Capability: tool.CapabilityBuildVerifyLocal, Effect: "deny", Authority: tool.AuthorityPolicy},
			{ID: "coding-static:deny-shell", Capability: tool.CapabilityShellExecuteLocal, Effect: "deny", Authority: tool.AuthorityPolicy},
		}
	default:
		return nil
	}
}

func codingStaticCatalogCoverage(envelope codingStaticExecutionEnvelope) tool.CatalogCoverage {
	if envelope.Identity == nil || !envelope.Identity.complete() || !envelope.Workspace.complete() {
		return tool.CatalogCoverage{State: tool.CatalogCoverageIncomplete, ReasonCode: tool.CatalogCoverageReasonIncomplete, ObservedAt: time.Now().UTC()}
	}
	return tool.CatalogCoverage{State: tool.CatalogCoverageComplete, ObservedAt: time.Now().UTC()}
}

func codingStaticReadOnlyProviderSpecs(envelope codingStaticExecutionEnvelope) ([]tool.ProviderSpec, error) {
	if !envelope.complete() {
		return nil, nil
	}
	workspaceID := "coding-workspace:" + strings.TrimSpace(envelope.Workspace.WorkspaceHandle)
	readSchema := semanticTrustedFileReadInvocationSchema()
	readAuthorization, err := tool.NewParameterAuthorization(readSchema)
	if err != nil {
		return nil, fmt.Errorf("authorize coding static read schema: %w", err)
	}
	repoSchema := semanticTrustedRepoInspectInvocationSchema()
	repoAuthorization, err := tool.NewParameterAuthorization(repoSchema)
	if err != nil {
		return nil, fmt.Errorf("authorize coding static repo schema: %w", err)
	}
	return []tool.ProviderSpec{
		{
			AdapterName: codingStaticReadAdapter,
			Binding: tool.ProviderBinding{
				Kind: "builtin", ProviderID: workspaceID, ImplementationID: codingStaticReadImplementation,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(readSchema)),
			},
			ParameterAuthorization: readAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal, Quality: 2}},
			Effects:                []tool.EffectClass{tool.EffectReadOnly},
			Ready:                  true,
			ChannelScopes:          []string{"coding"},
		},
		{
			AdapterName: codingStaticRepoAdapter,
			Binding: tool.ProviderBinding{
				Kind: "builtin", ProviderID: workspaceID, ImplementationID: codingStaticRepoImplementation,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(repoSchema)),
			},
			ParameterAuthorization: repoAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityRepoInspectVCS, Quality: 2}},
			Effects:                []tool.EffectClass{tool.EffectReadOnly},
			Ready:                  true,
			ChannelScopes:          []string{"coding"},
		},
	}, nil
}

// prepareCodingStaticShadowPlan prepares a governed local-read-only Coding
// plan without exposing it to a model.  It is intentionally callable with an
// incomplete envelope: the returned plan then carries catalog_incomplete
// Unmet needs rather than silently falling back to whichever legacy static
// definitions happen to be available.
func prepareCodingStaticShadowPlan(envelope codingStaticExecutionEnvelope, facts []tool.RoutingFact, budget tool.PlanningBudget, now time.Time) (codingStaticPlanPreparation, error) {
	if envelope.Identity == nil || !envelope.Identity.complete() {
		return codingStaticPlanPreparation{}, fmt.Errorf("coding static identity is incomplete")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)
	coverage := codingStaticCatalogCoverage(envelope)
	providers, err := codingStaticReadOnlyProviderSpecs(envelope)
	if err != nil {
		return codingStaticPlanPreparation{}, err
	}
	snapshot, err := catalog.PublishWithCoverage(providers, coverage, now)
	if err != nil {
		return codingStaticPlanPreparation{}, fmt.Errorf("publish coding static shadow catalog: %w", err)
	}
	constraints := codingStaticPostureConstraints(envelope.Posture)
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID:   envelope.Identity.RootTaskID,
		SessionID:    envelope.Identity.SessionID,
		TurnID:       envelope.Identity.TurnID,
		ChannelScope: "coding",
		Snapshot:     snapshot,
		Needs:        codingStaticReadOnlyCapabilityNeeds(),
		Facts:        append([]tool.RoutingFact(nil), facts...),
		Constraints:  constraints,
		Budget:       budget,
		Now:          now,
	})
	if err != nil {
		return codingStaticPlanPreparation{}, fmt.Errorf("plan coding static shadow capability: %w", err)
	}
	return codingStaticPlanPreparation{Catalog: snapshot, Plan: plan}, nil
}

// prepareCodingStaticShadowPlanForSubagent is the only runtime bridge into
// the S1-A shadow planner. It accepts the already host-populated agent state,
// but deliberately does not accept a project path, task text, runtime ID, or
// model-selected tool name. In particular, an absent workspace binding remains
// absent and is planned as catalog_incomplete rather than reconstructed.
func prepareCodingStaticShadowPlanForSubagent(subagent *CodingSubAgent, posture codingRequestKind, now time.Time) (*codingStaticPlanPreparation, error) {
	if subagent == nil || subagent.dynamicInvocationIdentity == nil || !subagent.dynamicInvocationIdentity.complete() {
		return nil, fmt.Errorf("coding static identity is incomplete")
	}
	role := subagent.role
	if role == "" {
		role = codingRoleWorker
	}
	prepared, err := prepareCodingStaticShadowPlan(codingStaticExecutionEnvelope{
		Identity:  subagent.dynamicInvocationIdentity,
		Workspace: subagent.staticWorkspaceBinding,
		Posture:   posture,
		Role:      role,
	}, nil, tool.PlanningBudget{}, now)
	if err != nil {
		return nil, err
	}
	return &prepared, nil
}
