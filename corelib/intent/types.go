// Package intent provides a unified intent classification service for user messages.
// It replaces scattered keyword-based classification modules with semantic
// embedding and LLM classification. Keyword registries may still support
// diagnostics or candidate recall, but they are not an execution-route authority.
package intent

import "context"

// IntentLabel represents a classified user intent.
type IntentLabel string

const (
	LabelCoding    IntentLabel = "coding"
	LabelSSH       IntentLabel = "ssh"
	LabelNonCoding IntentLabel = "non_coding"
	LabelBrowser   IntentLabel = "browser"
	LabelSearch    IntentLabel = "search"
	LabelLiveData  IntentLabel = "live_data"
	// LabelLiveDataVisual is the bounded presentation outcome for rendering
	// current, trusted facts as an image. It is deliberately distinct from a
	// desktop screenshot and from a generative-image request: its only valid
	// source is the lookup evidence collected for this turn.
	LabelLiveDataVisual   IntentLabel = "live_data_visual"
	LabelDocumentDelivery IntentLabel = "document_delivery"
	// LabelDocumentGenerate is a single-stage request to render current facts
	// into a PDF file. It is not document_delivery (open/send an existing
	// file), not attachment_delivery (echo an ingress file), and not a
	// multi-phase workflow_task such as a research report or business plan.
	LabelDocumentGenerate IntentLabel = "document_generate"
	// LabelAttachmentDelivery is the narrow, channel-scoped outcome of
	// delivering exactly one attachment already supplied in the current turn.
	// It deliberately excludes opening a local file, exporting/generating a
	// document, or sending to a user-selected destination.
	LabelAttachmentDelivery IntentLabel = "attachment_delivery"
	// LabelDocumentRead is a bounded, read-only request to inspect an existing
	// document supplied through a trusted input boundary. It intentionally does
	// not cover opening, exporting, sending, generating, or editing a document.
	LabelDocumentRead IntentLabel = "document_read"
	// LabelDocumentOpen is a request to open an existing local document with
	// the OS default handler. It is not reading attachment content
	// (document_read), not sending to a chosen target (document_delivery),
	// and not launching an unrelated application, URL, or folder (app_launch).
	LabelDocumentOpen IntentLabel = "document_open"
	LabelBusinessData IntentLabel = "business_data"
	LabelBugFix       IntentLabel = "bug_fix"
	LabelContinuation IntentLabel = "continuation"
	LabelMaintenance  IntentLabel = "maintenance"
	LabelOffice       IntentLabel = "office"
	LabelComputerUse  IntentLabel = "computer_use"
	// LabelScreenshot is a focused desktop screen-capture request. It is kept
	// distinct from computer_use so a user asking only to capture the display
	// gets the lightweight screenshot tool rather than the full desktop-control
	// surface.
	LabelScreenshot     IntentLabel = "screenshot"
	LabelKnowledgeWrite IntentLabel = "knowledge_write"
	LabelCurrentTime    IntentLabel = "current_time"
	LabelWorkflowTask   IntentLabel = "workflow_task"

	// S2b1 governed capability labels. Each maps to exactly one builtin
	// capability family in the semantic routing rule set; none of them is a
	// restatement of an existing label:
	//   - file_read/file_write are bounded local file outcomes, not coding
	//     (building or modifying software) and not document_read (a trusted
	//     channel attachment rather than a local path).
	//   - shell_command is a local host command; ssh remains the remote label.
	//   - git_inspect is read-only VCS inspection; git_mutate commits or
	//     publishes the bound repository and is the only label that reaches
	//     repository mutation.
	//   - web_fetch reads one user-supplied URL; search/live_data remain the
	//     open-ended lookup labels.
	//   - audio_record captures the microphone; audio_transcribe reads an
	//     existing audio file; audio_synthesize plays text as local speech.
	//   - audit_read inspects audit logs, session history, or health state.
	//   - knowledge_read retrieves from the local knowledge store;
	//     knowledge_write remains the ingestion label.
	LabelFileRead        IntentLabel = "file_read"
	LabelFileWrite       IntentLabel = "file_write"
	LabelShellCommand    IntentLabel = "shell_command"
	LabelGitInspect      IntentLabel = "git_inspect"
	LabelGitMutate       IntentLabel = "git_mutate"
	LabelAudioRecord     IntentLabel = "audio_record"
	LabelAudioTranscribe IntentLabel = "audio_transcribe"
	LabelAudioSynthesize IntentLabel = "audio_synthesize"
	// LabelAudioDeliver is a request to render text as speech and deliver that
	// audio to the current trusted channel. It is not local playback
	// (audio_synthesize) and not sending an existing file (document_delivery).
	LabelAudioDeliver  IntentLabel = "audio_deliver"
	LabelWebFetch      IntentLabel = "web_fetch"
	LabelAuditRead     IntentLabel = "audit_read"
	LabelKnowledgeRead IntentLabel = "knowledge_read"

	// S2b2 governed capability labels for the administration/misc builtin
	// families. Each maps to exactly one builtin capability family; none of
	// them restates an existing label:
	//   - app_launch opens an application, URL, or folder with the OS handler;
	//     document_open opens an existing local document with that handler;
	//     document_delivery keeps send/export to a specified target.
	//   - file_download saves a remote URL to local disk; web_fetch only reads
	//     the page text.
	//   - schedule_manage administers local scheduled jobs without binding a
	//     channel delivery. schedule_dispatch is the "fire to a group/person
	//     when due" outcome. It materializes only with a trusted typed
	//     destination; registering the intent is not a send.
	//   - config_manage changes the agent's own configuration/provider/limits.
	//   - memory_manage reads or updates the agent memory store; knowledge_*
	//     remain the knowledge-base labels.
	//   - task_track maintains the local task list; goal_manage maintains
	//     persistent long-running goals; workflow_task is a workflow_v2
	//     project started from /workflow or the workflow panel, not a named
	//     skill run in the current conversation.
	//   - template_manage administers session templates.
	//   - session_manage administers external coding sessions/providers; coding
	//     remains the "do the coding work" label.
	//   - delegate_task explicitly hands work to sub-agents/parallel workers;
	//     a plain "build X" request remains coding.
	//   - knowledge_admin administers/maintains the knowledge store;
	//     knowledge_read/knowledge_write remain retrieval and ingestion.
	LabelAppLaunch        IntentLabel = "app_launch"
	LabelFileDownload     IntentLabel = "file_download"
	LabelScheduleManage   IntentLabel = "schedule_manage"
	LabelScheduleDispatch IntentLabel = "schedule_dispatch"
	LabelConfigManage     IntentLabel = "config_manage"
	LabelMemoryManage     IntentLabel = "memory_manage"
	LabelTaskTrack        IntentLabel = "task_track"
	LabelGoalManage       IntentLabel = "goal_manage"
	LabelTemplateManage   IntentLabel = "template_manage"
	LabelSessionManage    IntentLabel = "session_manage"
	LabelDelegateTask     IntentLabel = "delegate_task"
	LabelKnowledgeAdmin   IntentLabel = "knowledge_admin"

	LabelAmbiguous IntentLabel = "ambiguous"
	LabelUnknown   IntentLabel = "unknown"
)

// AllLabels returns the complete set of valid intent labels.
func AllLabels() []IntentLabel {
	return []IntentLabel{
		LabelCoding,
		LabelSSH,
		LabelNonCoding,
		LabelBrowser,
		LabelSearch,
		LabelLiveData,
		LabelLiveDataVisual,
		LabelDocumentDelivery,
		LabelDocumentGenerate,
		LabelAttachmentDelivery,
		LabelDocumentRead,
		LabelDocumentOpen,
		LabelBusinessData,
		LabelBugFix,
		LabelContinuation,
		LabelMaintenance,
		LabelOffice,
		LabelComputerUse,
		LabelScreenshot,
		LabelKnowledgeWrite,
		LabelCurrentTime,
		LabelWorkflowTask,
		LabelFileRead,
		LabelFileWrite,
		LabelShellCommand,
		LabelGitInspect,
		LabelGitMutate,
		LabelAudioRecord,
		LabelAudioTranscribe,
		LabelAudioSynthesize,
		LabelAudioDeliver,
		LabelWebFetch,
		LabelAuditRead,
		LabelKnowledgeRead,
		LabelAppLaunch,
		LabelFileDownload,
		LabelScheduleManage,
		LabelScheduleDispatch,
		LabelConfigManage,
		LabelMemoryManage,
		LabelTaskTrack,
		LabelGoalManage,
		LabelTemplateManage,
		LabelSessionManage,
		LabelDelegateTask,
		LabelKnowledgeAdmin,
		LabelAmbiguous,
		LabelUnknown,
	}
}

// validLabels is a pre-computed set for O(1) IsValid lookups.
var validLabels = func() map[IntentLabel]bool {
	m := make(map[IntentLabel]bool, len(AllLabels()))
	for _, l := range AllLabels() {
		m[l] = true
	}
	return m
}()

// IsValid returns true if the label is in the taxonomy.
func (l IntentLabel) IsValid() bool {
	return validLabels[l]
}

// IsNonCapabilityLabel reports classifier states that carry no executable
// capability obligation. Broad Q&A (non_coding), continuation, unknown, and
// ambiguous are not capability families waiting to be migrated; semantic
// routing must skip them so they can use the legacy tool surface, and so they
// cannot discard an already-resolved governed need.
func (l IntentLabel) IsNonCapabilityLabel() bool {
	switch l {
	case "", LabelNonCoding, LabelContinuation, LabelAmbiguous, LabelUnknown:
		return true
	default:
		return false
	}
}

// KeywordStrength indicates how strongly a keyword can annotate recall evidence.
type KeywordStrength int

const (
	Strong KeywordStrength = iota // strong recall evidence; not an execution-route decision
	Weak                          // weak recall evidence; requires semantic confirmation
)

// KeywordEntry is a single entry in the keyword registry.
type KeywordEntry struct {
	Keyword  string
	Label    IntentLabel
	Strength KeywordStrength
	Creation bool // true for creation-oriented coding recall evidence
}

// ClassificationResult is the structured output of the UIC.
type ClassificationResult struct {
	Primary          IntentLabel   // exactly one primary intent
	Confidence       float64       // [0, 1]
	Secondary        []IntentLabel // zero or more secondary intents
	ToolNames        []string      // tool names to activate (from Tool Affinity)
	Layer            int           // 1, 2, or 3 (23 = fusion of L2+L3)
	Reason           string        // human-readable explanation
	CreationOriented bool          // true when the coding intent is creation-oriented (new project/feature)

	// WorkflowType is the workflow template type determined by L3 tree reasoning
	// or inferred from IntentDefinition in degraded mode.
	// Non-empty when the intent maps to a multi-phase workflow (e.g., "coding",
	// "presentation_design", "product_design"). Empty string means no workflow.
	// This eliminates the need for a separate IUM LLM call to determine workflow type.
	WorkflowType string

	// Degraded is true when the classification was produced in degraded mode
	// (one or both fusion channels failed). Consumers can use this to adjust
	// confidence thresholds.
	Degraded bool

	// ControlPlaneFailure means the L3 classifier received a successful model
	// response that did not satisfy its structured-output protocol.  This is
	// deliberately distinct from an unknown user intent: callers must not
	// continue into a legacy name router and accidentally send a tool-less
	// execution turn after losing the authority that selects its capability
	// surface.
	ControlPlaneFailure bool `json:"-"`

	// RunnerUp is the second-strongest label from an ambiguous embedding pass,
	// carried purely as escalation evidence for a later layer. It is never an
	// authorized intent on its own: only the L3 synthesis may promote it into a
	// declared composite after the tree supplies the complementary half.
	RunnerUp      IntentLabel `json:"-"`
	RunnerUpScore float64     `json:"-"`
}

// Labels returns the primary label followed by any secondary labels.
func (r ClassificationResult) Labels() []IntentLabel {
	if len(r.Secondary) == 0 {
		if r.Primary == "" {
			return nil
		}
		return []IntentLabel{r.Primary}
	}
	labels := make([]IntentLabel, 0, 1+len(r.Secondary))
	labels = append(labels, r.Primary)
	return append(labels, r.Secondary...)
}

// MessageContext is the input to the classifier.
type MessageContext struct {
	Text          string   // current user message text
	UserID        string   // for conversation context lookup
	RecentHistory []string // recent conversation messages (for continuation detection)
}

// LLMClassifyFunc is a callback for Layer 3 LLM classification.
// The caller (gui/) provides this based on their LLM config.
// Must respect the provided timeout via context.
type LLMClassifyFunc func(systemPrompt, userText string) (string, error)

// LLMClassifyContextFunc lets latency-sensitive callers cancel the underlying
// LLM transport when a classification deadline has expired.
type LLMClassifyContextFunc func(ctx context.Context, systemPrompt, userText string) (string, error)
