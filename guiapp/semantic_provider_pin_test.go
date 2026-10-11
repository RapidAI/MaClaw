package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func pinTestMCPProvider(serverID, serverName, toolName string, capabilities ...tool.CapabilityID) tool.ProviderSpec {
	authorization, err := tool.NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
	})
	if err != nil {
		panic(err)
	}
	provider := tool.ProviderSpec{
		AdapterName: serverID + "_" + toolName,
		Binding: tool.ProviderBinding{
			Kind: "mcp", ProviderID: serverID, ImplementationID: toolName,
			SchemaDigest: tool.SchemaDigest([]byte(serverID + toolName)),
		},
		ParameterAuthorization: authorization,
		Effects:                []tool.EffectClass{tool.EffectReadOnly},
		Ready:                  true,
	}
	for _, capability := range capabilities {
		provider.Provides = append(provider.Provides, tool.CapabilityProvision{Capability: capability, Quality: 1})
	}
	return provider
}

func pinTestBuiltinProvider(adapter string, capability tool.CapabilityID) tool.ProviderSpec {
	authorization, err := tool.NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
	})
	if err != nil {
		panic(err)
	}
	return tool.ProviderSpec{
		AdapterName: adapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "core", ImplementationID: adapter,
			SchemaDigest: tool.SchemaDigest([]byte(adapter)),
		},
		ParameterAuthorization: authorization,
		Provides:               []tool.CapabilityProvision{{Capability: capability, Quality: 1}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly},
		Ready:                  true,
	}
}

// This is the turn that exposed the gap: the user named the MCP product, and
// the managed surface shows the model a capability name that no catalog entry
// carries. The pin is what makes the named product reachable at all.
func TestSemanticProviderPinsNamedMCPProduct(t *testing.T) {
	providers := []tool.ProviderSpec{
		pinTestBuiltinProvider("host_information_search_web", "information.search.web"),
		pinTestMCPProvider("web-search-prime", "智谱深度搜索", "web_search_prime", "information.search.web"),
	}
	entries := []agentservice.MCPToolEntry{{ServerID: "web-search-prime", ServerName: "智谱深度搜索", ToolName: "web_search_prime"}}

	for _, text := range []string{
		"使用    web_search_prime mcp 搜索 张学友资料",
		"用 web-search-prime 搜索",
		"请用智谱深度搜索查一下",
	} {
		pins := semanticProviderPins(text, providers, entries)
		if len(pins) != 1 {
			t.Fatalf("text=%q pins = %#v", text, pins)
		}
		if pins[0].Capability != "information.search.web" || pins[0].Kind != "mcp" || pins[0].ProviderID != "web-search-prime" {
			t.Fatalf("text=%q pin = %#v", text, pins[0])
		}
	}
}

// A turn that does not name any implementation must stay unpinned, or every
// ordinary request would silently lock onto whichever MCP happens to rank
// first.
func TestSemanticProviderPinsStayEmptyForOrdinaryTurns(t *testing.T) {
	providers := []tool.ProviderSpec{
		pinTestBuiltinProvider("host_information_search_web", "information.search.web"),
		pinTestMCPProvider("web-search-prime", "智谱深度搜索", "web_search_prime", "information.search.web"),
	}
	entries := []agentservice.MCPToolEntry{{ServerID: "web-search-prime", ServerName: "智谱深度搜索", ToolName: "web_search_prime"}}

	for _, text := range []string{
		"搜索张学友资料",
		"北京天气怎么样",
		"帮我看看 web_search 怎么用",
		"",
	} {
		if pins := semanticProviderPins(text, providers, entries); len(pins) != 0 {
			t.Fatalf("text=%q must not pin: %#v", text, pins)
		}
	}
}

// Naming one product must not pin an unrelated server that publishes the same
// capability under a name the user did not use.
func TestSemanticProviderPinsOnlyNamedServer(t *testing.T) {
	providers := []tool.ProviderSpec{
		pinTestMCPProvider("web-search-prime", "智谱深度搜索", "web_search_prime", "information.search.web"),
		pinTestMCPProvider("wiki-server", "Confluence", "search_pages", "information.search.web"),
	}
	entries := []agentservice.MCPToolEntry{
		{ServerID: "web-search-prime", ServerName: "智谱深度搜索", ToolName: "web_search_prime"},
		{ServerID: "wiki-server", ServerName: "Confluence", ToolName: "search_pages"},
	}
	pins := semanticProviderPins("用 web_search_prime 搜索", providers, entries)
	if len(pins) != 1 || pins[0].ProviderID != "web-search-prime" {
		t.Fatalf("pins = %#v", pins)
	}
}

// A server publishes several capabilities; naming the product pins all of
// them, because the user named the server and not one of its tools.
func TestSemanticProviderPinsEveryCapabilityOfNamedServer(t *testing.T) {
	providers := []tool.ProviderSpec{
		pinTestMCPProvider("wiki-server", "Confluence", "get_page_children", "information.lookup", "document.read.local"),
	}
	entries := []agentservice.MCPToolEntry{{ServerID: "wiki-server", ServerName: "Confluence", ToolName: "get_page_children"}}
	pins := semanticProviderPins("用 Confluence 查一下", providers, entries)
	if len(pins) != 2 {
		t.Fatalf("pins = %#v", pins)
	}
	if pins[0].Capability != "document.read.local" || pins[1].Capability != "information.lookup" {
		t.Fatalf("pin order must be stable, got %q then %q", pins[0].Capability, pins[1].Capability)
	}
}

// A Skill already carries its own name in the prompt, so it is out of scope
// here; only MCP providers are pinnable.
func TestSemanticProviderPinsIgnoreNonMCPProviders(t *testing.T) {
	providers := []tool.ProviderSpec{
		pinTestBuiltinProvider("host_information_search_web", "information.search.web"),
	}
	entries := []agentservice.MCPToolEntry{{ServerID: "web-search-prime", ServerName: "智谱深度搜索", ToolName: "web_search_prime"}}
	if pins := semanticProviderPins("用 web_search_prime 搜索", providers, entries); len(pins) != 0 {
		t.Fatalf("pins = %#v", pins)
	}
}

func TestSemanticPinnedProviderLabelsPrefersDisplayName(t *testing.T) {
	providers := []tool.ProviderSpec{
		pinTestMCPProvider("web-search-prime", "智谱深度搜索", "web_search_prime", "information.search.web"),
	}
	entries := []agentservice.MCPToolEntry{{ServerID: "web-search-prime", ServerName: "智谱深度搜索", ToolName: "web_search_prime"}}
	pins := semanticProviderPins("用 web_search_prime 搜索", providers, entries)
	labels := semanticPinnedProviderLabels(pins, providers, entries)
	if len(labels) != 1 || labels[0] != "智谱深度搜索" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestSemanticUnmetPinnedProviderDetection(t *testing.T) {
	if !semanticUnmetPinnedProvider([]tool.UnmetNeed{{NeedID: "search", ReasonCode: tool.UnmetReasonPinnedProviderUnavailable}}) {
		t.Fatal("pinned unmet reason must be detected")
	}
	if semanticUnmetPinnedProvider([]tool.UnmetNeed{{NeedID: "search", ReasonCode: "no_feasible_provider"}}) {
		t.Fatal("an ordinary unmet reason must not be reported as a pinned refusal")
	}
}

// The refusal is the whole point of the pin: the user must not be told the
// catalog cannot cover the request when the real fact is that their named
// implementation is unavailable.
func TestPinnedProviderRefusalNamesTheProduct(t *testing.T) {
	err := semanticPinnedProviderUnavailableError{
		Providers: []string{"智谱深度搜索"},
		Unmet:     []tool.UnmetNeed{{NeedID: "search", ReasonCode: tool.UnmetReasonPinnedProviderUnavailable}},
	}
	if !strings.Contains(err.Error(), "智谱深度搜索") {
		t.Fatalf("error must name the product: %v", err)
	}
	response := semanticHostRejectResponseForPlanError(err)
	if response.Error != "semantic_pinned_provider_unavailable" {
		t.Fatalf("response = %#v", response)
	}
	if !strings.Contains(response.Text, "智谱深度搜索") || strings.Contains(response.Text, "能力目录") {
		t.Fatalf("user text must name the product, not the catalog: %q", response.Text)
	}
}

func TestSemanticSelectionProvenanceNamesDynamicProviderOnly(t *testing.T) {
	mcpSelection := tool.PlannedSelection{Provider: tool.ProviderBinding{
		Kind: "mcp", ProviderID: "web-search-prime", ImplementationID: "web_search_prime",
	}}
	if got := semanticSelectionProvenanceNote(mcpSelection); !strings.Contains(got, "web-search-prime/web_search_prime") {
		t.Fatalf("mcp provenance = %q", got)
	}
	skillSelection := tool.PlannedSelection{Provider: tool.ProviderBinding{Kind: "skill", ProviderID: "craft-search"}}
	if got := semanticSelectionProvenanceNote(skillSelection); !strings.Contains(got, "craft-search") {
		t.Fatalf("skill provenance = %q", got)
	}
	builtinSelection := tool.PlannedSelection{Provider: tool.ProviderBinding{
		Kind: "builtin", ProviderID: "core", ImplementationID: "host-websearch",
	}}
	if got := semanticSelectionProvenanceNote(builtinSelection); got != "" {
		t.Fatalf("a builtin answer needs no provenance: %q", got)
	}
	if got := withSemanticSelectionProvenance("result text", builtinSelection); got != "result text" {
		t.Fatalf("builtin result must be untouched: %q", got)
	}
}
