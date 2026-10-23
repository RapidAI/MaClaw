package agentservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestFailoverLLMProviderRotatesOn429AndCoolsDown(t *testing.T) {
	providerRateLimitCooldowns.Range(func(k, _ any) bool { providerRateLimitCooldowns.Delete(k); return true })
	app := corelib.AppConfig{MaclawLLMCurrentProvider: "A", MaclawLLMProviders: []corelib.MaclawLLMProvider{
		{Name: "A", URL: "https://a.example/v1", Key: "ka", Model: "m"},
		{Name: "NoKey", URL: "https://n.example/v1", Model: "m"},
		{Name: "B", URL: "https://b.example/v1", Key: "kb", Model: "m"},
	}}
	cur, err := ResolveLLMConfig(app)
	if err != nil {
		t.Fatal(err)
	}
	cb := &coreAgentCallbacks{appCfg: app, llmCfg: cur}
	e429 := &llm.HTTPStatusError{StatusCode: 429, Body: []byte(`{"retry_after_seconds":120}`)}
	next, ok := cb.FailoverLLMProvider(context.Background(), cur, e429)
	if !ok || next.ProviderName != "B" || cb.llmCfg.ProviderName != "B" {
		t.Fatalf("want B, got %v %+v", ok, next.ProviderName)
	}
	until, _ := providerRateLimitCooldowns.Load(providerCooldownKey("https://a.example/v1", "m"))
	if d := time.Until(until.(time.Time)); d < 110*time.Second {
		t.Fatalf("Retry-After not respected: %v", d)
	}
	// B is now also 429: A is still cooling down, so nothing is left.
	if _, ok := cb.FailoverLLMProvider(context.Background(), next, &llm.HTTPStatusError{StatusCode: 429}); ok {
		t.Fatal("expected no provider while A cools down")
	}
}
