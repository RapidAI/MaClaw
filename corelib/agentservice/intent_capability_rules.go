package agentservice

import (
	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// ReviewedIntentMinimumConfidence is the write-grant floor shared by GUI IM
// routing and headless dynamic routing. Hosts must not pick a second number.
const ReviewedIntentMinimumConfidence = 0.78

// CapabilityInformationSearchWeb is the web-search outcome shared by the IM
// builtin catalog and the reviewed host provider. information.lookup remains
// the MCP/Skill retrieval family and is not this capability.
const CapabilityInformationSearchWeb coretool.CapabilityID = "information.search.web"

const (
	QualifierSearchFreshness = "freshness"
	SearchFreshnessReference = "reference"
	SearchFreshnessCurrent   = "current"
	CapabilityLiveDataVisual = coretool.CapabilityID("visual.render.live_data")
)

// ReviewedCodingCapabilityNeedRule is the outcome contract shared by coding,
// bug_fix, and maintenance. Dedicated coding workbenches do not consult this
// rule; the IM shared loop and any host that opts into IM semantic rules do.
func ReviewedCodingCapabilityNeedRule() []IntentCapabilityNeedTemplate {
	return []IntentCapabilityNeedTemplate{
		{Capability: coretool.CapabilityFSReadLocal, Required: true, MaxInvocations: 12},
		{Capability: coretool.CapabilityFSWriteLocal, Required: true, MaxInvocations: 8},
		{Capability: coretool.CapabilityRepoInspectVCS, Required: true, MaxInvocations: 4},
		{Capability: coretool.CapabilityBuildVerifyLocal, Required: true, MaxInvocations: 6},
	}
}

// IMSemanticIntentCapabilityNeedRules is the owner-reviewed intent→outcome
// mapping used by the GUI IM shared loop. It is the authoritative copy of that
// map: GUI must not keep a second table, and adding a family here is how a
// host that publishes the IM catalog obtains it.
func IMSemanticIntentCapabilityNeedRules() map[intent.IntentLabel][]IntentCapabilityNeedTemplate {
	coding := ReviewedCodingCapabilityNeedRule()
	return map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelScreenshot: {
			{Capability: CapabilityVisualCapture, Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary}, Required: true},
			{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage}, Required: true},
		},
		intent.LabelCurrentTime: {
			{Capability: CapabilityCurrentTime, Required: true},
		},
		intent.LabelSearch: {
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Required: true, MaxInvocations: 5},
		},
		intent.LabelLiveData: {
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent}, Required: true},
		},
		intent.LabelLiveDataVisual: {
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent}, Required: true},
			{Capability: CapabilityLiveDataVisual, Required: true},
			{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage}, Required: true},
		},
		intent.LabelDocumentRead: {
			{Capability: CapabilityDocumentRead, Required: true},
		},
		intent.LabelDocumentOpen: {
			{Capability: coretool.CapabilitySystemLaunchLocal, Required: true},
		},
		intent.LabelDocumentDelivery: {
			{Capability: CapabilityArtifactDeliverSpecified, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile}, Required: true},
		},
		intent.LabelSSH: {
			{Capability: coretool.CapabilityShellExecuteRemoteHost, Required: true},
		},
		intent.LabelBrowser: {
			{Capability: coretool.CapabilityBrowserControlWeb, Required: true},
		},
		intent.LabelComputerUse: {
			{Capability: coretool.CapabilityComputerControlDesktop, Required: true},
		},
		intent.LabelAttachmentDelivery: {
			{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile}, Required: true},
		},
		intent.LabelDocumentGenerate: {
			{Capability: CapabilityDocumentGenerate, Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF}, Required: true},
			{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile}, Required: true},
		},
		intent.LabelOffice: {
			{Capability: coretool.CapabilityDocumentWriteOffice, Required: true, MaxInvocations: 8},
			{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile}, Required: false},
		},
		intent.LabelBusinessData: {
			{Capability: coretool.CapabilityBusinessDataRead, Required: false},
			{Capability: coretool.CapabilityBusinessDataMIS, Required: true},
		},
		intent.LabelDatabase: {
			// Sole required Read so petitions for business.data.read expand
			// SQL inspect (database_query), not MIS forms. Writes stay on
			// the unmanaged database tool or a later write-shaped label.
			{Capability: coretool.CapabilityBusinessDataRead, Required: true, MaxInvocations: 8},
		},
		intent.LabelKnowledgeWrite: {
			{Capability: coretool.CapabilityKnowledgeIngestLocal, Required: true},
		},
		intent.LabelFileRead: {
			{Capability: coretool.CapabilityFSReadLocal, Required: true},
		},
		intent.LabelGitInspect: {
			{Capability: coretool.CapabilityRepoInspectVCS, Required: true},
		},
		intent.LabelGitMutate: {
			{Capability: coretool.CapabilityRepoMutateVCS, Required: true},
		},
		intent.LabelWebFetch: {
			{Capability: coretool.CapabilityInformationFetchWeb, Required: true, MaxInvocations: 5},
		},
		intent.LabelAudioTranscribe: {
			{Capability: coretool.CapabilityAudioTranscribeSpeech, Required: true},
		},
		intent.LabelAuditRead: {
			{Capability: coretool.CapabilitySecurityAuditRead, Required: true},
		},
		intent.LabelKnowledgeRead: {
			{Capability: coretool.CapabilityKnowledgeReadLocal, Required: true},
		},
		intent.LabelFileWrite: {
			{Capability: coretool.CapabilityFSWriteLocal, Required: true},
		},
		intent.LabelShellCommand: {
			{Capability: coretool.CapabilityShellExecuteLocal, Required: true, MaxInvocations: 8},
		},
		intent.LabelAudioRecord: {
			{Capability: coretool.CapabilityAudioCaptureMicrophone, Required: true},
		},
		intent.LabelAppLaunch: {
			{Capability: coretool.CapabilitySystemLaunchLocal, Required: true},
		},
		intent.LabelFileDownload: {
			{Capability: coretool.CapabilityArtifactAcquireRemote, Required: true, MaxInvocations: 3},
		},
		intent.LabelConfigManage: {
			{Capability: coretool.CapabilityConfigManageSelf, Required: true},
		},
		intent.LabelMemoryManage: {
			{Capability: coretool.CapabilityMemoryManageAgent, Required: true},
		},
		intent.LabelTaskTrack: {
			{Capability: coretool.CapabilityTaskTrackLocal, Required: true},
		},
		intent.LabelGoalManage: {
			{Capability: coretool.CapabilityGoalManageLongRunning, Required: true},
		},
		intent.LabelTemplateManage: {
			{Capability: coretool.CapabilityTemplateManageSession, Required: true},
		},
		intent.LabelSessionManage: {
			{Capability: coretool.CapabilitySessionManageCoding, Required: true},
		},
		intent.LabelDelegateTask: {
			{Capability: coretool.CapabilityAgentDelegateSubtask, Required: true},
		},
		intent.LabelKnowledgeAdmin: {
			{Capability: coretool.CapabilityKnowledgeAdminMaintenance, Required: true},
		},
		intent.LabelScheduleManage: {
			{Capability: coretool.CapabilityScheduleAdministerLocal, Required: true},
		},
		intent.LabelScheduleDispatch: {
			{Capability: coretool.CapabilityScheduleAdministerLocal, Required: true},
			{Capability: coretool.CapabilityScheduleDispatchChannel, Required: true},
		},
		intent.LabelAudioSynthesize: {
			{Capability: coretool.CapabilityAudioSynthesizeLocal, Required: true},
		},
		intent.LabelAudioDeliver: {
			{Capability: coretool.CapabilityAudioRenderSpeech, Required: true},
			{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatVoice}, Required: true},
		},
		intent.LabelCoding:      coding,
		intent.LabelBugFix:      coding,
		intent.LabelMaintenance: coding,
	}
}
