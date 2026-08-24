package main

import (
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingStaticCompatibilityInventory is the explicit S0 inventory for the
// still-legacy Coding static belt.  It is deliberately an inventory rather
// than a second planner: before S1-C, these names remain compatibility
// definitions and name dispatch is still present.  Keeping the inventory
// closed makes every new model-visible static tool an intentional review
// decision instead of an unnoticed append to a definition slice.
//
// This inventory does not grant authority.  The current model request surface
// is recorded independently by the callbacks and is the final compatibility
// admission check before the legacy dispatcher.  S1-C replaces this belt with
// catalog selections, aliases, grants and durable admission.
type codingStaticCompatibilityInventoryItem struct {
	HostKind     string
	Name         string
	Capability   tool.CapabilityID
	Effect       tool.EffectClass
	BindingScope string
	ControlPlane bool
}

const (
	codingStaticCompatibilityHostLocal  = "local"
	codingStaticCompatibilityHostRemote = "remote"
)

var codingStaticCompatibilityInventory = []codingStaticCompatibilityInventoryItem{
	// Local workspace I/O and inspection.
	{codingStaticCompatibilityHostLocal, "Glob", tool.CapabilityFSReadLocal, tool.EffectReadOnly, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "ripgrep", tool.CapabilityFSReadLocal, tool.EffectReadOnly, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "read_file", tool.CapabilityFSReadLocal, tool.EffectReadOnly, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "list_directory", tool.CapabilityFSReadLocal, tool.EffectReadOnly, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "git_diff", tool.CapabilityRepoInspectVCS, tool.EffectReadOnly, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "edit_file", tool.CapabilityFSWriteLocal, tool.EffectLocalMutation, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "edit_lines", tool.CapabilityFSWriteLocal, tool.EffectLocalMutation, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "write_file", tool.CapabilityFSWriteLocal, tool.EffectLocalMutation, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "bash", tool.CapabilityShellExecuteLocal, tool.EffectSensitive, "local-workspace", false},
	// Local optional research/knowledge. These remain legacy compatibility
	// families until their own network/artifact contracts are migrated.
	{codingStaticCompatibilityHostLocal, "web_search", tool.CapabilityInformationFetchWeb, tool.EffectReadOnly, "network-policy", false},
	{codingStaticCompatibilityHostLocal, "web_fetch", tool.CapabilityInformationFetchWeb, tool.EffectReadOnly, "network-policy", false},
	{codingStaticCompatibilityHostLocal, "download_file", tool.CapabilityArtifactAcquireRemote, tool.EffectLocalMutation, "local-workspace", false},
	{codingStaticCompatibilityHostLocal, "current_datetime", tool.CapabilityID("information.current_datetime"), tool.EffectReadOnly, "host", false},
	{codingStaticCompatibilityHostLocal, "coding_knowledge_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly, "knowledge-owner", false},
	{codingStaticCompatibilityHostLocal, "knowledge_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly, "knowledge-owner", false},
	{codingStaticCompatibilityHostLocal, "knowledge_image_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly, "knowledge-owner", false},
	// Control plane never borrows a workspace/provider binding from the model.
	{codingStaticCompatibilityHostLocal, codeNavigationToolName, tool.CapabilityID("code.navigation"), tool.EffectReadOnly, "local-workspace", true},
	{codingStaticCompatibilityHostLocal, reportLocalizationToolName, tool.CapabilityID("code.localization_evidence"), tool.EffectReadOnly, "coding-revision", true},
	{codingStaticCompatibilityHostLocal, codingSubAgentSpawnToolName, tool.CapabilityAgentDelegateSubtask, tool.EffectSensitive, "parent-lineage", true},
	{codingStaticCompatibilityHostLocal, codingAgentTodoToolName, tool.CapabilityTaskTrackLocal, tool.EffectLocalMutation, "coding-revision", true},
	{codingStaticCompatibilityHostLocal, "goal", tool.CapabilityGoalManageLongRunning, tool.EffectLocalMutation, "coding-revision", true},

	// Remote workspace I/O. The ssh session/workdir is a separate S2 binding;
	// it must never be treated as a local workspace provider.
	{codingStaticCompatibilityHostRemote, "ssh_read_file", tool.CapabilityID("fs.read.remote"), tool.EffectReadOnly, "remote-workspace", false},
	{codingStaticCompatibilityHostRemote, "ssh_list_dir", tool.CapabilityID("fs.read.remote"), tool.EffectReadOnly, "remote-workspace", false},
	{codingStaticCompatibilityHostRemote, "ssh_write_file", tool.CapabilityID("fs.write.remote"), tool.EffectExternalEffect, "remote-workspace", false},
	{codingStaticCompatibilityHostRemote, "ssh_edit_file", tool.CapabilityID("fs.write.remote"), tool.EffectExternalEffect, "remote-workspace", false},
	{codingStaticCompatibilityHostRemote, "ssh_bash", tool.CapabilityShellExecuteRemoteHost, tool.EffectExternalEffect, "remote-workspace", false},
	{codingStaticCompatibilityHostRemote, "ssh_check_task", tool.CapabilityID("remote.task.inspect"), tool.EffectReadOnly, "remote-workspace", false},
	{codingStaticCompatibilityHostRemote, codeNavigationToolName, tool.CapabilityID("code.navigation"), tool.EffectReadOnly, "remote-workspace", true},
	{codingStaticCompatibilityHostRemote, reportLocalizationToolName, tool.CapabilityID("code.localization_evidence"), tool.EffectReadOnly, "coding-revision", true},
	{codingStaticCompatibilityHostRemote, codingSubAgentSpawnToolName, tool.CapabilityAgentDelegateSubtask, tool.EffectSensitive, "parent-lineage", true},
	{codingStaticCompatibilityHostRemote, codingAgentTodoToolName, tool.CapabilityTaskTrackLocal, tool.EffectLocalMutation, "coding-revision", true},
	{codingStaticCompatibilityHostRemote, "goal", tool.CapabilityGoalManageLongRunning, tool.EffectLocalMutation, "coding-revision", true},
	// Remote Coding invokes these on the local host, but they are still
	// model-visible from the remote callback and must be inventoried there.
	{codingStaticCompatibilityHostRemote, "web_search", tool.CapabilityInformationFetchWeb, tool.EffectReadOnly, "network-policy", false},
	{codingStaticCompatibilityHostRemote, "web_fetch", tool.CapabilityInformationFetchWeb, tool.EffectReadOnly, "network-policy", false},
	{codingStaticCompatibilityHostRemote, "download_file", tool.CapabilityArtifactAcquireRemote, tool.EffectLocalMutation, "local-host-workspace", false},
	{codingStaticCompatibilityHostRemote, "current_datetime", tool.CapabilityID("information.current_datetime"), tool.EffectReadOnly, "host", false},
	{codingStaticCompatibilityHostRemote, "coding_knowledge_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly, "knowledge-owner", false},
	{codingStaticCompatibilityHostRemote, "knowledge_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly, "knowledge-owner", false},
	{codingStaticCompatibilityHostRemote, "knowledge_image_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly, "knowledge-owner", false},
}

func codingStaticCompatibilityInventoryHas(hostKind, name string) bool {
	hostKind, name = strings.TrimSpace(hostKind), strings.TrimSpace(name)
	for _, item := range codingStaticCompatibilityInventory {
		if item.HostKind == hostKind && item.Name == name {
			return true
		}
	}
	return false
}

// filterCodingStaticCompatibilitySurface is an S0 fail-closed registration
// gate. It must be applied after posture/role filters so the result is the
// actual complete replacement surface, not a superset inferred from a task.
func filterCodingStaticCompatibilitySurface(hostKind string, definitions []map[string]interface{}) []map[string]interface{} {
	if len(definitions) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(definitions))
	for _, definition := range definitions {
		name := extractToolName(definition)
		if codingStaticCompatibilityInventoryHas(hostKind, name) {
			out = append(out, definition)
		}
	}
	return out
}

// filterUncorrelatedCodingStaticCompatibilityEffects applies the S0.5 normal
// mode containment policy. The current Coding HTTP/SSE adapters cannot bind a
// static tool call to a transport-owned response and call identity, so an
// ordinary request surface must not expose workspace mutation, command, SSH,
// or artifact-acquisition families through the legacy name dispatcher.
//
// Control-plane names deliberately retain their separate S3 revision/CAS and
// lineage gates. This helper does not grant them authority; it only avoids
// conflating their state-machine semantics with provider/workspace effects.
// A future S1-C adapter must replace this compatibility filter with its
// correlation-bound publish/bind/admit/journal cutover for each family.
func filterUncorrelatedCodingStaticCompatibilityEffects(hostKind string, definitions []map[string]interface{}) []map[string]interface{} {
	if len(definitions) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(definitions))
	for _, definition := range definitions {
		item, ok := codingStaticCompatibilityInventoryLookup(hostKind, extractToolName(definition))
		if !ok {
			// LongHorizon host tools are outside Coding's static inventory. Their
			// frozen episode policy/registry remains the independent owner, and
			// they must not be silently removed by this Coding-only effect policy.
			// The existing rendered-name fence still prevents absent registry names
			// from reaching the host dispatcher.
			if isCodingStaticCompatibilityExternalDefinition(definition) {
				out = append(out, definition)
			}
			continue
		}
		if !codingStaticCompatibilityItemAllowedWithoutTransportCorrelation(item) {
			continue
		}
		out = append(out, definition)
	}
	return out
}

// codingStaticCompatibilityItemAllowedWithoutTransportCorrelation keeps only
// the narrow S3 operation that has an explicit compare-and-apply token in the
// rendered schema. report_localization overwrites callback evidence without an
// expected revision/version, so a delayed same-surface call could replace a
// newer report even though no static workspace write is presently exposed.
// Delegating a child creates an independent runtime attempt and can consume
// isolate/approval capacity; that is an effectful lifecycle operation, not a
// revision-CAS update. Both wait for a correlation-bound admission/journal
// path even though the inventory classifies them as control-plane capability.
func codingStaticCompatibilityItemAllowedWithoutTransportCorrelation(item codingStaticCompatibilityInventoryItem) bool {
	if item.ControlPlane {
		switch item.Capability {
		case tool.CapabilityTaskTrackLocal:
			return true
		case tool.CapabilityAgentDelegateSubtask, tool.CapabilityID("code.localization_evidence"):
			return false
		}
	}
	return item.Effect == tool.EffectReadOnly
}

func isCodingStaticCompatibilityExternalDefinition(definition map[string]interface{}) bool {
	name := strings.TrimSpace(extractToolName(definition))
	return strings.HasPrefix(name, "computer_") || strings.HasPrefix(name, "browser_")
}

func codingStaticCompatibilityInventoryLookup(hostKind, name string) (codingStaticCompatibilityInventoryItem, bool) {
	hostKind, name = strings.TrimSpace(hostKind), strings.TrimSpace(name)
	for _, item := range codingStaticCompatibilityInventory {
		if item.HostKind == hostKind && item.Name == name {
			return item, true
		}
	}
	return codingStaticCompatibilityInventoryItem{}, false
}

func codingStaticCompatibilitySurfaceNames(definitions []map[string]interface{}) map[string]struct{} {
	names := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if name := strings.TrimSpace(extractToolName(definition)); name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

// codingStaticCompatibilitySurfaceObservation is the S0 audit shape for one
// actually rendered legacy Coding surface. It deliberately contains no task
// text, workspace path/handle, model arguments, aliases, grants, or provider
// credentials. A plan ID/digest is an opaque decision reference, not an
// executable authorization token.
type codingStaticCompatibilitySurfaceObservation struct {
	HostKind               string
	RootTaskID             string
	TurnID                 string
	StaticRevision         uint64
	Posture                string
	RenderedToolNames      []string
	ShadowState            string
	PlanID                 string
	CatalogGeneration      uint64
	OmittedReasons         []string
	UnmetReasons           []string
	LegacyOnlyCapabilities []string
	ShadowOnlyCapabilities []string
}

func newCodingStaticCompatibilitySurfaceObservation(hostKind string, revision uint64, posture codingRequestKind, definitions []map[string]interface{}, identity *trustedCodingInvocationIdentity, prepared *codingStaticPlanPreparation) codingStaticCompatibilitySurfaceObservation {
	observation := codingStaticCompatibilitySurfaceObservation{
		HostKind:       strings.TrimSpace(hostKind),
		StaticRevision: revision,
		Posture:        string(posture),
		ShadowState:    "not_prepared",
	}
	if identity != nil && identity.complete() {
		observation.RootTaskID = identity.RootTaskID
		observation.TurnID = identity.TurnID
	}
	for name := range codingStaticCompatibilitySurfaceNames(definitions) {
		observation.RenderedToolNames = append(observation.RenderedToolNames, name)
	}
	sort.Strings(observation.RenderedToolNames)
	if prepared == nil {
		return observation
	}
	observation.ShadowState = "prepared"
	observation.PlanID = prepared.Plan.ID
	observation.CatalogGeneration = prepared.Plan.CatalogGeneration
	for _, omitted := range prepared.Plan.Omitted {
		observation.OmittedReasons = append(observation.OmittedReasons, omitted.ReasonCode)
	}
	for _, unmet := range prepared.Plan.Unmet {
		observation.UnmetReasons = append(observation.UnmetReasons, unmet.ReasonCode)
	}
	observation.OmittedReasons = uniqueSortedCodingStaticObservationStrings(observation.OmittedReasons)
	observation.UnmetReasons = uniqueSortedCodingStaticObservationStrings(observation.UnmetReasons)
	if containsCodingStaticObservationString(observation.UnmetReasons, "catalog_incomplete") {
		observation.ShadowState = "catalog_incomplete"
	}
	observation.LegacyOnlyCapabilities, observation.ShadowOnlyCapabilities = codingStaticCompatibilityCapabilityDifference(hostKind, definitions, prepared.Plan)
	return observation
}

// codingStaticCompatibilityCapabilityDifference compares capability classes,
// not function names. Legacy definitions can legitimately have several names
// for one capability, whereas a shadow selection is one provider decision.
// This is S0 audit evidence only and must never feed rendering, admission, or
// a fallback decision.
func codingStaticCompatibilityCapabilityDifference(hostKind string, definitions []map[string]interface{}, plan tool.ToolPlan) (legacyOnly, shadowOnly []string) {
	legacy := make(map[string]struct{})
	for _, definition := range definitions {
		name := strings.TrimSpace(extractToolName(definition))
		for _, item := range codingStaticCompatibilityInventory {
			if item.HostKind == hostKind && item.Name == name {
				legacy[string(item.Capability)] = struct{}{}
				break
			}
		}
	}
	shadow := make(map[string]struct{})
	for _, selection := range plan.Selections {
		if capability := strings.TrimSpace(string(selection.FitProof.MatchedCapability)); capability != "" {
			shadow[capability] = struct{}{}
		}
	}
	for capability := range legacy {
		if _, found := shadow[capability]; !found {
			legacyOnly = append(legacyOnly, capability)
		}
	}
	for capability := range shadow {
		if _, found := legacy[capability]; !found {
			shadowOnly = append(shadowOnly, capability)
		}
	}
	sort.Strings(legacyOnly)
	sort.Strings(shadowOnly)
	return legacyOnly, shadowOnly
}

func uniqueSortedCodingStaticObservationStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsCodingStaticObservationString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// recordCodingStaticCompatibilitySurfaceObservation persists the S0 evidence
// only when the host has an audit log. The in-memory callback copy is updated
// independently, so a missing audit store never changes the rendered surface
// or converts an unavailable audit service into a routing failure.
func recordCodingStaticCompatibilitySurfaceObservation(handler *IMMessageHandler, userID string, observation codingStaticCompatibilitySurfaceObservation) {
	if handler == nil || handler.app == nil {
		return
	}
	handler.app.ensureAuditLog()
	if handler.app.auditLog == nil {
		return
	}
	_ = handler.app.auditLog.Log(security.AuditEntry{
		Timestamp: time.Now().UTC(),
		UserID:    strings.TrimSpace(userID),
		ToolName:  "coding_static_surface",
		Arguments: map[string]interface{}{
			"host_kind":                observation.HostKind,
			"root_task_id":             observation.RootTaskID,
			"turn_id":                  observation.TurnID,
			"static_revision":          observation.StaticRevision,
			"posture":                  observation.Posture,
			"rendered_tool_names":      append([]string(nil), observation.RenderedToolNames...),
			"shadow_state":             observation.ShadowState,
			"plan_id":                  observation.PlanID,
			"catalog_generation":       observation.CatalogGeneration,
			"omitted_reasons":          append([]string(nil), observation.OmittedReasons...),
			"unmet_reasons":            append([]string(nil), observation.UnmetReasons...),
			"legacy_only_capabilities": append([]string(nil), observation.LegacyOnlyCapabilities...),
			"shadow_only_capabilities": append([]string(nil), observation.ShadowOnlyCapabilities...),
		},
		RiskLevel:    security.RiskLow,
		PolicyAction: security.PolicyAudit,
		Result:       "coding static compatibility surface rendered",
		Source:       "coding_subagent",
	})
}
