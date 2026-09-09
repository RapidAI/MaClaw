package config

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestAppConfigSchemaCoversJSONFields(t *testing.T) {
	schema := AppConfigSchema()
	if len(schema) < 50 {
		t.Fatalf("schema unexpectedly small: %d fields", len(schema))
	}
	seen := make(map[string]AppConfigFieldMetadata, len(schema))
	for _, field := range schema {
		if field.Key == "" || field.Type == "" || field.Title == "" {
			t.Fatalf("invalid schema field: %#v", field)
		}
		if _, exists := seen[field.Key]; exists {
			t.Fatalf("duplicate schema key %q", field.Key)
		}
		seen[field.Key] = field
	}
	for _, key := range []string{"maclaw_llm_url", "maclaw_llm_key", "mcp_servers", "ssh_hosts", "default_proxy_enabled"} {
		field, ok := seen[key]
		if !ok {
			t.Fatalf("schema missing %s", key)
		}
		if !IsSharedClientField(key) || field.Scope != AppConfigScopeShared {
			t.Fatalf("shared field %s metadata = %#v", key, field)
		}
	}
	for key := range sharedClientConfigKeys {
		if _, ok := seen[key]; !ok {
			t.Fatalf("shared policy references unknown AppConfig field %q", key)
		}
	}
}

func TestAppConfigSchemaHonorsHostAndUserVisibility(t *testing.T) {
	guiOnly, ok := AppConfigFieldMap()["claude"]
	if !ok || guiOnly.Scope != AppConfigScopeGUIOnly || guiOnly.HeadlessSupport {
		t.Fatalf("claude should be GUI-only/headless-disabled: %#v", guiOnly)
	}
	if IsUserWebVisibleField("maclaw_llm_providers") {
		t.Fatal("complex provider config must use a structured editor")
	}
	if IsUserWebVisibleField("remote_machine_token") {
		t.Fatal("remote machine token must not be exposed to user web")
	}
	if !IsUserWebVisibleField("maclaw_llm_url") {
		t.Fatal("basic LLM URL should remain user-web visible")
	}
}

func TestAppConfigSchemaReturnsIndependentMapCopies(t *testing.T) {
	shared := SharedClientConfigKeys()
	shared["maclaw_llm_url"] = false
	if !IsSharedClientField("maclaw_llm_url") {
		t.Fatal("mutating returned shared map changed canonical policy")
	}
	hidden := UserWebHiddenConfigKeys()
	delete(hidden, "remote_machine_token")
	if IsUserWebVisibleField("remote_machine_token") {
		t.Fatal("mutating returned hidden map changed canonical policy")
	}
}

func TestCopyAppConfigFieldsCopiesOnlyMatchingFields(t *testing.T) {
	current := corelib.AppConfig{
		MaclawLLMUrl:      "https://current.example/v1",
		MaclawLLMKey:      "current-key",
		Projects:          []corelib.ProjectConfig{{Name: "current-project"}},
		ModelRoutes:       map[string]corelib.ModelRouteConfig{"reasoning": {Model: "current-model"}},
		AuxiliaryLLM:      corelib.AuxiliaryLLMConfig{URL: "https://aux.current", Model: "aux-current"},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{},
	}
	nextProfile := &corelib.MaclawLLMProfiles{}
	next := corelib.AppConfig{
		MaclawLLMUrl:      "https://next.example/v1",
		MaclawLLMKey:      "next-key",
		Projects:          []corelib.ProjectConfig{{Name: "next-project"}},
		ModelRoutes:       map[string]corelib.ModelRouteConfig{"fast": {Model: "next-model"}},
		AuxiliaryLLM:      corelib.AuxiliaryLLMConfig{URL: "https://aux.next", Model: "aux-next"},
		MaclawLLMProfiles: nextProfile,
	}

	got := CopyAppConfigFields(current, next, func(key string) bool {
		return key == "maclaw_llm_key" || key == "projects" || key == "model_routes" || key == "auxiliary_llm" || key == "maclaw_llm_profiles"
	})
	if got.MaclawLLMKey != current.MaclawLLMKey {
		t.Fatalf("matching scalar field not copied: %q", got.MaclawLLMKey)
	}
	if got.MaclawLLMUrl != next.MaclawLLMUrl {
		t.Fatalf("unmatched scalar field changed: %q", got.MaclawLLMUrl)
	}
	if len(got.Projects) != 1 || got.Projects[0].Name != "current-project" {
		t.Fatalf("slice field not copied: %#v", got.Projects)
	}
	if route, ok := got.ModelRoutes["reasoning"]; !ok || route.Model != "current-model" {
		t.Fatalf("map field not copied: %#v", got.ModelRoutes)
	}
	if got.AuxiliaryLLM.URL != current.AuxiliaryLLM.URL {
		t.Fatalf("struct field not copied: %#v", got.AuxiliaryLLM)
	}
	if got.MaclawLLMProfiles != current.MaclawLLMProfiles {
		t.Fatalf("pointer field not copied: %p != %p", got.MaclawLLMProfiles, current.MaclawLLMProfiles)
	}
}

func TestCopyAppConfigFieldsNilPredicateReturnsNext(t *testing.T) {
	next := corelib.AppConfig{MaclawLLMUrl: "https://next.example"}
	got := CopyAppConfigFields(corelib.AppConfig{MaclawLLMUrl: "https://current.example"}, next, nil)
	if got.MaclawLLMUrl != next.MaclawLLMUrl {
		t.Fatalf("nil predicate should return next unchanged: %q", got.MaclawLLMUrl)
	}
}
