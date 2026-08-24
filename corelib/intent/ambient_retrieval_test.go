package intent

import "testing"

func TestWantsAmbientRetrievalAllLabels(t *testing.T) {
	want := map[IntentLabel]bool{
		LabelNonCoding: false, LabelContinuation: false, LabelAmbiguous: false, LabelUnknown: false,
		LabelAudioRecord: false, LabelAudioTranscribe: false, LabelAudioSynthesize: false, LabelAudioDeliver: false,
		LabelScreenshot: false, LabelComputerUse: false, LabelCurrentTime: false,
		LabelAttachmentDelivery: false, LabelDocumentDelivery: false, LabelDocumentOpen: false, LabelDocumentGenerate: false,
		LabelAppLaunch: false, LabelFileDownload: false,
		LabelScheduleManage: false, LabelScheduleDispatch: false,
		LabelConfigManage: false, LabelSessionManage: false, LabelTemplateManage: false,
		LabelKnowledgeWrite: false, LabelKnowledgeAdmin: false,
		LabelSearch: false, LabelLiveData: false, LabelLiveDataVisual: false, LabelWebFetch: false,
		LabelCoding: true, LabelBugFix: true, LabelMaintenance: true,
		LabelDocumentRead: true, LabelFileRead: true, LabelAuditRead: true, LabelGitInspect: true,
		LabelFileWrite: true, LabelShellCommand: true, LabelGitMutate: true, LabelOffice: true, LabelBusinessData: true,
		LabelKnowledgeRead: true, LabelMemoryManage: true,
		LabelSSH: true, LabelBrowser: true, LabelTaskTrack: true, LabelGoalManage: true, LabelDelegateTask: true,
		LabelWorkflowTask: true,
	}
	if len(want) != len(AllLabels()) {
		t.Fatalf("table has %d labels, AllLabels has %d", len(want), len(AllLabels()))
	}
	for _, label := range AllLabels() {
		got := WantsAmbientRetrieval(label)
		if expected, ok := want[label]; !ok {
			t.Fatalf("AllLabels entry %q missing from table", label)
		} else if got != expected {
			t.Fatalf("%s: WantsAmbientRetrieval=%v, want %v", label, got, expected)
		}
	}
	if WantsAmbientRetrieval("") {
		t.Fatal("empty primary must not want ambient retrieval")
	}
}

func TestWantsAmbientRetrievalUsesPrimaryOnly(t *testing.T) {
	if WantsAmbientRetrieval(LabelLiveData) {
		t.Fatal("lookup primary must not append warehouse tools")
	}
	if !WantsAmbientRetrieval(LabelKnowledgeRead) {
		t.Fatal("knowledge_read primary still adds memory")
	}
}
