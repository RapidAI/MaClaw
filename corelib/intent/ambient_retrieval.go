package intent

// WantsAmbientRetrieval reports whether a managed turn whose primary label is
// primary should receive optional knowledge-read and memory-manage needs.
//
// Secondary labels are ignored: weather+PDF is live_data + document_generate
// and must not grow a warehouse tool. Unmapped primaries may return true; the
// caller still gates on Managed, so workflow_task does not Append today.
// file_delete is closed on purpose: the outcome is that one local file is
// gone, and the default-true branch must not put knowledge_search on it.
//
// This gate covers ONLY the warehouse companions. It deliberately does not
// govern information.lookup, the family an arbitrary installed MCP serves:
// that need is attached by the caller's own provider-availability rule
// (see CallerWantsDynamicLookup), because an installed vendor tool answering a
// question is a different decision from the host offering its own knowledge
// store. Folding the open family in here made "any installed MCP is callable"
// depend on this negative list: every label excluded below silently lost the
// ability to call the MCP the user installed for it, and each new exclusion
// widened the gap. Keeping the two decisions apart is what lets a new family
// of enterprise MCP keep working without editing this file.
func WantsAmbientRetrieval(primary IntentLabel) bool {
	if primary.IsNonCapabilityLabel() {
		return false
	}
	switch primary {
	case LabelAudioRecord, LabelAudioTranscribe, LabelAudioSynthesize, LabelAudioDeliver,
		LabelScreenshot, LabelComputerUse, LabelCurrentTime,
		LabelAttachmentDelivery, LabelDocumentDelivery, LabelDocumentOpen, LabelDocumentGenerate,
		LabelAppLaunch, LabelFileDownload,
		LabelScheduleManage, LabelScheduleDispatch,
		LabelConfigManage, LabelSessionManage, LabelTemplateManage,
		LabelKnowledgeWrite, LabelKnowledgeAdmin, LabelFileDelete,
		LabelWorklogRead, LabelWorklogUpdate,
		LabelMailboxRead, LabelMailboxSend,
		LabelSearch, LabelLiveData, LabelLiveDataVisual, LabelWebFetch:
		return false
	default:
		return true
	}
}

// CallerWantsDynamicLookup reports whether a managed turn may attach the open
// family that an arbitrary installed MCP serves (information.lookup).
//
// This is the demand-side rule for "any installed MCP is callable" and it is
// deliberately NOT a per-label list. A list has to be edited every time a new
// enterprise product ships, and the failure mode is silent: the MCP installs,
// observation succeeds, the contract publishes, and then no need ever names it,
// so the turn dies with no_feasible_provider before the model is consulted —
// indistinguishable from "nothing is installed". That is exactly what happened
// to the worklog MCP while its label sat in the exclusion list above.
//
// Instead this names what actually makes the open family unsafe, and everything
// else is allowed:
//
//   - A purely presentational or administrative outcome cannot be answered by
//     "call some vendor tool", so delivery/launch/admin labels stay closed.
//     They are also the families whose reviewed *host* provider must win.
//   - A closed continuing session (a short 继续, a petition re-plan) keeps its
//     existing surface: adding a vendor tool to a follow-up the user framed as
//     "carry on" changes the meaning of the turn, not just its tool list.
//
// Safety does not come from this list. An unreviewed MCP tool runs inside the
// effect envelope narrowed from its own vendor annotations, and anything not
// explicitly read-only settles through the external-effect receipt boundary.
// The host already decided whether the server may be installed at all; refusing
// to route to it afterwards only reproduces that decision worse and invisibly.
func CallerWantsDynamicLookup(primary IntentLabel) bool {
	if primary.IsNonCapabilityLabel() {
		return false
	}
	switch primary {
	case LabelAttachmentDelivery, LabelDocumentDelivery, LabelDocumentOpen,
		LabelAppLaunch, LabelFileDownload, LabelFileDelete,
		LabelScheduleManage, LabelScheduleDispatch,
		LabelConfigManage, LabelSessionManage, LabelTemplateManage,
		// Both halves of the knowledge family are host-owned outcomes: the
		// result of a knowledge write is "it is in the host's store", which no
		// vendor tool can produce, and routing the user's content to an
		// installed third-party server is not what "记住这条" asked for. They
		// are closed together so the family cannot drift half-open.
		LabelKnowledgeAdmin, LabelKnowledgeWrite,
		LabelCurrentTime,
		LabelAudioDeliver, LabelAudioSynthesize, LabelAudioRecord, LabelAudioTranscribe,
		LabelScreenshot, LabelComputerUse:
		return false
	default:
		return true
	}
}
