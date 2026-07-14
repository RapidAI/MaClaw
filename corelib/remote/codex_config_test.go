package remote

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildCodexConfigTomlAvoidsOpenAIReservedProviderName(t *testing.T) {
	content := BuildCodexConfigToml(&corelib.ModelConfig{
		ModelName: "openai",
		ModelId:   "codex/gpt-5.4",
		ModelUrl:  "http://127.0.0.1:20128/v1",
		ApiKey:    "sk-test",
		WireApi:   "responses",
	})
	if strings.Contains(content, `model_provider = "openai"`) || strings.Contains(content, "[model_providers.openai]") {
		t.Fatalf("reserved openai provider key leaked into config:\n%s", content)
	}
	if !strings.Contains(content, `model_provider = "openai-compatible"`) || !strings.Contains(content, "[model_providers.openai-compatible]") {
		t.Fatalf("openai provider was not normalized safely:\n%s", content)
	}
}

func TestBuildCodexConfigTomlMatchesConfigfileSwitchDefaults(t *testing.T) {
	content := BuildCodexConfigToml(&corelib.ModelConfig{
		ModelName: "Custom",
		ModelId:   "gpt-5.5",
		ModelUrl:  "https://example.test/v1",
		WireApi:   "responses",
	})
	for _, want := range []string{
		`model_provider = "custom"`,
		`model = "gpt-5.5"`,
		`base_url = "https://example.test/v1"`,
		`wire_api = "responses"`,
		`supports_websockets = false`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("config missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{
		`requires_openai_auth = true`,
		`responses_websockets_v2 = true`,
		`model_context_window`,
		`model_auto_compact_token_limit`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("config should not contain %q:\n%s", forbidden, content)
		}
	}
}

func TestBuildCodexConfigTomlDoesNotAddTigerProxyContextSettings(t *testing.T) {
	content := BuildCodexConfigToml(&corelib.ModelConfig{
		ModelName: "tigerproxy",
		ModelId:   "gpt-5.5",
		ModelUrl:  "http://127.0.0.1:18086/v1",
		WireApi:   "responses",
	})
	for _, want := range []string{
		`model_context_window = 199000`,
		`model_auto_compact_token_limit = 180000`,
	} {
		if strings.Contains(content, want) {
			t.Fatalf("generic config unexpectedly contains %q:\n%s", want, content)
		}
	}
}

func TestBuildCodexConfigTomlNilModelUsesDefaults(t *testing.T) {
	content := BuildCodexConfigToml(nil)
	if !strings.Contains(content, `model_provider = "custom"`) || !strings.Contains(content, `model = "gpt-5.4"`) {
		t.Fatalf("nil model defaults not applied:\n%s", content)
	}
}

func TestBuildCodexConfigTomlIncludesCustomCodeGenClientName(t *testing.T) {
	content := BuildCodexConfigToml(&corelib.ModelConfig{
		ModelName: "CodeGen",
		ModelId:   "codegen-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		WireApi:   "responses",
		AgentType: "custom-agent",
	})
	if !strings.Contains(content, `http_headers = { "X-Codegen-Client-Name" = "custom-agent" }`) {
		t.Fatalf("custom CodeGen client header missing:\n%s", content)
	}
}

func TestBuildCodexConfigTomlNormalizesLegacyOpenClawClientName(t *testing.T) {
	content := BuildCodexConfigToml(&corelib.ModelConfig{
		ModelName: "CodeGen",
		ModelId:   "codegen-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		WireApi:   "responses",
		AgentType: "openclaw",
	})
	if strings.Contains(content, `"X-Codegen-Client-Name" = "openclaw"`) {
		t.Fatalf("legacy openclaw client leaked into config:\n%s", content)
	}
	if !strings.Contains(content, `"X-Codegen-Client-Name" = "tigerclaw"`) {
		t.Fatalf("normalized tigerclaw header missing:\n%s", content)
	}
}
