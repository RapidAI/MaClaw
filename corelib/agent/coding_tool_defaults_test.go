package agent

import "testing"

func TestDefaultCodexProvidersUseGLM52CodingPlan(t *testing.T) {
	for _, provider := range DefaultCodexProviders() {
		if provider.ModelName != "GLM" {
			continue
		}
		if provider.ModelId != "GLM-5.2" {
			t.Fatalf("GLM Codex model = %q, want GLM-5.2", provider.ModelId)
		}
		if provider.ModelUrl != "https://open.bigmodel.cn/api/coding/paas/v4" {
			t.Fatalf("GLM Codex URL = %q", provider.ModelUrl)
		}
		return
	}
	t.Fatalf("GLM provider not found")
}
