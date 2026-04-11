package browser

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPolicyFromArgsParsesDomainLists(t *testing.T) {
	policy := policyFromArgs(map[string]interface{}{
		"allowed_domains":               []interface{}{"example.com", "sub.example.com"},
		"blocked_domains":               []interface{}{"forbidden.com"},
		"allow_cross_origin_navigation": false,
	})
	if len(policy.AllowedDomains) != 2 {
		t.Fatalf("AllowedDomains len = %d", len(policy.AllowedDomains))
	}
	if len(policy.BlockedDomains) != 1 || policy.BlockedDomains[0] != "forbidden.com" {
		t.Fatalf("BlockedDomains = %#v", policy.BlockedDomains)
	}
	if policy.AllowCrossOriginNavigation {
		t.Fatal("AllowCrossOriginNavigation = true, want false")
	}
}

func TestMarshalBrowserResultIncludesDisplay(t *testing.T) {
	result := marshalBrowserResult(true, "ok", map[string]interface{}{"session_id": "abc"})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if payload["display"] != "ok" {
		t.Fatalf("display = %#v", payload["display"])
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data missing: %#v", payload)
	}
	if data["session_id"] != "abc" {
		t.Fatalf("session_id = %#v", data["session_id"])
	}
}

func TestRegisterToolsContainsBrowserSessionMVP(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{
		"browser_session_start",
		"browser_session_stop",
		"browser_observe",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_wait",
		"browser_back",
		"browser_refresh",
		"browser_extract",
	} {
		toolDef, ok := reg.Get(name)
		if !ok {
			t.Fatalf("tool %q not registered", name)
		}
		if toolDef == nil {
			t.Fatalf("tool %q definition is nil", name)
		}
	}
}

func TestBrowserSessionToolsRequireSessionID(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{
		"browser_session_stop",
		"browser_observe",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_wait",
		"browser_back",
		"browser_refresh",
		"browser_extract",
	} {
		toolDef, ok := reg.Get(name)
		if !ok || toolDef == nil {
			t.Fatalf("tool %q not registered", name)
		}
		found := false
		for _, req := range toolDef.Required {
			if req == "session_id" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tool %q required = %#v, want session_id", name, toolDef.Required)
		}
		if _, ok := toolDef.InputSchema["session_id"]; !ok {
			t.Fatalf("tool %q missing session_id in input schema", name)
		}
	}
}

func TestBrowserExtractSchemaIncludesContinuationFields(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_extract")
	if !ok || toolDef == nil {
		t.Fatal("browser_extract not registered")
	}
	if got := toolDef.Description; !strings.Contains(got, "offset/max_chars") {
		t.Fatalf("Description = %q", got)
	}
	for _, field := range []string{"offset", "max_chars"} {
		entry, ok := toolDef.InputSchema[field]
		if !ok {
			t.Fatalf("browser_extract missing schema field %q", field)
		}
		meta, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("schema[%q] = %#v", field, entry)
		}
		if meta["type"] != "integer" {
			t.Fatalf("schema[%q].type = %#v", field, meta["type"])
		}
	}
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
