package tool

import "fmt"

// BuiltinCapabilityOntologyVersion identifies the first reviewed version of
// the builtin capability ontology. A semantic contract change requires a new
// registry version, never a silent mutation of an in-use ID.
const BuiltinCapabilityOntologyVersion = "builtin-capability-ontology-v1"

// Builtin capability IDs use a stable "verb outcome + domain object" naming.
// Tool names, brand names, protocol names and UI copy must never enter an ID:
// one capability describes an outcome and may be provided by several builtin,
// Skill, or MCP implementations.
const (
	CapabilityShellExecuteLocal       CapabilityID = "shell.execute.local"
	CapabilityShellExecuteRemoteHost  CapabilityID = "shell.execute.remote_host"
	CapabilityBuildVerifyLocal        CapabilityID = "build.verify.local"
	CapabilityFSReadLocal             CapabilityID = "fs.read.local"
	CapabilityFSWriteLocal            CapabilityID = "fs.write.local"
	CapabilitySystemLaunchLocal       CapabilityID = "system.launch.local"
	CapabilityRepoInspectVCS          CapabilityID = "repo.inspect.vcs"
	CapabilityRepoMutateVCS           CapabilityID = "repo.mutate.vcs"
	CapabilityDocumentWriteOffice     CapabilityID = "document.write.office"
	CapabilityDocumentRenderPDF       CapabilityID = "document.render.pdf"
	CapabilityInformationFetchWeb     CapabilityID = "information.fetch.web"
	CapabilityArtifactAcquireRemote   CapabilityID = "artifact.acquire.remote"
	CapabilityComputerControlDesktop  CapabilityID = "computer.control.desktop"
	CapabilityBrowserControlWeb       CapabilityID = "browser.control.web"
	CapabilityAudioCaptureMicrophone  CapabilityID = "audio.capture.microphone"
	CapabilityAudioSynthesizeSpeech   CapabilityID = "audio.synthesize.speech"
	CapabilityAudioSynthesizeLocal    CapabilityID = "audio.synthesize.local"
	CapabilityAudioRenderSpeech       CapabilityID = "audio.render.speech"
	CapabilityAudioTranscribeSpeech   CapabilityID = "audio.transcribe.speech"
	CapabilityMessageSendIM           CapabilityID = "message.send.im"
	CapabilityScheduleManageLocal     CapabilityID = "schedule.manage.local"
	CapabilityScheduleAdministerLocal CapabilityID = "schedule.administer.local"
	CapabilityScheduleDispatchChannel CapabilityID = "schedule.dispatch.channel"
	CapabilitySessionManageCoding     CapabilityID = "session.manage.coding"
	CapabilityAgentDelegateSubtask    CapabilityID = "agent.delegate.subtask"
	CapabilityTaskTrackLocal          CapabilityID = "task.track.local"
	CapabilityGoalManageLongRunning   CapabilityID = "goal.manage.longrunning"
	CapabilityMemoryManageAgent       CapabilityID = "memory.manage.agent"
	// CapabilityMemoryRecallAgent is the query half of agent memory.
	//
	// It is split from CapabilityMemoryManageAgent because ambient, light,
	// /btw, and VE only need recall; granting manage also grants save/delete,
	// and light close drops the Sensitive manage adapter.
	CapabilityMemoryRecallAgent         CapabilityID = "memory.recall.agent"
	CapabilityKnowledgeReadLocal        CapabilityID = "knowledge.read.local"
	CapabilityKnowledgeIngestLocal      CapabilityID = "knowledge.ingest.local"
	CapabilityKnowledgeAdminMaintenance CapabilityID = "knowledge.admin.maintenance"
	CapabilityConfigManageSelf          CapabilityID = "config.manage.self"
	CapabilitySecurityAuditRead         CapabilityID = "security.audit.read"
	CapabilityTemplateManageSession     CapabilityID = "template.manage.session"
	// CapabilityBusinessDataRead is the query half of the MIS integration.
	//
	// It is split from CapabilityBusinessDataMIS because one capability
	// covering both meant a turn that only needed to look something up had to
	// be granted the one that can also change it, and the restricted workflow
	// states that deny mutation denied the lookup with it — a planning turn
	// could not read the data it was planning against.
	CapabilityBusinessDataRead     CapabilityID = "business.data.read"
	CapabilityBusinessDataMIS      CapabilityID = "business.data.mis"
	CapabilityInteractionAskUser   CapabilityID = "interaction.ask.user"
	CapabilityGovernanceInspectExp CapabilityID = "governance.inspect.experience"
)

// builtinCapabilityOntologyOwner is the reviewed owner of every builtin
// ontology entry. Product-domain registries (for example the IM registry)
// keep their own owner and take precedence on duplicate IDs.
const builtinCapabilityOntologyOwner = "core"

// BuiltinCapabilityOntology returns the reviewed, versioned capability
// vocabulary for the builtin tool inventory. The control-plane capabilities
// behind the skill and MCP compatibility gateways (skill.lifecycle.manage,
// mcp.lifecycle.manage) are deliberately absent: CapabilityDescriptor has no
// control-plane marker, and that state is governed per provider through
// ProviderSpec.Classification instead of a plannable capability.
//
// Qualifiers are restrained on purpose: only families with a genuine
// sub-capability distinction declare one. An empty Values set permits any
// non-empty normalised value.
func BuiltinCapabilityOntology() []CapabilityDescriptor {
	return []CapabilityDescriptor{
		{
			ID: CapabilityShellExecuteLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Execute a local shell command or passthrough task on the host.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityShellExecuteRemoteHost, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Execute a command on a remote host over an authenticated session.",
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			// Distinct from shell.execute.local on purpose. This capability
			// names a reviewed task and lets the host decide the command line,
			// so a plan can grant build/test/lint verification without also
			// granting the arbitrary local execution that would carry file and
			// repository mutation along with it.
			ID: CapabilityBuildVerifyLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Run a reviewed build, test, or lint task in the bound workspace.",
			Qualifiers: map[string]QualifierConstraint{
				"task": {Values: []string{"build", "test", "lint", "format_check"}},
			},
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityFSReadLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Read or search local filesystem content without changing it.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityFSWriteLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Create or modify local filesystem content.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilitySystemLaunchLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Open a local file, application, or URL with the system handler.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityRepoInspectVCS, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Inspect version-control status and diffs without mutating the repository.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityRepoMutateVCS, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Mutate version-control history or remotes, such as commit or push.",
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilityDocumentWriteOffice, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Create or modify an office document.",
			Qualifiers: map[string]QualifierConstraint{
				"format": {Values: []string{"spreadsheet", "word", "presentation"}},
			},
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityDocumentRenderPDF, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Render content into a PDF document artifact.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityInformationFetchWeb, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Fetch the content of one approved web resource.",
			Qualifiers: map[string]QualifierConstraint{
				"domain": {},
			},
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityArtifactAcquireRemote, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Download a remote resource into a local artifact.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityComputerControlDesktop, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Observe or drive the local desktop through the merged computer-use entry.",
			Qualifiers: map[string]QualifierConstraint{
				"input": {Values: []string{"screenshot", "mouse", "keyboard", "window"}},
			},
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilityBrowserControlWeb, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Drive a web browser session through the merged browser entry.",
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilityAudioCaptureMicrophone, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Record audio from the local microphone.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityAudioSynthesizeSpeech, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Synthesize speech audio from text and hand it to a channel playback path.",
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilityAudioSynthesizeLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Synthesize speech from text and play it on the local desktop or TUI host without channel delivery.",
			Effects: []EffectClass{EffectLocalMutation},
		},
		{
			ID: CapabilityAudioRenderSpeech, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary:  "Render text into a speech audio artifact without playing it or sending it.",
			Effects:  []EffectClass{EffectLocalMutation},
			Produces: []ArtifactContract{{Kind: "audio", MIMEType: "audio/wav", Required: true}},
		},
		{
			ID: CapabilityAudioTranscribeSpeech, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Transcribe speech audio into text without side effects.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityMessageSendIM, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Send a message, file, or profile update to an IM conversation.",
			Qualifiers: map[string]QualifierConstraint{
				"format": {Values: []string{"text", "file", "image"}},
			},
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilityScheduleManageLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Create, update, or cancel scheduled local jobs that may later deliver to a channel.",
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilityScheduleAdministerLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "List, create, update, or delete local scheduled jobs without binding channel delivery.",
			Effects: []EffectClass{EffectLocalMutation},
		},
		{
			ID: CapabilityScheduleDispatchChannel, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Register a due-time channel delivery intent for a trusted destination; fire remains receipt-bound.",
			Effects: []EffectClass{EffectExternalEffect},
		},
		{
			ID: CapabilitySessionManageCoding, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "List, drive, interrupt, or stop external coding sessions and providers.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityAgentDelegateSubtask, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Delegate a subtask to another agent, group, or coding worker.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityTaskTrackLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Track local task list state for the current work.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityGoalManageLongRunning, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Manage long-running goal state for the current session.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityMemoryManageAgent, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Read or update the agent memory store.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityMemoryRecallAgent, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Recall agent memory without changing the store.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityKnowledgeReadLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Search local knowledge without changing the store.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityKnowledgeIngestLocal, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Ingest or import content into the local knowledge store.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityKnowledgeAdminMaintenance, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Administer or maintain the local knowledge store.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityConfigManageSelf, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Change the agent's own configuration, provider, or runtime limits.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilitySecurityAuditRead, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Read audit logs, session history, or health state without side effects.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityTemplateManageSession, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Manage session templates.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityBusinessDataRead, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Query business data through the MIS integration without changing it.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityBusinessDataMIS, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Operate business data through the MIS integration.",
			Effects: []EffectClass{EffectSensitive},
		},
		{
			ID: CapabilityInteractionAskUser, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Ask the user a clarifying question and read the answer.",
			Effects: []EffectClass{EffectReadOnly},
		},
		{
			ID: CapabilityGovernanceInspectExp, Version: "v1", Owner: builtinCapabilityOntologyOwner,
			Summary: "Inspect reviewed experience-learning records.",
			Effects: []EffectClass{EffectReadOnly},
		},
	}
}

// RegisterBuiltinCapabilityOntology registers the builtin ontology into an
// unsealed registry. Registration is additive and idempotent: an ID that is
// already present (for example a product-domain descriptor registered before
// this call) is skipped unchanged, so host-reviewed vocabularies always take
// precedence over the builtin defaults. The caller remains responsible for
// sealing the registry before it is used to validate provider contracts.
func RegisterBuiltinCapabilityOntology(registry *CapabilityRegistry) error {
	if registry == nil {
		return fmt.Errorf("nil capability registry")
	}
	if registry.Sealed() {
		return fmt.Errorf("capability registry %q is sealed", registry.Version())
	}
	for _, descriptor := range BuiltinCapabilityOntology() {
		if _, exists := registry.Lookup(descriptor.ID); exists {
			continue
		}
		if err := registry.Register(descriptor); err != nil {
			return fmt.Errorf("register builtin capability %q: %w", descriptor.ID, err)
		}
	}
	return nil
}
