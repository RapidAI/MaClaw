package agentservice

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDefaultParameterDefinitionsIncludeSharedUserConfig(t *testing.T) {
	defs := DefaultParameterDefinitions()
	byKey := map[string]ParameterDefinition{}
	for _, def := range defs {
		byKey[def.Key] = def
	}
	for _, key := range []string{
		"maclaw_llm_providers",
		"mcp_servers",
		"local_mcp_servers",
		"ssh_hosts",
		"skill_hub_urls",
		"external_skill_dirs",
		"skill_sources_allowed",
		"memory_auto_compress",
		"knowledge_skill_token_budget",
		"security_policy_mode",
		"sandbox_mode",
		"network_level",
	} {
		if byKey[key].Key == "" {
			t.Fatalf("schema missing %s", key)
		}
	}
	for _, key := range []string{"mcp_servers", "local_mcp_servers", "ssh_hosts", "skill_hub_urls", "external_skill_dirs", "skill_sources_allowed"} {
		if byKey[key].Type != "array" {
			t.Fatalf("%s type = %q, want array", key, byKey[key].Type)
		}
	}
	for _, key := range []string{"memory_auto_compress", "yolo_mode_allowed"} {
		if byKey[key].Type != "bool" {
			t.Fatalf("%s type = %q, want bool", key, byKey[key].Type)
		}
	}
}

func TestDefaultParameterDefinitionsCoverTopLevelAppConfigFields(t *testing.T) {
	defs := DefaultParameterDefinitions()
	byKey := map[string]ParameterDefinition{}
	for _, def := range defs {
		byKey[def.Key] = def
	}
	typ := reflect.TypeOf(corelib.AppConfig{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		key := jsonFieldName(field)
		if key == "" {
			continue
		}
		if !appConfigFieldAvailableInMaClawSrv(key) {
			if byKey[key].Key != "" {
				t.Fatalf("schema should not expose MaClawSrv-hidden app_config field %s", key)
			}
			continue
		}
		def := byKey[key]
		if def.Key == "" {
			t.Fatalf("schema missing top-level app_config field %s", key)
		}
		if def.Type == "" || def.Title == "" {
			t.Fatalf("schema field %s missing type/title: %#v", key, def)
		}
	}
}

func TestDefaultParameterDefinitionsHideCodingToolConfig(t *testing.T) {
	defs := DefaultParameterDefinitions()
	byKey := map[string]ParameterDefinition{}
	for _, def := range defs {
		byKey[def.Key] = def
	}
	for _, key := range []string{"claude", "gemini", "codex", "opencode", "codebuddy", "iflow", "kilo", "cursor", "projects", "current_project", "active_tool", "default_tool", "default_tool_provider", "extra_tool_configs", "default_proxy_scope_coding_tools", "use_windows_terminal", "nl_skills"} {
		if byKey[key].Key != "" {
			t.Fatalf("schema should hide coding/desktop field %s", key)
		}
	}
}

func TestResolveLLMConfigRejectsManagedByHubPlaceholder(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMUrl:   "http://127.0.0.1/managed-by-hub",
		MaclawLLMKey:   "managed-by-hub",
		MaclawLLMModel: "default",
	}
	validation := ValidateAppConfig(cfg)
	if validation.Valid || len(validation.Issues) == 0 {
		t.Fatalf("ValidateAppConfig = %#v, want managed-by-hub validation issues", validation)
	}
	_, err := ResolveLLMConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "managed-by-hub") {
		t.Fatalf("ResolveLLMConfig err = %v, want managed-by-hub placeholder error", err)
	}
}

func TestResolveLLMConfigRejectsManagedByHubProviderCredential(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:  "hub",
			URL:   "https://hub.example.test/api/llm/v1",
			Key:   "managed-by-hub",
			Model: "auto",
		}},
	}
	validation := ValidateAppConfig(cfg)
	if validation.Valid || len(validation.Issues) == 0 {
		t.Fatalf("ValidateAppConfig = %#v, want managed-by-hub provider issue", validation)
	}
	_, err := ResolveLLMConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "managed-by-hub") {
		t.Fatalf("ResolveLLMConfig err = %v, want managed-by-hub provider error", err)
	}
}

func TestValidateAppConfigReportsInvalidSSHHosts(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example.test/v1",
		MaclawLLMKey:   "test-key",
		MaclawLLMModel: "test-model",
		SSHHosts: []corelib.SSHHostEntry{
			{Label: "prod", Host: "10.0.0.10", User: "deploy", AuthMethod: "agent"},
			{Label: "prod", Host: "10.0.0.11", User: "deploy", AuthMethod: "token"},
			{Label: "broken", Host: "", User: "", Port: 70000},
		},
	}
	validation := ValidateAppConfig(cfg)
	if validation.Valid {
		t.Fatalf("ValidateAppConfig = %#v, want SSH host validation issues", validation)
	}
	keys := map[string]bool{}
	for _, issue := range validation.Issues {
		keys[issue.Key] = true
	}
	for _, key := range []string{"ssh_hosts[1].label", "ssh_hosts[1].auth_method", "ssh_hosts[2].host", "ssh_hosts[2].user", "ssh_hosts[2].port"} {
		if !keys[key] {
			t.Fatalf("expected issue for %s, got %#v", key, validation.Issues)
		}
	}
}
