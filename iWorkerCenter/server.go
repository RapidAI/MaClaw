package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/app"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

type CenterProvider struct {
	ID          string
	Name        string
	Protocol    string
	BaseURL     string
	APIKey      string
	Model       string
	Priority    int
	Features    []string
	Description string
	Enabled     bool
	TimeoutSec  int
	CostTier    string
}

type openAIChatRequest struct {
	Model    string             `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
	Stream   bool               `json:"stream,omitempty"`
}

type openAIChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content,omitempty"`
}

type anthropicRequest struct {
	Model     string              `json:"model"`
	Messages  []anthropicMessage  `json:"messages"`
	System    string              `json:"system,omitempty"`
	MaxTokens int                 `json:"max_tokens"`
	Stream    bool                `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type centerSettingsFile struct {
	Providers         []centerProviderFile `json:"providers"`
	WorkTypeKeywords  map[string][]string  `json:"work_type_keywords,omitempty"`
	WorkTypeTier      map[string]string    `json:"work_type_tier,omitempty"`
	RoleProviderBoost map[string][]string  `json:"role_provider_boost,omitempty"`
}

type CenterStatus struct {
	Status        string `json:"status"`
	ProviderCount int    `json:"provider_count"`
	ConfigPath    string `json:"config_path"`
}

type centerProviderFile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Protocol    string   `json:"protocol"`
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	Model       string   `json:"model"`
	Priority    int      `json:"priority"`
	Features    []string `json:"features"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	TimeoutSec  int      `json:"timeout_sec"`
	CostTier    string   `json:"cost_tier"`
}

type centerServer struct {
	addr           string
	providers      []CenterProvider
	routingRules   RoutingRules
	client         *http.Client
	forward        func(context.Context, CenterProvider, openAIChatRequest) ([]byte, error)
	providerLoader func() []CenterProvider
	center         *app.Center
}

// newCenterServer creates the LLM proxy server (used by buildMux).
func newCenterServer(addr string) *centerServer {
	server := &centerServer{
		addr:           addr,
		providers:      defaultCenterProviders(),
		client:         &http.Client{Timeout: 60 * time.Second},
		providerLoader: loadCenterProviders,
	}
	server.forward = server.forwardRequest
	server.refreshProviders()
	return server
}

func (s *centerServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.refreshProviders()
	status, err := centerStatusSnapshot()
	if err != nil {
		writeCenterError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         status.Status,
		"provider_count": status.ProviderCount,
		"config_path":    status.ConfigPath,
	})
}

func (s *centerServer) handleModels(w http.ResponseWriter, _ *http.Request) {
	s.refreshProviders()
	data := make([]map[string]any, 0, len(s.providers))
	for _, provider := range s.providers {
		if !provider.Enabled {
			continue
		}
		data = append(data, map[string]any{
			"id":      provider.ID,
			"object":  "model",
			"owned_by": provider.Protocol,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (s *centerServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.refreshProviders()
	if r.Method != http.MethodPost {
		writeCenterError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		writeCenterError(w, http.StatusBadRequest, "read body failed")
		return
	}

	var req openAIChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeCenterError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Extract task_type from raw body
	var extra struct {
		TaskType string `json:"task_type"`
	}
	_ = json.Unmarshal(body, &extra)

	// Build ClassifyInput
	messageContent := extractMessageText(req.Messages)
	classifyInput := ClassifyInput{
		TaskType:       extra.TaskType,
		MessageContent: messageContent,
		ColleagueName:  "", // future enhancement
	}

	// Use routing rules loaded by refreshProviders (no extra disk IO)
	rules := s.routingRules

	// Classify the request (pure in-memory, should be <1ms)
	classResult := Classify(classifyInput, rules)

	// Generate request ID and prepare audit log fields
	reqID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	summary := messageContent
	if len([]rune(summary)) > 200 {
		summary = string([]rune(summary)[:200])
	}

	// Determine providers: use tier-aware ranking, fall back if classification was too slow
	var providers []CenterProvider
	if classResult.Latency > 10*time.Millisecond {
		// Classification exceeded latency budget, fall back
		providers = s.rankProviders(req)
	} else {
		providers = s.rankProvidersWithTier(req, classResult.CostTier, rules.RoleProviderBoost, "")
	}

	// Log [TaskRoute] audit entry
	providerID := "none"
	if len(providers) > 0 {
		providerID = providers[0].ID
	}
	log.Printf("%s", FormatTaskRouteLog(classResult, reqID, providerID, summary))

	tenantID := tenant.TenantIDFromContext(r.Context())

	// Record audit log (best-effort, non-blocking)
	if s.center != nil && s.center.AuditRepo != nil {
		go func() {
			_ = s.center.AuditRepo.Insert(tenantID, &audit.ProxyLog{
				RequestID:  reqID,
				ProviderID: providerID,
				Model:      req.Model,
				WorkType:   string(classResult.WorkType),
				CostTier:   string(classResult.CostTier),
				Status:     "pending",
				Summary:    summary,
			})
		}()
	}

	if len(providers) == 0 {
		writeCenterError(w, http.StatusServiceUnavailable, "no available provider")
		return
	}

	var lastErr error
	for _, provider := range providers {
		respBody, err := s.forward(r.Context(), provider, req)
		if err == nil {
			// Update audit log to success
			if s.center != nil && s.center.AuditRepo != nil {
				go func(pid string) {
					_ = s.center.AuditRepo.Insert(tenantID, &audit.ProxyLog{
						RequestID:  reqID,
						ProviderID: pid,
						Model:      req.Model,
						WorkType:   string(classResult.WorkType),
						CostTier:   string(classResult.CostTier),
						Status:     "ok",
						LatencyMs:  int(time.Since(now).Milliseconds()),
						Summary:    summary,
					})
				}(provider.ID)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(respBody)
			return
		}
		lastErr = err
		log.Printf("[iWorkerCenter] provider %s failed, fallback next: %v", provider.ID, err)
	}

	// Record failure audit
	if s.center != nil && s.center.AuditRepo != nil {
		errMsg := ""
		if lastErr != nil {
			errMsg = lastErr.Error()
		}
		go func() {
			_ = s.center.AuditRepo.Insert(tenantID, &audit.ProxyLog{
				RequestID:  reqID,
				ProviderID: providerID,
				Model:      req.Model,
				WorkType:   string(classResult.WorkType),
				CostTier:   string(classResult.CostTier),
				Status:     "error",
				LatencyMs:  int(time.Since(now).Milliseconds()),
				Summary:    summary,
				ErrorMsg:   errMsg,
			})
		}()
	}

	writeCenterError(w, http.StatusBadGateway, lastErr.Error())
}

func (s *centerServer) pickProvider(req openAIChatRequest) *CenterProvider {
	providers := s.rankProviders(req)
	if len(providers) == 0 {
		return nil
	}
	provider := providers[0]
	return &provider
}

func (s *centerServer) rankProviders(req openAIChatRequest) []CenterProvider {
	modelHint := strings.TrimSpace(req.Model)
	if modelHint != "" {
		for _, provider := range s.providers {
			if provider.Enabled && strings.EqualFold(provider.ID, modelHint) {
				return []CenterProvider{provider}
			}
		}
	}

	joined := strings.ToLower(extractMessageText(req.Messages))
	type candidate struct {
		provider CenterProvider
		score    int
	}
	candidates := make([]candidate, 0, len(s.providers))
	for _, provider := range s.providers {
		if !provider.Enabled {
			continue
		}
		score := provider.Priority
		for _, feature := range provider.Features {
			if strings.Contains(joined, strings.ToLower(feature)) {
				score += 20
			}
		}
		candidates = append(candidates, candidate{provider: provider, score: score})
	}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	providers := make([]CenterProvider, 0, len(candidates))
	for _, item := range candidates {
		providers = append(providers, item.provider)
	}
	return providers
}

func (s *centerServer) rankProvidersWithTier(
	req openAIChatRequest,
	costTier string,
	roleBoost map[string][]string,
	roleCode string,
) []CenterProvider {
	// Step 1: If model explicitly specifies a provider ID, bypass tier filtering
	modelHint := strings.TrimSpace(req.Model)
	if modelHint != "" {
		for _, provider := range s.providers {
			if provider.Enabled && strings.EqualFold(provider.ID, modelHint) {
				return []CenterProvider{provider}
			}
		}
	}

	// Step 2: Filter enabled providers by matching CostTier
	joined := strings.ToLower(extractMessageText(req.Messages))
	boostedIDs := make(map[string]bool)
	if roleCode != "" && roleBoost != nil {
		for _, id := range roleBoost[roleCode] {
			boostedIDs[id] = true
		}
	}

	type candidate struct {
		provider CenterProvider
		score    int
	}
	var candidates []candidate
	for _, provider := range s.providers {
		if !provider.Enabled {
			continue
		}
		if !strings.EqualFold(provider.CostTier, costTier) {
			continue
		}
		score := provider.Priority
		// Feature match bonus: +20 per matching feature keyword
		for _, feature := range provider.Features {
			if strings.Contains(joined, strings.ToLower(feature)) {
				score += 20
			}
		}
		// Role boost: +10 if provider is in the role's preferred list
		if boostedIDs[provider.ID] {
			score += 10
		}
		candidates = append(candidates, candidate{provider: provider, score: score})
	}

	// Step 6: If no providers match the tier, fall back to existing rankProviders
	if len(candidates) == 0 {
		return s.rankProviders(req)
	}

	// Step 5: Sort by score descending
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	providers := make([]CenterProvider, 0, len(candidates))
	for _, item := range candidates {
		providers = append(providers, item.provider)
	}

	// Append providers from other tiers as cross-tier fallback (Req 3.3)
	tierIDs := make(map[string]bool, len(providers))
	for _, p := range providers {
		tierIDs[p.ID] = true
	}
	fallback := s.rankProviders(req)
	for _, p := range fallback {
		if !tierIDs[p.ID] {
			providers = append(providers, p)
		}
	}

	return providers
}

func extractMessageText(messages []openAIChatMessage) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		switch content := msg.Content.(type) {
		case string:
			parts = append(parts, content)
		case []any:
			for _, item := range content {
				if block, ok := item.(map[string]any); ok {
					if text, ok := block["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (s *centerServer) forwardRequest(ctx context.Context, provider CenterProvider, req openAIChatRequest) ([]byte, error) {
	if strings.EqualFold(provider.Protocol, "anthropic") {
		return s.forwardAnthropic(ctx, provider, req)
	}
	return s.forwardOpenAI(ctx, provider, req)
}

func (s *centerServer) forwardOpenAI(ctx context.Context, provider CenterProvider, req openAIChatRequest) ([]byte, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream openai status=%d body=%s", resp.StatusCode, truncateForLog(body))
	}
	return body, nil
}

func (s *centerServer) forwardAnthropic(ctx context.Context, provider CenterProvider, req openAIChatRequest) ([]byte, error) {
	anthReq := anthropicRequest{
		Model:     provider.Model,
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
		MaxTokens: 4096,
		Stream:    false,
	}
	for _, msg := range req.Messages {
		content := messageContentString(msg.Content)
		if msg.Role == "system" {
			if anthReq.System == "" {
				anthReq.System = content
			} else if content != "" {
				anthReq.System += "\n" + content
			}
			continue
		}
		anthReq.Messages = append(anthReq.Messages, anthropicMessage{
			Role:    msg.Role,
			Content: content,
		})
	}
	payload, err := json.Marshal(anthReq)
	if err != nil {
		return nil, err
	}
	endpoint := corelib.AnthropicMessagesEndpoint(provider.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(httpReq, provider.APIKey)
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream anthropic status=%d body=%s", resp.StatusCode, truncateForLog(body))
	}
	parsed, err := llm.ParseNonStreamOpenAIResponseBody(convertAnthropicResponseToOpenAIBody(body, provider.ID))
	if err == nil && parsed != nil {
		return convertResponseToOpenAIBody(parsed, provider.ID)
	}
	converted, err := convertAnthropicBodyDirect(body, provider.ID)
	if err != nil {
		return nil, err
	}
	return converted, nil
}

func defaultCenterProviders() []CenterProvider {
	return []CenterProvider{
		{
			ID:          "office-openai",
			Name:        "办公写作服务",
			Protocol:    "openai",
			BaseURL:     "https://office.example.com/v1",
			Model:       "gpt-4.1",
			Priority:    100,
			Features:    []string{"公文", "纪要", "中文", "办公"},
			Description: "适合通知、纪要、日报与正式文档。",
			Enabled:     true,
			TimeoutSec:  60,
			CostTier:    "high",
		},
		{
			ID:          "analysis-anthropic",
			Name:        "分析归因服务",
			Protocol:    "anthropic",
			BaseURL:     "https://analysis.example.com",
			Model:       "claude-sonnet-4-6",
			Priority:    90,
			Features:    []string{"分析", "归因", "质量"},
			Description: "适合异常说明、质量分析与整改建议。",
			Enabled:     true,
			TimeoutSec:  60,
			CostTier:    "high",
		},
	}
}

func (s *centerServer) refreshProviders() {
	if s.providerLoader == nil {
		return
	}

	// Read settings once, derive both providers and routing rules
	settings, err := readCenterSettings()
	if err != nil {
		s.providers = defaultCenterProviders()
		s.routingRules = DefaultRoutingRules()
		return
	}

	providers := normalizeCenterProviders(settings)
	if len(providers) == 0 {
		providers = defaultCenterProviders()
	}
	s.providers = providers

	s.routingRules = RoutingRules{
		WorkTypeKeywords:  settings.WorkTypeKeywords,
		WorkTypeTier:      settings.WorkTypeTier,
		RoleProviderBoost: settings.RoleProviderBoost,
	}.MergeWithDefaults()
}

func loadCenterProviders() []CenterProvider {
	settings, err := readCenterSettings()
	if err != nil {
		return defaultCenterProviders()
	}
	return normalizeCenterProviders(settings)
}

func loadCenterProvidersAndRules() ([]CenterProvider, RoutingRules) {
	settings, err := readCenterSettings()
	if err != nil {
		return defaultCenterProviders(), DefaultRoutingRules()
	}
	providers := normalizeCenterProviders(settings)
	rules := RoutingRules{
		WorkTypeKeywords:  settings.WorkTypeKeywords,
		WorkTypeTier:      settings.WorkTypeTier,
		RoleProviderBoost: settings.RoleProviderBoost,
	}
	rules = rules.MergeWithDefaults()
	return providers, rules
}

func readCenterSettings() (centerSettingsFile, error) {
	path, err := centerSettingsPath()
	if err != nil {
		return centerSettingsFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return centerSettingsFile{Providers: defaultCenterProviderFiles()}, nil
		}
		return centerSettingsFile{}, err
	}
	var settings centerSettingsFile
	if err := json.Unmarshal(data, &settings); err != nil {
		return centerSettingsFile{}, err
	}
	if len(settings.Providers) == 0 {
		settings.Providers = defaultCenterProviderFiles()
	}
	return settings, nil
}

func writeCenterSettings(settings centerSettingsFile) error {
	path, err := centerSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	settings.Providers = normalizeCenterProviderFiles(settings.Providers)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func normalizeCenterProviders(settings centerSettingsFile) []CenterProvider {
	providers := make([]CenterProvider, 0, len(settings.Providers))
	for _, provider := range normalizeCenterProviderFiles(settings.Providers) {
		baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
		if strings.TrimSpace(provider.ID) == "" || baseURL == "" || strings.TrimSpace(provider.Model) == "" {
			continue
		}
		costTier := strings.TrimSpace(provider.CostTier)
		if costTier == "" {
			costTier = "medium"
		}
		providers = append(providers, CenterProvider{
			ID:          strings.TrimSpace(provider.ID),
			Name:        strings.TrimSpace(provider.Name),
			Protocol:    strings.TrimSpace(provider.Protocol),
			BaseURL:     baseURL,
			APIKey:      strings.TrimSpace(provider.APIKey),
			Model:       strings.TrimSpace(provider.Model),
			Priority:    provider.Priority,
			Features:    provider.Features,
			Description: strings.TrimSpace(provider.Description),
			Enabled:     provider.Enabled,
			TimeoutSec:  provider.TimeoutSec,
			CostTier:    costTier,
		})
	}
	if len(providers) == 0 {
		return defaultCenterProviders()
	}
	return providers
}

func normalizeCenterProviderFiles(providers []centerProviderFile) []centerProviderFile {
	if len(providers) == 0 {
		return defaultCenterProviderFiles()
	}
	normalized := make([]centerProviderFile, 0, len(providers))
	for _, provider := range providers {
		protocol := strings.TrimSpace(provider.Protocol)
		if protocol == "" {
			protocol = "openai"
		}
		timeoutSec := provider.TimeoutSec
		if timeoutSec <= 0 {
			timeoutSec = 60
		}
		features := provider.Features
		if features == nil {
			features = []string{}
		}
		costTier := strings.TrimSpace(provider.CostTier)
		if costTier == "" {
			costTier = "medium"
		}
		normalized = append(normalized, centerProviderFile{
			ID:          strings.TrimSpace(provider.ID),
			Name:        strings.TrimSpace(provider.Name),
			Protocol:    protocol,
			BaseURL:     strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/"),
			APIKey:      strings.TrimSpace(provider.APIKey),
			Model:       strings.TrimSpace(provider.Model),
			Priority:    provider.Priority,
			Features:    features,
			Description: strings.TrimSpace(provider.Description),
			Enabled:     provider.Enabled,
			TimeoutSec:  timeoutSec,
			CostTier:    costTier,
		})
	}
	return normalized
}

func defaultCenterProviderFiles() []centerProviderFile {
	defaults := defaultCenterProviders()
	providers := make([]centerProviderFile, 0, len(defaults))
	for _, provider := range defaults {
		providers = append(providers, centerProviderFile{
			ID:          provider.ID,
			Name:        provider.Name,
			Protocol:    provider.Protocol,
			BaseURL:     provider.BaseURL,
			APIKey:      provider.APIKey,
			Model:       provider.Model,
			Priority:    provider.Priority,
			Features:    append([]string(nil), provider.Features...),
			Description: provider.Description,
			Enabled:     provider.Enabled,
			TimeoutSec:  provider.TimeoutSec,
			CostTier:    provider.CostTier,
		})
	}
	return providers
}

func centerStatusSnapshot() (CenterStatus, error) {
	path, err := centerSettingsPath()
	if err != nil {
		return CenterStatus{}, err
	}
	providers := loadCenterProviders()
	return CenterStatus{
		Status:        "ok",
		ProviderCount: len(providers),
		ConfigPath:    path,
	}, nil
}

func centerSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".iworkercenter", "settings.json"), nil
}

func convertAnthropicResponseToOpenAIBody(body []byte, model string) []byte {
	converted, err := convertAnthropicBodyDirect(body, model)
	if err != nil {
		return body
	}
	return converted
}

func convertAnthropicBodyDirect(body []byte, model string) ([]byte, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	textParts := make([]string, 0, len(resp.Content))
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	openai := map[string]any{
		"id":      fmt.Sprintf("chatcmpl-center-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": strings.Join(textParts, "\n"),
				},
				"finish_reason": "stop",
			},
		},
	}
	return json.Marshal(openai)
}

func convertResponseToOpenAIBody(resp *llm.Response, model string) ([]byte, error) {
	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	wire := map[string]any{
		"id":      fmt.Sprintf("chatcmpl-center-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
	return json.Marshal(wire)
}

func messageContentString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func writeCenterError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
		},
	})
}

func truncateForLog(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 400 {
		return text[:400] + "..."
	}
	return text
}
