package intent

// WantsAmbientRetrieval reports whether a managed turn whose primary label is
// primary should receive optional knowledge-read and memory-manage needs.
// Secondary labels are ignored: weather+PDF is live_data + document_generate
// and must not grow a warehouse tool. Unmapped primaries may return true; the
// caller still gates on Managed, so workflow_task does not Append today.
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
		LabelKnowledgeWrite, LabelKnowledgeAdmin,
		LabelSearch, LabelLiveData, LabelLiveDataVisual, LabelWebFetch:
		return false
	default:
		return true
	}
}
