package agent

import "testing"

func TestDefaultCodexProvidersUseGLM51CodingPlan(t *testing.T) {
	for _, provider := range DefaultCodexProviders() {
		if provider.ModelName != "GLM" {
			continue
		}
		if provider.ModelId != "glm-5.1" {
			t.Fatalf("GLM Codex model = %q, want glm-5.1", provider.ModelId)
		}
		if provider.ModelUrl != "https://open.bigmodel.cn/api/coding/paas/v4" {
			t.Fatalf("GLM Codex URL = %q", provider.ModelUrl)
		}
		return
	}
	t.Fatalf("GLM provider not found")
}
