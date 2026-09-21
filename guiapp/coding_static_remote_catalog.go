package guiapp

// coding_static_remote_catalog.go is the S2-shadow (slice 5, shadow-only)
// counterpart of coding_static_catalog.go: remote read-only provider specs
// (fs.read.remote / repo.inspect.remote) over the verified SSH session
// binding, a remote shadow ToolPlan, and observation wiring. This is S0.5-
// style shadow/audit only: the remote static band keeps serving; the plan is
// computed, recorded, and compared — never rendered, never granted, never
// dispatched.

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingStaticRemoteSessionBinding is the host-issued proof that a verified
// remote connection exists: the already-connected SSH session handle. It is a
// DIFFERENT TYPE from codingStaticWorkspaceBinding on purpose: local and
// remote bindings must never be interchangeable, and complete() hard-codes
// the host kind so a local-flavoured binding can never satisfy a remote
// envelope (and vice versa).
type codingStaticRemoteSessionBinding struct {
	SessionHandle string
	HostKind      string // "remote" only; S1-A local workspaces can never satisfy this.
}

func (b codingStaticRemoteSessionBinding) complete() bool {
	return strings.TrimSpace(b.SessionHandle) != "" && strings.TrimSpace(b.HostKind) == "remote"
}

// codingStaticRemoteExecutionEnvelope is the host-owned input to the remote
// shadow planner. Like the local envelope it deliberately contains neither
// task wording nor tool names; the session handle is opaque identity resolved
// by the remote execution path, never a model-controlled parameter.
type codingStaticRemoteExecutionEnvelope struct {
	Identity *trustedCodingInvocationIdentity
	Session  codingStaticRemoteSessionBinding
	Posture  codingRequestKind
	Role     codingSubAgentRole
}

func (e codingStaticRemoteExecutionEnvelope) complete() bool {
	return e.Identity != nil && e.Identity.complete() && e.Session.complete()
}

const (
	codingStaticRemoteReadAdapter        = "coding_static_remote_workspace_read"
	codingStaticRemoteReadImplementation = "coding-static-remote-workspace-read-v1"
	codingStaticRemoteRepoAdapter        = "coding_static_remote_repo_inspect"
	codingStaticRemoteRepoImplementation = "coding-static-remote-repo-inspect-v1"

	codingStaticRemoteCatalogNeedEvidence = "host:coding-static-remote-capability-policy:v1"

	// codingStaticRemoteBindingVersion is the compile-time version of the
	// remote-session binding rules (opaque handle, host-kind hard-coding,
	// session-keyed provider identity). The hermetic conformance suite pins
	// its own version constant against this one — the equality test is the
	// machine evidence that the suite covers the implementation it claims to.
	codingStaticRemoteBindingVersion = "coding-remote-binding-v1"
)

// codingStaticRemoteSelectionBoundToSession is the plan/binding-layer check a
// future remote S1-B executor must perform before running any shadow
// selection: the selection's provider identity must be exactly this verified
// session and the binding must still be complete. A selection planned under
// handle H1 is therefore not executable under H2 — rebind invalidation is
// proven here, at the binding layer, without any network. The handle itself
// is opaque: this function never parses it, and it carries no authority
// beyond exact-string identity.
func codingStaticRemoteSelectionBoundToSession(selection tool.PlannedSelection, session codingStaticRemoteSessionBinding) bool {
	if !session.complete() {
		return false
	}
	return strings.TrimSpace(selection.Provider.ProviderID) == "coding-remote-session:"+strings.TrimSpace(session.SessionHandle)
}

// codingStaticRemoteReadOnlyCapabilityNeeds is the reviewed remote read-only
// demand side. It mirrors codingStaticReadOnlyCapabilityNeeds with the remote
// capability IDs; write, shell, build, and artifact capabilities stay out of
// this catalog until their separate contracts exist.
func codingStaticRemoteReadOnlyCapabilityNeeds() []tool.CapabilityNeed {
	return []tool.CapabilityNeed{
		{
			ID:          "need:coding-static-remote:fs.read.remote:0001",
			Capability:  tool.CapabilityFSReadRemote,
			Polarity:    tool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{codingStaticRemoteCatalogNeedEvidence},
		},
		{
			ID:          "need:coding-static-remote:repo.inspect.remote:0001",
			Capability:  tool.CapabilityRepoInspectRemote,
			Polarity:    tool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{codingStaticRemoteCatalogNeedEvidence},
		},
	}
}

// codingStaticRemotePostureConstraints mirrors the local posture denials for
// the remote capability IDs: the remote shadow inventory exposes no mutation
// provider at all, and these denials keep a later provider append from
// silently making an inquiry/operational remote turn writable or shell-capable.
func codingStaticRemotePostureConstraints(posture codingRequestKind) []tool.RoutingConstraint {
	switch posture {
	case codingRequestInquiry, codingRequestOperational:
		return []tool.RoutingConstraint{
			{ID: "coding-static-remote:deny-write", Capability: tool.CapabilityID("fs.write.remote"), Effect: "deny", Authority: tool.AuthorityPolicy},
			{ID: "coding-static-remote:deny-shell", Capability: tool.CapabilityShellExecuteRemoteHost, Effect: "deny", Authority: tool.AuthorityPolicy},
		}
	default:
		return nil
	}
}

func codingStaticRemoteCatalogCoverage(envelope codingStaticRemoteExecutionEnvelope) tool.CatalogCoverage {
	if !envelope.complete() {
		return tool.CatalogCoverage{State: tool.CatalogCoverageIncomplete, ReasonCode: tool.CatalogCoverageReasonIncomplete, ObservedAt: time.Now().UTC()}
	}
	return tool.CatalogCoverage{State: tool.CatalogCoverageComplete, ObservedAt: time.Now().UTC()}
}

// codingStaticRemoteReadOnlyProviderSpecs builds the two remote read-only
// provider specs. An incomplete envelope (including a binding-swap attempt
// where HostKind is not "remote") yields nil specs — there is no by-name
// fallback and no reconstruction from task text. Every spec's ProviderID is
// keyed to the session handle, so a planned selection's FitProof binds
// exactly this verified session and no other.
func codingStaticRemoteReadOnlyProviderSpecs(envelope codingStaticRemoteExecutionEnvelope) ([]tool.ProviderSpec, error) {
	if !envelope.complete() {
		return nil, nil
	}
	sessionID := "coding-remote-session:" + strings.TrimSpace(envelope.Session.SessionHandle)
	readSchema := semanticTrustedFileReadInvocationSchema()
	readAuthorization, err := tool.NewParameterAuthorization(readSchema)
	if err != nil {
		return nil, fmt.Errorf("authorize coding static remote read schema: %w", err)
	}
	repoSchema := semanticTrustedRepoInspectInvocationSchema()
	repoAuthorization, err := tool.NewParameterAuthorization(repoSchema)
	if err != nil {
		return nil, fmt.Errorf("authorize coding static remote repo schema: %w", err)
	}
	return []tool.ProviderSpec{
		{
			AdapterName: codingStaticRemoteReadAdapter,
			Binding: tool.ProviderBinding{
				Kind: "builtin", ProviderID: sessionID, ImplementationID: codingStaticRemoteReadImplementation,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(readSchema)),
			},
			ParameterAuthorization: readAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadRemote, Quality: 2}},
			Effects:                []tool.EffectClass{tool.EffectReadOnly},
			Ready:                  true,
			ChannelScopes:          []string{"coding"},
		},
		{
			AdapterName: codingStaticRemoteRepoAdapter,
			Binding: tool.ProviderBinding{
				Kind: "builtin", ProviderID: sessionID, ImplementationID: codingStaticRemoteRepoImplementation,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(repoSchema)),
			},
			ParameterAuthorization: repoAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityRepoInspectRemote, Quality: 2}},
			Effects:                []tool.EffectClass{tool.EffectReadOnly},
			Ready:                  true,
			ChannelScopes:          []string{"coding"},
		},
	}, nil
}

// prepareCodingStaticRemoteShadowPlan prepares the governed remote-read-only
// plan without exposing it to a model. An incomplete envelope (absent or
// swapped binding, incomplete identity) is planned as catalog_incomplete
// Unmet needs — never reconstructed, never fell back to legacy names.
//
// Degradation answer (checklist Q①, explicitly accepted): when the verified
// session binding is absent, the plan carries no remote read selections and
// the observation records catalog_incomplete. The remote band's read tools
// (ssh_read_file / ssh_list_dir) stay available through the existing legacy
// S0.5 path in that state; losing the shadow comparison is accepted because
// inventing a binding would be strictly worse than a recorded gap.
func prepareCodingStaticRemoteShadowPlan(envelope codingStaticRemoteExecutionEnvelope, facts []tool.RoutingFact, budget tool.PlanningBudget, now time.Time) (codingStaticPlanPreparation, error) {
	if envelope.Identity == nil || !envelope.Identity.complete() {
		return codingStaticPlanPreparation{}, fmt.Errorf("coding static remote identity is incomplete")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)
	coverage := codingStaticRemoteCatalogCoverage(envelope)
	providers, err := codingStaticRemoteReadOnlyProviderSpecs(envelope)
	if err != nil {
		return codingStaticPlanPreparation{}, err
	}
	snapshot, err := catalog.PublishWithCoverage(providers, coverage, now)
	if err != nil {
		return codingStaticPlanPreparation{}, fmt.Errorf("publish coding static remote shadow catalog: %w", err)
	}
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID:   envelope.Identity.RootTaskID,
		SessionID:    envelope.Identity.SessionID,
		TurnID:       envelope.Identity.TurnID,
		ChannelScope: "coding",
		Snapshot:     snapshot,
		Needs:        codingStaticRemoteReadOnlyCapabilityNeeds(),
		Facts:        tool.CloneRoutingFacts(facts),
		Constraints:  codingStaticRemotePostureConstraints(envelope.Posture),
		Budget:       budget,
		Now:          now,
	})
	if err != nil {
		return codingStaticPlanPreparation{}, fmt.Errorf("plan coding static remote shadow capability: %w", err)
	}
	return codingStaticPlanPreparation{Catalog: snapshot, Plan: plan}, nil
}

// prepareCodingStaticRemoteShadowPlanForRemoteSubagent is the runtime bridge
// into the remote shadow planner. The session binding comes only from the
// already-connected SSH session handle; an absent handle stays absent and is
// planned as catalog_incomplete (see the degradation note above).
func prepareCodingStaticRemoteShadowPlanForRemoteSubagent(r *RemoteCodingSubAgent, posture codingRequestKind, now time.Time) *codingStaticPlanPreparation {
	if r == nil || r.dynamicInvocationIdentity == nil || !r.dynamicInvocationIdentity.complete() {
		return nil
	}
	role := r.role
	if role == "" {
		role = codingRoleWorker
	}
	prepared, err := prepareCodingStaticRemoteShadowPlan(codingStaticRemoteExecutionEnvelope{
		Identity: r.dynamicInvocationIdentity,
		Session:  codingStaticRemoteSessionBinding{SessionHandle: r.sessionID, HostKind: "remote"},
		Posture:  posture,
		Role:     role,
	}, nil, tool.PlanningBudget{}, now)
	if err != nil {
		return nil
	}
	return &prepared
}
