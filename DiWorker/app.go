package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

var doSimpleLLMRequest = agent.DoSimpleLLMRequest

type App struct {
	ctx context.Context
}

type AppInfo struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

type Colleague struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
}

type TaskItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Owner       string `json:"owner"`
	Status      string `json:"status"`
	UpdatedAt   string `json:"updated_at"`
	Description string `json:"description"`
}

type HistoryTaskItem struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Owner          string `json:"owner"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updated_at"`
	Description    string `json:"description"`
	Draft          string `json:"draft,omitempty"`
	ExpectedOutput string `json:"expected_output,omitempty"`
	Result         string `json:"result,omitempty"`
	Model          string `json:"model,omitempty"`
}

type WelcomeData struct {
	Greeting    string      `json:"greeting"`
	Hint        string      `json:"hint"`
	Colleagues  []Colleague `json:"colleagues"`
	QuickTasks  []string    `json:"quick_tasks"`
	RecentTasks []TaskItem  `json:"recent_tasks"`
}

type SubmitTaskRequest struct {
	TaskType              string `json:"task_type"`
	SelectedColleagueName string `json:"selected_colleague_name"`
	Draft                 string `json:"draft"`
	ExpectedOutput        string `json:"expected_output"`
}

type SubmitTaskResult struct {
	TaskType       string `json:"task_type"`
	ColleagueName  string `json:"colleague_name"`
	ExpectedOutput string `json:"expected_output"`
	Model          string `json:"model"`
	Content        string `json:"content"`
}

type RoleProfile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CenterConfig struct {
	Enabled    bool   `json:"enabled"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	BaseURL    string `json:"base_url"`
	TimeoutSec int    `json:"timeout_sec"`
}

type RoutingPolicy struct {
	Mode            string `json:"mode"`
	DefaultProvider string `json:"default_provider"`
	AllowFallback   bool   `json:"allow_fallback"`
}

type ProviderCapabilities struct {
	SupportsStream bool `json:"supports_stream"`
	SupportsVision bool `json:"supports_vision"`
	MaxContext     int  `json:"max_context"`
}

type UpstreamProvider struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Enabled      bool                 `json:"enabled"`
	Protocol     string               `json:"protocol"`
	BaseURL      string               `json:"base_url"`
	APIKey       string               `json:"api_key"`
	Model        string               `json:"model"`
	Priority     int                  `json:"priority"`
	Features     []string             `json:"features"`
	Description  string               `json:"description"`
	Capabilities ProviderCapabilities `json:"capabilities"`
}

type DiWorkerSettings struct {
	RoleProfile RoleProfile        `json:"role_profile"`
	Center      CenterConfig       `json:"center"`
	Routing     RoutingPolicy      `json:"routing"`
	Providers   []UpstreamProvider `json:"providers"`
}

type CenterHealthStatus struct {
	Reachable      bool   `json:"reachable"`
	Status         string `json:"status"`
	ProviderCount  int    `json:"provider_count"`
	ConfigPath     string `json:"config_path"`
	Message        string `json:"message"`
	ResolvedBaseURL string `json:"resolved_base_url"`
}

type centerSyncSettingsFile struct {
	Providers []centerSyncProviderFile `json:"providers"`
}

type centerSyncProviderFile struct {
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
}

type maclawConfigFile struct {
	MaclawLLMUrl             string              `json:"maclaw_llm_url"`
	MaclawLLMKey             string              `json:"maclaw_llm_key"`
	MaclawLLMModel           string              `json:"maclaw_llm_model"`
	MaclawLLMProtocol        string              `json:"maclaw_llm_protocol"`
	MaclawLLMContextLength   int                 `json:"maclaw_llm_context_length"`
	MaclawLLMTimeoutSec      int                 `json:"maclaw_llm_timeout_sec"`
	MaclawLLMProviders       []maclawLLMProvider `json:"maclaw_llm_providers"`
	MaclawLLMCurrentProvider string              `json:"maclaw_llm_current_provider"`
}

type maclawLLMProvider struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Key            string `json:"key"`
	Model          string `json:"model"`
	Protocol       string `json:"protocol,omitempty"`
	ContextLength  int    `json:"context_length,omitempty"`
	TimeoutSec     int    `json:"timeout_sec,omitempty"`
	SupportsVision bool   `json:"supports_vision"`
	AgentType      string `json:"agent_type,omitempty"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Name:    "DiWorker",
		Tagline: "你的数字化同事",
	}
}

// FetchColleagues returns colleagues from iWorkerCenter if available, otherwise local defaults.
// Exposed as a Wails binding so the frontend can refresh colleague data independently.
func (a *App) FetchColleagues() []Colleague {
	settings, _ := readDiWorkerSettings()
	if settings.Center.Enabled {
		baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
		if baseURL == "" {
			baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
		}
		if centerColleagues := fetchCenterColleagues(baseURL, 5); len(centerColleagues) > 0 {
			colleagues := make([]Colleague, 0, len(centerColleagues))
			for _, cc := range centerColleagues {
				colleagues = append(colleagues, centerColleagueToLocal(cc))
			}
			return colleagues
		}
	}
	return defaultColleagues()
}

// FetchRoles returns roles from iWorkerCenter if available.
func (a *App) FetchRoles() []CenterRole {
	settings, _ := readDiWorkerSettings()
	if settings.Center.Enabled {
		baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
		if baseURL == "" {
			baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
		}
		if roles := fetchCenterRoles(baseURL, 5); len(roles) > 0 {
			return roles
		}
	}
	return nil
}

// FetchCapabilities returns capabilities from iWorkerCenter.
// If colleagueID is provided, returns only capabilities bound to that colleague.
func (a *App) FetchCapabilities(colleagueID string) []CenterCapability {
	settings, _ := readDiWorkerSettings()
	if settings.Center.Enabled {
		baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
		if baseURL == "" {
			baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
		}
		if caps := fetchCenterCapabilities(baseURL, colleagueID, 5); len(caps) > 0 {
			return caps
		}
	}
	return nil
}

func defaultColleagues() []Colleague {
	return []Colleague{
		{ID: "xiaodi", Name: "小迪", Role: "你的办公同事", Description: "擅长通知、纪要、周报和邮件草稿。", Strengths: []string{"通知", "纪要", "周报", "邮件"}, Tasks: []string{"写通知", "会议纪要", "周报总结", "邮件草稿"}},
		{ID: "aning", Name: "阿宁", Role: "你的数据同事", Description: "擅长表格整理、数据汇总和分析摘要。", Strengths: []string{"表格整理", "数据汇总", "图表分析"}, Tasks: []string{"整理表格", "汇总数据", "生成图表", "写分析摘要"}},
		{ID: "laochen", Name: "老陈", Role: "你的生产同事", Description: "擅长日报、交接班和异常说明。", Strengths: []string{"生产日报", "交接班", "异常汇总"}, Tasks: []string{"生产日报", "交接班记录", "异常说明", "上报摘要"}},
		{ID: "xiaozhou", Name: "小周", Role: "你的质量同事", Description: "擅长问题归类、原因分析和整改建议。", Strengths: []string{"质量说明", "原因分析", "整改建议"}, Tasks: []string{"质量说明", "问题归类", "整改建议", "原因分析"}},
	}
}

func (a *App) GetWelcomeData() WelcomeData {
	greeting := "今天想找哪位同事帮你处理工作？"
	hint := "选一位同事，或者直接告诉我你要做什么。"

	// Try fetching real colleagues from iWorkerCenter
	var colleagues []Colleague
	settings, _ := readDiWorkerSettings()
	if settings.Center.Enabled {
		baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
		if baseURL == "" {
			baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
		}
		if centerColleagues := fetchCenterColleagues(baseURL, 5); len(centerColleagues) > 0 {
			colleagues = make([]Colleague, 0, len(centerColleagues))
			for _, cc := range centerColleagues {
				colleagues = append(colleagues, centerColleagueToLocal(cc))
			}
		}
	}

	// Fallback to local mock data
	if len(colleagues) == 0 {
		colleagues = defaultColleagues()
	}

	// Build quick tasks from colleagues' tasks
	quickTasks := []string{"写通知", "会议纪要", "周报总结", "整理表格", "异常上报", "生产日报"}
	if len(colleagues) > 0 {
		seen := make(map[string]bool)
		var collected []string
		for _, c := range colleagues {
			for _, t := range c.Tasks {
				if !seen[t] && len(collected) < 8 {
					seen[t] = true
					collected = append(collected, t)
				}
			}
		}
		if len(collected) > 0 {
			quickTasks = collected
		}
	}

	// Load recent tasks from history
	recentTasks := []TaskItem{
		{ID: "task-101", Title: "整理今日生产异常", Owner: "老陈", Status: "处理中", UpdatedAt: "今天 15:20", Description: "汇总产线异常并生成汇报摘要"},
		{ID: "task-102", Title: "周会纪要整理", Owner: "小迪", Status: "已完成", UpdatedAt: "今天 11:40", Description: "提炼会议结论和待办事项"},
		{ID: "task-103", Title: "质检问题归类", Owner: "小周", Status: "待确认", UpdatedAt: "昨天 18:05", Description: "按原因和影响范围整理质量问题"},
	}
	if history, err := readTaskHistory(); err == nil && len(history) > 0 {
		items := make([]TaskItem, 0, len(history))
		for _, h := range history {
			if len(items) >= 5 {
				break
			}
			items = append(items, TaskItem{
				ID: h.ID, Title: h.Title, Owner: h.Owner,
				Status: h.Status, UpdatedAt: h.UpdatedAt, Description: h.Description,
			})
		}
		if len(items) > 0 {
			recentTasks = items
		}
	}

	return WelcomeData{
		Greeting:    greeting,
		Hint:        hint,
		Colleagues:  colleagues,
		QuickTasks:  quickTasks,
		RecentTasks: recentTasks,
	}
}

func (a *App) LoadTaskHistory() ([]HistoryTaskItem, error) {
	items, err := readTaskHistory()
	if err != nil {
		if os.IsNotExist(err) {
			return []HistoryTaskItem{}, nil
		}
		return nil, fmt.Errorf("读取任务历史失败: %w", err)
	}
	return items, nil
}

func (a *App) SaveTaskHistory(items []HistoryTaskItem) error {
	if err := writeTaskHistory(items); err != nil {
		return fmt.Errorf("保存任务历史失败: %w", err)
	}
	return nil
}

func (a *App) LoadDiWorkerSettings() (DiWorkerSettings, error) {
	settings, err := readDiWorkerSettings()
	if err != nil {
		if os.IsNotExist(err) {
			return defaultDiWorkerSettings(), nil
		}
		return DiWorkerSettings{}, fmt.Errorf("读取 DiWorker 配置失败: %w", err)
	}
	return normalizeDiWorkerSettings(settings), nil
}

func (a *App) SaveDiWorkerSettings(settings DiWorkerSettings) error {
	normalized := normalizeDiWorkerSettings(settings)
	if err := writeDiWorkerSettings(normalized); err != nil {
		return fmt.Errorf("保存 DiWorker 配置失败: %w", err)
	}
	if err := syncCenterSettings(normalized); err != nil {
		return fmt.Errorf("同步中心配置失败: %w", err)
	}
	return nil
}

func (a *App) CheckCenterHealth() (CenterHealthStatus, error) {
	settings, err := a.LoadDiWorkerSettings()
	if err != nil {
		return CenterHealthStatus{}, err
	}
	return checkCenterHealth(settings)
}

func (a *App) SubmitTask(req SubmitTaskRequest) (SubmitTaskResult, error) {
	draft := strings.TrimSpace(req.Draft)
	if draft == "" {
		return SubmitTaskResult{}, fmt.Errorf("请先填写需求描述")
	}

	taskType := strings.TrimSpace(req.TaskType)
	if taskType == "" {
		taskType = "自由输入"
	}
	colleagueName := strings.TrimSpace(req.SelectedColleagueName)
	if colleagueName == "" {
		colleagueName = "自动匹配同事"
	}
	expectedOutput := strings.TrimSpace(req.ExpectedOutput)
	if expectedOutput == "" {
		expectedOutput = "summary"
	}

	// Build system prompt with shared memory injection
	systemPrompt := "你是 DiWorker 的数字化同事，请使用简体中文直接产出可交付内容。输出要紧贴用户任务，不要解释模型规则，不要输出无关前言。"

	// Fetch shared memories from iWorkerCenter (non-blocking, best-effort)
	settings, _ := readDiWorkerSettings()
	if settings.Center.Enabled {
		baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
		if baseURL == "" {
			baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
		}
		roleCode := colleagueRoleCode(colleagueName)
		memories := fetchSharedMemories(baseURL, roleCode, 5)
		if memoryBlock := buildMemorySystemPrompt(memories); memoryBlock != "" {
			systemPrompt = systemPrompt + "\n\n以下是你需要了解的企业背景和角色知识，请在回答中自然运用这些信息：\n\n" + memoryBlock
		}
	}

	messages := []interface{}{
		map[string]string{
			"role":    "system",
			"content": systemPrompt,
		},
		map[string]string{
			"role": "user",
			"content": fmt.Sprintf("任务类型：%s\n接手同事：%s\n预期输出：%s\n\n请根据以下需求直接生成结果：\n%s", taskType, colleagueName, expectedOutputLabel(expectedOutput), draft),
		},
	}

	primaryCfg, fallbackCfg, err := loadSubmitLLMConfigs()
	if err != nil {
		return SubmitTaskResult{}, err
	}

	resp, usedCfg, err := submitTaskWithFallback(messages, primaryCfg, fallbackCfg)
	if err != nil {
		return SubmitTaskResult{}, fmt.Errorf("提交任务失败: %w", err)
	}

	// Strip thinking tags from reasoning models (DeepSeek, Kimi, QwQ, etc.)
	content := agent.StripThinkingTags(resp.Content)

	return SubmitTaskResult{
		TaskType:       taskType,
		ColleagueName:  colleagueName,
		ExpectedOutput: expectedOutput,
		Model:          usedCfg.Model,
		Content:        strings.TrimSpace(content),
	}, nil
}

func expectedOutputLabel(value string) string {
	switch value {
	case "document":
		return "正式文档"
	case "table":
		return "结构化表格"
	default:
		return "摘要 / 汇报"
	}
}

func taskHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".diworker", "task_history.json"), nil
}

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".diworker", "settings.json"), nil
}

func readTaskHistory() ([]HistoryTaskItem, error) {
	path, err := taskHistoryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []HistoryTaskItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func writeTaskHistory(items []HistoryTaskItem) error {
	path, err := taskHistoryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readDiWorkerSettings() (DiWorkerSettings, error) {
	path, err := settingsPath()
	if err != nil {
		return DiWorkerSettings{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DiWorkerSettings{}, err
	}
	var settings DiWorkerSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return DiWorkerSettings{}, err
	}
	return normalizeDiWorkerSettings(settings), nil
}

func writeDiWorkerSettings(settings DiWorkerSettings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalizeDiWorkerSettings(settings), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func syncCenterSettings(settings DiWorkerSettings) error {
	path, err := centerSyncSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := centerSyncSettingsFile{
		Providers: make([]centerSyncProviderFile, 0, len(settings.Providers)),
	}
	for _, provider := range settings.Providers {
		features := provider.Features
		if features == nil {
			features = []string{}
		}
		protocol := strings.TrimSpace(provider.Protocol)
		if protocol == "" {
			protocol = "openai"
		}
		timeoutSec := settings.Center.TimeoutSec
		if providerTimeout := providerTimeoutSec(provider); providerTimeout > 0 {
			timeoutSec = providerTimeout
		}
		if timeoutSec <= 0 {
			timeoutSec = corelib.DefaultLLMTimeoutSec
		}
		payload.Providers = append(payload.Providers, centerSyncProviderFile{
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
		})
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func centerSyncSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".iworkercenter", "settings.json"), nil
}

func providerTimeoutSec(provider UpstreamProvider) int {
	return 0
}

func checkCenterHealth(settings DiWorkerSettings) (CenterHealthStatus, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimSpace(buildCenterBaseURL(settings.Center.Host, settings.Center.Port))
	}
	if baseURL == "" {
		return CenterHealthStatus{
			Reachable:      false,
			Message:        "未配置数字员工中心地址",
			ResolvedBaseURL: "",
		}, nil
	}
	timeoutSec := settings.Center.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = corelib.DefaultLLMTimeoutSec
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return CenterHealthStatus{
			Reachable:      false,
			Message:        err.Error(),
			ResolvedBaseURL: baseURL,
		}, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return CenterHealthStatus{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return CenterHealthStatus{
			Reachable:      false,
			Message:        fmt.Sprintf("health status=%d", resp.StatusCode),
			ResolvedBaseURL: baseURL,
		}, nil
	}
	var payload struct {
		Status        string `json:"status"`
		ProviderCount int    `json:"provider_count"`
		ConfigPath    string `json:"config_path"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CenterHealthStatus{}, err
	}
	return CenterHealthStatus{
		Reachable:      true,
		Status:         strings.TrimSpace(payload.Status),
		ProviderCount:  payload.ProviderCount,
		ConfigPath:     strings.TrimSpace(payload.ConfigPath),
		Message:        "ok",
		ResolvedBaseURL: baseURL,
	}, nil
}

func loadSubmitLLMConfig() (corelib.MaclawLLMConfig, error) {
	primaryCfg, _, err := loadSubmitLLMConfigs()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	return primaryCfg, nil
}

func loadSubmitLLMConfigs() (corelib.MaclawLLMConfig, *corelib.MaclawLLMConfig, error) {
	settings, err := readDiWorkerSettings()
	if err == nil {
		normalized := normalizeDiWorkerSettings(settings)
		if cfg, ok := centerLLMConfig(normalized); ok {
			fallbackCfg, fallbackErr := loadMaclawLLMConfig()
			if fallbackErr == nil {
				return cfg, &fallbackCfg, nil
			}
			if os.IsNotExist(fallbackErr) {
				return cfg, nil, nil
			}
			return corelib.MaclawLLMConfig{}, nil, fmt.Errorf("读取 LLM 配置失败: %w", fallbackErr)
		}
	} else if !os.IsNotExist(err) {
		return corelib.MaclawLLMConfig{}, nil, fmt.Errorf("读取 DiWorker 配置失败: %w", err)
	}
	fallbackCfg, err := loadMaclawLLMConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, nil, err
	}
	return fallbackCfg, nil, nil
}

func submitTaskWithFallback(messages []interface{}, primaryCfg corelib.MaclawLLMConfig, fallbackCfg *corelib.MaclawLLMConfig) (*agent.LLMSimpleResponse, corelib.MaclawLLMConfig, error) {
	resp, err := runSimpleLLMRequest(primaryCfg, messages)
	if err == nil {
		return resp, primaryCfg, nil
	}
	if fallbackCfg == nil {
		return nil, corelib.MaclawLLMConfig{}, err
	}
	fallbackResp, fallbackErr := runSimpleLLMRequest(*fallbackCfg, messages)
	if fallbackErr != nil {
		return nil, corelib.MaclawLLMConfig{}, fmt.Errorf("center failed: %w; fallback failed: %v", err, fallbackErr)
	}
	return fallbackResp, *fallbackCfg, nil
}

func runSimpleLLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}) (*agent.LLMSimpleResponse, error) {
	client := &http.Client{Timeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second}
	return doSimpleLLMRequest(cfg, messages, client, time.Duration(cfg.EffectiveTimeoutSec())*time.Second)
}

func centerLLMConfig(settings DiWorkerSettings) (corelib.MaclawLLMConfig, bool) {
	if !settings.Center.Enabled {
		return corelib.MaclawLLMConfig{}, false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimSpace(buildCenterBaseURL(settings.Center.Host, settings.Center.Port))
	}
	if baseURL == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	model := strings.TrimSpace(settings.Routing.DefaultProvider)
	if model == "" {
		model = firstEnabledProviderID(settings.Providers)
	}
	if model == "" {
		return corelib.MaclawLLMConfig{}, false
	}
	cfg := corelib.MaclawLLMConfig{
		URL:        baseURL,
		Model:      model,
		Protocol:   "openai",
		TimeoutSec: settings.Center.TimeoutSec,
		Key:        firstProviderAPIKey(settings.Providers, model),
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = corelib.DefaultLLMTimeoutSec
	}
	return cfg, true
}

func buildCenterBaseURL(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func firstEnabledProviderID(providers []UpstreamProvider) string {
	for _, provider := range providers {
		if provider.Enabled && strings.TrimSpace(provider.ID) != "" {
			return strings.TrimSpace(provider.ID)
		}
	}
	return ""
}

func firstProviderAPIKey(providers []UpstreamProvider, providerID string) string {
	for _, provider := range providers {
		if strings.TrimSpace(provider.ID) == strings.TrimSpace(providerID) {
			return strings.TrimSpace(provider.APIKey)
		}
	}
	return ""
}

func defaultDiWorkerSettings() DiWorkerSettings {
	return DiWorkerSettings{
		RoleProfile: RoleProfile{
			Name:        "小迪",
			Description: "你的数字办公助理，擅长通知、纪要与汇报整理。",
		},
		Center: CenterConfig{
			Enabled:    false,
			Host:       "127.0.0.1",
			Port:       9377,
			BaseURL:    "http://127.0.0.1:9377",
			TimeoutSec: 60,
		},
		Routing: RoutingPolicy{
			Mode:            "smart",
			DefaultProvider: "office-openai",
			AllowFallback:   true,
		},
		Providers: []UpstreamProvider{
			{
				ID:          "office-openai",
				Name:        "办公写作服务",
				Enabled:     true,
				Protocol:    "openai",
				BaseURL:     "https://office.example.com/v1",
				APIKey:      "",
				Model:       "gpt-4.1",
				Priority:    100,
				Features:    []string{"公文", "纪要", "中文"},
				Description: "适合通知、纪要、日报与正式文档。",
				Capabilities: ProviderCapabilities{
					SupportsStream: true,
					SupportsVision: false,
					MaxContext:     128000,
				},
			},
			{
				ID:          "analysis-anthropic",
				Name:        "分析归因服务",
				Enabled:     true,
				Protocol:    "anthropic",
				BaseURL:     "https://analysis.example.com",
				APIKey:      "",
				Model:       "claude-sonnet-4-6",
				Priority:    90,
				Features:    []string{"分析", "归因", "质量"},
				Description: "适合异常说明、质量分析与整改建议。",
				Capabilities: ProviderCapabilities{
					SupportsStream: true,
					SupportsVision: false,
					MaxContext:     200000,
				},
			},
		},
	}
}

func normalizeDiWorkerSettings(settings DiWorkerSettings) DiWorkerSettings {
	defaults := defaultDiWorkerSettings()

	if strings.TrimSpace(settings.RoleProfile.Name) == "" {
		settings.RoleProfile.Name = defaults.RoleProfile.Name
	}
	if strings.TrimSpace(settings.RoleProfile.Description) == "" {
		settings.RoleProfile.Description = defaults.RoleProfile.Description
	}
	if strings.TrimSpace(settings.Center.Host) == "" {
		settings.Center.Host = defaults.Center.Host
	}
	if settings.Center.Port <= 0 {
		settings.Center.Port = defaults.Center.Port
	}
	if strings.TrimSpace(settings.Center.BaseURL) == "" {
		settings.Center.BaseURL = defaults.Center.BaseURL
	}
	if settings.Center.TimeoutSec <= 0 {
		settings.Center.TimeoutSec = defaults.Center.TimeoutSec
	}
	if strings.TrimSpace(settings.Routing.Mode) == "" {
		settings.Routing.Mode = defaults.Routing.Mode
	}
	if strings.TrimSpace(settings.Routing.DefaultProvider) == "" {
		settings.Routing.DefaultProvider = defaults.Routing.DefaultProvider
	}
	if len(settings.Providers) == 0 {
		settings.Providers = defaults.Providers
	}

	for i := range settings.Providers {
		provider := &settings.Providers[i]
		if provider.Features == nil {
			provider.Features = []string{}
		}
		if strings.TrimSpace(provider.Protocol) == "" {
			provider.Protocol = "openai"
		}
		if provider.Capabilities.MaxContext <= 0 {
			provider.Capabilities.MaxContext = defaultProviderMaxContext(provider.ID, defaults.Providers)
		}
	}

	return settings
}

func defaultProviderMaxContext(providerID string, providers []UpstreamProvider) int {
	for _, provider := range providers {
		if provider.ID == providerID {
			return provider.Capabilities.MaxContext
		}
	}
	return 128000
}

func loadMaclawLLMConfig() (corelib.MaclawLLMConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("读取用户目录失败: %w", err)
	}

	configPath := filepath.Join(home, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("读取 LLM 配置失败: %w", err)
	}

	var cfgFile maclawConfigFile
	if err := json.Unmarshal(data, &cfgFile); err != nil {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("解析 LLM 配置失败: %w", err)
	}

	if cfg, ok := currentProviderConfig(cfgFile); ok {
		return cfg, nil
	}

	cfg := corelib.MaclawLLMConfig{
		URL:            strings.TrimRight(strings.TrimSpace(cfgFile.MaclawLLMUrl), "/"),
		Key:            strings.TrimSpace(cfgFile.MaclawLLMKey),
		Model:          strings.TrimSpace(cfgFile.MaclawLLMModel),
		Protocol:       strings.TrimSpace(cfgFile.MaclawLLMProtocol),
		ContextLength:  cfgFile.MaclawLLMContextLength,
		TimeoutSec:     cfgFile.MaclawLLMTimeoutSec,
		SupportsVision: false,
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("未找到可用的 LLM 配置，请先在主程序中配置模型")
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = corelib.DefaultLLMTimeoutSec
	}
	return cfg, nil
}

func currentProviderConfig(cfgFile maclawConfigFile) (corelib.MaclawLLMConfig, bool) {
	current := strings.TrimSpace(cfgFile.MaclawLLMCurrentProvider)
	for _, provider := range cfgFile.MaclawLLMProviders {
		if strings.TrimSpace(provider.Name) != current {
			continue
		}
		cfg := corelib.MaclawLLMConfig{
			URL:            strings.TrimRight(strings.TrimSpace(provider.URL), "/"),
			Key:            strings.TrimSpace(provider.Key),
			Model:          strings.TrimSpace(provider.Model),
			Protocol:       strings.TrimSpace(provider.Protocol),
			ContextLength:  provider.ContextLength,
			TimeoutSec:     provider.TimeoutSec,
			SupportsVision: provider.SupportsVision,
			AgentType:      provider.AgentType,
		}
		if cfg.URL == "" || cfg.Model == "" {
			return corelib.MaclawLLMConfig{}, false
		}
		if cfg.TimeoutSec <= 0 {
			cfg.TimeoutSec = corelib.DefaultLLMTimeoutSec
		}
		return cfg, true
	}
	return corelib.MaclawLLMConfig{}, false
}

// --- Collaboration Wails bindings ---

// FetchCollaborations returns collaboration tasks from iWorkerCenter.
// If colleagueID is provided, returns only tasks assigned to that colleague.
func (a *App) FetchCollaborations(colleagueID string) []CenterCollabTask {
	settings, _ := readDiWorkerSettings()
	if !settings.Center.Enabled {
		return nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
	if baseURL == "" {
		baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
	}
	return fetchCenterCollaborations(baseURL, colleagueID, 5)
}

// FetchWorkflowInstances returns workflow instances from iWorkerCenter.
func (a *App) FetchWorkflowInstances() []CenterWorkflowInstance {
	settings, _ := readDiWorkerSettings()
	if !settings.Center.Enabled {
		return nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.Center.BaseURL), "/")
	if baseURL == "" {
		baseURL = buildCenterBaseURL(settings.Center.Host, settings.Center.Port)
	}
	return fetchCenterWorkflowInstances(baseURL, 5)
}
