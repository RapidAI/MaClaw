package tool

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// LegacyAdapterProvision is the reviewed, transitional mapping from a legacy
// model function name to a user-facing capability. It is deliberately more
// than a name allow-list: owner, fixed adapter contract, maximum effect and
// removal date are all required before a legacy fallback can be rendered.
//
// It is not a substitute for ProviderBinding or ParameterAuthorization. Those
// are provided by the semantic planner. This catalog exists only to stop the
// old Router from treating a BM25 hit as a capability grant during migration.
type LegacyAdapterProvision struct {
	ToolName        string
	Capability      CapabilityID
	Owner           string
	AdapterContract string
	Effects         []EffectClass
	DeleteAfter     time.Time
}

// RoutingRecommendation is the migration-safe output of the legacy Router.
// It contains reviewed capability candidates and evidence, never definitions
// or an execution grant. Hosts may feed it to a LegacyAdapterPlan/semantic
// planner, but must not render it directly as a model tool surface.
type RoutingRecommendation struct {
	CandidateCapabilities []CapabilityID
	SearchQuery           string
	Confidence            float64
	Evidence              []RoutingEvidence
}

// RoutingEvidence explains why a reviewed candidate was considered. It is
// diagnostic/host-only data; exposing it to the model could reveal hidden
// catalog entries or scoring details.
type RoutingEvidence struct {
	ToolName        string
	Capability      CapabilityID
	Reason          string
	Score           float64
	AdapterContract string
}

func (r RoutingRecommendation) clone() RoutingRecommendation {
	out := r
	out.CandidateCapabilities = append([]CapabilityID(nil), r.CandidateCapabilities...)
	out.Evidence = append([]RoutingEvidence(nil), r.Evidence...)
	return out
}

// LegacyCandidateToolNames is derived from reviewed provisions, rather than a
// parallel manually-maintained list. A name cannot become routeable merely by
// being added to a generic core collection.
var LegacyCandidateToolNames = legacyCandidateToolNamesFromProvisions()

func (p LegacyAdapterProvision) validate() error {
	if strings.TrimSpace(p.ToolName) == "" {
		return fmt.Errorf("legacy adapter tool name is required")
	}
	if strings.TrimSpace(string(p.Capability)) == "" {
		return fmt.Errorf("legacy adapter %q capability is required", p.ToolName)
	}
	if strings.TrimSpace(p.Owner) == "" {
		return fmt.Errorf("legacy adapter %q owner is required", p.ToolName)
	}
	if strings.TrimSpace(p.AdapterContract) == "" {
		return fmt.Errorf("legacy adapter %q contract is required", p.ToolName)
	}
	if p.DeleteAfter.IsZero() {
		return fmt.Errorf("legacy adapter %q delete-after is required", p.ToolName)
	}
	if len(p.Effects) == 0 {
		return fmt.Errorf("legacy adapter %q effects are required", p.ToolName)
	}
	return validateEffectClasses(p.Effects)
}

func cloneLegacyAdapterProvision(in LegacyAdapterProvision) LegacyAdapterProvision {
	out := in
	out.Effects = append([]EffectClass(nil), in.Effects...)
	return out
}

// LegacyAdapterProvisionForTool returns the immutable migration contract for
// a legacy name. A missing or expired provision is catalog_incomplete; callers
// must not infer a capability from a tool name, description, BM25 score, or
// prior session state.
func LegacyAdapterProvisionForTool(name string, now time.Time) (LegacyAdapterProvision, bool) {
	p, ok := legacyAdapterProvisions[strings.TrimSpace(name)]
	if !ok || (!now.IsZero() && now.After(p.DeleteAfter)) {
		return LegacyAdapterProvision{}, false
	}
	return cloneLegacyAdapterProvision(p), true
}

// LegacyAdapterProvisionForToolAt is the explicit-time variant used by
// migration tests and offline audits. Runtime routing should use
// LegacyAdapterProvisionForTool with the trusted request clock.
func LegacyAdapterProvisionForToolAt(name string, now time.Time) (LegacyAdapterProvision, bool) {
	return LegacyAdapterProvisionForTool(name, now)
}

// LegacyAdapterProvisions returns a stable, defensive copy for diagnostics,
// migration audits and tests. It is never model-visible routing input.
func LegacyAdapterProvisions() []LegacyAdapterProvision {
	names := make([]string, 0, len(legacyAdapterProvisions))
	for name := range legacyAdapterProvisions {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]LegacyAdapterProvision, 0, len(names))
	for _, name := range names {
		out = append(out, cloneLegacyAdapterProvision(legacyAdapterProvisions[name]))
	}
	return out
}

func legacyCandidateToolNamesFromProvisions() map[string]bool {
	names := make(map[string]bool, len(legacyAdapterProvisions))
	for name := range legacyAdapterProvisions {
		names[name] = true
	}
	return names
}

func legacyAdapterFallbackAllowed(name string, now time.Time) bool {
	_, ok := LegacyAdapterProvisionForTool(name, now)
	return ok
}

func legacyAdapterCandidateAllowed(name string, now time.Time) bool {
	_, ok := LegacyAdapterProvisionForTool(name, now)
	return ok
}

// IsLegacyModelDynamicGateway reports host-implemented transport functions
// whose model arguments select a mutable provider/resource identity. They are
// intentionally not legacy model capabilities: a caller must use the managed
// semantic catalog, where the binding and contract are observed and frozen
// outside model arguments. Host-controlled entrypoints may still invoke the
// registered implementation directly.
func IsLegacyModelDynamicGateway(name string) bool {
	switch strings.TrimSpace(name) {
	case "call_mcp_tool", "manage_skill":
		return true
	default:
		return false
	}
}

// LegacyAdapterCatalogIncomplete reports whether a name belongs to the legacy
// compatibility catalog but has no live reviewed provision. It is useful to
// hosts for an explicit catalog_incomplete response rather than falling back
// to a fuzzy name match.
func LegacyAdapterCatalogIncomplete(name string, now time.Time) bool {
	name = strings.TrimSpace(name)
	if !LegacyCandidateToolNames[name] {
		return false
	}
	return !legacyAdapterFallbackAllowed(name, now)
}

func mustLegacyAdapterProvisions(entries []LegacyAdapterProvision) map[string]LegacyAdapterProvision {
	result := make(map[string]LegacyAdapterProvision, len(entries))
	for _, entry := range entries {
		entry.ToolName = strings.TrimSpace(entry.ToolName)
		if err := entry.validate(); err != nil {
			panic(err)
		}
		if _, duplicate := result[entry.ToolName]; duplicate {
			panic(fmt.Sprintf("duplicate legacy adapter provision %q", entry.ToolName))
		}
		result[entry.ToolName] = cloneLegacyAdapterProvision(entry)
	}
	return result
}

// All entries expire on the same review milestone deliberately. Adding a new
// legacy name requires an explicit owner and contract review rather than a
// casual edit to CoreToolNames.
var legacyAdapterProvisions = mustLegacyAdapterProvisions([]LegacyAdapterProvision{
	// GUI's built-in legacy surface. These entries are intentionally explicit:
	// a model-visible static definition cannot bypass the same owner/contract/
	// deletion review merely because it was added after Router selection by a
	// host policy filter. Dynamic client/MCP/skill definitions are not listed
	// here; they must migrate to a provider-bound semantic selection instead.
	{ToolName: "ask_user", Capability: "interaction.clarify", Owner: "agent-runtime", AdapterContract: "legacy-user-clarify-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "craft_tool", Capability: "workspace.automation.run", Owner: "coding-runtime", AdapterContract: "legacy-workspace-automation-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "create_template", Capability: "session.template.manage", Owner: "agent-runtime", AdapterContract: "legacy-session-template-create-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "delegate_task", Capability: "task.delegate", Owner: "agent-runtime", AdapterContract: "legacy-task-delegate-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "im_message", Capability: "message.send", Owner: "messaging-platform", AdapterContract: "legacy-im-message-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "interrupt_session", Capability: "session.control", Owner: "agent-runtime", AdapterContract: "legacy-session-interrupt-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "kill_session", Capability: "session.control", Owner: "agent-runtime", AdapterContract: "legacy-session-kill-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "launch_template", Capability: "session.template.manage", Owner: "agent-runtime", AdapterContract: "legacy-session-template-launch-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "list_mcp_tools", Capability: "catalog.inspect", Owner: "catalog-platform", AdapterContract: "legacy-mcp-catalog-inspect-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "list_providers", Capability: "provider.inspect", Owner: "agent-runtime", AdapterContract: "legacy-provider-inspect-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "list_templates", Capability: "session.template.inspect", Owner: "agent-runtime", AdapterContract: "legacy-session-template-list-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "manage_config", Capability: "config.manage", Owner: "configuration-platform", AdapterContract: "legacy-config-manage-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "manage_schedule", Capability: "schedule.manage", Owner: "scheduler-platform", AdapterContract: "legacy-schedule-manage-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "manage_template", Capability: "session.template.manage", Owner: "agent-runtime", AdapterContract: "legacy-session-template-manage-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "mis_data", Capability: "business.data.read", Owner: "business-data-platform", AdapterContract: "legacy-business-data-read-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "open", Capability: "desktop.resource.open", Owner: "desktop-platform", AdapterContract: "legacy-desktop-open-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "parallel_execute", Capability: "task.parallel.execute", Owner: "agent-runtime", AdapterContract: "legacy-task-parallel-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "project_manage", Capability: "project.manage", Owner: "workspace-platform", AdapterContract: "legacy-project-manage-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "recommend_tool", Capability: "catalog.recommend", Owner: "catalog-platform", AdapterContract: "legacy-catalog-recommend-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "screenshot", Capability: "desktop.capture", Owner: "desktop-platform", AdapterContract: "legacy-desktop-capture-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "send_file", Capability: "artifact.deliver", Owner: "messaging-platform", AdapterContract: "legacy-artifact-deliver-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "send_input", Capability: "session.input.send", Owner: "agent-runtime", AdapterContract: "legacy-session-input-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "send_to_im", Capability: "artifact.deliver", Owner: "messaging-platform", AdapterContract: "legacy-im-artifact-deliver-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "set_max_iterations", Capability: "agent.iteration.manage", Owner: "agent-runtime", AdapterContract: "legacy-agent-iteration-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "ssh", Capability: "remote.shell", Owner: "remote-runtime", AdapterContract: "legacy-remote-shell-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "web_search", Capability: "information.search.web", Owner: "web-platform", AdapterContract: "legacy-web-search-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "task", Capability: "task.control", Owner: "agent-runtime", AdapterContract: "legacy-task-control-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "async_wait", Capability: "task.wait", Owner: "agent-runtime", AdapterContract: "legacy-task-wait-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "compress_context", Capability: "context.compact", Owner: "agent-runtime", AdapterContract: "legacy-context-compact-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "list_sessions", Capability: "session.inspect", Owner: "agent-runtime", AdapterContract: "legacy-session-inspect-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "get_session_output", Capability: "session.output.read", Owner: "agent-runtime", AdapterContract: "legacy-session-output-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "get_session_events", Capability: "session.events.read", Owner: "agent-runtime", AdapterContract: "legacy-session-events-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "bash", Capability: "workspace.command.run", Owner: "coding-runtime", AdapterContract: "legacy-workspace-command-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "read_file", Capability: "workspace.file.read", Owner: "coding-runtime", AdapterContract: "legacy-workspace-read-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "read_tool_result", Capability: "tool-result.read", Owner: "agent-runtime", AdapterContract: "legacy-tool-result-read-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "FileRead", Capability: "workspace.file.read", Owner: "coding-runtime", AdapterContract: "legacy-workspace-fileread-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "ripgrep", Capability: "workspace.repo.search", Owner: "coding-runtime", AdapterContract: "legacy-workspace-search-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "Glob", Capability: "workspace.file.list", Owner: "coding-runtime", AdapterContract: "legacy-workspace-glob-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "write_file", Capability: "workspace.file.write", Owner: "coding-runtime", AdapterContract: "legacy-workspace-write-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "edit_file", Capability: "workspace.file.write", Owner: "coding-runtime", AdapterContract: "legacy-workspace-edit-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "list_directory", Capability: "workspace.file.list", Owner: "coding-runtime", AdapterContract: "legacy-workspace-list-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "memory", Capability: "memory.manage", Owner: "memory-platform", AdapterContract: "legacy-memory-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_search", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-search-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_search_facets", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-search-facets-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_context_pack", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-context-pack-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_explain", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-explain-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_image_search", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-image-search-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "current_datetime", Capability: "time.current.read", Owner: "agent-runtime", AdapterContract: "legacy-current-datetime-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "web_fetch", Capability: "web.fetch", Owner: "web-platform", AdapterContract: "legacy-web-fetch-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "download_file", Capability: "web.download", Owner: "web-platform", AdapterContract: "legacy-web-download-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "set_nickname", Capability: "profile.nickname.set", Owner: "identity-platform", AdapterContract: "legacy-profile-nickname-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "discover_tool", Capability: "catalog.discover", Owner: "catalog-platform", AdapterContract: "legacy-catalog-discover-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "goal", Capability: "task.goal.manage", Owner: "agent-runtime", AdapterContract: "legacy-goal-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "tts", Capability: "audio.speech.render", Owner: "media-platform", AdapterContract: "legacy-tts-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "asr", Capability: "audio.speech.transcribe", Owner: "media-platform", AdapterContract: "legacy-asr-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "record_audio", Capability: "audio.record", Owner: "media-platform", AdapterContract: "legacy-record-audio-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},

	// Remainder of the desktop host surface. Capability and effect declarations
	// mirror the reviewed semantic annotations at each tool's registration
	// (annotateSemanticTool); families without registration annotations carry
	// the same owner/contract review as the entries above. The closed legacy
	// replacement is fail-closed, so every model-visible static host name must
	// appear here or the whole surface is rejected as catalog_incomplete.
	{ToolName: "archive", Capability: "workspace.archive.manage", Owner: "workspace-platform", AdapterContract: "legacy-archive-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "browser", Capability: "browser.control.web", Owner: "browser-platform", AdapterContract: "legacy-browser-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "check_health", Capability: "system.health.check", Owner: "agent-runtime", AdapterContract: "legacy-check-health-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_click", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-click-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_done", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-done-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_drag", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-drag-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_find", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-find-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_focus", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-focus-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_key", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-key-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_observe", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-observe-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_playbook", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-playbook-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_scroll", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-scroll-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_scroll_into_view", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-scroll-into-view-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_select", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-select-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_type", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-type-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "computer_wait", Capability: "computer.control.desktop", Owner: "desktop-platform", AdapterContract: "legacy-computer-wait-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "edit_lines", Capability: "fs.write.local", Owner: "coding-runtime", AdapterContract: "legacy-edit-lines-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "experience_learning", Capability: "experience.learning.manage", Owner: "agent-runtime", AdapterContract: "legacy-experience-learning-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "generate_pdf", Capability: "document.generate.file", Owner: "document-platform", AdapterContract: "legacy-generate-pdf-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "git_commit", Capability: "repo.mutate.vcs", Owner: "coding-runtime", AdapterContract: "legacy-git-commit-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "git_diff", Capability: "repo.inspect.vcs", Owner: "coding-runtime", AdapterContract: "legacy-git-diff-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "git_push", Capability: "repo.mutate.vcs", Owner: "coding-runtime", AdapterContract: "legacy-git-push-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "git_status", Capability: "repo.inspect.vcs", Owner: "coding-runtime", AdapterContract: "legacy-git-status-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "group_discussion", Capability: "group.discussion.manage", Owner: "messaging-platform", AdapterContract: "legacy-group-discussion-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_click", Capability: "gui.automation.control", Owner: "desktop-platform", AdapterContract: "legacy-gui-click-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_list_displays", Capability: "gui.automation.inspect", Owner: "desktop-platform", AdapterContract: "legacy-gui-list-displays-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_list_flows", Capability: "gui.automation.inspect", Owner: "desktop-platform", AdapterContract: "legacy-gui-list-flows-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_observe", Capability: "gui.automation.inspect", Owner: "desktop-platform", AdapterContract: "legacy-gui-observe-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_record_start", Capability: "gui.automation.record", Owner: "desktop-platform", AdapterContract: "legacy-gui-record-start-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_record_stop", Capability: "gui.automation.record", Owner: "desktop-platform", AdapterContract: "legacy-gui-record-stop-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_replay", Capability: "gui.automation.control", Owner: "desktop-platform", AdapterContract: "legacy-gui-replay-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_screenshot", Capability: "desktop.capture", Owner: "desktop-platform", AdapterContract: "legacy-gui-screenshot-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_type", Capability: "gui.automation.control", Owner: "desktop-platform", AdapterContract: "legacy-gui-type-v1", Effects: []EffectClass{EffectExternalEffect}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "gui_verify", Capability: "gui.automation.inspect", Owner: "desktop-platform", AdapterContract: "legacy-gui-verify-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "import_mcp_servers", Capability: "catalog.mcp.import", Owner: "catalog-platform", AdapterContract: "legacy-import-mcp-servers-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_backfill_quality_labels", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-backfill-quality-labels-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_backfill_source_auto_labels", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-backfill-source-auto-labels-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_capabilities", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-capabilities-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_delete_source", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-delete-source-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_disable_quality_sensitive_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-disable-quality-sensitive-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_disable_sensitive_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-disable-sensitive-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_disable_source", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-disable-source-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_disable_sources_by_filter", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-disable-sources-by-filter-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_discover_urls", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-discover-urls-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_doctor", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-doctor-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_enable_source", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-enable-source-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_enable_sources_by_filter", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-enable-sources-by-filter-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_entity_profile", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-entity-profile-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_execute_quality_maintenance_plan", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-execute-quality-maintenance-plan-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_export_snapshot", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-export-snapshot-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_fact_graph", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-fact-graph-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_fact_index", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-fact-index-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_health", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-health-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_import_directory", Capability: "knowledge.ingest.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-import-directory-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_import_files", Capability: "knowledge.ingest.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-import-files-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_import_hub_share", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-import-hub-share-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_import_snapshot", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-import-snapshot-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_import_status", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-import-status-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_link_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-link-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_duplicate_cards", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-duplicate-cards-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_import_batches", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-import-batches-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_import_items", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-import-items-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_source_labels", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-source-labels-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_source_link_events", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-source-link-events-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_source_links", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-source-links-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_source_versions", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-source-versions-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_list_suppressed_cards", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-list-suppressed-cards-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_maintain", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-maintain-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_preview_source_refresh", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-preview-source-refresh-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_preview_sources_refresh", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-preview-sources-refresh-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_preview_sources_refresh_by_filter", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-preview-sources-refresh-by-filter-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_preview_topic_links", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-preview-topic-links-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_quality_maintenance_plan", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-quality-maintenance-plan-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_quality_maintenance_policies", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-quality-maintenance-policies-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_rebuild_quality_gaps", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-rebuild-quality-gaps-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_rebuild_source_derived", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-rebuild-source-derived-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_rebuild_sources_derived", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-rebuild-sources-derived-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_rebuild_sources_derived_by_filter", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-rebuild-sources-derived-by-filter-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_refresh_changed_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-refresh-changed-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_refresh_changed_sources_by_filter", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-refresh-changed-sources-by-filter-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_refresh_source", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-refresh-source-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_refresh_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-refresh-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_refresh_sources_by_filter", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-refresh-sources-by-filter-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_refresh_topic_links", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-refresh-topic-links-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_restore_suppressed_cards", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-restore-suppressed-cards-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_restore_suppressed_cards_bulk", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-restore-suppressed-cards-bulk-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_retry_import_batch", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-retry-import-batch-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_save_text", Capability: "knowledge.ingest.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-save-text-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_save_url", Capability: "knowledge.ingest.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-save-url-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_save_urls", Capability: "knowledge.ingest.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-save-urls-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_scan_sensitive", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-scan-sensitive-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_share_to_hub", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-share-to-hub-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_detail", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-detail-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_digest", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-digest-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_graph", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-graph-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_neighborhood", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-neighborhood-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_path", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-path-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_quality", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-quality-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_source_timeline", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-source-timeline-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_stats", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-stats-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_suggest", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-suggest-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_suppress_duplicate_cards", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-suppress-duplicate-cards-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_suppress_duplicate_groups", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-suppress-duplicate-groups-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_suppress_quality_duplicate_groups", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-suppress-quality-duplicate-groups-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_topic_relevance", Capability: "knowledge.read.local", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-topic-relevance-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_unlink_sources", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-unlink-sources-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_update_source_labels", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-update-source-labels-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_update_source_metadata", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-update-source-metadata-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "knowledge_url_domain_policies", Capability: "knowledge.admin.maintenance", Owner: "knowledge-platform", AdapterContract: "legacy-knowledge-url-domain-policies-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "manage_user_model", Capability: "config.manage.self", Owner: "configuration-platform", AdapterContract: "legacy-manage-user-model-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "mis_query", Capability: "business.data.read", Owner: "business-data-platform", AdapterContract: "legacy-mis-query-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "ocr_recognize", Capability: "ocr.recognize.local", Owner: "media-platform", AdapterContract: "legacy-ocr-recognize-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "office", Capability: "document.write.office", Owner: "document-platform", AdapterContract: "legacy-office-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "passthrough_task", Capability: "shell.execute.local", Owner: "coding-runtime", AdapterContract: "legacy-passthrough-task-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "query_audit_log", Capability: "security.audit.read", Owner: "security-platform", AdapterContract: "legacy-query-audit-log-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "schedule_administer", Capability: "schedule.administer.local", Owner: "scheduler-platform", AdapterContract: "legacy-schedule-administer-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "search_and_install_skill", Capability: "catalog.skill.install", Owner: "catalog-platform", AdapterContract: "legacy-search-and-install-skill-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "search_files", Capability: "fs.read.local", Owner: "coding-runtime", AdapterContract: "legacy-search-files-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "session_search", Capability: "security.audit.read", Owner: "security-platform", AdapterContract: "legacy-session-search-v1", Effects: []EffectClass{EffectReadOnly}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "switch_llm_provider", Capability: "config.manage.self", Owner: "configuration-platform", AdapterContract: "legacy-switch-llm-provider-v1", Effects: []EffectClass{EffectSensitive}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "tts_local", Capability: "audio.synthesize.local", Owner: "media-platform", AdapterContract: "legacy-tts-local-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
	{ToolName: "tts_render", Capability: "audio.render.speech", Owner: "media-platform", AdapterContract: "legacy-tts-render-v1", Effects: []EffectClass{EffectLocalMutation}, DeleteAfter: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
})

func init() {
	for name := range LegacyCandidateToolNames {
		if _, ok := legacyAdapterProvisions[name]; !ok {
			panic(fmt.Sprintf("legacy candidate %q has no reviewed adapter provision", name))
		}
	}
	for name := range legacyAdapterProvisions {
		if !LegacyCandidateToolNames[name] {
			panic(fmt.Sprintf("legacy adapter provision %q is not in the candidate catalog", name))
		}
	}
}
