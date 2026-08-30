package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The 2026-08-28 birthday-PPT turn end to end: the classification is the
// declared lookup+generate composite (primary=live_data,
// secondary=document_generate), and the task still needs to ACQUIRE photos and
// to write the deck. download_file must ride the document archetype bundle
// onto the face (an offer, not a petition), so the turn's single effectful
// petition stays available for the office leg the composite did not declare.
// Before the bundle keyed on the declared document half, download_file was
// petition-only: the model spent the effectful petition on it and the later
// office petition hit the spent budget and was hard-denied three times.
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
	if !planHasCapabilities(surface.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("composite plan=%#v", surface.plan.Selections)
	}
	// The acquire leg is on the face from the start: a grant, not a petition.
	downloadName := semanticGrantNameForAdapter(surface, semanticTrustedAcquireRemoteAdapter)
	if downloadName != "download_file" {
		t.Fatalf("download_file must ride the document bundle onto the face: grants=%#v", surface.grants)
	}
	// The phased legs stay gated until the lookup completes.
	if semanticGrantNameForAdapter(surface, "generate_pdf") != "" {
		t.Fatal("generate must stay phase-gated before the lookup")
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	if got := cb.ExecuteTool(downloadName, `{"url":"https://example.com/ragdoll.jpg"}`); !strings.Contains(got, "Acquired remote artifact") {
		t.Fatalf("on-face download must execute without a petition: %q", got)
	}
	// No petition was spent on the acquire leg, so the office petition the
	// task actually needs still succeeds and widens the surface.
	granted, message := cb.PetitionToolCall("office")
	if !granted || !strings.Contains(message, "office") {
		t.Fatalf("office petition must succeed after on-face downloads: granted=%v message=%q", granted, message)
	}
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedOfficeWriteAdapter); name != "office" {
		t.Fatalf("child surface office grant=%q, want office", name)
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityDocumentWriteOffice, tool.CapabilityArtifactAcquireRemote, "document.generate.file") {
		t.Fatalf("child plan must keep the parent legs and add office: %#v", cb.semanticSurface.plan.Selections)
	}
}
