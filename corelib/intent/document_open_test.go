package intent

import "testing"

func TestDocumentOpenDefinitionIsLocalHandlerOpen(t *testing.T) {
	for _, def := range DefaultDefinitions() {
		if def.Label != LabelDocumentOpen {
			continue
		}
		if def.MayTriggerWorkflow {
			t.Fatal("document_open must not trigger a workflow")
		}
		if len(def.ToolNames) != 0 {
			t.Fatalf("document_open must not pin tool names, got %#v", def.ToolNames)
		}
		return
	}
	t.Fatal("LabelDocumentOpen definition missing")
}

func TestDocumentDeliveryNoLongerPinsOpen(t *testing.T) {
	for _, def := range DefaultDefinitions() {
		if def.Label != LabelDocumentDelivery {
			continue
		}
		for _, name := range def.ToolNames {
			if name == "open" || name == "craft_tool" || name == "download_file" {
				t.Fatalf("document_delivery still pins leftover tool %q: %#v", name, def.ToolNames)
			}
		}
		return
	}
	t.Fatal("LabelDocumentDelivery definition missing")
}

func TestDocumentOpenAdaptersAreNonCoding(t *testing.T) {
	r := &ClassificationResult{Primary: LabelDocumentOpen, Confidence: 0.9}
	mapped, _, _, _, _ := r.ToTaskIntent()
	if mapped != "non_coding" {
		t.Fatalf("ToTaskIntent=%q", mapped)
	}
	gate, _, _, _, _ := r.ToGateIntent()
	if gate != "non_coding" {
		t.Fatalf("ToGateIntent=%q", gate)
	}
	if !r.IsNonCodingLike() {
		t.Fatal("IsNonCodingLike=false")
	}
}

func TestDocumentOpenKeywordsDoNotStealSendOrAppLaunch(t *testing.T) {
	registry := NewKeywordRegistry()
	for _, keyword := range []string{"open the PDF on my desktop", "open the PDF document on my desktop", "open this document with the default app"} {
		matches := registry.Match(keyword)
		found := false
		for _, match := range matches {
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelDocumentOpen {
				found = true
			}
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelDocumentDelivery {
				t.Fatalf("%q leaked onto document_delivery", keyword)
			}
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelAppLaunch {
				t.Fatalf("%q leaked onto app_launch", keyword)
			}
		}
		if !found {
			t.Fatalf("%q is not registered on document_open", keyword)
		}
	}
	for _, keyword := range []string{"send file", "launch app", "open url"} {
		for _, match := range registry.Match(keyword) {
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelDocumentOpen {
				t.Fatalf("%q leaked onto document_open", keyword)
			}
		}
	}
}
