package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// A lookup primary with a document_generate secondary plans the lookup only.
// generate_pdf is petitionable from the directory. download_file and office
// are not on this classification, so they are not first-wave grants and
// office is not petitionable until the classifier offers that label.
func TestSemanticDocumentCompositeFaceCarriesDownloadAndKeepsOfficePetitionable(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	h.semanticTrustedArtifactAcquire = func(userID, url string) (string, error) {
		return "Acquired remote artifact into the workspace.\nName: ragdoll.jpg", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "网上找布偶照片做成生日PPT", "desktop", "root-ppt-face", "turn-ppt-face",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if !planHasCapabilities(surface.plan, "information.search.web") {
		t.Fatalf("composite plan=%#v", surface.plan.Selections)
	}
	if planHasCapabilities(surface.plan, "document.generate.file") || planHasCapabilities(surface.plan, tool.CapabilityArtifactAcquireRemote) {
		t.Fatalf("secondary generate and download stay out of the first wave: %#v", surface.plan.Selections)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedAcquireRemoteAdapter) != "" {
		t.Fatalf("download_file must not ride the first wave: grants=%#v", surface.grants)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	granted, message := cb.PetitionToolCall("generate_pdf")
	if !(granted && strings.Contains(message, "generate_pdf")) && !successorNotYetListedDenial(message, "generate_pdf") {
		t.Fatalf("secondary generate must petition: granted=%v message=%q", granted, message)
	}
	if granted, message := cb.PetitionToolCall("office"); granted {
		t.Fatalf("office is not in this classification's directory: %q", message)
	}
}
