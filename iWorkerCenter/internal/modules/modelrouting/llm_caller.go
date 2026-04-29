package modelrouting

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
)

const ExperienceExtractionWorkType = "experience_extraction"

type LLMCaller struct {
	read  *sql.DB
	audit *audit.Repo
	proxy *corelib.LLMEndpointProxy
}

func NewLLMCaller(read *sql.DB, auditRepo *audit.Repo) *LLMCaller {
	return &LLMCaller{read: read, audit: auditRepo, proxy: corelib.NewLLMEndpointProxy()}
}

func (c *LLMCaller) TenantExtractFunc() func(tenantID, systemPrompt, userPrompt string) (string, error) {
	return func(tenantID, systemPrompt, userPrompt string) (string, error) {
		return c.Chat(context.Background(), tenantID, ExperienceExtractionWorkType, "", systemPrompt, userPrompt)
	}
}

func (c *LLMCaller) Chat(ctx context.Context, tenantID, workType, roleCode, systemPrompt, userPrompt string) (string, error) {
	if c == nil || c.read == nil {
		return "", fmt.Errorf("model routing caller is not configured")
	}
	provider, err := c.resolveProvider(ctx, tenantID, workType, roleCode)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"model":       provider.Model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	started := time.Now()
	result, err := c.proxy.ForwardProviderRequest(ctx, provider, body, provider.Model)
	latencyMs := time.Since(started).Milliseconds()
	if err != nil {
		c.recordAudit(tenantID, provider.ID, provider.Model, workType, "error", latencyMs, "", err.Error())
		return "", err
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		errMsg := fmt.Sprintf("upstream returned HTTP %d: %s", result.StatusCode, trimForAudit(string(result.Body), 500))
		c.recordAudit(tenantID, provider.ID, provider.Model, workType, "error", latencyMs, "", errMsg)
		return "", fmt.Errorf("%s", errMsg)
	}
	content, err := openAICompatContent(result.Body)
	if err != nil {
		c.recordAudit(tenantID, provider.ID, provider.Model, workType, "error", latencyMs, "", err.Error())
		return "", err
	}
	c.recordAudit(tenantID, provider.ID, provider.Model, workType, "ok", latencyMs, trimForAudit(content, 240), "")
	return content, nil
}

func (c *LLMCaller) resolveProvider(ctx context.Context, tenantID, workType, roleCode string) (corelib.LLMEndpointProvider, error) {
	workType = strings.TrimSpace(workType)
	if workType == "" {
		workType = "*"
	}
	roleCode = strings.TrimSpace(roleCode)
	var endpointID string
	if err := c.read.QueryRowContext(ctx, `SELECT endpoint_id FROM model_routing_policies
		WHERE tenant_id=? AND status='active' AND endpoint_id!=''
		  AND (work_type=? OR work_type='*') AND (role_code=? OR role_code='*')
		ORDER BY CASE WHEN work_type=? THEN 0 ELSE 1 END, CASE WHEN role_code=? THEN 0 ELSE 1 END, priority DESC
		LIMIT 1`, tenantID, workType, roleCode, workType, roleCode).Scan(&endpointID); err != nil && err != sql.ErrNoRows {
		return corelib.LLMEndpointProvider{}, err
	}
	if strings.TrimSpace(endpointID) != "" {
		provider, err := c.providerByID(ctx, tenantID, endpointID)
		if err == nil {
			return provider, nil
		}
		if err != sql.ErrNoRows {
			return corelib.LLMEndpointProvider{}, err
		}
	}
	return c.defaultProvider(ctx, tenantID)
}

func (c *LLMCaller) providerByID(ctx context.Context, tenantID, endpointID string) (corelib.LLMEndpointProvider, error) {
	return c.scanProvider(c.read.QueryRowContext(ctx, `SELECT id, name, protocol, base_url, api_key, model FROM model_endpoints WHERE tenant_id=? AND id=? AND status='active'`, tenantID, endpointID))
}

func (c *LLMCaller) defaultProvider(ctx context.Context, tenantID string) (corelib.LLMEndpointProvider, error) {
	return c.scanProvider(c.read.QueryRowContext(ctx, `SELECT id, name, protocol, base_url, api_key, model FROM model_endpoints WHERE tenant_id=? AND status='active' ORDER BY priority DESC, updated_at DESC LIMIT 1`, tenantID))
}

type providerRow interface {
	Scan(dest ...any) error
}

func (c *LLMCaller) scanProvider(row providerRow) (corelib.LLMEndpointProvider, error) {
	var provider corelib.LLMEndpointProvider
	if err := row.Scan(&provider.ID, &provider.Name, &provider.Protocol, &provider.APIURL, &provider.APIKey, &provider.Model); err != nil {
		return corelib.LLMEndpointProvider{}, err
	}
	if strings.TrimSpace(provider.APIURL) == "" || strings.TrimSpace(provider.Model) == "" {
		return corelib.LLMEndpointProvider{}, fmt.Errorf("model endpoint %s is incomplete", provider.ID)
	}
	provider.UpstreamTimeoutSec = 60
	provider.MaxConcurrency = 2
	provider.MaxQueueWaiters = 16
	provider.QueueTimeoutMS = 5000
	provider.CircuitBreakerThreshold = 3
	provider.CircuitBreakerCooldownMS = 30000
	provider.FailureBackoffBaseMS = 500
	provider.FailureBackoffMaxMS = 5000
	return provider, nil
}

func openAICompatContent(body []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode llm response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm response has no choices")
	}
	switch content := parsed.Choices[0].Message.Content.(type) {
	case string:
		return strings.TrimSpace(content), nil
	case []any:
		parts := []string{}
		for _, item := range content {
			if obj, ok := item.(map[string]any); ok {
				if text, ok := obj["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.TrimSpace(strings.Join(parts, "\n")), nil
		}
	}
	return "", fmt.Errorf("llm response content is empty")
}

func (c *LLMCaller) recordAudit(tenantID, providerID, model, workType, status string, latencyMs int64, summary, errMsg string) {
	if c == nil || c.audit == nil {
		return
	}
	_ = c.audit.Insert(tenantID, &audit.ProxyLog{
		RequestID:  fmt.Sprintf("modelrouting-%s-%d", workType, time.Now().UnixNano()),
		ProviderID: providerID,
		Model:      model,
		WorkType:   workType,
		CostTier:   "internal",
		Status:     status,
		LatencyMs:  int(latencyMs),
		Summary:    summary,
		ErrorMsg:   errMsg,
	})
}

func trimForAudit(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
