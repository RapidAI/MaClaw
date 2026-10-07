package corelib

import "testing"

func TestKimiLockedSamplingDetection(t *testing.T) {
	locked := []MaclawLLMConfig{
		{URL: "https://api.kimi.com/coding/v1", Model: "kimi-for-coding"},
		{URL: "https://api.kimi.ai/coding/v1", Model: "custom-alias"},
		{URL: "https://api.moonshot.cn/v1", Model: "kimi-k2.6"},
		{URL: "https://api.moonshot.cn/v1", Model: "moonshotai/kimi-k2.7-code"},
		{URL: "https://example.com/v1", Model: "kimi-k3"},
		{URL: "https://example.com/v1", Model: "Kimi-For-Coding"},
		{URL: "https://example.com/v1", Model: "moonshot:kimi-k2.6"},
		{URL: "https://example.com/v1", Model: "kimi-k2.6[256k]"},
	}
	for _, cfg := range locked {
		body := map[string]interface{}{"model": cfg.Model, "temperature": 0.0, "top_p": 1.0, "max_tokens": 64}
		OmitKimiLockedSampling(cfg, body)
		if _, ok := body["temperature"]; ok {
			t.Errorf("%s %s kept temperature", cfg.URL, cfg.Model)
		}
		if _, ok := body["top_p"]; ok {
			t.Errorf("%s %s kept top_p", cfg.URL, cfg.Model)
		}
		if body["max_tokens"] != 64 {
			t.Errorf("%s %s max_tokens = %#v", cfg.URL, cfg.Model, body["max_tokens"])
		}
	}

	open := []MaclawLLMConfig{
		{URL: "https://api.moonshot.cn/v1", Model: "moonshot-v1-8k"},
		{URL: "https://api.openai.com/v1", Model: "gpt-4o"},
		{URL: "https://api.kimi.com/v1", Model: "other-model"},
		{URL: "https://api.openai.com/v1", Model: "kimi-k20-custom"},
		{URL: "https://evil.example/v1?next=https://api.kimi.com/coding/v1", Model: "gpt-4o"},
	}
	for _, cfg := range open {
		body := map[string]interface{}{"model": cfg.Model, "temperature": 0.2}
		if OmitKimiLockedSampling(cfg, body) {
			t.Errorf("%s %s treated as locked", cfg.URL, cfg.Model)
		}
		if _, ok := body["temperature"]; !ok {
			t.Errorf("%s %s dropped temperature", cfg.URL, cfg.Model)
		}
	}

	// The body can name the upstream model when the saved config still has
	// another id. Router suffixes and underscore spellings are the same model.
	routed := map[string]interface{}{
		"model":             "moonshot/kimi_for_coding:latest",
		"temperature":       0,
		"top_p":             1,
		"n":                 2,
		"presence_penalty":  0.2,
		"frequency_penalty": 0.1,
	}
	if !OmitKimiLockedSampling(MaclawLLMConfig{URL: "https://example.com/v1", Model: "gpt-4o"}, routed) {
		t.Fatal("routed kimi model was not detected")
	}
	for _, key := range []string{"temperature", "top_p", "n", "presence_penalty", "frequency_penalty"} {
		if _, ok := routed[key]; ok {
			t.Fatalf("routed body kept %s", key)
		}
	}

	schemeless := map[string]interface{}{"model": "alias", "temperature": 0.4}
	if !OmitKimiLockedSampling(MaclawLLMConfig{URL: "api.kimi.com/coding/v1", Model: "alias"}, schemeless) {
		t.Fatal("scheme-less Kimi Code URL was not detected")
	}
	if _, ok := schemeless["temperature"]; ok {
		t.Fatal("scheme-less Kimi Code URL kept temperature")
	}
}

func TestForwardDropsKimiTemperature(t *testing.T) {
	body := map[string]interface{}{
		"model":       "kimi-for-coding",
		"messages":    []interface{}{map[string]interface{}{"role": "user", "content": "ping"}},
		"temperature": 0,
		"top_p":       1,
		"n":           2,
	}
	sanitizeOpenAICompatForwardBody(MaclawLLMConfig{
		URL:   "https://api.kimi.com/coding/v1",
		Model: "kimi-for-coding",
	}, body)
	for _, key := range []string{"temperature", "top_p", "n"} {
		if _, ok := body[key]; ok {
			t.Fatalf("forward kept %s = %#v", key, body[key])
		}
	}
	if _, ok := body["messages"]; !ok {
		t.Fatal("forward dropped messages")
	}
}
