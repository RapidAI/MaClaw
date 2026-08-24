package tool

import "testing"

func TestSemanticModelFunctionNameIsStableAndIgnoresGrantTokens(t *testing.T) {
	if got := SemanticModelFunctionName("semantic_search_trusted_web"); got != "web_search" {
		t.Fatalf("search=%q", got)
	}
	if got := SemanticModelFunctionName("generate_pdf"); got != "generate_pdf" {
		t.Fatalf("pdf=%q", got)
	}
	if got := SemanticModelFunctionName("semantic_deliver_current_file"); got != "send_file" {
		t.Fatalf("deliver=%q", got)
	}
	if got := SemanticModelFunctionName("semantic_deliver_specified_target"); got != "send_to_im" {
		t.Fatalf("specified=%q", got)
	}
	if got := SemanticModelFunctionName("semantic_send_trusted_im"); got != "send_im_text" {
		t.Fatalf("message=%q", got)
	}
	if got := SemanticModelFunctionName("mcp.acme.ping"); got != "" {
		t.Fatalf("dynamic adapters must not invent a prompt name: %q", got)
	}
	if got := RenderedSemanticFunctionName("semantic_search_trusted_web", "invoke_token"); got != "web_search" {
		t.Fatalf("search render=%q", got)
	}
	if got := RenderedSemanticFunctionName("mcp.acme.ping", "invoke_token"); got != "invoke_token" {
		t.Fatalf("dynamic render=%q", got)
	}
}

func TestSemanticModelFunctionNamesAreUnique(t *testing.T) {
	seen := make(map[string]string, len(semanticModelFunctionNames))
	for adapter, name := range semanticModelFunctionNames {
		if name == "" {
			t.Fatalf("adapter %q mapped to empty name", adapter)
		}
		if prev, ok := seen[name]; ok {
			t.Fatalf("prompt name %q mapped from both %q and %q", name, prev, adapter)
		}
		seen[name] = adapter
	}
	if SemanticModelFunctionName("semantic_send_trusted_im") == SemanticModelFunctionName("semantic_deliver_specified_target") {
		t.Fatal("text IM send and specified-target file delivery must not share a model name")
	}
}
