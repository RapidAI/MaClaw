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
		"nl_skills",
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
	for _, key := range []string{"mcp_servers", "local_mcp_servers", "nl_skills", "skill_hub_urls", "external_skill_dirs", "skill_sources_allowed"} {
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
		def := byKey[key]
		if def.Key == "" {
			t.Fatalf("schema missing top-level app_config field %s", key)
		}
		if def.Type == "" || def.Title == "" {
			t.Fatalf("schema field %s missing type/title: %#v", key, def)
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
