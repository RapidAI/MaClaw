package llm

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLiveOpenAISDKCompatProviders(t *testing.T) {
	providers := []struct {
		name   string
		envKey string
		cfg    corelib.MaclawLLMConfig
	}{
		{
			name:   "deepseek",
			envKey: "DEEPSEEK_API_KEY",
			cfg: corelib.MaclawLLMConfig{
				URL:       "https://api.deepseek.com/v1",
				Model:     "deepseek-v4-flash",
				Protocol:  "openai",
				AgentType: "opencode",
			},
		},
		{
			name:   "glm-coding-plan",
			envKey: "BIGMODEL_API_KEY",
			cfg: corelib.MaclawLLMConfig{
				URL:       "https://open.bigmodel.cn/api/coding/paas/v4",
				Model:     "glm-5.1",
				Protocol:  "openai",
				AgentType: "Kilo Code",
			},
		},
	}

	for _, provider := range providers {
		t.Run(provider.name, func(t *testing.T) {
			apiKey := strings.TrimSpace(os.Getenv(provider.envKey))
			if apiKey == "" {
				t.Skipf("%s not set", provider.envKey)
			}
			cfg := provider.cfg
			cfg.Key = apiKey
			client := &http.Client{Timeout: 90 * time.Second}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			messages := []interface{}{map[string]interface{}{
				"role":    "user",
				"content": "Reply with exactly: ok",
			}}

			resp, err := DoOpenAIRequest(ctx, cfg, messages, nil, client)
			if err != nil {
				t.Fatalf("non-stream request failed: %v", err)
			}
			if resp == nil || len(resp.Choices) == 0 {
				t.Fatalf("non-stream response missing choices: %#v", resp)
			}
			if strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
				t.Fatalf("non-stream response empty content: %#v", resp.Choices[0])
			}

			var streamed strings.Builder
			streamResp, err := DoOpenAIRequestStream(ctx, cfg, messages, nil, client, func(token string) {
				streamed.WriteString(token)
			})
			if err != nil {
				t.Fatalf("stream request failed: %v", err)
			}
			if streamResp == nil || len(streamResp.Choices) == 0 {
				t.Fatalf("stream response missing choices: %#v", streamResp)
			}
			if strings.TrimSpace(streamResp.Choices[0].Message.Content) == "" && strings.TrimSpace(streamed.String()) == "" {
				t.Fatalf("stream response empty content: %#v streamed=%q", streamResp.Choices[0], streamed.String())
			}
		})
	}
}

func TestLiveAnthropicSDKCompatProviders(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("BIGMODEL_API_KEY"))
	if apiKey == "" {
		t.Skip("BIGMODEL_API_KEY not set")
	}
	cfg := corelib.MaclawLLMConfig{
		URL:       "https://open.bigmodel.cn/api/anthropic",
		Key:       apiKey,
		Model:     "glm-5.1",
		Protocol:  "anthropic",
		AgentType: "claude code 2.0",
	}
	client := &http.Client{Timeout: 90 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	messages := []interface{}{map[string]interface{}{
		"role":    "user",
		"content": "Reply with exactly: ok",
	}}

	resp, err := DoAnthropicRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		t.Fatalf("non-stream request failed: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		t.Fatalf("non-stream response empty: %#v", resp)
	}

	var streamed strings.Builder
	streamResp, err := DoAnthropicRequestStream(ctx, cfg, messages, nil, client, func(token string) {
		streamed.WriteString(token)
	})
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	if streamResp == nil || len(streamResp.Choices) == 0 {
		t.Fatalf("stream response missing choices: %#v", streamResp)
	}
	if strings.TrimSpace(streamResp.Choices[0].Message.Content) == "" && strings.TrimSpace(streamed.String()) == "" {
		t.Fatalf("stream response empty: %#v streamed=%q", streamResp.Choices[0], streamed.String())
	}
}
