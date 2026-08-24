package intent

import "testing"

func TestUnifiedClassifierTreeOnlyActivatesSecondaryToolAffinities(t *testing.T) {
	uic := New(Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"search","score":0.94},{"skill":"document_generate","score":0.88},{"skill":"non_coding","score":0.60}]}`, nil
	}})

	result := uic.Classify(MessageContext{Text: "search papers, make a PDF report, and send it"})
	if result.Primary != LabelSearch {
		t.Fatalf("primary = %s, want %s", result.Primary, LabelSearch)
	}
	if len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("secondary = %#v, want document_generate", result.Secondary)
	}
	tools := map[string]bool{}
	for _, name := range result.ToolNames {
		tools[name] = true
	}
	for _, name := range []string{"web_search", "web_fetch", "download_file"} {
		if !tools[name] {
			t.Fatalf("expected tool %s from primary search affinity, got %#v", name, result.ToolNames)
		}
	}
	for _, name := range []string{"generate_pdf", "send_file", "office"} {
		if tools[name] {
			t.Fatalf("document_generate must not pin tool name %s via affinity, got %#v", name, result.ToolNames)
		}
	}
}

func TestUnifiedClassifierTreeRecognizesWeatherPDFOutputAsComposite(t *testing.T) {
	uic := New(Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		if userText != "北京天气，输出 格式化pdf报告" {
			t.Fatalf("userText=%q", userText)
		}
		return `{"top":[{"skill":"live_data","score":0.95},{"skill":"document_generate","score":0.90},{"skill":"non_coding","score":0.40}]}`, nil
	}})

	result := uic.Classify(MessageContext{Text: "北京天气，输出 格式化pdf报告"})
	if result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want live_data + document_generate", result)
	}
}

func TestNormalizeDeclaredCompositeMakesLookupTheExecutionAnchor(t *testing.T) {
	result := ClassificationResult{
		Primary: LabelDocumentGenerate,
		Secondary: []IntentLabel{
			LabelLiveData,
		},
		Confidence: .95,
	}
	NormalizeDeclaredComposite(&result)
	if result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want live_data + document_generate", result)
	}

	searchMixed := ClassificationResult{Primary: LabelDocumentGenerate, Secondary: []IntentLabel{LabelSearch, LabelLiveData}}
	NormalizeDeclaredComposite(&searchMixed)
	if searchMixed.Primary != LabelDocumentGenerate {
		t.Fatalf("search+live_data pair must remain classifier-owned: %+v", searchMixed)
	}

	for _, additional := range []IntentLabel{LabelDocumentDelivery, LabelCoding} {
		mixed := ClassificationResult{Primary: LabelDocumentGenerate, Secondary: []IntentLabel{LabelLiveData, additional}}
		NormalizeDeclaredComposite(&mixed)
		if mixed.Primary != LabelDocumentGenerate {
			t.Fatalf("lookup+document_generate+%s must remain classifier-owned: %+v", additional, mixed)
		}
	}
}
