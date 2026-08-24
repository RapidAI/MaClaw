package agentservice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// EchoExecutor is a safe default executor used by the scaffold.
// Replace it with a real adapter that boots a per-instance Maclaw runtime.
type EchoExecutor struct{}

func (EchoExecutor) DescribeCapabilities(ctx context.Context, req ExecuteRequest) (*AgentCapabilities, error) {
	_ = ctx
	_ = req
	return &AgentCapabilities{Executor: "echo", SupportsSessions: true}, nil
}

func (EchoExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	_ = ctx
	content := fmt.Sprintf("instance=%s\nmaclaw executor adapter is not wired yet.\nreceived: %s", req.Instance.ID, req.Message.Content)
	if len(req.Message.Attachments) > 0 {
		content += fmt.Sprintf("\nattachments: %d", len(req.Message.Attachments))
	}
	return &ExecuteResult{Content: content, OutputType: "text/plain", Metadata: map[string]string{"agent_id": req.Session.AgentID}}, nil
}

// SimpleLLMExecutor provides a real shared-session LLM response path before the
// full Maclaw runtime is extracted into an importable package.
type SimpleLLMExecutor struct {
	HTTPClient *http.Client
}

func (e SimpleLLMExecutor) DescribeCapabilities(ctx context.Context, req ExecuteRequest) (*AgentCapabilities, error) {
	_ = ctx
	_ = req
	return &AgentCapabilities{Executor: "simple_llm", SupportsSessions: true}, nil
}

func (e SimpleLLMExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	llmCfg, err := ResolveLLMConfig(req.Config)
	if err != nil {
		return nil, err
	}
	messages := buildConversation(req, llmCfg)
	client := e.clientFor(llmCfg)
	respText, err := simpleLLMRequest(ctx, llmCfg, messages, client)
	if err != nil {
		return nil, err
	}
	return &ExecuteResult{
		Content:    respText,
		OutputType: "text/plain",
		Metadata: map[string]string{
			"executor": "simple_llm",
			"agent_id": req.Session.AgentID,
			"provider": llmCfg.ProviderName,
			"model":    llmCfg.Model,
			"protocol": llmCfg.Protocol,
			"wire_api": llmCfg.WireAPI,
		},
	}, nil
}

func (e SimpleLLMExecutor) clientFor(cfg corelib.MaclawLLMConfig) *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second}
}

func TestLLMConfig(ctx context.Context, cfg corelib.AppConfig, client *http.Client) *ConfigTestResult {
	validation := ValidateAppConfig(cfg)
	if !validation.Valid {
		return &ConfigTestResult{
			Success:    false,
			Error:      "configuration is invalid",
			Message:    "Please complete required parameters before testing connectivity.",
			Validation: &validation,
		}
	}

	llmCfg, err := ResolveLLMConfig(cfg)
	if err != nil {
		return &ConfigTestResult{Success: false, Error: err.Error(), Validation: &validation}
	}
	if client == nil {
		client = &http.Client{Timeout: time.Duration(llmCfg.EffectiveTimeoutSec()) * time.Second}
	}

	messages := []interface{}{
		map[string]string{"role": "system", "content": "You are testing whether this MaClawSrv LLM configuration works. Reply with exactly: CONFIG_OK"},
		map[string]string{"role": "user", "content": "Return CONFIG_OK"},
	}

	startedAt := time.Now()
	text, err := simpleLLMRequest(ctx, llmCfg, messages, client)
	latencyMs := time.Since(startedAt).Milliseconds()
	result := &ConfigTestResult{
		Success:      err == nil,
		LatencyMs:    latencyMs,
		Endpoint:     llmCfg.URL,
		ProviderName: llmCfg.ProviderName,
		Model:        llmCfg.Model,
		Protocol:     llmCfg.Protocol,
		WireAPI:      llmCfg.WireAPI,
		Validation:   &validation,
	}
	if err != nil {
		result.Error = err.Error()
		result.Message = "LLM connectivity test failed."
		return result
	}
	result.Message = truncateForDisplay(text)
	if strings.TrimSpace(result.Message) == "" {
		result.Message = "LLM connectivity test succeeded."
	}
	return result
}

func buildConversation(req ExecuteRequest, cfg corelib.MaclawLLMConfig) []interface{} {
	conversation := make([]interface{}, 0, len(req.History)+2)
	conversation = append(conversation, map[string]string{"role": "system", "content": serviceSystemPrompt(req)})
	for _, msg := range req.History {
		if msg.ID == req.Message.ID {
			continue
		}
		role := string(msg.Role)
		if role == "" {
			continue
		}
		conversation = append(conversation, map[string]string{"role": role, "content": msg.Content})
	}
	// SimpleLLMExecutor can serve concurrent tenants in one process. Bind the
	// trusted request config to attachment auto-extraction so one tenant's
	// persisted OfficeRead rollout does not select another tenant's parser.
	// Environment overrides remain honored by the agent resolver for emergency
	// operational rollback.
	userContent := agent.BuildUserContentWithAttachmentStagingDirAndOfficeReadConfigWithContext(req.Message.Content, req.Message.Attachments, cfg.Protocol, cfg.SupportsVision, nil, nil, attachmentStagingDir(req.Instance.Workspace), officeReadConfigFromAppConfig(req.Config), cfg.EffectiveContextTokens())
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})
	return conversation
}

// attachmentStagingDir keeps service attachments within an instance workspace.
// Service-created instances always have a workspace; returning "" for malformed
// direct executor calls preserves the shared desktop fallback instead of
// accidentally creating a relative .attachments directory under the host CWD.
func attachmentStagingDir(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, ".attachments")
}
func serviceSystemPrompt(req ExecuteRequest) string {
	return fmt.Sprintf(
		"You are MaClawSrv, a REST-exposed MaClaw agent runtime. Tenant=%s User=%s Instance=%s Session=%s. Keep answers practical and concise. Match the user's language. Shared user data root: %s.",
		req.Principal.TenantID,
		req.Principal.UserID,
		req.Instance.ID,
		req.Session.ID,
		req.DataDir,
	)
}

func simpleLLMRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (string, error) {
	if !cfg.IsResponsesAPI() && strings.EqualFold(cfg.Protocol, "anthropic") {
		return doSimpleAnthropicRequest(ctx, cfg, messages, client)
	}
	return doSimpleOpenAIRequest(ctx, cfg, messages, client)
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (string, error) {
	var (
		req  *http.Request
		data []byte
		err  error
	)
	if cfg.IsResponsesAPI() {
		req, data, _, err = llm.NewResponsesAPIRequest(ctx, cfg, messages, llm.ResponsesAPIRequestOptions{Stream: false})
	} else {
		req, data, _, err = llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{Stream: false})
	}
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm http %d: body_len=%d req_len=%d", resp.StatusCode, len(body), len(data))
	}

	var parsed *llm.Response
	if cfg.IsResponsesAPI() {
		parsed, err = llm.ParseNonStreamResponsesAPIBody(body)
	} else {
		parsed, err = llm.ParseNonStreamOpenAIResponseBody(body)
	}
	if err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	text = stripThinkingTags(text)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("llm returned empty content")
	}
	return text, nil
}

func doSimpleAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (string, error) {
	resp, err := llm.DoAnthropicRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("anthropic returned no choices")
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		text = resp.Choices[0].Message.ReasoningContent
	}
	if strings.TrimSpace(text) != "" {
		return stripThinkingTags(text), nil
	}
	return "", fmt.Errorf("anthropic returned empty content")
}

func truncateForDisplay(text string) string {
	text = strings.TrimSpace(stripThinkingTags(text))
	if len(text) > 200 {
		return text[:200] + "..."
	}
	return text
}

func stripThinkingTags(s string) string {
	for {
		lower := strings.ToLower(s)
		start := strings.Index(lower, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(lower[start:], "</think>")
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}
