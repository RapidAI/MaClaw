package tool

import (
	"strings"
	"testing"
)

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
	if got := RenderedSemanticFunctionName("host_information_search_web", "invoke_token"); got != "web_search" {
		t.Fatalf("srv search render=%q", got)
	}
}

func TestSemanticModelFunctionNamesShareOnlyAcrossHostCatalogs(t *testing.T) {
	byName := map[string][]string{}
	for adapter, name := range semanticModelFunctionNames {
		if name == "" {
			t.Fatalf("adapter %q mapped to empty name", adapter)
		}
		byName[name] = append(byName[name], adapter)
	}
	for name, adapters := range byName {
		if len(adapters) == 1 {
			continue
		}
		if len(adapters) != 2 {
			t.Fatalf("prompt name %q has %d adapters: %v", name, len(adapters), adapters)
		}
		gui, srv := false, false
		for _, adapter := range adapters {
			switch {
			case strings.HasPrefix(adapter, "semantic_"):
				gui = true
			case strings.HasPrefix(adapter, "host_"):
				srv = true
			}
		}
		if !gui || !srv {
			t.Fatalf("prompt name %q must pair a GUI semantic adapter with an srv host adapter, got %v", name, adapters)
		}
	}
	if SemanticModelFunctionName("semantic_send_trusted_im") == SemanticModelFunctionName("semantic_deliver_specified_target") {
		t.Fatal("text IM send and specified-target file delivery must not share a model name")
	}
	if SemanticModelFunctionName("host_information_search_web") != "web_search" {
		t.Fatal("srv web search must use the GUI prompt name")
	}
}
