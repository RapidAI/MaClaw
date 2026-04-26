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
