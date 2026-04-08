package browser

import (
	"encoding/json"
	"fmt"
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

func TestSessionErrorPreservesRootCause(t *testing.T) {
	msg := sessionError(fmt.Errorf("CDP 连接失败: 调试端口 9222 已被占用"))
	if got := msg; !containsAll(got, []string{"浏览器连接失败", "CDP 连接失败", "9222", "browser_connect"}) {
		t.Fatalf("sessionError = %q", got)
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
