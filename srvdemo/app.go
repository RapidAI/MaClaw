package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	ctx context.Context
}

type DemoSettings struct {
	BaseURL       string `json:"base_url"`
	AdminSecret   string `json:"admin_secret"`
	APIKey        string `json:"api_key"`
	APISecret     string `json:"api_secret"`
	AccessToken   string `json:"access_token"`
	SkipTLSVerify bool   `json:"skip_tls_verify"`
	TimeoutSec    int    `json:"timeout_sec"`
}

type LoginInput struct {
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	APISecret     string `json:"api_secret"`
	SkipTLSVerify bool   `json:"skip_tls_verify"`
	TimeoutSec    int    `json:"timeout_sec"`
}

type LoginResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   string `json:"expires_at"`
	Principal   string `json:"principal"`
	Me          string `json:"me"`
}

type APITextResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type CreateInstanceInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SendMessageInput struct {
	InstanceID string `json:"instance_id"`
	SessionID  string `json:"session_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
}

type ConversationQueryInput struct {
	InstanceID string `json:"instance_id"`
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id"`
}

type ConversationRefreshResult struct {
	Session  string `json:"session,omitempty"`
	Messages string `json:"messages,omitempty"`
	Run      string `json:"run,omitempty"`
}

type CreateTenantInput struct {
	Name string `json:"name"`
}

type CreateUserInput struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

type CreateCredentialInput struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

type ProvisionDemoInput struct {
	TenantName     string `json:"tenant_name"`
	UserName       string `json:"user_name"`
	UserEmail      string `json:"user_email"`
	CredentialName string `json:"credential_name"`
	APIKey         string `json:"api_key"`
	APISecret      string `json:"api_secret"`
}

type ProvisionDemoResult struct {
	TenantID       string `json:"tenant_id"`
	UserID         string `json:"user_id"`
	CredentialID   string `json:"credential_id"`
	APIKey         string `json:"api_key"`
	APISecret      string `json:"api_secret"`
	TenantResponse string `json:"tenant_response"`
	UserResponse   string `json:"user_response"`
	CredentialResp string `json:"credential_response"`
}

type QuickStartInput struct {
	ConfigJSON          string `json:"config_json"`
	InstanceName        string `json:"instance_name"`
	InstanceDescription string `json:"instance_description"`
	MessageTitle        string `json:"message_title"`
	MessageContent      string `json:"message_content"`
}

type SkillSearchPayload struct {
	Query            string   `json:"query"`
	Sources          []string `json:"sources,omitempty"`
	TopN             int      `json:"top_n,omitempty"`
	SkillHubURL      string   `json:"skill_hub_url,omitempty"`
	SkillMarketURL   string   `json:"skill_market_url,omitempty"`
	GitHubToken      string   `json:"github_token,omitempty"`
	IncludeInstalled bool     `json:"include_installed,omitempty"`
}

type SkillImprovePayload struct {
	AutoFix bool `json:"auto_fix,omitempty"`
}

type SkillInstallPayload struct {
	Source         string `json:"source"`
	RepoURL        string `json:"repo_url,omitempty"`
	RawURL         string `json:"raw_url,omitempty"`
	RepoFullName   string `json:"repo_full_name,omitempty"`
	FilePath       string `json:"file_path,omitempty"`
	Branch         string `json:"branch,omitempty"`
	DefinitionType string `json:"definition_type,omitempty"`
	ZipBase64      string `json:"zip_base64,omitempty"`
	SkillHubURL    string `json:"skill_hub_url,omitempty"`
	SkillID        string `json:"skill_id,omitempty"`
	Overwrite      bool   `json:"overwrite,omitempty"`
	GitHubToken    string `json:"github_token,omitempty"`
}

type SkillImportPayload struct {
	ZipBase64   string `json:"zip_base64"`
	Overwrite   bool   `json:"overwrite,omitempty"`
	ArchiveName string `json:"archive_name,omitempty"`
}

type SkillUploadPayload struct {
	SkillMarketURL string `json:"skill_market_url,omitempty"`
	Email          string `json:"email"`
}
type MCPServerPayload struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	EndpointURL string            `json:"endpoint_url,omitempty"`
	AuthType    string            `json:"auth_type,omitempty"`
	AuthSecret  string            `json:"auth_secret,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	AutoStart   bool              `json:"auto_start,omitempty"`
}

type MCPServerUpdatePayload struct {
	Name        *string           `json:"name,omitempty"`
	EndpointURL *string           `json:"endpoint_url,omitempty"`
	AuthType    *string           `json:"auth_type,omitempty"`
	AuthSecret  *string           `json:"auth_secret,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     *string           `json:"command,omitempty"`
	Args        *[]string         `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Disabled    *bool             `json:"disabled,omitempty"`
	AutoStart   *bool             `json:"auto_start,omitempty"`
}

type QuickStartResult struct {
	ConfigSaved    string `json:"config_saved,omitempty"`
	Validation     string `json:"validation,omitempty"`
	Instance       string `json:"instance,omitempty"`
	Message        string `json:"message,omitempty"`
	InstanceID     string `json:"instance_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	ConfigIsValid  bool   `json:"config_is_valid"`
	MessageWasSent bool   `json:"message_was_sent"`
}

type tokenResponse struct {
	AccessToken string          `json:"access_token"`
	TokenType   string          `json:"token_type"`
	ExpiresAt   time.Time       `json:"expires_at"`
	Principal   json.RawMessage `json:"principal"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) LoadSettings() (DemoSettings, error) {
	settings, err := loadSettings()
	if err != nil {
		if os.IsNotExist(err) {
			return defaultSettings(), nil
		}
		return DemoSettings{}, err
	}
	return normalizeSettings(settings), nil
}

func (a *App) SaveSettings(settings DemoSettings) (DemoSettings, error) {
	settings = normalizeSettings(settings)
	if err := writeSettings(settings); err != nil {
		return DemoSettings{}, err
	}
	return settings, nil
}

func (a *App) ClearToken() (DemoSettings, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return DemoSettings{}, err
	}
	settings.AccessToken = ""
	return a.SaveSettings(settings)
}

func (a *App) HealthCheck() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/health", nil, requestOptions{})
}

func (a *App) Login(input LoginInput) (*LoginResult, error) {
	settings := normalizeSettings(DemoSettings{
		BaseURL:       input.BaseURL,
		APIKey:        input.APIKey,
		APISecret:     input.APISecret,
		SkipTLSVerify: input.SkipTLSVerify,
		TimeoutSec:    input.TimeoutSec,
	})
	payload := map[string]string{
		"api_key":    strings.TrimSpace(settings.APIKey),
		"api_secret": settings.APISecret,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	result, err := doRequest(settings, http.MethodPost, "/api/v1/auth/token", body, requestOptions{})
	if err != nil {
		return nil, err
	}
	var token tokenResponse
	if err := json.Unmarshal([]byte(result.Body), &token); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	settings.AccessToken = strings.TrimSpace(token.AccessToken)
	if current, loadErr := a.LoadSettings(); loadErr == nil {
		settings.AdminSecret = current.AdminSecret
	}
	if _, err := a.SaveSettings(settings); err != nil {
		return nil, err
	}
	meResult, err := doRequest(settings, http.MethodGet, "/api/v1/me", nil, requestOptions{NeedsAuth: true})
	if err != nil {
		return nil, err
	}
	return &LoginResult{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
		ExpiresAt:   token.ExpiresAt.Format(time.RFC3339),
		Principal:   prettyJSON(token.Principal),
		Me:          meResult.Body,
	}, nil
}

func (a *App) GetMe() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/me", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetConfigSchema() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/config/schema", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetConfig() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/config", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetInstanceCapabilities(instanceID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID+"/capabilities", nil, requestOptions{NeedsAuth: true})
}

func (a *App) UpdateConfig(raw string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := normalizeJSONBody(raw, true)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPut, "/api/v1/config", body, requestOptions{NeedsAuth: true})
}

func (a *App) ValidateConfig(raw string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := normalizeJSONBody(raw, false)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/config/validate", body, requestOptions{NeedsAuth: true})
}

func (a *App) TestConfig(raw string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := normalizeJSONBody(raw, false)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/config/test", body, requestOptions{NeedsAuth: true})
}

func (a *App) GetUsageSummary() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/usage/summary", nil, requestOptions{NeedsAuth: true})
}

func (a *App) ListSkills() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/skills", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetSkill(name string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill_name is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/skills/"+name, nil, requestOptions{NeedsAuth: true})
}

func (a *App) SearchSkills(input SkillSearchPayload) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/skills/search", body, requestOptions{NeedsAuth: true})
}

func (a *App) ValidateSkill(name string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill_name is required")
	}
	return doRequest(settings, http.MethodPost, "/api/v1/skills/"+name+"/validate", []byte(`{}`), requestOptions{NeedsAuth: true})
}

func (a *App) ImproveSkill(name string, autoFix bool) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill_name is required")
	}
	body, err := json.Marshal(SkillImprovePayload{AutoFix: autoFix})
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/skills/"+name+"/improve", body, requestOptions{NeedsAuth: true})
}

func (a *App) UploadSkill(name string, input SkillUploadPayload) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill_name is required")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/skills/"+name+"/upload", body, requestOptions{NeedsAuth: true})
}

func (a *App) GetSkillUploadStatus(submissionID, baseURL string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return nil, fmt.Errorf("submission_id is required")
	}
	path := "/api/v1/skill-uploads/" + submissionID
	if strings.TrimSpace(baseURL) != "" {
		path += "?base_url=" + url.QueryEscape(strings.TrimSpace(baseURL))
	}
	return doRequest(settings, http.MethodGet, path, nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetSkillMarketAccount(email, baseURL string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	path := "/api/v1/skill-market/account?email=" + url.QueryEscape(email)
	if strings.TrimSpace(baseURL) != "" {
		path += "&base_url=" + url.QueryEscape(strings.TrimSpace(baseURL))
	}
	return doRequest(settings, http.MethodGet, path, nil, requestOptions{NeedsAuth: true})
}

func (a *App) DeleteSkill(name string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill_name is required")
	}
	return doRequest(settings, http.MethodDelete, "/api/v1/skills/"+name, nil, requestOptions{NeedsAuth: true})
}

func (a *App) InstallSkill(input SkillInstallPayload) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/skills/install", body, requestOptions{NeedsAuth: true})
}

func (a *App) ImportSkill(input SkillImportPayload) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/skills/import", body, requestOptions{NeedsAuth: true})
}

func (a *App) ExportSkill(name string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill_name is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/skills/"+name+"/export", nil, requestOptions{NeedsAuth: true})
}

func (a *App) ListMCPServers() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/mcp/servers", nil, requestOptions{NeedsAuth: true})
}

func (a *App) CreateMCPServer(input MCPServerPayload) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/mcp/servers", body, requestOptions{NeedsAuth: true})
}

func (a *App) GetMCPServer(serverID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/mcp/servers/"+serverID, nil, requestOptions{NeedsAuth: true})
}

func (a *App) UpdateMCPServer(serverID string, input MCPServerUpdatePayload) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPatch, "/api/v1/mcp/servers/"+serverID, body, requestOptions{NeedsAuth: true})
}

func (a *App) DeleteMCPServer(serverID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	return doRequest(settings, http.MethodDelete, "/api/v1/mcp/servers/"+serverID, nil, requestOptions{NeedsAuth: true})
}

func (a *App) StartMCPServer(serverID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	return doRequest(settings, http.MethodPost, "/api/v1/mcp/servers/"+serverID+"/start", nil, requestOptions{NeedsAuth: true})
}

func (a *App) StopMCPServer(serverID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	return doRequest(settings, http.MethodPost, "/api/v1/mcp/servers/"+serverID+"/stop", nil, requestOptions{NeedsAuth: true})
}

func (a *App) CheckMCPServer(serverID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	return doRequest(settings, http.MethodPost, "/api/v1/mcp/servers/"+serverID+"/health-check", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetMCPServerTools(serverID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/mcp/servers/"+serverID+"/tools", nil, requestOptions{NeedsAuth: true})
}

func (a *App) ListInstances() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances", nil, requestOptions{NeedsAuth: true})
}

func (a *App) CreateInstance(input CreateInstanceInput) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	payload := map[string]string{
		"name":        strings.TrimSpace(input.Name),
		"description": strings.TrimSpace(input.Description),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/instances", body, requestOptions{NeedsAuth: true})
}

func (a *App) GetInstance(instanceID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID, nil, requestOptions{NeedsAuth: true})
}

func (a *App) ListSessions(instanceID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID+"/sessions", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetSession(instanceID, sessionID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	sessionID = strings.TrimSpace(sessionID)
	if instanceID == "" || sessionID == "" {
		return nil, fmt.Errorf("instance_id and session_id are required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID+"/sessions/"+sessionID, nil, requestOptions{NeedsAuth: true})
}

func (a *App) ListMessages(instanceID, sessionID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	sessionID = strings.TrimSpace(sessionID)
	if instanceID == "" || sessionID == "" {
		return nil, fmt.Errorf("instance_id and session_id are required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID+"/sessions/"+sessionID+"/messages", nil, requestOptions{NeedsAuth: true})
}

func (a *App) ListRuns(instanceID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID+"/runs", nil, requestOptions{NeedsAuth: true})
}

func (a *App) GetRun(instanceID, runID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	runID = strings.TrimSpace(runID)
	if instanceID == "" || runID == "" {
		return nil, fmt.Errorf("instance_id and run_id are required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/instances/"+instanceID+"/runs/"+runID, nil, requestOptions{NeedsAuth: true})
}

func (a *App) RefreshConversation(input ConversationQueryInput) (*ConversationRefreshResult, error) {
	instanceID := strings.TrimSpace(input.InstanceID)
	sessionID := strings.TrimSpace(input.SessionID)
	runID := strings.TrimSpace(input.RunID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	out := &ConversationRefreshResult{}
	if sessionID != "" {
		session, err := a.GetSession(instanceID, sessionID)
		if err != nil {
			return nil, err
		}
		messages, err := a.ListMessages(instanceID, sessionID)
		if err != nil {
			return nil, err
		}
		out.Session = session.Body
		out.Messages = messages.Body
	}
	if runID != "" {
		run, err := a.GetRun(instanceID, runID)
		if err != nil {
			return nil, err
		}
		out.Run = run.Body
	}
	return out, nil
}

func (a *App) SendMessage(input SendMessageInput) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	instanceID := strings.TrimSpace(input.InstanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	payload := map[string]string{
		"session_id": strings.TrimSpace(input.SessionID),
		"title":      strings.TrimSpace(input.Title),
		"content":    content,
	}
	if payload["session_id"] == "" {
		delete(payload, "session_id")
	}
	if payload["title"] == "" {
		delete(payload, "title")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/instances/"+instanceID+"/messages", body, requestOptions{NeedsAuth: true})
}

func (a *App) ListTenants() (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodGet, "/api/v1/admin/tenants", nil, requestOptions{NeedsAdmin: true})
}

func (a *App) CreateTenant(input CreateTenantInput) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	payload := map[string]string{"name": strings.TrimSpace(input.Name)}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/admin/tenants", body, requestOptions{NeedsAdmin: true})
}

func (a *App) ListUsers(tenantID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/admin/tenants/"+tenantID+"/users", nil, requestOptions{NeedsAdmin: true})
}

func (a *App) CreateUser(input CreateUserInput) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	payload := map[string]string{
		"name":  strings.TrimSpace(input.Name),
		"email": strings.TrimSpace(input.Email),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/admin/tenants/"+tenantID+"/users", body, requestOptions{NeedsAdmin: true})
}

func (a *App) ListCredentials(tenantID, userID string) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return nil, fmt.Errorf("tenant_id and user_id are required")
	}
	return doRequest(settings, http.MethodGet, "/api/v1/admin/tenants/"+tenantID+"/users/"+userID+"/credentials", nil, requestOptions{NeedsAdmin: true})
}

func (a *App) CreateCredential(input CreateCredentialInput) (*APITextResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	tenantID := strings.TrimSpace(input.TenantID)
	userID := strings.TrimSpace(input.UserID)
	if tenantID == "" || userID == "" {
		return nil, fmt.Errorf("tenant_id and user_id are required")
	}
	payload := map[string]string{
		"name":       strings.TrimSpace(input.Name),
		"api_key":    strings.TrimSpace(input.APIKey),
		"api_secret": strings.TrimSpace(input.APISecret),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return doRequest(settings, http.MethodPost, "/api/v1/admin/tenants/"+tenantID+"/users/"+userID+"/credentials", body, requestOptions{NeedsAdmin: true})
}

func (a *App) ProvisionDemo(input ProvisionDemoInput) (*ProvisionDemoResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	tenantName := strings.TrimSpace(input.TenantName)
	if tenantName == "" {
		tenantName = "Demo Tenant"
	}
	userName := strings.TrimSpace(input.UserName)
	if userName == "" {
		userName = "Demo User"
	}
	credentialName := strings.TrimSpace(input.CredentialName)
	if credentialName == "" {
		credentialName = "demo-credential"
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		apiKey = "demo-" + randomToken(6)
	}
	apiSecret := strings.TrimSpace(input.APISecret)
	if apiSecret == "" {
		apiSecret = randomToken(18)
	}

	tenantBody, _ := json.Marshal(map[string]string{"name": tenantName})
	tenantResp, err := doRequest(settings, http.MethodPost, "/api/v1/admin/tenants", tenantBody, requestOptions{NeedsAdmin: true})
	if err != nil {
		return nil, err
	}
	var tenant struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(tenantResp.Body), &tenant); err != nil || strings.TrimSpace(tenant.ID) == "" {
		return nil, fmt.Errorf("parse tenant response: %w", err)
	}

	userBody, _ := json.Marshal(map[string]string{
		"name":  userName,
		"email": strings.TrimSpace(input.UserEmail),
	})
	userResp, err := doRequest(settings, http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users", userBody, requestOptions{NeedsAdmin: true})
	if err != nil {
		return nil, err
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(userResp.Body), &user); err != nil || strings.TrimSpace(user.ID) == "" {
		return nil, fmt.Errorf("parse user response: %w", err)
	}

	credBody, _ := json.Marshal(map[string]string{
		"name":       credentialName,
		"api_key":    apiKey,
		"api_secret": apiSecret,
	})
	credResp, err := doRequest(settings, http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials", credBody, requestOptions{NeedsAdmin: true})
	if err != nil {
		return nil, err
	}
	var credential struct {
		ID     string `json:"id"`
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal([]byte(credResp.Body), &credential); err != nil || strings.TrimSpace(credential.ID) == "" {
		return nil, fmt.Errorf("parse credential response: %w", err)
	}
	if strings.TrimSpace(credential.APIKey) != "" {
		apiKey = strings.TrimSpace(credential.APIKey)
	}

	settings.APIKey = apiKey
	settings.APISecret = apiSecret
	if _, err := a.SaveSettings(settings); err != nil {
		return nil, err
	}
	return &ProvisionDemoResult{
		TenantID:       tenant.ID,
		UserID:         user.ID,
		CredentialID:   credential.ID,
		APIKey:         apiKey,
		APISecret:      apiSecret,
		TenantResponse: tenantResp.Body,
		UserResponse:   userResp.Body,
		CredentialResp: credResp.Body,
	}, nil
}

func (a *App) QuickStartDemo(input QuickStartInput) (*QuickStartResult, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return nil, err
	}
	result := &QuickStartResult{}

	trimmedConfig := strings.TrimSpace(input.ConfigJSON)
	if trimmedConfig != "" {
		body, err := normalizeJSONBody(trimmedConfig, true)
		if err != nil {
			return nil, err
		}
		saved, err := doRequest(settings, http.MethodPut, "/api/v1/config", body, requestOptions{NeedsAuth: true})
		if err != nil {
			return nil, err
		}
		result.ConfigSaved = saved.Body
	}

	validation, err := doRequest(settings, http.MethodPost, "/api/v1/config/validate", nil, requestOptions{NeedsAuth: true})
	if err != nil {
		return nil, err
	}
	result.Validation = validation.Body
	var validationPayload struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal([]byte(validation.Body), &validationPayload); err == nil {
		result.ConfigIsValid = validationPayload.Valid
	}
	if !result.ConfigIsValid {
		return result, fmt.Errorf("config is not valid yet")
	}

	instanceName := strings.TrimSpace(input.InstanceName)
	if instanceName == "" {
		instanceName = "demo-instance"
	}
	payload := map[string]string{
		"name":        instanceName,
		"description": strings.TrimSpace(input.InstanceDescription),
	}
	instanceBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	instanceResp, err := doRequest(settings, http.MethodPost, "/api/v1/instances", instanceBody, requestOptions{NeedsAuth: true})
	if err != nil {
		return result, err
	}
	result.Instance = instanceResp.Body
	var instancePayload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(instanceResp.Body), &instancePayload); err == nil {
		result.InstanceID = strings.TrimSpace(instancePayload.ID)
	}

	messageContent := strings.TrimSpace(input.MessageContent)
	if result.InstanceID == "" || messageContent == "" {
		return result, nil
	}
	messagePayload := map[string]string{
		"title":   strings.TrimSpace(input.MessageTitle),
		"content": messageContent,
	}
	if strings.TrimSpace(messagePayload["title"]) == "" {
		delete(messagePayload, "title")
	}
	messageBody, err := json.Marshal(messagePayload)
	if err != nil {
		return nil, err
	}
	messageResp, err := doRequest(settings, http.MethodPost, "/api/v1/instances/"+result.InstanceID+"/messages", messageBody, requestOptions{NeedsAuth: true})
	if err != nil {
		return result, err
	}
	result.Message = messageResp.Body
	result.MessageWasSent = true
	var messageResult struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal([]byte(messageResp.Body), &messageResult); err == nil {
		result.SessionID = strings.TrimSpace(messageResult.Session.ID)
		result.RunID = strings.TrimSpace(messageResult.Run.ID)
	}
	return result, nil
}

func defaultSettings() DemoSettings {
	return DemoSettings{
		BaseURL:    "http://127.0.0.1:18080",
		TimeoutSec: 60,
	}
}

func normalizeSettings(settings DemoSettings) DemoSettings {
	settings.BaseURL = normalizeBaseURL(settings.BaseURL)
	settings.AdminSecret = strings.TrimSpace(settings.AdminSecret)
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.APISecret = strings.TrimSpace(settings.APISecret)
	settings.AccessToken = strings.TrimSpace(settings.AccessToken)
	if settings.TimeoutSec <= 0 {
		settings.TimeoutSec = 60
	}
	return settings
}

func normalizeBaseURL(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultSettings().BaseURL
	}
	return strings.TrimRight(v, "/")
}

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".srvdemo", "settings.json"), nil
}

func loadSettings() (DemoSettings, error) {
	path, err := settingsPath()
	if err != nil {
		return DemoSettings{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DemoSettings{}, err
	}
	var settings DemoSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return DemoSettings{}, err
	}
	return normalizeSettings(settings), nil
}

func writeSettings(settings DemoSettings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalizeSettings(settings), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalizeJSONBody(raw string, required bool) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if required {
			return nil, fmt.Errorf("json body is required")
		}
		return nil, nil
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return data, nil
}

type requestOptions struct {
	NeedsAuth  bool
	NeedsAdmin bool
}

func doRequest(settings DemoSettings, method, path string, body []byte, opts requestOptions) (*APITextResult, error) {
	settings = normalizeSettings(settings)
	if opts.NeedsAuth && strings.TrimSpace(settings.AccessToken) == "" {
		return nil, fmt.Errorf("access token is empty, please login first")
	}
	if opts.NeedsAdmin && strings.TrimSpace(settings.AdminSecret) == "" {
		return nil, fmt.Errorf("admin_secret is empty, please save admin settings first")
	}
	req, err := http.NewRequestWithContext(context.Background(), method, settings.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if opts.NeedsAuth {
		req.Header.Set("Authorization", "Bearer "+settings.AccessToken)
	}
	if opts.NeedsAdmin {
		req.Header.Set("X-MaClaw-Admin-Secret", settings.AdminSecret)
	}
	resp, err := httpClient(settings).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	result := &APITextResult{StatusCode: resp.StatusCode, Body: prettyJSON(raw)}
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("request failed: %s", resp.Status)
	}
	return result, nil
}

func httpClient(settings DemoSettings) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if settings.SkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Timeout:   time.Duration(settings.TimeoutSec) * time.Second,
		Transport: transport,
	}
}

func prettyJSON(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, trimmed, "", "  "); err == nil {
		return out.String()
	}
	return string(trimmed)
}

func randomToken(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 16
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
