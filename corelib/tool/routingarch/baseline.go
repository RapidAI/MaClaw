package routingarch

// This file is the reviewed allowlist for every boundary site the scanner
// finds. It is the machine-verifiable form of design section 8 item 11: each
// remaining legacy site has a recorded reason and a deletion condition, and a
// new site cannot appear without a reviewer adding it here.
//
// The accompanying test fails in both directions. A finding without an entry
// is an unreviewed boundary crossing. An entry without a finding is stale and
// must be deleted, so the inventory shrinks as families migrate instead of
// silently keeping obsolete permissions.

// Reason explains why a site is allowed and what removes it.
type Reason string

const (
	// ReasonSemanticOwner marks the layer that owns the boundary. The design
	// names exactly these owners: the catalog/planner, the renderer, the
	// executor, the invocation issuer and the artifact broker.
	ReasonSemanticOwner Reason = "owning layer of this boundary; the design assigns the mechanism here"

	// ReasonTrustedFactProducer marks a reviewed producer of typed facts and
	// constraints. Host attachment publication and the capability-policy
	// adapters are the only ways an authorization, confirmation, artifact or
	// provider-health claim may enter planning.
	ReasonTrustedFactProducer Reason = "reviewed trusted fact/constraint producer (design 6, C-6)"

	// ReasonChannelTransportProjection marks a channel gateway that holds
	// artifact bytes purely as transport projection. It never authors an
	// ArtifactRef identity; the broker does. Removed when every gateway
	// consumes payloads exclusively through a projected access grant.
	ReasonChannelTransportProjection Reason = "channel gateway transport projection, not artifact provenance (design 9.4)"

	// ReasonLegacyNameRouter marks the legacy keyword/name routing surface.
	// A capability-managed turn no longer reaches it. Deleted by design C-2
	// once the remaining unmapped families (coding, bug_fix, maintenance,
	// workflow_task) and the generic Q&A surface are migrated.
	ReasonLegacyNameRouter Reason = "legacy name router, unmanaged turns only; delete with design C-2"

	// ReasonLegacyPolicyFilter marks policy narrowing applied to the legacy
	// tool slice. A managed turn is narrowed by capability constraints in the
	// planner instead. Deleted together with the legacy surface it filters.
	ReasonLegacyPolicyFilter Reason = "policy narrowing over the legacy tool slice; delete with the surface it filters"

	// ReasonProviderNameCallLegacy marks a Skill/MCP call that still goes
	// through a discovery name. Deleted when that entry point routes through
	// an immutable binding.
	ReasonProviderNameCallLegacy Reason = "name-based Skill/MCP call outside the semantic execution adapter; migrate to an immutable binding"

	// ReasonProviderNameTransport marks a call below the point where the
	// provider was chosen: an owner-defaulting delegation inside one type, or
	// the transport a revalidated binding uses to reach the server it just
	// proved. Deleted only if the transport itself starts taking a binding.
	ReasonProviderNameTransport Reason = "transport beneath a resolved binding or an owner-defaulting delegation, not a selection point"

	// ReasonHumanDirectedInvocation marks a provider named by a person in the
	// desktop UI. The rule constrains what a model may select, so a human
	// running a named skill is not the crossing it guards. Where a model
	// started the run, that model-facing site is listed on its own.
	ReasonHumanDirectedInvocation Reason = "a person named the provider in the desktop UI, not a model tool call"

	// ReasonInstalledDefinitionStep marks a provider named by an installed
	// skill definition's steps or by a configured app workflow. The model does
	// not write the name at call time, but a model-authored skill can still
	// carry one. Deleted when definition steps resolve to bindings at install
	// time instead of at each run.
	ReasonInstalledDefinitionStep Reason = "provider named by an installed skill definition step or configured app workflow; bind these at install time"

	// ReasonCLICommandDispatch marks a CLI subcommand dispatcher whose name
	// collides with a provider-call selector. maclaw-tui's RunSkill routes
	// list/search/install/remove and executes no skill. It stays listed so the
	// selector keeps its width.
	ReasonCLICommandDispatch Reason = "CLI subcommand dispatcher whose name collides with a provider-call selector; executes no provider"

	// ReasonControlPlaneProbe marks lifecycle/discovery code that reaches a
	// provider by name outside any user task. The design keeps provider
	// lifecycle on the control plane, so this is not an Agent execution path.
	ReasonControlPlaneProbe Reason = "control-plane lifecycle/discovery probe, not an Agent execution path (design 12.13)"

	// ReasonDiscoveryListing marks a name/query filter over a discovery
	// inventory rendered to a human, not over the model tool surface.
	ReasonDiscoveryListing Reason = "filters a discovery inventory for display, not the model tool surface"

	// ReasonNameMatchOnly marks a site that matches a detector prefix but
	// does not touch a tool set. It stays listed so the detector keeps its
	// width instead of being narrowed until it stops catching real cases.
	ReasonNameMatchOnly Reason = "matches the detector prefix but modifies no tool set"

	// ReasonZeroValueSentinel marks a composite literal the literal detector
	// recorded only because the symbol appears in an empty failure return
	// (for example `return InvocationGrant{}, err`). No fact, grant or
	// artifact identity is authored at the site. It stays listed so the
	// literal detector keeps its width; deleted only if the sentinel return
	// is rewritten to a pointer or nil form.
	ReasonZeroValueSentinel Reason = "zero-value sentinel in an error/failure return, not an authored fact/grant/ref; listed so the literal detector keeps its width"
)

// Baseline maps a rule to the reviewed sites for that rule, keyed by
// Finding.Key (relative file path plus symbol).
var Baseline = map[Rule]map[string]Reason{
	RuleArtifactRefAuthoring: {
		"corelib/tool/semantic_artifact_store.go:ArtifactRef":     ReasonSemanticOwner,
		"corelib/tool/semantic_artifact_store.go:ArtifactPayload": ReasonSemanticOwner,
		"corelib/tool/semantic_planner.go:ArtifactRef":            ReasonSemanticOwner,
		"corelib/tool/semantic_route_state_store.go:ArtifactRef":  ReasonSemanticOwner,
		"guiapp/semantic_artifacts.go:ArtifactRef":                ReasonSemanticOwner,
		"guiapp/semantic_artifacts.go:ArtifactPayload":            ReasonSemanticOwner,
		"guiapp/im_agent_loop_shared.go:ArtifactPayload":          ReasonChannelTransportProjection,
		"guiapp/lansenger_gateway.go:ArtifactPayload":             ReasonChannelTransportProjection,
		"guiapp/weixin_gateway.go:ArtifactPayload":                ReasonChannelTransportProjection,
		// Empty sentinel on the not-found path of the repeat-family artifact
		// lookup; no artifact identity is authored at the site.
		"corelib/tool/semantic_artifact_projection.go:ArtifactRef": ReasonZeroValueSentinel,
	},
	RuleInvocationGrantMint: {
		"corelib/tool/semantic_invocation.go:InvocationGrant":                                 ReasonSemanticOwner,
		"corelib/tool/semantic_invocation.go:NewInvocationIssuerWithStore":                    ReasonSemanticOwner,
		"corelib/tool/semantic_invocation.go:NewRandomInvocationIssuerWithStore":              ReasonSemanticOwner,
		"corelib/agentservice/dynamic_semantic_routing_store.go:NewInvocationIssuerWithStore": ReasonSemanticOwner,
		"guiapp/semantic_invocation_store.go:NewInvocationIssuerWithStore":                    ReasonSemanticOwner,
		"guiapp/semantic_invocation_store.go:NewRandomInvocationIssuer":                       ReasonSemanticOwner,
		// The SQLite execution coordinator mints surface-ready grants inside
		// the render/issue transaction; the issuer signs and persists them.
		"corelib/tool/semantic_execution_coordinator.go:InvocationGrant": ReasonSemanticOwner,
		// Empty sentinels on failure paths; no grant is authored at the site.
		"corelib/tool/semantic_model_request_surface.go:InvocationGrant": ReasonZeroValueSentinel,
		"corelib/tool/semantic_repeat.go:InvocationGrant":                ReasonZeroValueSentinel,
		"guiapp/coding_durable_dynamic_surface.go:InvocationGrant":       ReasonZeroValueSentinel,
		"guiapp/im_agent_loop_shared.go:InvocationGrant":                 ReasonZeroValueSentinel,
	},
	RuleProviderNameCall: {
		"corelib/agentservice/mcp_integration.go:CallTool": ReasonSemanticOwner,
		"guiapp/local_mcp_manager.go:CallTool":             ReasonControlPlaneProbe,
		"guiapp/mcp_auto_discovery.go:CallTool":            ReasonControlPlaneProbe,

		// The model-facing name gateways. Every entry here is a place a model
		// can still choose a Skill or an MCP tool by writing its name.
		"guiapp/tool_registry_builtin.go:toolCallMCPTool":   ReasonProviderNameCallLegacy,
		"guiapp/tool_registry_builtin.go:toolRunSkill":      ReasonProviderNameCallLegacy,
		"guiapp/im_tool_manage_skill.go:toolRunSkill":       ReasonProviderNameCallLegacy,
		"guiapp/im_tool_skill_run.go:StartRunForOwner":      ReasonProviderNameCallLegacy,
		"guiapp/im_tools_misc.go:CallToolForOwner":          ReasonProviderNameCallLegacy,
		"guiapp/coding_subagent_mcp.go:toolCallMCPTool":     ReasonProviderNameCallLegacy,
		"tui/tool_manage_skill.go:skillRunDetailed":         ReasonProviderNameCallLegacy,
		"tui/tool_manage_skill.go:skillRunPipelineDetailed": ReasonProviderNameCallLegacy,

		"guiapp/app_nl_mcp.go:CallToolForOwner":                 ReasonProviderNameTransport,
		"guiapp/local_mcp_manager.go:CallToolForOwner":          ReasonProviderNameTransport,
		"guiapp/semantic_dynamic_providers.go:CallToolForOwner": ReasonProviderNameTransport,
		"guiapp/app_nl_skills.go:ExecuteWithArgs":               ReasonProviderNameTransport,
		"guiapp/app_nl_skills.go:executeSkillByNameDetailed":    ReasonProviderNameTransport,
		"guiapp/skill_runner.go:StartRunForOwner":               ReasonProviderNameTransport,

		"guiapp/agent_view_mcp.go:toolCallMCPTool":    ReasonHumanDirectedInvocation,
		"guiapp/agent_view_skill.go:StartRunForOwner": ReasonHumanDirectedInvocation,
		"guiapp/app_nl_skills.go:StartRunForOwner":    ReasonHumanDirectedInvocation,

		"corelib/skill/pipeline.go:RunSubSkill":                        ReasonInstalledDefinitionStep,
		"guiapp/app_maclaw_app_approval.go:executeSkillByNameDetailed": ReasonInstalledDefinitionStep,
		"guiapp/app_nl_skills.go:CallToolForOwner":                     ReasonInstalledDefinitionStep,
		"guiapp/skill_runner.go:CallToolForOwner":                      ReasonInstalledDefinitionStep,

		"tui/main.go:RunSkill":                 ReasonCLICommandDispatch,
		"tui/commands/run_capture.go:RunSkill": ReasonCLICommandDispatch,

		// agentservice CoreAgent's manage_skill(action="run") handler: the
		// model writes the skill name, the same shape as the gateways above.
		"corelib/agentservice/skill_integration.go:RunSkill": ReasonProviderNameCallLegacy,
	},
	RuleRoutingFactAuthoring: {
		"corelib/agentservice/dynamic_host_docread.go:RoutingFact":                ReasonTrustedFactProducer,
		"corelib/agentservice/reviewed_dynamic_capabilities.go:RoutingConstraint": ReasonTrustedFactProducer,
		"guiapp/expert_capability_policy.go:RoutingConstraint":                    ReasonTrustedFactProducer,
		"guiapp/semantic_capability_policy.go:RoutingConstraint":                  ReasonTrustedFactProducer,
		"guiapp/semantic_audio_transcribe.go:RoutingFact":                         ReasonTrustedFactProducer,
		"guiapp/semantic_tool_routing.go:RoutingFact":                             ReasonTrustedFactProducer,
		"guiapp/semantic_tool_routing.go:RoutingConstraint":                       ReasonTrustedFactProducer,
		// Phase-1 permission snapshot -> planner bridge: reviewed policy
		// decisions enter planning as AuthorityPolicy constraints.
		"corelib/agentservice/permission_planner_bridge.go:RoutingConstraint": ReasonTrustedFactProducer,
		// Coding static catalogs record the host posture (no mutation provider
		// on inquiry/operational turns) as explicit planner deny constraints.
		"guiapp/coding_static_catalog.go:RoutingConstraint":        ReasonTrustedFactProducer,
		"guiapp/coding_static_remote_catalog.go:RoutingConstraint": ReasonTrustedFactProducer,
	},
	RuleToolSurfaceMutation: {
		"guiapp/im_handler_wiring.go:routeTools":                 ReasonLegacyNameRouter,
		"guiapp/im_handler_wiring.go:routeToolsForUser":          ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_tool_augment.go:routeToolsForUser": ReasonLegacyNameRouter,

		"guiapp/im_agent_loop_tool_augment.go:augmentToolsFromInjection":   ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_tool_augment.go:augmentToolsFromSessionPins": ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_round_prep.go:augmentToolsFromInjection":     ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_round_prep.go:augmentToolsFromSessionPins":   ReasonLegacyNameRouter,

		"guiapp/im_agent_loop_recovery.go:restoreToolsAfterSkillRecover":     ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_tool_restore.go:restoreToolsAfterSkillRecover": ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_shared.go:removeToolDefinitionByName":          ReasonLegacyNameRouter,
		"guiapp/im_agent_loop_tools.go:ensureToolResultReader":               ReasonLegacyNameRouter,

		"guiapp/im_execution_profile.go:filterToolsForExecutionProfile":                 ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tools.go:filterToolsForExecutionProfile":                  ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tool_exec.go:filterToolsForExecutionProfile":              ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tools.go:filterToolsForHardwareAutoSpeech":                ReasonLegacyPolicyFilter,
		"guiapp/expert_session_policy.go:filterToolsForExpert":                          ReasonLegacyPolicyFilter,
		"guiapp/expert_session_policy.go:filterToolsForExpertUser":                      ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tools.go:filterToolsForExpertUser":                        ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tool_augment.go:filterToolsForExpertUser":                 ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tool_exec.go:filterToolsForExpertUser":                    ReasonLegacyPolicyFilter,
		"guiapp/lansenger_group_permissions.go:filterToolsForLansengerGroupPermissions": ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tools.go:filterToolsForLansengerGroupPermissions":         ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tool_augment.go:filterToolsForLansengerGroupPermissions":  ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tool_exec.go:filterToolsForLansengerGroupPermissions":     ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tool_restore.go:filterToolsForLansengerGroupPermissions":  ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_truncation.go:filterToolsForLansengerGroupPermissions":    ReasonLegacyPolicyFilter,
		"guiapp/ve_tool_policy.go:filterToolsForVE":                                     ReasonLegacyPolicyFilter,
		"guiapp/ve_tool_policy.go:filterToolsForVEWithConfig":                           ReasonLegacyPolicyFilter,
		"guiapp/app_ve_handler.go:filterToolsForVEWithConfig":                           ReasonLegacyPolicyFilter,
		"guiapp/im_skill_preference.go:filterToolsForSkillPreference":                   ReasonLegacyPolicyFilter,
		"guiapp/im_skill_preference.go:filterToolsForRemoteSkillSearch":                 ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tools.go:filterToolsForSkillPreference":                   ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_tools.go:filterToolsForRemoteSkillSearch":                 ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_truncation.go:filterToolsForSkillPreference":              ReasonLegacyPolicyFilter,
		"guiapp/im_agent_loop_truncation.go:filterToolsForRemoteSkillSearch":            ReasonLegacyPolicyFilter,
		"guiapp/coding_subagent.go:filterToolsByHorizonSurface":                         ReasonLegacyPolicyFilter,
		// Agent-guided workflow surface suppression; same policy-narrowing
		// class as the skill-preference filters above.
		"guiapp/im_skill_preference.go:filterToolsForAgentGuidedWorkflow": ReasonLegacyPolicyFilter,
		// Leftover pin union re-applies the expert/lansenger allow-lists to
		// the post-pin surface in the IM wiring.
		"guiapp/im_handler_wiring.go:filterToolsForExpertUser":                ReasonLegacyPolicyFilter,
		"guiapp/im_handler_wiring.go:filterToolsForLansengerGroupPermissions": ReasonLegacyPolicyFilter,

		"corelib/mcp/filter.go:FilterTools":   ReasonDiscoveryListing,
		"guiapp/im_tools_misc.go:FilterTools": ReasonDiscoveryListing,

		"guiapp/tool_cache_maintenance.go:ensureToolCachePath": ReasonNameMatchOnly,
		"guiapp/openhuman_wiring.go:injectToolMemoryHint":      ReasonNameMatchOnly,
	},
}
