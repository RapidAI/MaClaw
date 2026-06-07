package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveWebSearchProviderMatchesNameOrType(t *testing.T) {
	providers := []corelib.WebSearchProvider{
		{Name: "Admin Search", Type: "tinyfish", Key: "search-key", BaseURL: "https://search.example"},
	}
	for _, current := range []string{"Admin Search", "tinyfish"} {
		cb := &coreAgentCallbacks{appCfg: corelib.AppConfig{
			WebSearchProviders:       providers,
			WebSearchCurrentProvider: current,
		}}
		got := cb.resolveWebSearchProvider()
		if got.Name != "Admin Search" || got.Type != "tinyfish" || got.Key != "search-key" || got.BaseURL != "https://search.example" {
			t.Fatalf("current %q resolved provider = %#v", current, got)
		}
	}
}
