package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// imSemanticPolicyStateBlocked is the GUI workflow state in which a phase is
// waiting on user confirmation and every capability with a side effect is
// blocked. It is a policy state, not a tool name.
const imSemanticPolicyStateBlocked = "blocked"

// imSemanticCapabilityPolicyAdapter is the GUI equivalent of the reviewed
// Core Agent capability-policy adapter. It projects only the trusted workflow
// tool-filter state into capability-level constraints for the families
// migrated in the S2a/S2b1/S2b2 slices. Rules may select solely on workflow
// state and may only tighten to deny; they never name a tool, adapter,
// provider, or legacy ToolNames entry.
//
// The mapping mirrors the legacy per-phase allowlists at capability level:
//   - doc_only phases still permit the office document outcome (the legacy
//     DocOnlyAllowedTools set contains the office entry), the local shell
//     outcome (the same set carries bash for document parsing helpers), the
//     OS-handler launch outcome (the set carries open), the agent memory
//     outcome (the set carries memory) and the IM send outcome (the set
//     carries send_file/send_to_im/set_nickname; im_message is absent from
//     the legacy set, a name-level distinction intentionally collapsed at
//     capability granularity, matching the S2b1 read-only collapse). They
//     deny business data, knowledge ingest, knowledge admin, local file
//     writes, microphone capture, remote downloads, self-configuration, task
//     tracking, goal management, session templates, coding-session
//     administration, sub-task delegation and every external-effect family
//     except message.send.im (none of manage_config/task/goal/
//     manage_template/list_sessions/delegate_task/download_file appears in
//     the legacy doc-only set).
//   - planning and ops_controlled phases deny all sensitive/external
//     migrated families. Legacy planning permits write_file, memory and open,
//     and legacy ops_controlled permits bash/ssh; capability granularity
//     spans those distinctions, and an ops-controlled phase cannot
//     pre-approve a capability-planned effect in this slice, so denial is
//     the conservative projection in both states.
//   - a blocked phase (waiting on confirmation) denies all sensitive and
//     external families.
//
// The read-only families migrated in S2b1 (fs.read.local, repo.inspect.vcs,
// information.fetch.web, audio.transcribe.speech, security.audit.read,
// knowledge.read.local) and the catalog-only read-only S2b2 entries
// (interaction.ask.user, governance.inspect.experience) carry no constraints
// in any state, matching the S2a treatment of read-only families: a
// side-effect-free selection cannot widen a restricted workflow's mutation
// surface. The legacy allowlists do split individual read-only tool names
// (for example search_files is absent from the doc-only set); that
// name-level distinction is intentionally collapsed at capability
// granularity here.
//
// Constraints for the catalog-only external-effect families are inert today:
// no intent rule maps their labels to a need, so no selection can be denied.
// They are declared now so enabling a family later cannot silently widen a
// restricted workflow.
func imSemanticCapabilityPolicyAdapter() agentservice.StaticCapabilityPolicyAdapter {
	deny := func(state string, capabilities ...tool.CapabilityID) agentservice.CapabilityPolicyRule {
		constraints := make([]tool.RoutingConstraint, 0, len(capabilities))
		for _, capability := range capabilities {
			constraints = append(constraints, tool.RoutingConstraint{
				ID:         "policy:" + state + ":deny-" + string(capability),
				Capability: capability,
				Effect:     "deny",
				Authority:  tool.AuthorityPolicy,
			})
		}
		return agentservice.CapabilityPolicyRule{WorkflowPolicy: state, Constraints: constraints}
	}
	sensitive := []tool.CapabilityID{
		tool.CapabilityDocumentWriteOffice,
		tool.CapabilityBusinessDataMIS,
		tool.CapabilityKnowledgeIngestLocal,
		tool.CapabilityFSWriteLocal,
		tool.CapabilityShellExecuteLocal,
		tool.CapabilityAudioCaptureMicrophone,
		// S2b2 managed administration families.
		tool.CapabilitySystemLaunchLocal,
		tool.CapabilityArtifactAcquireRemote,
		tool.CapabilityConfigManageSelf,
		tool.CapabilityMemoryManageAgent,
		tool.CapabilityTaskTrackLocal,
		tool.CapabilityGoalManageLongRunning,
		tool.CapabilityTemplateManageSession,
		tool.CapabilitySessionManageCoding,
		tool.CapabilityAgentDelegateSubtask,
		tool.CapabilityKnowledgeAdminMaintenance,
		tool.CapabilityScheduleAdministerLocal,
		tool.CapabilityAudioSynthesizeLocal,
		tool.CapabilityAudioRenderSpeech,
	}
	external := []tool.CapabilityID{
		tool.CapabilityShellExecuteRemoteHost,
		tool.CapabilityBrowserControlWeb,
		tool.CapabilityComputerControlDesktop,
		// S2b2 catalog-only external families plus schedule dispatch
		// (receipt-aware, still denied in restricted workflow states).
		tool.CapabilityScheduleManageLocal,
		tool.CapabilityScheduleDispatchChannel,
		tool.CapabilityAudioSynthesizeSpeech,
		tool.CapabilityMessageSendIM,
		tool.CapabilityRepoMutateVCS,
	}
	all := append(append([]tool.CapabilityID(nil), sensitive...), external...)
	return agentservice.StaticCapabilityPolicyAdapter{Rules: []agentservice.CapabilityPolicyRule{
		deny(string(v2.ToolPolicyDocOnly), tool.CapabilityBusinessDataMIS, tool.CapabilityKnowledgeIngestLocal,
			tool.CapabilityFSWriteLocal, tool.CapabilityAudioCaptureMicrophone,
			tool.CapabilityArtifactAcquireRemote, tool.CapabilityConfigManageSelf,
			tool.CapabilityTaskTrackLocal, tool.CapabilityGoalManageLongRunning,
			tool.CapabilityTemplateManageSession, tool.CapabilitySessionManageCoding,
			tool.CapabilityAgentDelegateSubtask, tool.CapabilityKnowledgeAdminMaintenance,
			tool.CapabilityScheduleAdministerLocal, tool.CapabilityAudioSynthesizeLocal, tool.CapabilityAudioRenderSpeech,
			tool.CapabilityShellExecuteRemoteHost, tool.CapabilityBrowserControlWeb, tool.CapabilityComputerControlDesktop,
			tool.CapabilityScheduleManageLocal, tool.CapabilityScheduleDispatchChannel, tool.CapabilityAudioSynthesizeSpeech, tool.CapabilityRepoMutateVCS),
		deny(string(v2.ToolPolicyPlanning), all...),
		deny(string(v2.ToolPolicyOpsControlled), all...),
		deny(imSemanticPolicyStateBlocked, all...),
	}}
}

// imSemanticWorkflowPolicyState projects the trusted GUI workflow tool-filter
// decision onto the policy-state vocabulary consumed by
// imSemanticCapabilityPolicyAdapter. An applied ToolFilterNone decision means
// the active phase is blocked on confirmation, which is a restriction, not an
// unrestricted state. An empty result means no workflow restriction applies.
func imSemanticWorkflowPolicyState(policy v2.ToolFilterPolicy, apply bool) string {
	if !apply {
		return ""
	}
	switch policy {
	case v2.ToolFilterNone:
		return imSemanticPolicyStateBlocked
	case v2.ToolFilterFull:
		return ""
	default:
		return string(policy)
	}
}

// semanticCapabilityPolicyConstraints derives the capability-level routing
// constraints for one semantic planning request. The workflow state is read
// from the trusted host workflow engines for this session owner; user text,
// model output and legacy tool names never enter this projection. The
// execution-time legacy workflow gate still applies unchanged underneath, so
// a planning-context approximation can only tighten a turn, never widen it.
func (h *IMMessageHandler) semanticCapabilityPolicyConstraints(userID string) ([]tool.RoutingConstraint, error) {
	if h == nil {
		return nil, nil
	}
	_, policy, apply := h.workflowToolFilterOwnerPolicyAndDecision(userID, nil)
	state := imSemanticWorkflowPolicyState(policy, apply)
	var constraints []tool.RoutingConstraint
	if state != "" {
		_, workflowConstraints, err := imSemanticCapabilityPolicyAdapter().DynamicCapabilityConstraints(
			agentservice.DynamicCapabilityNeedRequest{WorkflowPolicy: state})
		if err != nil {
			return nil, err
		}
		constraints = append(constraints, workflowConstraints...)
	}
	expertConstraints, err := expertCapabilityPolicyConstraintsForUser(userID)
	if err != nil {
		return nil, err
	}
	constraints = append(constraints, expertConstraints...)
	if len(constraints) == 0 {
		return nil, nil
	}
	return constraints, nil
}
