package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDemoSkillLLMConfigKeepsRealKeyAndDropsServiceGroup(t *testing.T) {
	svc, p, _ := setupServiceWithStore(t, NewMemoryStore())
	if _, err := svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "real-secret-key", MaclawLLMModel: "deepseek-flash"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	api, err := svc.GetUserConfig(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if api.AppConfig.MaclawLLMKey == "real-secret-key" {
		t.Fatalf("precondition: API config should mask the key")
	}
	cfg, err := svc.DemoSkillLLMConfig(p)
	if err != nil {
		t.Fatalf("DemoSkillLLMConfig: %v", err)
	}
	if cfg.Key != "real-secret-key" || cfg.Model != "deepseek-flash" || cfg.URL != "https://llm.example/v1" {
		t.Fatalf("config = url %q model %q keyOK %v", cfg.URL, cfg.Model, cfg.Key == "real-secret-key")
	}
	if cfg.ServiceGroupID != "" {
		t.Fatalf("service group should be unpinned, got %q", cfg.ServiceGroupID)
	}
}
