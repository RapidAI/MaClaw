package agentservice

import (
	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// ReviewedDynamicCapabilityRegistryVersion identifies the reviewed production
// vocabulary for dynamic providers. A host must construct a new registry
// version when it changes a capability contract; it must never append
// provider-discovered vocabulary to this registry at runtime.
const ReviewedDynamicCapabilityRegistryVersion = "dynamic-capabilities-v39"

const (
	// CapabilityInformationLookup is a read-only retrieval outcome. It is not
	// a tool, Skill, MCP server, package, market listing, or provider name.
	// SessionGovernedTask treats it as read-only: a succeeded lookup is not
	// replayed as an unfinished mutation.
	CapabilityInformationLookup coretool.CapabilityID = "information.lookup"
	QualifierInformationScope                         = "scope"
	InformationScopeReference                         = "reference"
	InformationScopeCurrent                           = "current"
	// CapabilityCurrentTime is a host-owned local clock read. It is not
	// information.lookup: mapping "what time is it" onto lookup would send a
	// web search. GUI IM builtins are still absent from this registry.
	CapabilityCurrentTime coretool.CapabilityID = "information.current_time"
	// CapabilityKnowledgeRead is a host-owned local knowledge-store read. It
	// is not information.lookup: mapping "search my notes" onto lookup would
	// send a web search. Knowledge admin stays out of this registry.
	CapabilityKnowledgeRead = coretool.CapabilityKnowledgeReadLocal
	// CapabilityKnowledgeWrite is a host-owned local knowledge-store ingest.
	// The host process observes SaveText/SaveURL/Import*, so the handler
	// result is the local completion receipt. Schema is text XOR url XOR
	// path; filesystem type decides file vs directory import. No channel,
	// destination, file_path, or admin. It is not knowledge.read.local,
	// fs.write.local, or document.generate.file. GUI knowledge_save_* /
	// knowledge_import_* names stay out.
	CapabilityKnowledgeWrite = coretool.CapabilityKnowledgeIngestLocal
	// CapabilityAuditRead is a host-owned principal-scoped audit/history
	// read. It is not information.lookup and does not import GUI
	// query_audit_log / session_search / check_health.
	CapabilityAuditRead = coretool.CapabilitySecurityAuditRead
	// CapabilityWebFetch is a host-owned read of one caller-supplied URL. It
	// is not information.lookup (that would search) and not
	// artifact.acquire.remote (that would save a file).
	CapabilityWebFetch = coretool.CapabilityInformationFetchWeb
	// CapabilityFileDownload saves one public HTTP(S) URL into the bound
	// workspace. Schema is url only; path, save_path, channel, and
	// destination stay out. The filename comes from the URL basename.
	// It is not information.fetch.web, fs.write.local, or a send.
	// LabelFileDownload maps here. GUI download_file / web_fetch /
	// wget / curl names stay out. The host process observes the write,
	// so the handler result is the local completion receipt.
	CapabilityFileDownload = coretool.CapabilityArtifactAcquireRemote
	// CapabilityFileRead is a host-owned workspace-scoped filesystem inspect.
	// Optional query searches the workspace; Office/PDF uses the native
	// document reader. It is not document.read.local (channel attachments),
	// knowledge.read.local, or fs.write.local. GUI read_file / list_directory
	// / search_files names stay out.
	CapabilityFileRead = coretool.CapabilityFSReadLocal
	// CapabilityRepoInspect is a host-owned workspace git status/diff read. It
	// is not repo.mutate.vcs and does not import GUI git_status / git_diff /
	// git_commit.
	CapabilityRepoInspect = coretool.CapabilityRepoInspectVCS
	// CapabilityDocumentRead is a host-owned read of one trusted current-turn
	// document attachment. It is not fs.read.local (workspace paths) and does
	// not import GUI office / read_file. The model schema has no path.
	CapabilityDocumentRead coretool.CapabilityID = "document.read.local"
	// CapabilityAudioTranscribe is a host-owned read of one trusted
	// current-turn audio attachment. It is not audio.capture.microphone,
	// audio.synthesize.local, or audio.render.speech. The model schema has
	// no path. GUI asr names stay out.
	CapabilityAudioTranscribe = coretool.CapabilityAudioTranscribeSpeech
	// CapabilityFileWrite is a host-owned workspace-scoped filesystem
	// mutation. The host process observes the write, so the handler result
	// is the local completion receipt. It is not fs.read.local,
	// knowledge.ingest.local, or document.generate.file. GUI write_file /
	// edit_file names stay out. Schema has path, content, and optional mode
	// only — no channel, destination, or group_name.
	CapabilityFileWrite = coretool.CapabilityFSWriteLocal
	// CapabilityOfficeWrite is a host-owned workspace spreadsheet write.
	// format=spreadsheet only; word/presentation stay unpublished. It is not
	// fs.write.local or document.generate.file. GUI office / write_excel
	// names stay out. Schema has path and sheets only.
	CapabilityOfficeWrite = coretool.CapabilityDocumentWriteOffice
	// CapabilityDocumentGenerate renders current facts into one PDF and
	// publishes an ArtifactRef. It is not fs.write.local, not
	// document.write.office, and not a send. LabelDocumentGenerate maps
	// here as format=pdf plus current-channel file deliver. Generate must
	// not emit [file_base64] or call SendMedia. GUI generate_pdf / office
	// names stay out. The host process observes the write, so the handler
	// result is the local completion receipt.
	CapabilityDocumentGenerate = coretool.CapabilityID("document.generate.file")
	// CapabilityAudioRender renders text into one WAV ArtifactRef. It is
	// not audio.synthesize.local (desktop playback), not
	// audio.transcribe.speech, and not a send. LabelAudioDeliver maps
	// here plus current-channel voice deliver. Render must not emit
	// [voice_base64] or call SendMedia. GUI tts / tts_render / tts_local
	// names stay out. The host process observes the write, so the
	// handler result is the local completion receipt.
	CapabilityAudioRender = coretool.CapabilityAudioRenderSpeech
	// CapabilityAudioSynthesize plays rendered speech on the host.
	// It is not audio.render.speech (that publishes a WAV for deliver),
	// not audio.transcribe.speech, and not a send. LabelAudioSynthesize
	// maps here. LabelAudioDeliver stays on render plus current-channel
	// voice. Playback must not emit [voice_base64] or call SendMedia.
	// GUI tts / tts_render / tts_local names stay out. Schema is text
	// only. The host process observes Start, so the handler result is
	// the local completion receipt.
	CapabilityAudioSynthesize = coretool.CapabilityAudioSynthesizeLocal
	// CapabilityVisualCapture captures the host primary display into one
	// PNG ArtifactRef. It is not computer.control.desktop and not a send.
	// LabelScreenshot maps here as display=primary plus current-channel
	// image deliver. Capture must not emit [file_base64] or call
	// SendMedia. GUI screenshot names stay out. Schema is empty. The host
	// process observes the capture, so the handler result is the local
	// completion receipt.
	CapabilityVisualCapture = coretool.CapabilityID("visual.capture.desktop")
	QualifierCaptureDisplay = "display"
	CaptureDisplayPrimary   = "primary"
	// CapabilitySystemLaunch opens one trusted workspace document or one
	// public http(s) URL with the OS default handler. kind=document and
	// kind=url only; applications and folders stay unpublished. It is not
	// document.read.local, not a send, and not computer.control.desktop.
	// LabelDocumentOpen maps to kind=document (path only).
	// LabelAppLaunch maps to kind=url (url only). GUI open names stay
	// out. The host process observes Start, so the handler result is the
	// local completion receipt.
	CapabilitySystemLaunch = coretool.CapabilitySystemLaunchLocal
	QualifierLaunchKind    = "kind"
	LaunchKindDocument     = "document"
	LaunchKindURL          = "url"
	// CapabilityShellExecute is a host-owned workspace command. Schema is
	// command plus optional timeout; cwd is the bound workspace. It is not
	// shell.execute.remote_host. GUI bash / project_path stay out. The host
	// process waits for exit, so the handler result is the local completion
	// receipt.
	CapabilityShellExecute = coretool.CapabilityShellExecuteLocal
	// CapabilityBuildVerify runs one reviewed build/test/lint task in the
	// bound workspace. Schema is a task drawn from a closed set plus an
	// optional workspace subdirectory; the host owns the command line. It is
	// deliberately not shell.execute.local: granting verification must not
	// also grant the arbitrary local execution that carries file and
	// repository mutation with it. GUI bash / project_path stay out.
	CapabilityBuildVerify = coretool.CapabilityBuildVerifyLocal
	// CapabilityDelegateSubtask waits for one bound child to finish. Schema
	// is task only; delegate_to stays out. No runner means unmet. Started is
	// not completed; timeout is unknown. GUI delegate_task names stay out.
	CapabilityDelegateSubtask = coretool.CapabilityAgentDelegateSubtask
	// CapabilitySSHExecute runs one command on a host-bound remote session.
	// Schema is command only; host, credentials, session_id, and label stay
	// out. No live session means unmet. Timeout or disconnect is unknown.
	// GUI ssh names stay out. This is not a channel send.
	CapabilitySSHExecute = coretool.CapabilityShellExecuteRemoteHost
	// CapabilityBrowserControl performs one host-observed browser action.
	// Schema is action plus optional url; cookies and login state cannot be
	// injected. No driver means unmet. Timeout or disconnect is unknown.
	// GUI browser names stay out.
	CapabilityBrowserControl = coretool.CapabilityBrowserControlWeb
	// CapabilityComputerUse performs one host-observed desktop action.
	// Schema is action only. No CU runtime means unmet. Timeout or a
	// missing runtime after publish is unknown. GUI computer_* names stay
	// out. Click without a host target fails closed.
	CapabilityComputerUse = coretool.CapabilityComputerControlDesktop
	// CapabilityMessageSend prepares one text send to a host-authenticated
	// IM destination. Schema is text only. Channel, group_name, and
	// destination stay out. No UIC label: catalog may publish, UIC will
	// not name it. Prepare is not a send. GUI send_to_im / im_message
	// names stay out.
	CapabilityMessageSend  = coretool.CapabilityMessageSendIM
	QualifierMessageFormat = "format"
	MessageFormatText      = "text"
	// CapabilityArtifactDeliverSpecified sends one workspace trusted
	// document, image, or voice file to a host-authenticated IM
	// destination. Schema is path only. Channel, group_name, destination,
	// and file_name stay out. Zip and other arbitrary files stay
	// unpublished. LabelDocumentDelivery maps here as format=file;
	// execute accepts the current-channel image and voice allowlists
	// when the path is a trusted media file. Prepare is not a send.
	// GUI send_file / send_to_im names stay out. Nil SendMedia or a
	// missing media id is unknown.
	CapabilityArtifactDeliverSpecified = coretool.CapabilityID("artifact.deliver.specified_target")
	// CapabilityArtifactDeliverCurrent echoes the current turn's one trusted
	// document, image, or audio attachment to the inbound channel. Schema is
	// empty. Path, artifact ID, channel, and destination stay out.
	// LabelAttachmentDelivery maps here as format=file; bind rewrites
	// format=image or format=voice when the one attachment is a trusted
	// image or audio. LabelDocumentGenerate also requires this need after
	// generate publishes the PDF ArtifactRef. LabelAudioDeliver maps
	// here as format=voice after render publishes the WAV ArtifactRef.
	// LabelScreenshot maps here as format=image after capture publishes
	// the PNG ArtifactRef. LabelAudioSynthesize stays out. Prepare is
	// not a send. GUI send_file / send_to_im / screenshot names stay
	// out. Nil SendMedia is unknown.
	CapabilityArtifactDeliverCurrent = coretool.CapabilityID("artifact.deliver.current_channel")
	QualifierArtifactFormat          = "format"
	ArtifactFormatFile               = "file"
	ArtifactFormatImage              = "image"
	ArtifactFormatVoice              = "voice"
	// CapabilityRepoMutate commits or pushes the bound workspace
	// repository. Schema is action plus optional message. Commit stages
	// tracked modifications only and is complete only when HEAD has moved.
	// Push is complete only when the remote ref reads back as the pushed
	// commit; an unreadable remote is unknown, never success or failure.
	// Reached by the git_mutate label. GUI git_commit / git_push names stay out.
	CapabilityRepoMutate = coretool.CapabilityRepoMutateVCS
	// CapabilityMemoryManage is a host-owned agent memory store read or
	// update. Field presence decides save/recall/delete/list; the model
	// schema has no action soup. It is not knowledge.read.local or
	// knowledge.ingest.local. GUI memory names and
	// NormalizeMemoryToolAction aliases stay out. Owner comes from the
	// trusted principal only. The host process observes HandleTool, so the
	// handler result is the local completion receipt.
	CapabilityMemoryManage = coretool.CapabilityMemoryManageAgent
	// CapabilityMemoryRecall is the query half of agent memory. Ambient
	// retrieval, light close, /btw, and VE use this ReadOnly capability
	// instead of CapabilityMemoryManage so save/delete stay off the surface.
	CapabilityMemoryRecall = coretool.CapabilityMemoryRecallAgent
	// CapabilityTaskTrack is a host-owned local todo-list mutation. Field
	// presence decides create/update/delete/list; the model schema has no
	// action soup. It is not goal.manage.long_running,
	// schedule.administer.local, or agent.delegate.subtask. GUI task names
	// and delegate/depends_on stay out. The host process observes the
	// session task store, so the handler result is the local completion
	// receipt.
	CapabilityTaskTrack = coretool.CapabilityTaskTrackLocal
	// CapabilityGoalManage is a host-owned long-running goal record. Field
	// presence decides create/get/complete/fail; the model schema has no
	// action soup. It is not task.track.local, schedule.administer.local,
	// or agent.delegate.subtask. This slice does not start the GUI
	// continuation engine and does not accept token_budget, max_turns,
	// pause, or resume. Owner comes from the trusted principal only. The
	// host process observes the goal store, so the handler result is the
	// local completion receipt.
	CapabilityGoalManage = coretool.CapabilityGoalManageLongRunning
	// CapabilityTemplateManage is a host-owned session-template record.
	// Field presence decides create/get/list; the model schema has no
	// action soup. It is not session.manage.coding or config.manage.self.
	// This slice does not launch a coding session and does not accept
	// yolo_mode, model_config, env_vars, or launch. The host process
	// observes the template manager, so the handler result is the local
	// completion receipt.
	CapabilityTemplateManage = coretool.CapabilityTemplateManageSession
	// CapabilityScheduleAdminister is a host-owned local schedule record.
	// Field presence decides create/update/delete/list; the model schema has
	// no action soup. It is not schedule.dispatch.channel,
	// schedule.manage.local, or task.track.local. This slice does not bind
	// Delivery, list_targets, or start a fire executor. The host process
	// observes the schedule store, so the handler result is the local
	// completion receipt.
	CapabilityScheduleAdminister = coretool.CapabilityScheduleAdministerLocal
	// CapabilityScheduleDispatch is the due-time channel send. It is not
	// schedule.administer.local. The host adapter is published only when the
	// inbound transport already authenticated a typed destination.
	CapabilityScheduleDispatch = coretool.CapabilityScheduleDispatchChannel
	// CapabilityKnowledgeAdmin is a host-owned knowledge-source
	// administration surface. Field presence decides list/get/status/refresh;
	// the model schema has no action soup. It is not knowledge.read.local or
	// knowledge.ingest.local. This slice does not import quality plans,
	// snapshots, hub share, labels, or links. The host process observes the
	// knowledge store, so the handler result is the local completion receipt.
	CapabilityKnowledgeAdmin = coretool.CapabilityKnowledgeAdminMaintenance
	// CapabilityConfigManage is a host-owned agent-self configuration
	// surface. Field presence decides get versus a single safe mutation;
	// the model schema has no action soup and no provider/url/key/model.
	// It is not session.manage.coding. Provider switch is fail-closed.
	// The host process observes the config store, so the handler result
	// is the local completion receipt.
	CapabilityConfigManage = coretool.CapabilityConfigManageSelf
	// CapabilitySessionManage is a host-owned coding-session inspect
	// surface. Field presence decides list versus get; the model schema
	// has no action soup and no drive/interrupt/send fields. It is not
	// template.manage.session or agent.delegate.subtask. This slice does
	// not launch, drive, or interrupt a session. The host process
	// observes the inspect result, so the handler result is the local
	// completion receipt.
	CapabilitySessionManage = coretool.CapabilitySessionManageCoding
)

// NewReviewedDynamicCapabilityRegistry returns the host-reviewed, sealed
// vocabulary that a lifecycle publisher may use for the first low-risk
// dynamic families. It covers retrieval plus the host-owned local clock and
// knowledge-store read, principal-scoped audit read, single-URL web fetch,
// workspace-scoped filesystem read, workspace git inspect, trusted
// current-turn document attachment read, trusted current-turn audio
// transcription, workspace-scoped filesystem
// write, local knowledge-store ingest, agent memory manage, local
// task-list tracking, long-running goal records, session-template
// records, local schedule records, knowledge-source administration,
// agent-self configuration, and coding-session inspect.
// Write, ingest, memory, task, goal, template, schedule administer,
// knowledge admin, config manage, and session inspect use a host-owned
// local mutation receipt: the same process observes the change, so the
// handler result is completion.
// Reviewed UIC coverage for families with a host receipt is complete.
// LabelAudioRecord, LabelBusinessData, coding/workflow/continuation,
// app/folder launch, credential access, session
// drive/interrupt, GUI goal continuation, template launch,
// quality maintenance plans, snapshots, hub share,
// derived-memory surgery, and provider switch require their own
// reviewed families and receipt workers before they can enter this
// registry. Specified-target trusted document/image/voice deliver is
// host-owned and receipt-bound: claim first, and a nil SendMedia is
// unknown.
// schedule.dispatch.channel is host-owned and receipt-bound: dest only
// from inbound transport, and a nil send is unknown.
// ssh / browser / CU publish only when a bound session or host runner
// already exists; timeout and disconnect are unknown, not accepted.
func NewReviewedDynamicCapabilityRegistry() (*coretool.CapabilityRegistry, error) {
	registry := coretool.NewCapabilityRegistry(ReviewedDynamicCapabilityRegistryVersion)
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityInformationLookup,
		Version: "v1",
		Summary: "Retrieve information without changing local or external state.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierInformationScope: {
				Values:   []string{InformationScopeReference, InformationScopeCurrent},
				Required: true,
			},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityCurrentTime,
		Version: "v1",
		Summary: "Read the current local date, time, weekday and timezone.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityKnowledgeRead,
		Version: "v1",
		Summary: "Retrieve from the host-owned local knowledge store without changing it.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityAuditRead,
		Version: "v1",
		Summary: "Read the calling principal's audit events without changing them.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityWebFetch,
		Version: "v1",
		Summary: "Fetch the text of one caller-supplied web URL without saving it.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityFileDownload,
		Version: "v1",
		Summary: "Download one public HTTP(S) URL into the bound workspace without choosing a save path.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityFileRead,
		Version: "v1",
		Summary: "Read or list workspace files without changing them.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityRepoInspect,
		Version: "v1",
		Summary: "Inspect workspace git status and diffs without mutating the repository.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityDocumentRead,
		Version: "v1",
		Summary: "Read one trusted current-turn document attachment without exposing its path.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierDocumentFormat: {
				Values:   []string{DocumentFormatPDF, DocumentFormatWord, DocumentFormatSpreadsheet, DocumentFormatPresentation, DocumentFormatText},
				Required: true,
			},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityAudioTranscribe,
		Version: "v1",
		Summary: "Transcribe one trusted current-turn audio attachment into text without exposing its path.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityFileWrite,
		Version: "v1",
		Summary: "Create or modify a workspace file without delivering it to a channel.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityOfficeWrite,
		Version: "v1",
		Summary: "Write a spreadsheet into a workspace path without delivering it to a channel.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierDocumentFormat: {Values: []string{DocumentFormatSpreadsheet}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityDocumentGenerate,
		Version: "v1",
		Summary: "Render current facts into a PDF file and publish an ArtifactRef without delivering it.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierDocumentFormat: {Values: []string{DocumentFormatPDF}, Required: true},
		},
		Effects:  []coretool.EffectClass{coretool.EffectLocalMutation},
		Produces: []coretool.ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}},
		Owner:    "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:       CapabilityAudioRender,
		Version:  "v1",
		Summary:  "Render text into a speech audio artifact without playing it or sending it.",
		Effects:  []coretool.EffectClass{coretool.EffectLocalMutation},
		Produces: []coretool.ArtifactContract{{Kind: "audio", MIMEType: "audio/wav", Required: true}},
		Owner:    "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityAudioSynthesize,
		Version: "v1",
		Summary: "Play synthesized speech on the host without delivering it.",
		Effects: []coretool.EffectClass{coretool.EffectLocalMutation},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityVisualCapture,
		Version: "v1",
		Summary: "Capture the host primary display into a PNG artifact without delivering it.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierCaptureDisplay: {Values: []string{CaptureDisplayPrimary}, Required: true},
		},
		Effects:  []coretool.EffectClass{coretool.EffectLocalMutation},
		Produces: []coretool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Owner:    "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilitySystemLaunch,
		Version: "v1",
		Summary: "Open one trusted workspace document or one public http(s) URL with the system handler without launching apps or folders.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierLaunchKind: {Values: []string{LaunchKindDocument, LaunchKindURL}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityShellExecute,
		Version: "v1",
		Summary: "Run one local command in the bound workspace without choosing a working directory.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityBuildVerify,
		Version: "v1",
		Summary: "Run one reviewed build, test, or lint task in the bound workspace without supplying a command line.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			"task": {Values: coretool.BuildVerifyTasks()},
		},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityDelegateSubtask,
		Version: "v1",
		Summary: "Wait for one bound child subtask to finish. Started is not completed.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilitySSHExecute,
		Version: "v1",
		Summary: "Run one command on a host-bound remote session. Host and credentials are not model fields.",
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityBrowserControl,
		Version: "v1",
		Summary: "Perform one host-observed browser action. Cookies and login state cannot be injected.",
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityComputerUse,
		Version: "v1",
		Summary: "Perform one host-observed desktop action. Missing CU runtimes stay unpublished.",
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityKnowledgeWrite,
		Version: "v1",
		Summary: "Ingest supplied text, one URL, or one workspace path into the local knowledge store.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityMemoryManage,
		Version: "v1",
		Summary: "Read or update the calling principal's agent memory store.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityMemoryRecall,
		Version: "v1",
		Summary: "Recall the calling principal's agent memory without changing the store.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityTaskTrack,
		Version: "v1",
		Summary: "Create, update, list, or delete local todo items for the current work.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityGoalManage,
		Version: "v1",
		Summary: "Create, inspect, complete, or fail the calling principal's long-running goal record.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityTemplateManage,
		Version: "v1",
		Summary: "Create, inspect, or list session templates without launching a session.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityScheduleAdminister,
		Version: "v1",
		Summary: "Create, update, list, or delete local scheduled tasks without channel delivery.",
		Effects: []coretool.EffectClass{coretool.EffectLocalMutation},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityScheduleDispatch,
		Version: "v1",
		Summary: "Prepare a due-time channel dispatch to a host-authenticated destination. This is not a send.",
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityKnowledgeAdmin,
		Version: "v1",
		Summary: "List, inspect, enable, disable, delete, or refresh the calling principal's knowledge sources.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityConfigManage,
		Version: "v1",
		Summary: "Read or update the calling principal's safe agent-self settings without switching the LLM provider.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilitySessionManage,
		Version: "v1",
		Summary: "List or inspect the calling principal's coding sessions without driving, interrupting, or launching them.",
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityMessageSend,
		Version: "v1",
		Summary: "Prepare one text send to a host-authenticated IM destination. This is not a send.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierMessageFormat: {Values: []string{MessageFormatText}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityRepoMutate,
		Version: "v1",
		Summary: "Commit the bound workspace repository when HEAD has moved. Push completes only when the remote ref reads back as the pushed commit; an unreadable remote is unknown.",
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityArtifactDeliverSpecified,
		Version: "v1",
		Summary: "Prepare one workspace trusted document, image, or voice file for a host-authenticated IM destination. This is not a send.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierArtifactFormat: {Values: []string{ArtifactFormatFile, ArtifactFormatImage, ArtifactFormatVoice}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      CapabilityArtifactDeliverCurrent,
		Version: "v1",
		Summary: "Prepare the current turn's one trusted document, image, or audio attachment for the inbound channel. This is not a send.",
		Qualifiers: map[string]coretool.QualifierConstraint{
			QualifierArtifactFormat: {Values: []string{ArtifactFormatFile, ArtifactFormatImage, ArtifactFormatVoice}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Owner:   "semantic-routing-review",
	}); err != nil {
		return nil, err
	}
	if err := registry.Seal(); err != nil {
		return nil, err
	}
	return registry, nil
}

// ReviewedDynamicIntentCapabilityNeedRules maps only governed UIC labels to
// the reviewed dynamic vocabulary. It deliberately does not read
// ClassificationResult.ToolNames, Tool Affinity, provider metadata, or
// discovery output. GUI IM families (search.web) are intentionally
// absent: they are not published on this
// registry and have no dynamic receipt worker. document.generate.file is
// host-owned: render and publish an ArtifactRef, then current-channel file
// deliver. information.current_time and
// knowledge.read.local, security.audit.read, information.fetch.web,
// artifact.acquire.remote, fs.read.local, repo.inspect.vcs, document.read.local, fs.write.local,
// document.write.office (spreadsheet), shell.execute.local,
// agent.delegate.subtask, shell.execute.remote_host,
// browser.control.web, computer.control.desktop, knowledge.ingest.local,
// memory.manage.agent, task.track.local,
// goal.manage.longrunning, template.manage.session,
// schedule.administer.local, schedule.dispatch.channel,
// knowledge.admin.maintenance,
// config.manage.self, session.manage.coding,
// audio.transcribe.speech, audio.synthesize.local,
// system.launch.local (one trusted workspace
// document or one public http(s) URL), artifact.deliver.specified_target
// (one trusted workspace document, image, or voice), and artifact.deliver.current_channel (one trusted
// attachment) are host-owned and are not satisfied by
// information.lookup. Hosts may use these rules only with a real semantic
// classifier; a keyword fallback must leave the request unmanaged.
func ReviewedDynamicIntentCapabilityNeedRules() map[intent.IntentLabel][]IntentCapabilityNeedTemplate {
	return map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{
			Capability: CapabilityInformationLookup,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelLiveData: {{
			Capability: CapabilityInformationLookup,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeCurrent},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelCurrentTime: {{
			Capability: CapabilityCurrentTime,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelKnowledgeRead: {{
			Capability: CapabilityKnowledgeRead,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelAuditRead: {{
			Capability: CapabilityAuditRead,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelWebFetch: {{
			Capability: CapabilityWebFetch,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelFileDownload: {{
			Capability: CapabilityFileDownload,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelFileRead: {{
			Capability: CapabilityFileRead,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelGitInspect: {{
			Capability: CapabilityRepoInspect,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		// Repository mutation became expressible once the adapter grew a push
		// receipt read back from the remote. Inspection is deliberately not
		// bundled in: a request to commit is not a request to read diffs, and
		// granting both would hand a write turn a capability it never asked for.
		intent.LabelGitMutate: {{
			Capability: CapabilityRepoMutate,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelDocumentRead: {{
			Capability: CapabilityDocumentRead,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelAudioTranscribe: {{
			Capability: CapabilityAudioTranscribe,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelFileWrite: {{
			Capability: CapabilityFileWrite,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelOffice: {{
			Capability: CapabilityOfficeWrite,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatSpreadsheet},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelShellCommand: {{
			Capability: CapabilityShellExecute,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelDelegateTask: {{
			Capability: CapabilityDelegateSubtask,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelSSH: {{
			Capability: CapabilitySSHExecute,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelBrowser: {{
			Capability: CapabilityBrowserControl,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelComputerUse: {{
			Capability: CapabilityComputerUse,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelKnowledgeWrite: {{
			Capability: CapabilityKnowledgeWrite,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelMemoryManage: {{
			Capability: CapabilityMemoryManage,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelTaskTrack: {{
			Capability: CapabilityTaskTrack,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelGoalManage: {{
			Capability: CapabilityGoalManage,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelTemplateManage: {{
			Capability: CapabilityTemplateManage,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelScheduleManage: {{
			Capability: CapabilityScheduleAdminister,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelScheduleDispatch: {{
			Capability: CapabilityScheduleAdminister,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}, {
			Capability: CapabilityScheduleDispatch,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelKnowledgeAdmin: {{
			Capability: CapabilityKnowledgeAdmin,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelConfigManage: {{
			Capability: CapabilityConfigManage,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelSessionManage: {{
			Capability: CapabilitySessionManage,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelDocumentDelivery: {{
			Capability: CapabilityArtifactDeliverSpecified,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelAttachmentDelivery: {{
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelDocumentGenerate: {{
			Capability: CapabilityDocumentGenerate,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}, {
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelAudioSynthesize: {{
			Capability: CapabilityAudioSynthesize,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelAudioDeliver: {{
			Capability: CapabilityAudioRender,
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}, {
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatVoice},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelScreenshot: {{
			Capability: CapabilityVisualCapture,
			Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}, {
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelDocumentOpen: {{
			Capability: CapabilitySystemLaunch,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindDocument},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
		intent.LabelAppLaunch: {{
			Capability: CapabilitySystemLaunch,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindURL},
			Polarity:   coretool.NeedRequire,
			Required:   true,
		}},
	}
}

// ReviewedDynamicCapabilityPolicyAdapter projects the only legacy workflow
// state whose existing semantics forbid general information retrieval. The
// rule is expressed solely at capability level. Other workflow/mutation
// states add no grant and no extra authority; their normal read-only behavior
// is already bounded by the descriptor and provider contract.
func ReviewedDynamicCapabilityPolicyAdapter() StaticCapabilityPolicyAdapter {
	return StaticCapabilityPolicyAdapter{Rules: []CapabilityPolicyRule{{
		WorkflowPolicy: "ops_controlled",
		Constraints: []coretool.RoutingConstraint{{
			ID:         "policy:ops_controlled:deny-information-lookup",
			Capability: CapabilityInformationLookup,
			Effect:     "deny",
			Authority:  coretool.AuthorityPolicy,
		}},
	}}}
}
