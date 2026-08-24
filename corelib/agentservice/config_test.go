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
		"skill_runner_timeout_sec",
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
	if def := byKey["skill_runner_timeout_sec"]; def.Type != "integer" || !strings.Contains(def.Description, "240-14400") || !strings.Contains(def.Description, "global_timeout") {
		t.Fatalf("skill_runner_timeout_sec schema should describe bounds and override behavior, got %#v", def)
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
	for _, key := range []string{"claude", "codex", "opencode", "codebuddy", "iflow", "kilo", "projects", "current_project", "active_tool", "default_tool", "default_tool_provider", "extra_tool_configs", "default_proxy_scope_coding_tools", "use_windows_terminal", "nl_skills"} {
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

func TestResolveLLMConfigNormalizesCodeGenSSOAutoProvider(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "CodeGen",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:      "CodeGen",
			URL:       "https://codegen.qianxin-inc.cn/api/v1/anthropic",
			Key:       "codegen-token",
			Model:     "auto",
			Protocol:  "anthropic",
			AgentType: "openclaw",
			AuthType:  "sso",
		}},
	}
	llmCfg, err := ResolveLLMConfig(cfg)
	if err != nil {
		t.Fatalf("ResolveLLMConfig() error = %v", err)
	}
	if llmCfg.Model != corelib.CodeGenDefaultModelID {
		t.Fatalf("CodeGen model = %q, want %q", llmCfg.Model, corelib.CodeGenDefaultModelID)
	}
	if llmCfg.Protocol != "openai" {
		t.Fatalf("CodeGen protocol = %q, want openai", llmCfg.Protocol)
	}
	if llmCfg.URL != "https://codegen.qianxin-inc.cn/api/v1" {
		t.Fatalf("CodeGen URL = %q, want OpenAI base URL", llmCfg.URL)
	}
	if llmCfg.AgentType != corelib.CodeGenClientName {
		t.Fatalf("CodeGen agent type = %q, want %q", llmCfg.AgentType, corelib.CodeGenClientName)
	}
}

func TestResolveLLMConfigPreservesXAIOAuthMetadata(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "xAI-Grok",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:             "xAI-Grok",
			URL:              "https://api.x.ai/v1",
			Key:              "xai-oauth-token",
			OAuthAccessToken: "xai-oauth-token",
			Model:            "grok-4.5",
			Protocol:         "openai",
			AuthType:         "oauth",
			WireAPI:          "responses",
		}},
	}

	llmCfg, err := ResolveLLMConfig(cfg)
	if err != nil {
		t.Fatalf("ResolveLLMConfig() error = %v", err)
	}
	if llmCfg.ProviderName != "xAI-Grok" || llmCfg.AuthType != "oauth" {
		t.Fatalf("xAI OAuth metadata = provider %q auth %q, want xAI-Grok/oauth", llmCfg.ProviderName, llmCfg.AuthType)
	}
	if llmCfg.Key != "xai-oauth-token" || !llmCfg.IsResponsesAPI() {
		t.Fatalf("xAI OAuth config = %+v", llmCfg)
	}
}

func TestResolveLLMConfigAppliesGlobalThinkingMode(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMThinkingMode:    "disabled",
		MaclawLLMCurrentProvider: "DeepSeek",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "test-key", Model: "deepseek-reasoner",
		}},
	}
	llmCfg, err := ResolveLLMConfig(cfg)
	if err != nil {
		t.Fatalf("ResolveLLMConfig() error = %v", err)
	}
	if llmCfg.ThinkingMode != "disabled" {
		t.Fatalf("ThinkingMode = %q, want disabled", llmCfg.ThinkingMode)
	}
}

func TestResolveLLMConfigMigratesZhipuCodingDefault(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: corelib.ZhipuCodingProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: corelib.ZhipuCodingProviderName, URL: "https://open.bigmodel.cn/api/anthropic", Key: "zhipu-key", Model: "GLM-5.2", Protocol: "anthropic",
		}},
	}
	llmCfg, err := ResolveLLMConfig(cfg)
	if err != nil {
		t.Fatalf("ResolveLLMConfig() error = %v", err)
	}
	if llmCfg.Model != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("Model = %q, want migrated %q", llmCfg.Model, corelib.ZhipuCodingDefaultModel)
	}

	flat := effectiveLLMFlatConfig(cfg)
	if flat.MaclawLLMModel != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("effective flat model = %q, want migrated %q", flat.MaclawLLMModel, corelib.ZhipuCodingDefaultModel)
	}
	if cfg.MaclawLLMProviders[0].Model != "GLM-5.2" {
		t.Fatalf("ResolveLLMConfig mutated stored provider model: %q", cfg.MaclawLLMProviders[0].Model)
	}

	alias := corelib.AppConfig{
		MaclawLLMCurrentProvider: "Zhipu GLM Coding",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: corelib.ZhipuCodingProviderName, URL: "https://open.bigmodel.cn/api/anthropic", Key: "zhipu-key", Model: "GLM-5.2", Protocol: "anthropic",
		}},
	}
	llmCfg, err = ResolveLLMConfig(alias)
	if err != nil {
		t.Fatalf("TUI alias ResolveLLMConfig() error = %v", err)
	}
	if llmCfg.Model != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("TUI alias Model = %q, want migrated %q", llmCfg.Model, corelib.ZhipuCodingDefaultModel)
	}
}

func TestNormalizeLLMConfigForSaveMigratesZhipuCodingDefault(t *testing.T) {
	providers := []corelib.MaclawLLMProvider{{
		Name: corelib.ZhipuCodingProviderName, URL: "https://open.bigmodel.cn/api/anthropic", Key: "zhipu-key", Model: "GLM-5.2", Protocol: "anthropic",
	}}
	current := corelib.AppConfig{
		MaclawLLMCurrentProvider: corelib.ZhipuCodingProviderName,
		MaclawLLMUrl:             "https://open.bigmodel.cn/api/anthropic",
		MaclawLLMKey:             "zhipu-key",
		MaclawLLMModel:           "GLM-5.2",
		MaclawLLMProviders:       append([]corelib.MaclawLLMProvider(nil), providers...),
	}
	next := normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMCurrentProvider: corelib.ZhipuCodingProviderName,
		MaclawLLMUrl:             "https://open.bigmodel.cn/api/anthropic",
		MaclawLLMKey:             "zhipu-key",
		MaclawLLMModel:           "GLM-5.2",
		MaclawLLMProviders:       append([]corelib.MaclawLLMProvider(nil), providers...),
	})
	if next.MaclawLLMModel != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("saved flat model = %q, want %q", next.MaclawLLMModel, corelib.ZhipuCodingDefaultModel)
	}
	if next.MaclawLLMProviders[0].Model != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("saved provider model = %q, want %q", next.MaclawLLMProviders[0].Model, corelib.ZhipuCodingDefaultModel)
	}
}

func TestNormalizeLLMConfigForSaveKeepsCustomZhipuFlatModel(t *testing.T) {
	providers := []corelib.MaclawLLMProvider{{
		Name: corelib.ZhipuCodingProviderName, URL: "https://open.bigmodel.cn/api/anthropic", Key: "zhipu-key", Model: "GLM-5.2", Protocol: "anthropic",
	}}
	current := corelib.AppConfig{
		MaclawLLMCurrentProvider: corelib.ZhipuCodingProviderName,
		MaclawLLMUrl:             "https://open.bigmodel.cn/api/anthropic",
		MaclawLLMKey:             "zhipu-key",
		MaclawLLMModel:           "GLM-5.2",
		MaclawLLMProviders:       append([]corelib.MaclawLLMProvider(nil), providers...),
	}
	next := normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMCurrentProvider: corelib.ZhipuCodingProviderName,
		MaclawLLMUrl:             "https://open.bigmodel.cn/api/anthropic",
		MaclawLLMKey:             "zhipu-key",
		MaclawLLMModel:           "glm-5-turbo",
		MaclawLLMProviders:       append([]corelib.MaclawLLMProvider(nil), providers...),
	})
	if next.MaclawLLMModel != "glm-5-turbo" {
		t.Fatalf("custom flat model overwritten: %q", next.MaclawLLMModel)
	}
	if next.MaclawLLMProviders[0].Model != "glm-5-turbo" {
		t.Fatalf("custom model not synced to provider: %q", next.MaclawLLMProviders[0].Model)
	}

	aliased := normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMCurrentProvider: "Zhipu GLM Coding",
		MaclawLLMUrl:             "https://open.bigmodel.cn/api/anthropic",
		MaclawLLMKey:             "zhipu-key",
		MaclawLLMModel:           "glm-5-turbo",
		MaclawLLMProviders:       append([]corelib.MaclawLLMProvider(nil), providers...),
	})
	if aliased.MaclawLLMModel != "glm-5-turbo" {
		t.Fatalf("TUI alias save overwrote custom model: %q", aliased.MaclawLLMModel)
	}
	if aliased.MaclawLLMProviders[0].Model != "glm-5-turbo" {
		t.Fatalf("TUI alias save did not sync custom model: %q", aliased.MaclawLLMProviders[0].Model)
	}
}

func TestNormalizeLLMFlatConfigFillsSelectedProvider(t *testing.T) {
	cfg := normalizeLLMFlatConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:  "other",
			URL:   "https://other.example.test/v1",
			Key:   "other-key",
			Model: "other-model",
		}, {
			Name:  "hub",
			URL:   "https://hub.example.test/api/llm/v1",
			Key:   "hub-key",
			Model: "auto",
		}},
	})
	if cfg.MaclawLLMUrl != "https://hub.example.test/api/llm/v1" || cfg.MaclawLLMKey != "hub-key" || cfg.MaclawLLMModel != "auto" {
		t.Fatalf("normalizeLLMFlatConfig did not project selected provider: %#v", cfg)
	}

	cfg = normalizeLLMFlatConfig(corelib.AppConfig{
		MaclawLLMUrl:             "https://custom.example.test/v1",
		MaclawLLMKey:             "custom-key",
		MaclawLLMModel:           "custom-model",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	})
	if cfg.MaclawLLMUrl != "https://custom.example.test/v1" || cfg.MaclawLLMKey != "custom-key" || cfg.MaclawLLMModel != "custom-model" {
		t.Fatalf("normalizeLLMFlatConfig should not overwrite explicit flat fields: %#v", cfg)
	}

	cfg = effectiveLLMFlatConfig(corelib.AppConfig{
		MaclawLLMUrl:             "https://stale.example.test/v1",
		MaclawLLMKey:             "stale-key",
		MaclawLLMModel:           "stale-model",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	})
	if cfg.MaclawLLMUrl != "https://hub.example.test/api/llm/v1" || cfg.MaclawLLMKey != "hub-key" || cfg.MaclawLLMModel != "auto" {
		t.Fatalf("effectiveLLMFlatConfig should expose selected provider as effective flat fields: %#v", cfg)
	}

	cfg = normalizeLLMFlatConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "CodeGen",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "CodeGen",
			URL:      "https://codegen.qianxin-inc.cn/api/v1/anthropic",
			Key:      "codegen-token",
			Model:    "auto",
			Protocol: "anthropic",
			AuthType: "sso",
		}},
	})
	if cfg.MaclawLLMUrl != "https://codegen.qianxin-inc.cn/api/v1" || cfg.MaclawLLMModel != corelib.CodeGenDefaultModelID || cfg.MaclawLLMProtocol != "" {
		t.Fatalf("normalizeLLMFlatConfig should normalize CodeGen SSO provider, got %#v", cfg)
	}
}

func TestMaskedSecretPlaceholderVariants(t *testing.T) {
	for _, value := range []string{"******", "********", "__masked__", " __MASKED__ "} {
		if !maskedSecretPlaceholder(value) || !IsMaskedSecretPlaceholder(value) {
			t.Fatalf("%q should be treated as masked placeholder", value)
		}
	}
	for _, value := range []string{"", "real-secret", "***", "masked"} {
		if maskedSecretPlaceholder(value) || IsMaskedSecretPlaceholder(value) {
			t.Fatalf("%q should not be treated as masked placeholder", value)
		}
	}
}

func TestNormalizeLLMConfigForSaveSyncsVisibleFlatEditsToProvider(t *testing.T) {
	codegen := normalizeLLMConfigForSave(corelib.AppConfig{}, corelib.AppConfig{
		MaclawLLMCurrentProvider: "CodeGen",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "CodeGen",
			URL:      "https://codegen.qianxin-inc.cn/api/v1/anthropic",
			Key:      "codegen-token",
			Model:    "auto",
			Protocol: "anthropic",
			AuthType: "sso",
		}},
	})
	if codegen.MaclawLLMProviders[0].Model != corelib.CodeGenDefaultModelID || codegen.MaclawLLMProviders[0].Protocol != "openai" {
		t.Fatalf("normalizeLLMConfigForSave should normalize CodeGen SSO provider, got %#v", codegen)
	}

	created := normalizeLLMConfigForSave(corelib.AppConfig{}, corelib.AppConfig{
		MaclawLLMUrl:             "https://flat.example.test/v1",
		MaclawLLMKey:             "flat-key",
		MaclawLLMModel:           "flat-model",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	})
	if created.MaclawLLMProviders[0].URL != "https://hub.example.test/api/llm/v1" || created.MaclawLLMProviders[0].Key != "hub-key" || created.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("initial provider config should not be overwritten by legacy flat fields, got %#v", created)
	}

	current := normalizeLLMFlatConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	})
	next := normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMUrl:             "https://custom.example.test/v1",
		MaclawLLMKey:             "custom-key",
		MaclawLLMModel:           "custom-model",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	})
	if next.MaclawLLMProviders[0].URL != "https://custom.example.test/v1" || next.MaclawLLMProviders[0].Key != "custom-key" || next.MaclawLLMProviders[0].Model != "custom-model" {
		t.Fatalf("visible flat LLM edits should update selected provider, got %#v", next)
	}

	next = normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMUrl:             "https://partial.example.test/v1",
		MaclawLLMKey:             "******",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	})
	if next.MaclawLLMProviders[0].URL != "https://partial.example.test/v1" || next.MaclawLLMProviders[0].Key != "hub-key" || next.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("partial flat LLM edit should not blank provider fields, got %#v", next)
	}
	if next.MaclawLLMKey != "hub-key" || next.MaclawLLMModel != "auto" {
		t.Fatalf("partial flat LLM edit should refresh flat key/model, got %#v", next)
	}

	next = mergeSecretPreserving(current, corelib.AppConfig{
		MaclawLLMKey:             "__masked__",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "********", Model: "auto"}},
	})
	next = normalizeLLMConfigForSave(current, next)
	if next.MaclawLLMProviders[0].Key != "hub-key" || next.MaclawLLMKey != "hub-key" {
		t.Fatalf("alternate masked LLM placeholders should preserve provider key, got %#v", next)
	}

	next = normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://admin.example.test/v1", Key: "admin-key", Model: "admin-model"}},
	})
	if next.MaclawLLMUrl != "https://admin.example.test/v1" || next.MaclawLLMKey != "admin-key" || next.MaclawLLMModel != "admin-model" {
		t.Fatalf("provider edits should refresh flat LLM fields, got %#v", next)
	}

	current = normalizeLLMFlatConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "primary",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "primary", URL: "https://primary.example.test/v1", Key: "primary-key", Model: "primary-model"}, {
			Name: "backup", URL: "https://backup.example.test/v1", Key: "backup-key", Model: "backup-model",
		}},
	})
	next = normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMUrl:             current.MaclawLLMUrl,
		MaclawLLMKey:             current.MaclawLLMKey,
		MaclawLLMModel:           current.MaclawLLMModel,
		MaclawLLMCurrentProvider: "backup",
		MaclawLLMProviders:       current.MaclawLLMProviders,
	})
	if next.MaclawLLMUrl != "https://backup.example.test/v1" || next.MaclawLLMKey != "backup-key" || next.MaclawLLMModel != "backup-model" {
		t.Fatalf("provider switch should refresh stale flat LLM fields, got %#v", next)
	}

	staleCurrent := current
	staleCurrent.MaclawLLMUrl = "https://stale-flat.example.test/v1"
	staleCurrent.MaclawLLMKey = "stale-flat-key"
	staleCurrent.MaclawLLMModel = "stale-flat-model"
	next = normalizeLLMConfigForSave(staleCurrent, corelib.AppConfig{
		MaclawLLMUrl:             staleCurrent.MaclawLLMUrl,
		MaclawLLMKey:             staleCurrent.MaclawLLMKey,
		MaclawLLMModel:           staleCurrent.MaclawLLMModel,
		MaclawLLMCurrentProvider: "backup",
		MaclawLLMProviders:       staleCurrent.MaclawLLMProviders,
	})
	if next.MaclawLLMProviders[1].URL != "https://backup.example.test/v1" || next.MaclawLLMProviders[1].Key != "backup-key" || next.MaclawLLMProviders[1].Model != "backup-model" {
		t.Fatalf("provider switch should not copy stale flat fields into provider, got %#v", next)
	}

	current = corelib.AppConfig{
		MaclawLLMUrl:             "https://custom.example.test/v1",
		MaclawLLMKey:             "custom-key",
		MaclawLLMModel:           "custom-model",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
	}
	next = normalizeLLMConfigForSave(current, corelib.AppConfig{
		MaclawLLMUrl:             "https://custom.example.test/v1",
		MaclawLLMKey:             "custom-key",
		MaclawLLMModel:           "custom-model",
		MaclawLLMCurrentProvider: "hub",
		MaclawLLMProviders:       current.MaclawLLMProviders,
	})
	if next.MaclawLLMProviders[0].URL != "https://hub.example.test/api/llm/v1" || next.MaclawLLMProviders[0].Key != "hub-key" || next.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("unchanged stale flat fields should not overwrite selected provider, got %#v", next)
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
