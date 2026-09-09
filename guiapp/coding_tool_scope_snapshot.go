package guiapp

import (
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

var codingScopeSubscribers = struct {
	sync.RWMutex
	next  atomic.Uint64
	items map[uint64]codingScopeSubscription
}{items: make(map[uint64]codingScopeSubscription)}

type codingScopeSubscription struct {
	callback    *codingSubAgentCallbacks
	tenantID    string
	principalID string
	workspace   string
}

func subscribeCodingToolScope(c *codingSubAgentCallbacks) uint64 {
	if c == nil {
		return 0
	}
	id := codingScopeSubscribers.next.Add(1)
	codingScopeSubscribers.Lock()
	s := codingScopeSubscription{callback: c}
	if c.subagent != nil {
		s.workspace = strings.TrimSpace(c.subagent.projectPath)
	}
	if c.subagent != nil && c.subagent.dynamicInvocationIdentity != nil {
		s.tenantID = c.subagent.dynamicInvocationIdentity.TenantID
		s.principalID = c.subagent.dynamicInvocationIdentity.PrincipalID
	}
	codingScopeSubscribers.items[id] = s
	codingScopeSubscribers.Unlock()
	return id
}

func (a *App) NotifyCodingMCPServerChanged(tenantID, serverID, reason string) {
	if a == nil {
		return
	}
	tenantID, serverID = strings.TrimSpace(tenantID), strings.TrimSpace(serverID)
	codingScopeSubscribers.RLock()
	items := make([]*codingSubAgentCallbacks, 0)
	for _, s := range codingScopeSubscribers.items {
		if tenantID != "" && s.tenantID != tenantID {
			continue
		}
		if serverID != "" && !s.callback.codingScopeUsesMCPServer(serverID) {
			continue
		}
		items = append(items, s.callback)
	}
	codingScopeSubscribers.RUnlock()
	for _, c := range items {
		c.invalidateCodingToolScopeSnapshotForReason(reason)
	}
}

func (c *codingSubAgentCallbacks) codingScopeUsesMCPServer(serverID string) bool {
	if c == nil {
		return false
	}
	serverID = strings.TrimSpace(serverID)
	c.toolScopeMu.RLock()
	defer c.toolScopeMu.RUnlock()
	for _, m := range c.matchedMCPTools {
		if strings.TrimSpace(m.ServerID) == serverID {
			return true
		}
	}
	return false
}

func unsubscribeCodingToolScope(id uint64) {
	if id == 0 {
		return
	}
	codingScopeSubscribers.Lock()
	delete(codingScopeSubscribers.items, id)
	codingScopeSubscribers.Unlock()
}

// invalidateAllCodingToolScopes is the host event bridge for formal tasks.
func invalidateAllCodingToolScopes(reason string) { invalidateCodingToolScopesForTenant("", reason) }

func invalidateCodingToolScopesForTenant(tenantID, reason string) {
	codingScopeSubscribers.RLock()
	items := make([]*codingSubAgentCallbacks, 0, len(codingScopeSubscribers.items))
	for _, s := range codingScopeSubscribers.items {
		if strings.TrimSpace(tenantID) != "" && s.tenantID != strings.TrimSpace(tenantID) {
			continue
		}
		items = append(items, s.callback)
	}
	codingScopeSubscribers.RUnlock()
	for _, c := range items {
		c.invalidateCodingToolScopeSnapshotForReason(reason)
	}
}

// NotifyCodingCapabilityChanged is the single App-facing hook for MCP/Skill
// registry, connection health, or routing policy changes.
func (a *App) NotifyCodingCapabilityChanged(reason string) {
	if a == nil {
		return
	}
	invalidateAllCodingToolScopes(reason)
}

func (a *App) NotifyCodingCapabilityChangedForTenant(tenantID, reason string) {
	if a == nil {
		return
	}
	invalidateCodingToolScopesForTenant(tenantID, reason)
}

// NotifyCodingWorkspaceCapabilityChanged limits invalidation to one tenant
// workspace, preventing unrelated tasks from replanning.
func (a *App) NotifyCodingWorkspaceCapabilityChanged(tenantID, workspace, reason string) {
	if a == nil {
		return
	}
	tenantID, workspace = strings.TrimSpace(tenantID), strings.TrimSpace(workspace)
	codingScopeSubscribers.RLock()
	items := make([]*codingSubAgentCallbacks, 0)
	for _, s := range codingScopeSubscribers.items {
		if tenantID != "" && s.tenantID != tenantID {
			continue
		}
		if workspace != "" && s.workspace != workspace {
			continue
		}
		items = append(items, s.callback)
	}
	codingScopeSubscribers.RUnlock()
	for _, c := range items {
		c.invalidateCodingToolScopeSnapshotForReason(reason)
	}
}

// finalizeCodingToolScopeSnapshot freezes the dynamic capability surface for
// one callback/task. The digest is derived only from host-admitted stable
// identifiers and the routing policy version; user wording is intentionally
// absent so changing a prompt cannot make a tool appear or disappear.
func (c *codingSubAgentCallbacks) finalizeCodingToolScopeSnapshot() {
	if c == nil {
		return
	}
	// The planner digest is the authoritative request identity.  Adopt it at
	// every finalization boundary because a callback may be constructed before
	// the host finishes preparing the immutable shadow plan.
	if !c.ensureCodingPlannerSnapshotAdopted() {
		return
	}
	c.toolScopeMu.Lock()
	defer c.toolScopeMu.Unlock()
	if c.toolSnapshotPlannerConflict {
		return
	}

	// A digest adopted from the semantic planner is the authority for the
	// request. It must survive repeated prompt/render calls and may only be
	// removed by an explicit host capability/policy invalidation.
	if c.toolSnapshotID != "" && c.toolSnapshotPlannerDigest {
		return
	}
	// Do not freeze a partial selection. The caller marks each side selected
	// only after it has completed host discovery (an empty side is valid).
	if !c.matchedSkillsSelected || !c.matchedMCPToolsSelected {
		return
	}
	// Once a fallback has been committed, preserve it for the rest of this
	// task scope. A later planner adoption can explicitly replace this
	// fallback and mark the result authoritative.
	if c.toolSnapshotID != "" {
		return
	}

	material := codingToolScopeSnapshotMaterialForCallbacks(c)
	digest := codingSubAgentBindingDigest(material)
	if digest == "" {
		// json.Marshal currently cannot fail for this material, but retaining an
		// empty result on an unexpected serialization failure is safer than
		// publishing an unbound/ambiguous snapshot identifier.
		return
	}
	c.toolSnapshotID = "toolsnap:" + digest
	c.toolSnapshotPlannerDigest = false
}

// The fallback snapshot is deliberately a canonical identity projection. It
// contains binding/schema/policy identities, never scores, descriptions,
// prompts, or other user wording. Keeping this projection local to the GUI
// callback preserves compatibility while the common planner is being wired;
// once a planner digest is adopted, finalize never recomputes it.
type codingToolScopeSnapshotMaterial struct {
	Version          string                            `json:"version"`
	SelectionPosture string                            `json:"selection_posture"`
	Identity         codingToolScopeInvocationIdentity `json:"identity"`
	Policy           codingToolScopePolicyIdentity     `json:"policy"`
	Workspace        codingToolScopeWorkspaceIdentity  `json:"workspace"`
	Planner          codingToolScopePlannerIdentity    `json:"planner"`
	Skills           []codingToolScopeSkillIdentity    `json:"skills"`
	MCPTools         []codingToolScopeMCPIdentity      `json:"mcp_tools"`
}

type codingToolScopeInvocationIdentity struct {
	TenantID    string `json:"tenant_id,omitempty"`
	PrincipalID string `json:"principal_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	RootTaskID  string `json:"root_task_id,omitempty"`
	TurnID      string `json:"turn_id,omitempty"`
}

type codingToolScopePolicyIdentity struct {
	Digest                     string `json:"digest,omitempty"`
	NormalizedDigest           string `json:"normalized_digest,omitempty"`
	Mode                       string `json:"mode,omitempty"`
	ProjectRoot                string `json:"project_root,omitempty"`
	RemoteTarget               string `json:"remote_target,omitempty"`
	ReadOnly                   bool   `json:"read_only"`
	WorkspaceIsolated          bool   `json:"workspace_isolated"`
	FinalDiffGateRequired      bool   `json:"final_diff_gate_required"`
	FinalWorkspaceGateRequired bool   `json:"final_workspace_gate_required"`
	WriteSetUnknown            bool   `json:"write_set_unknown"`
}

type codingToolScopeWorkspaceIdentity struct {
	ProjectPath     string `json:"project_path,omitempty"`
	WorkspaceHandle string `json:"workspace_handle,omitempty"`
	HostKind        string `json:"host_kind,omitempty"`
}

type codingToolScopePlannerIdentity struct {
	PlanID            string `json:"plan_id,omitempty"`
	CatalogDigest     string `json:"catalog_digest,omitempty"`
	SnapshotDigest    string `json:"snapshot_digest,omitempty"`
	CatalogGeneration int64  `json:"catalog_generation,omitempty"`
}

type codingToolScopeSkillIdentity struct {
	StableID       string   `json:"stable_id,omitempty"`
	QualifiedID    string   `json:"qualified_id,omitempty"`
	Name           string   `json:"name,omitempty"`
	Version        string   `json:"version,omitempty"`
	ContentDigest  string   `json:"content_digest,omitempty"`
	ContractDigest string   `json:"contract_digest,omitempty"`
	SchemaDigest   string   `json:"schema_digest,omitempty"`
	RequiredArgs   []string `json:"required_args,omitempty"`
}

type codingToolScopeMCPIdentity struct {
	ServerID       string   `json:"server_id,omitempty"`
	ServerName     string   `json:"server_name,omitempty"`
	ToolName       string   `json:"tool_name,omitempty"`
	SchemaDigest   string   `json:"schema_digest,omitempty"`
	ContractDigest string   `json:"contract_digest,omitempty"`
	RequiredArgs   []string `json:"required_args,omitempty"`
	ArgumentHints  []string `json:"argument_hints,omitempty"`
}

func codingToolScopeSnapshotMaterialForCallbacks(c *codingSubAgentCallbacks) codingToolScopeSnapshotMaterial {
	material := codingToolScopeSnapshotMaterial{
		Version:          "coding-tool-scope-v2",
		SelectionPosture: "scope",
		Identity:         codingToolScopeInvocationIdentity{},
		Policy:           codingToolScopePolicyIdentity{},
		Workspace:        codingToolScopeWorkspaceIdentity{},
		Planner:          codingToolScopePlannerIdentity{},
		Skills:           make([]codingToolScopeSkillIdentity, 0, len(c.matchedSkills)),
		MCPTools:         make([]codingToolScopeMCPIdentity, 0, len(c.matchedMCPTools)),
	}
	if !c.scopeBasedSelection {
		material.SelectionPosture = "legacy"
	}
	for _, skill := range c.matchedSkills {
		material.Skills = append(material.Skills, codingToolScopeSkillIdentity{
			StableID:       strings.TrimSpace(skill.StableID),
			QualifiedID:    strings.TrimSpace(skill.QualifiedID),
			Name:           strings.TrimSpace(skill.Name),
			Version:        strings.TrimSpace(skill.Version),
			ContentDigest:  strings.TrimSpace(skill.ContentDigest),
			ContractDigest: strings.TrimSpace(skill.ContractDigest),
			SchemaDigest:   strings.TrimSpace(skill.SchemaDigest),
			RequiredArgs:   codingToolScopeCanonicalStrings(skill.RequiredArgs),
		})
	}
	for _, mcp := range c.matchedMCPTools {
		material.MCPTools = append(material.MCPTools, codingToolScopeMCPIdentity{
			ServerID:       strings.TrimSpace(mcp.ServerID),
			ServerName:     strings.TrimSpace(mcp.ServerName),
			ToolName:       strings.TrimSpace(mcp.ToolName),
			SchemaDigest:   strings.TrimSpace(mcp.SchemaDigest),
			ContractDigest: strings.TrimSpace(mcp.ContractDigest),
			RequiredArgs:   codingToolScopeCanonicalStrings(mcp.RequiredArgs),
			ArgumentHints:  codingToolScopeCanonicalStrings(mcp.ArgumentHints),
		})
	}
	sort.SliceStable(material.Skills, func(i, j int) bool {
		return codingToolScopeSkillSortKey(material.Skills[i]) < codingToolScopeSkillSortKey(material.Skills[j])
	})
	sort.SliceStable(material.MCPTools, func(i, j int) bool {
		return codingToolScopeMCPSortKey(material.MCPTools[i]) < codingToolScopeMCPSortKey(material.MCPTools[j])
	})

	if c.subagent == nil {
		return material
	}
	sa := c.subagent
	material.Workspace = codingToolScopeWorkspaceIdentity{ProjectPath: strings.TrimSpace(sa.projectPath), WorkspaceHandle: strings.TrimSpace(sa.staticWorkspaceBinding.WorkspaceHandle), HostKind: strings.TrimSpace(sa.staticWorkspaceBinding.HostKind)}
	material.Identity = codingToolScopeInvocationIdentity{}
	if identity := sa.dynamicInvocationIdentity; identity != nil {
		material.Identity = codingToolScopeInvocationIdentity{
			TenantID: strings.TrimSpace(identity.TenantID), PrincipalID: strings.TrimSpace(identity.PrincipalID),
			SessionID: strings.TrimSpace(identity.SessionID), RootTaskID: strings.TrimSpace(identity.RootTaskID), TurnID: strings.TrimSpace(identity.TurnID),
		}
	}
	policy := codingruntime.PolicySnapshot{}
	if sa.runtimeAttempt != nil {
		policy = sa.runtimeAttempt.Policy
	}
	normalizedPolicyDigest, digestErr := codingruntime.PolicyDigest(policy)
	material.Policy = codingToolScopePolicyIdentity{
		Digest: strings.TrimSpace(policy.Digest), NormalizedDigest: strings.TrimSpace(normalizedPolicyDigest),
		Mode: strings.TrimSpace(policy.Mode), ProjectRoot: strings.TrimSpace(policy.ProjectRoot), RemoteTarget: strings.TrimSpace(policy.RemoteTarget),
		ReadOnly: policy.ReadOnly, WorkspaceIsolated: policy.WorkspaceIsolated,
		FinalDiffGateRequired: policy.FinalDiffGateRequired, FinalWorkspaceGateRequired: policy.FinalWorkspaceGateRequired,
		WriteSetUnknown: policy.WriteSet.Unknown,
	}
	if digestErr != nil {
		// Keep malformed policy identity in the material rather than dropping it.
		// The fallback remains deterministic and a later planner adoption can
		// replace it with the authoritative digest.
		material.Policy.NormalizedDigest = "invalid-policy:" + codingSubAgentBindingDigest(struct {
			Mode         string
			ProjectRoot  string
			RemoteTarget string
			ReadOnly     bool
			WriteSet     interface{}
		}{policy.Mode, policy.ProjectRoot, policy.RemoteTarget, policy.ReadOnly, policy.WriteSet})
	}
	if staticPlan := codingStaticShadowPlanOf(sa); staticPlan != nil {
		material.Planner = codingToolScopePlannerIdentity{
			PlanID: strings.TrimSpace(staticPlan.Plan.ID), CatalogDigest: strings.TrimSpace(staticPlan.Plan.CatalogDigest),
			SnapshotDigest: strings.TrimSpace(staticPlan.Plan.SnapshotDigest), CatalogGeneration: int64(staticPlan.Plan.CatalogGeneration),
		}
	}
	return material
}

func codingToolScopeCanonicalStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func codingToolScopeSkillSortKey(value codingToolScopeSkillIdentity) string {
	return strings.Join([]string{value.StableID, value.QualifiedID, value.Name, value.Version, value.ContentDigest, value.ContractDigest, value.SchemaDigest, strings.Join(value.RequiredArgs, "\x00")}, "\x00")
}

func codingToolScopeMCPSortKey(value codingToolScopeMCPIdentity) string {
	return strings.Join([]string{value.ServerID, value.ServerName, value.ToolName, value.SchemaDigest, value.ContractDigest, strings.Join(value.RequiredArgs, "\x00"), strings.Join(value.ArgumentHints, "\x00")}, "\x00")
}

func (c *codingSubAgentCallbacks) codingToolSnapshotID() string {
	if c == nil {
		return ""
	}
	c.toolScopeMu.RLock()
	defer c.toolScopeMu.RUnlock()
	if c.toolSnapshotPlannerConflict {
		return ""
	}
	return c.toolSnapshotID
}

// ensureCodingPlannerSnapshotAdopted binds the callback to the immutable
// planner identity when one is available.  It intentionally reads only the
// host-prepared plan: task wording, scores, and provider names cannot create
// or replace a tool snapshot.  A conflicting planner digest is terminal for
// this callback until the host explicitly invalidates the scope.
func (c *codingSubAgentCallbacks) ensureCodingPlannerSnapshotAdopted() bool {
	if c == nil {
		return false
	}
	c.toolScopeMu.RLock()
	conflict := c.toolSnapshotPlannerConflict
	c.toolScopeMu.RUnlock()
	if conflict {
		return false
	}
	staticPlan := codingStaticShadowPlanOf(c.subagent)
	if staticPlan == nil {
		return true
	}
	plan := staticPlan.Plan
	digest := strings.TrimSpace(plan.SnapshotDigest)
	c.toolScopeMu.Lock()
	if c.toolSnapshotPlannerInvalidated {
		// The host may leave the old plan attached while asynchronously
		// preparing a replacement. Re-adopting the same pointer would
		// resurrect an invalidated snapshot. Treat the pointer as the plan
		// generation: mutating a plan in place is not a replacement and must
		// remain fenced until the host publishes a new preparation object.
		// A nil pointer is a valid "replacement pending" marker; any later
		// non-nil preparation is then a new host decision.
		unchanged := c.toolSnapshotPlannerInvalidatedPlan == staticPlan
		if unchanged {
			c.toolScopeMu.Unlock()
			return true
		}
		c.toolSnapshotPlannerInvalidated = false
		c.toolSnapshotPlannerInvalidatedPlan = nil
		c.toolSnapshotPlannerInvalidatedDigest = ""
	}
	c.toolScopeMu.Unlock()
	if digest == "" {
		// A planner result without its full snapshot identity is not safe to
		// adopt.  Leave the callback on its deterministic fallback until the
		// host supplies a complete plan.
		return true
	}
	if strings.TrimSpace(plan.ID) == "" || strings.TrimSpace(plan.RootTaskID) == "" {
		log.Printf("[coding-subagent] planner snapshot rejected reason=invalid_plan_identity")
		return true
	}
	if identity := c.subagent.dynamicInvocationIdentity; identity != nil && identity.complete() && identity.RootTaskID != plan.RootTaskID {
		log.Printf("[coding-subagent] planner snapshot rejected reason=plan_root_mismatch")
		return true
	}
	if c.adoptCodingToolSnapshotDigest(digest) {
		c.toolScopeMu.Lock()
		c.toolSnapshotPlannerAdoptedPlan = staticPlan
		c.toolScopeMu.Unlock()
		return true
	}
	c.toolScopeMu.Lock()
	c.toolSnapshotPlannerConflict = true
	current := c.toolSnapshotID
	c.toolScopeMu.Unlock()
	log.Printf("[coding-subagent] planner snapshot conflict expected=%s current=%s", digest, current)
	return false
}

// adoptCodingToolSnapshotDigest records the digest issued by the semantic
// planner. Once adopted, local collection hashing must not replace it.
func (c *codingSubAgentCallbacks) adoptCodingToolSnapshotDigest(digest string) bool {
	digest = strings.TrimSpace(digest)
	if c == nil || digest == "" {
		return false
	}
	c.toolScopeMu.Lock()
	defer c.toolScopeMu.Unlock()
	// A planner digest is immutable once adopted. A local fallback, however,
	// may have been published before the planner result arrived; replace that
	// fallback so the durable planner identity wins without weakening the
	// conflicting-planner guard.
	if c.toolSnapshotID != "" && c.toolSnapshotPlannerDigest && c.toolSnapshotID != digest {
		return false
	}
	c.toolSnapshotID = digest
	c.toolSnapshotPlannerDigest = true
	return true
}

// invalidateCodingToolScopeSnapshot is called only when host capability state
// or the routing policy changes. User wording and iteration changes must not
// call it: those belong to the same task scope and must retain one stable
// tool surface.
func (c *codingSubAgentCallbacks) invalidateCodingToolScopeSnapshot() {
	c.invalidateCodingToolScopeSnapshotForReason("host_capability_changed")
}

func (c *codingSubAgentCallbacks) invalidateCodingToolScopeSnapshotForReason(reason string) {
	if c == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unspecified"
	}
	c.toolScopeMu.Lock()
	defer c.toolScopeMu.Unlock()
	previous := c.toolSnapshotID
	fencedPlan := (*codingStaticPlanPreparation)(nil)
	fencedPlan = codingStaticShadowPlanOf(c.subagent)
	if fencedPlan == nil {
		fencedPlan = c.toolSnapshotPlannerAdoptedPlan
	}
	if fencedPlan != nil {
		c.toolSnapshotPlannerInvalidated = true
		c.toolSnapshotPlannerInvalidatedPlan = fencedPlan
		c.toolSnapshotPlannerInvalidatedDigest = strings.TrimSpace(fencedPlan.Plan.SnapshotDigest)
	} else {
		// Keep an explicit nil-generation fence. Clearing it here would let a
		// late reference to the pre-invalidation plan be adopted after the
		// host temporarily detached the plan.
		c.toolSnapshotPlannerInvalidated = true
		c.toolSnapshotPlannerInvalidatedPlan = nil
		c.toolSnapshotPlannerInvalidatedDigest = ""
	}
	c.toolSnapshotID = ""
	c.toolSnapshotPlannerDigest = false
	c.toolSnapshotPlannerConflict = false
	c.matchedSkills = nil
	c.matchedMCPTools = nil
	c.hostAdmittedSkills = nil
	c.hostAdmittedMCPTools = nil
	c.hostDynamicBindingsAdmitted = false
	c.matchedSkillsSelected = false
	c.matchedMCPToolsSelected = false
	log.Printf("[coding-subagent] tool scope snapshot invalidated snapshot=%s reason=%s", previous, reason)
}
