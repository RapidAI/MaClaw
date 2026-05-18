package intent

import "testing"

func TestUnifiedClassifierTreeOnlyActivatesSecondaryToolAffinities(t *testing.T) {
	uic := New(Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"search","score":0.94},{"skill":"document_delivery","score":0.88},{"skill":"non_coding","score":0.60}]}`, nil
	}})

	result := uic.Classify(MessageContext{Text: "search papers, make a PDF report, and send it"})
	if result.Primary != LabelSearch {
		t.Fatalf("primary = %s, want %s", result.Primary, LabelSearch)
	}
	if len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentDelivery {
		t.Fatalf("secondary = %#v, want document_delivery", result.Secondary)
	}
	tools := map[string]bool{}
	for _, name := range result.ToolNames {
		tools[name] = true
	}
	for _, name := range []string{"web_search", "send_file", "open", "craft_tool"} {
		if !tools[name] {
			t.Fatalf("expected tool %s from primary+secondary affinities, got %#v", name, result.ToolNames)
		}
	}
}
