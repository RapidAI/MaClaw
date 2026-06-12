package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLiveMaclawHubOpenAIToolsCompat(t *testing.T) {
	if strings.TrimSpace(os.Getenv("MACLAW_HUB_LIVE")) == "" {
		t.Skip("MACLAW_HUB_LIVE not set")
	}
	cfg := loadLiveMaclawHubConfig(t)
	client := &http.Client{Timeout: 90 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	messages := []interface{}{map[string]interface{}{
		"role":    "user",
		"content": "Use the tool to get the weather for Beijing. Do not answer directly.",
	}}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "get_weather",
			"description": "Get current weather for a city.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"city"},
			},
		},
	}}

	resp, err := DoOpenAIRequest(ctx, cfg, messages, tools, client)
	if err != nil {
		t.Fatalf("hub non-stream tools request failed: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("hub non-stream response missing choices: %#v", resp)
	}
	if len(resp.Choices[0].Message.ToolCalls) == 0 {
		t.Fatalf("hub non-stream response missing tool calls: %#v", resp.Choices[0])
	}

	var streamed strings.Builder
	streamResp, err := DoOpenAIRequestStream(ctx, cfg, messages, tools, client, func(token string) {
		streamed.WriteString(token)
	})
	if err != nil {
		t.Fatalf("hub stream tools request failed: %v", err)
	}
	if streamResp == nil || len(streamResp.Choices) == 0 {
		t.Fatalf("hub stream response missing choices: %#v", streamResp)
	}
	if len(streamResp.Choices[0].Message.ToolCalls) == 0 {
		t.Fatalf("hub stream response missing tool calls: %#v streamed=%q", streamResp.Choices[0], streamed.String())
	}
}

func loadLiveMaclawHubConfig(t *testing.T) corelib.MaclawLLMConfig {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".maclaw", "config.json"))
	if err != nil {
		t.Fatalf("read ~/.maclaw/config.json: %v", err)
	}
	var raw struct {
		URL          string `json:"maclaw_llm_url"`
		Key          string `json:"maclaw_llm_key"`
		Model        string `json:"maclaw_llm_model"`
		Protocol     string `json:"maclaw_llm_protocol"`
		ProviderName string `json:"maclaw_llm_current_provider"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse ~/.maclaw/config.json: %v", err)
	}
	cfg := corelib.MaclawLLMConfig{
		URL:          strings.TrimSpace(raw.URL),
		Key:          strings.TrimSpace(raw.Key),
		Model:        strings.TrimSpace(raw.Model),
		Protocol:     strings.TrimSpace(raw.Protocol),
		ProviderName: strings.TrimSpace(raw.ProviderName),
	}
	if cfg.URL == "" || cfg.Key == "" {
		t.Skip("maclaw hub URL/key not configured")
	}
	if cfg.Model == "" {
		cfg.Model = "auto"
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "openai"
	}
	return cfg
}
