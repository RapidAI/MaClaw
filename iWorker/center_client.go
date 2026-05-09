package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const centerClientJSONLimit = 2 * 1024 * 1024

var (
	errCenterJSONTooLarge = errors.New("center response exceeds size limit")
	errCenterJSONTrailing = errors.New("center response contains trailing data")
)

func decodeCenterJSON(r io.Reader, v any, limit int64) error {
	if limit <= 0 {
		limit = centerClientJSONLimit
	}
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return errCenterJSONTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errCenterJSONTrailing
		}
		return err
	}
	return nil
}

func readCenterErrorBody(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, 512*1024))
	return strings.TrimSpace(string(body))
}

type centerHTTPError struct {
	Operation  string
	StatusCode int
	Code       string
	Message    string
	Body       string
}

func (e centerHTTPError) Error() string {
	prefix := strings.TrimSpace(e.Operation)
	if prefix == "" {
		prefix = "iWorkerCenter request"
	}
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = strings.TrimSpace(e.Body)
	}
	if strings.TrimSpace(e.Code) != "" {
		if detail != "" {
			detail = e.Code + ": " + detail
		} else {
			detail = e.Code
		}
	}
	if detail == "" {
		return fmt.Sprintf("%s failed: status=%d", prefix, e.StatusCode)
	}
	return fmt.Sprintf("%s failed: status=%d %s", prefix, e.StatusCode, detail)
}

func newCenterHTTPError(operation string, statusCode int, body string) error {
	err := centerHTTPError{Operation: operation, StatusCode: statusCode, Body: strings.TrimSpace(body)}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &payload) == nil {
		err.Code = strings.TrimSpace(payload.Error.Code)
		err.Message = strings.TrimSpace(payload.Error.Message)
	}
	return err
}

// CenterColleague represents a colleague fetched from iWorkerCenter.
type CenterColleague struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	RoleID      string   `json:"role_id"`
	RoleName    string   `json:"role_name"`
	RoleCode    string   `json:"role_code"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
}

// CenterRole represents a role fetched from iWorkerCenter.
type CenterRole struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      string   `json:"description"`
	DefaultStrengths []string `json:"default_strengths"`
	ApplicableTasks  []string `json:"applicable_tasks"`
}

// fetchCenterColleagues retrieves active colleagues from iWorkerCenter.
// Returns nil on any error (non-blocking).
func fetchCenterColleagues(centerBaseURL string, tenantID string, timeoutSec int) []CenterColleague {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/colleagues"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var result struct {
		Colleagues []CenterColleague `json:"colleagues"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil
	}
	return result.Colleagues
}

// fetchCenterRoles retrieves active roles from iWorkerCenter.
func fetchCenterRoles(centerBaseURL string, tenantID string, timeoutSec int) []CenterRole {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/roles"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var result struct {
		Roles []CenterRole `json:"roles"`
	}
	if err := decodeCenterJSON(resp.Body, &result, 1*1024*1024); err != nil {
		return nil
	}
	return result.Roles
}

// centerColleagueToLocal converts a CenterColleague to the local Colleague type
// used by the frontend (Wails binding).
func centerColleagueToLocal(c CenterColleague) Colleague {
	role := c.RoleName
	if role == "" && c.RoleCode != "" {
		role = "\u4f60\u7684" + c.RoleCode + "\u540c\u4e8b"
	}
	strengths := c.Strengths
	if strengths == nil {
		strengths = []string{}
	}
	tasks := c.Tasks
	if tasks == nil {
		tasks = []string{}
	}
	return Colleague{
		ID:          c.ID,
		Name:        c.Name,
		Role:        role,
		Description: c.Description,
		Strengths:   strengths,
		Tasks:       tasks,
	}
}

// CenterCapability represents a capability package fetched from iWorkerCenter.
type CenterCapability struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	RiskLevel   string `json:"risk_level"`
}

// fetchCenterCapabilities retrieves active capabilities from iWorkerCenter.
// If colleagueID is provided, returns only capabilities bound to that colleague.
func fetchCenterCapabilities(centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) []CenterCapability {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	endpoint := strings.TrimRight(centerBaseURL, "/") + "/client/capabilities"
	if colleagueID != "" {
		endpoint += "?colleague_id=" + url.QueryEscape(colleagueID)
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var result struct {
		Capabilities []CenterCapability `json:"capabilities"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil
	}
	return result.Capabilities
}

type CenterRuntimeSkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
}

type CenterRuntimeCapability struct {
	CapabilityID string                  `json:"capability_id"`
	Name         string                  `json:"name"`
	Source       string                  `json:"source"`
	Version      string                  `json:"version"`
	RiskLevel    string                  `json:"risk_level"`
	Entry        CenterRuntimeSkillEntry `json:"entry"`
}

type CenterMCPServer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ServerType   string   `json:"server_type"`
	Endpoint     string   `json:"endpoint"`
	Command      string   `json:"command,omitempty"`
	Args         []string `json:"args"`
	EnvKeys      []string `json:"env_keys"`
	DepartmentID string   `json:"department_id"`
	RiskLevel    string   `json:"risk_level"`
	Status       string   `json:"status"`
	InstalledAt  string   `json:"installed_at"`
}

type CenterInstalledTools struct {
	Skills     []CenterRuntimeCapability `json:"skills"`
	MCPServers []CenterMCPServer         `json:"mcp_servers"`
	Source     string                    `json:"source,omitempty"`
	CachedAt   string                    `json:"cached_at,omitempty"`
	Stale      bool                      `json:"stale,omitempty"`
	SkillError string                    `json:"skill_error,omitempty"`
	MCPError   string                    `json:"mcp_error,omitempty"`
	CacheError string                    `json:"cache_error,omitempty"`
}
type CenterConfigBundle struct {
	ID           string `json:"id"`
	Version      int    `json:"version"`
	ContentType  string `json:"content_type"`
	Payload      string `json:"payload"`
	Status       string `json:"status"`
	Note         string `json:"note"`
	CreatedAt    string `json:"created_at"`
	PublishedAt  string `json:"published_at"`
	Source       string `json:"source,omitempty"`
	CachedAt     string `json:"cached_at,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
	ApplyStatus  string `json:"apply_status,omitempty"`
	ApplyMessage string `json:"apply_message,omitempty"`
}

func fetchCenterConfigBundle(centerBaseURL, tenantID string, timeoutSec int) (CenterConfigBundle, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return CenterConfigBundle{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	req, err := http.NewRequest(http.MethodGet, centerBaseURL+"/client/config/latest", nil)
	if err != nil {
		return CenterConfigBundle{}, err
	}
	setCenterTenantHeader(req, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return CenterConfigBundle{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return CenterConfigBundle{Status: "not_published", Source: "none", CachedAt: time.Now().UTC().Format(time.RFC3339)}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return CenterConfigBundle{}, fmt.Errorf("iWorkerCenter config bundle failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var bundle CenterConfigBundle
	if err := decodeCenterJSON(resp.Body, &bundle, centerClientJSONLimit); err != nil {
		return CenterConfigBundle{}, err
	}
	bundle.Source = "center"
	bundle.CachedAt = time.Now().UTC().Format(time.RFC3339)
	return bundle, nil
}

func reportCenterConfigApplyResult(centerBaseURL, tenantID, departmentID, workerID string, bundle CenterConfigBundle, status, message string, timeoutSec int) error {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" || strings.TrimSpace(workerID) == "" || strings.TrimSpace(bundle.ID) == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "success"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "iWorker fetched and cached the published config bundle"
	}
	body, err := json.Marshal(map[string]any{
		"bundle_id":     bundle.ID,
		"version":       bundle.Version,
		"worker_id":     strings.TrimSpace(workerID),
		"department_id": strings.TrimSpace(departmentID),
		"status":        status,
		"message":       message,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, centerBaseURL+"/client/config/apply-result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(req, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return fmt.Errorf("iWorkerCenter config apply report failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func fetchCenterRuntimeCapabilities(centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) []CenterRuntimeCapability {
	items, err := fetchCenterRuntimeCapabilitiesResult(centerBaseURL, tenantID, colleagueID, timeoutSec)
	if err != nil {
		return nil
	}
	return items
}

func fetchCenterRuntimeCapabilitiesResult(centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) ([]CenterRuntimeCapability, error) {
	if centerBaseURL == "" || strings.TrimSpace(colleagueID) == "" {
		return nil, fmt.Errorf("center base URL and colleague_id are required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	values := url.Values{}
	values.Set("runtime", "1")
	values.Set("colleague_id", colleagueID)
	endpoint := strings.TrimRight(centerBaseURL, "/") + "/client/capabilities?" + values.Encode()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter runtime capabilities failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		RuntimeEntries []CenterRuntimeCapability `json:"runtime_entries"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	if result.RuntimeEntries == nil {
		result.RuntimeEntries = []CenterRuntimeCapability{}
	}
	return result.RuntimeEntries, nil
}

func fetchCenterMCPServers(centerBaseURL string, tenantID string, departmentID string, timeoutSec int) []CenterMCPServer {
	items, err := fetchCenterMCPServersResult(centerBaseURL, tenantID, departmentID, timeoutSec)
	if err != nil {
		return nil
	}
	return items
}

func fetchCenterMCPServersResult(centerBaseURL string, tenantID string, departmentID string, timeoutSec int) ([]CenterMCPServer, error) {
	if centerBaseURL == "" {
		return nil, fmt.Errorf("center base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	values := url.Values{}
	if strings.TrimSpace(departmentID) != "" {
		values.Set("department_id", departmentID)
	}
	endpoint := strings.TrimRight(centerBaseURL, "/") + "/client/mcp-servers"
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter MCP servers failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		MCPServers []CenterMCPServer `json:"mcp_servers"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	if result.MCPServers == nil {
		result.MCPServers = []CenterMCPServer{}
	}
	return result.MCPServers, nil
}

// CenterCollabTask represents a collaboration task fetched from iWorkerCenter.
type CenterCollabTask struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	FromColleagueID string `json:"from_colleague_id"`
	ToColleagueID   string `json:"to_colleague_id"`
	ToRoleCode      string `json:"to_role_code"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	Result          string `json:"result"`
	WorkflowStepID  string `json:"workflow_step_instance_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Source          string `json:"source,omitempty"`
	CachedAt        string `json:"cached_at,omitempty"`
	Stale           bool   `json:"stale,omitempty"`
}

// fetchCenterCollaborations retrieves collaboration tasks from iWorkerCenter.
// If colleagueID is provided, returns only tasks assigned to that colleague.
func fetchCenterCollaborations(centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) []CenterCollabTask {
	tasks, err := fetchCenterCollaborationsContext(context.Background(), centerBaseURL, tenantID, colleagueID, timeoutSec)
	if err != nil {
		return nil
	}
	return tasks
}

func fetchCenterCollaborationsContext(ctx context.Context, centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) ([]CenterCollabTask, error) {
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	endpoint := strings.TrimRight(centerBaseURL, "/") + "/client/collaborations"
	if colleagueID != "" {
		endpoint += "?colleague_id=" + url.QueryEscape(colleagueID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		return nil, fmt.Errorf("iWorkerCenter collaboration list failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Tasks []CenterCollabTask `json:"tasks"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	return result.Tasks, nil
}

type CreateCenterCollaborationTaskRequest struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	FromColleagueID string `json:"from_colleague_id"`
	ToColleagueID   string `json:"to_colleague_id,omitempty"`
	ToRoleCode      string `json:"to_role_code,omitempty"`
	Priority        int    `json:"priority"`
	SourceType      string `json:"source_type,omitempty"`
}

func createCenterCollaborationTask(centerBaseURL, tenantID string, reqBody CreateCenterCollaborationTaskRequest, timeoutSec int) (CenterCollabTask, error) {
	return createCenterCollaborationTaskContext(context.Background(), centerBaseURL, tenantID, reqBody, timeoutSec)
}

func createCenterCollaborationTaskContext(ctx context.Context, centerBaseURL, tenantID string, reqBody CreateCenterCollaborationTaskRequest, timeoutSec int) (CenterCollabTask, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return CenterCollabTask{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if strings.TrimSpace(reqBody.Title) == "" {
		return CenterCollabTask{}, fmt.Errorf("title is required")
	}
	if strings.TrimSpace(reqBody.FromColleagueID) == "" {
		return CenterCollabTask{}, fmt.Errorf("from_colleague_id is required")
	}
	if strings.TrimSpace(reqBody.ToColleagueID) == "" && strings.TrimSpace(reqBody.ToRoleCode) == "" {
		return CenterCollabTask{}, fmt.Errorf("to_colleague_id or to_role_code is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return CenterCollabTask{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, centerBaseURL+"/runtime/collaboration/create", bytes.NewReader(payload))
	if err != nil {
		return CenterCollabTask{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(httpReq, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CenterCollabTask{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return CenterCollabTask{}, fmt.Errorf("iWorkerCenter collaboration create failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var task CenterCollabTask
	if err := decodeCenterJSON(resp.Body, &task, centerClientJSONLimit); err != nil {
		return CenterCollabTask{}, err
	}
	return task, nil
}

func completeCenterCollaborationTask(centerBaseURL, tenantID, taskID, actorID, result, note string, timeoutSec int) error {
	_, err := transitionCenterCollaborationTaskContext(context.Background(), centerBaseURL, tenantID, taskID, "complete", actorID, result, note, timeoutSec)
	return err
}

func transitionCenterCollaborationTaskContext(ctx context.Context, centerBaseURL, tenantID, taskID, action, actorID, result, note string, timeoutSec int) (CenterCollabTask, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	taskID = strings.TrimSpace(taskID)
	action = strings.Trim(strings.TrimSpace(action), "/")
	if centerBaseURL == "" {
		return CenterCollabTask{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if taskID == "" {
		return CenterCollabTask{}, fmt.Errorf("task_id is required")
	}
	if action == "" {
		return CenterCollabTask{}, fmt.Errorf("action is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	payload, err := json.Marshal(map[string]string{
		"actor_id": strings.TrimSpace(actorID),
		"result":   strings.TrimSpace(result),
		"note":     strings.TrimSpace(note),
	})
	if err != nil {
		return CenterCollabTask{}, err
	}
	endpoint := centerBaseURL + "/runtime/collaboration/" + url.PathEscape(taskID) + "/" + url.PathEscape(action)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return CenterCollabTask{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(httpReq, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CenterCollabTask{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CenterCollabTask{}, fmt.Errorf("iWorkerCenter collaboration transition failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var response struct {
		Task CenterCollabTask `json:"task"`
	}
	if err := decodeCenterJSON(resp.Body, &response, centerClientJSONLimit); err != nil {
		return CenterCollabTask{}, err
	}
	if strings.TrimSpace(response.Task.ID) == "" {
		return CenterCollabTask{}, fmt.Errorf("iWorkerCenter collaboration transition response missing task")
	}
	return response.Task, nil
}

// CenterWorkflowInstance represents a workflow instance from iWorkerCenter.
type CenterWorkflowInstance struct {
	ID                             string `json:"id"`
	DefinitionID                   string `json:"definition_id"`
	Title                          string `json:"title"`
	InitiatorID                    string `json:"initiator_id"`
	CurrentStepID                  string `json:"current_step_id"`
	CurrentStepAssigneeColleagueID string `json:"current_step_assignee_colleague_id"`
	Status                         string `json:"status"`
	CreatedAt                      string `json:"created_at"`
	UpdatedAt                      string `json:"updated_at"`
	Source                         string `json:"source,omitempty"`
	CachedAt                       string `json:"cached_at,omitempty"`
	Stale                          bool   `json:"stale,omitempty"`
}

// CenterWorkflowStepInstance represents the authoritative workflow step state returned by iWorkerCenter.
type CenterWorkflowStepInstance struct {
	ID                  string `json:"id"`
	InstanceID          string `json:"instance_id"`
	StepDefinitionID    string `json:"step_definition_id"`
	AssigneeColleagueID string `json:"assignee_colleague_id"`
	CollaborationTaskID string `json:"collaboration_task_id"`
	Status              string `json:"status"`
	Result              string `json:"result"`
	SortOrder           int    `json:"sort_order"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

// CenterWorkflowStepTransitionResult contains the authoritative step and instance after a workflow action.
type CenterWorkflowStepTransitionResult struct {
	Status   string                     `json:"status"`
	Step     CenterWorkflowStepInstance `json:"step"`
	Instance CenterWorkflowInstance     `json:"instance"`
}

// fetchCenterWorkflowInstances retrieves workflow instances from iWorkerCenter.
func fetchCenterWorkflowInstances(centerBaseURL string, tenantID string, timeoutSec int) []CenterWorkflowInstance {
	instances, err := fetchCenterWorkflowInstancesResult(context.Background(), centerBaseURL, tenantID, "", timeoutSec)
	if err != nil {
		return nil
	}
	return instances
}

func fetchCenterWorkflowInstancesResult(ctx context.Context, centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) ([]CenterWorkflowInstance, error) {
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	endpoint := strings.TrimRight(centerBaseURL, "/") + "/client/workflow-instances"
	if strings.TrimSpace(colleagueID) != "" {
		endpoint += "?colleague_id=" + url.QueryEscape(strings.TrimSpace(colleagueID))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter workflow instances failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Instances []CenterWorkflowInstance `json:"instances"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	if result.Instances == nil {
		result.Instances = []CenterWorkflowInstance{}
	}
	return result.Instances, nil
}

func transitionCenterWorkflowStepContext(ctx context.Context, centerBaseURL string, tenantID string, stepID string, action string, actorID string, result string, note string, timeoutSec int) (CenterWorkflowStepTransitionResult, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	stepID = strings.TrimSpace(stepID)
	action = strings.TrimSpace(action)
	if centerBaseURL == "" {
		return CenterWorkflowStepTransitionResult{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if stepID == "" {
		return CenterWorkflowStepTransitionResult{}, fmt.Errorf("workflow step id is required")
	}
	switch action {
	case "start", "resume", "complete", "reject":
	default:
		return CenterWorkflowStepTransitionResult{}, fmt.Errorf("unsupported workflow step action %q", action)
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	payload, _ := json.Marshal(map[string]any{
		"actor_id": actorID,
		"result":   result,
		"note":     note,
	})
	endpoint := centerBaseURL + "/runtime/workflows/steps/" + url.PathEscape(stepID) + "/" + url.PathEscape(action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return CenterWorkflowStepTransitionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(req, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return CenterWorkflowStepTransitionResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CenterWorkflowStepTransitionResult{}, newCenterHTTPError("iWorkerCenter workflow step transition", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var transition CenterWorkflowStepTransitionResult
	if err := decodeCenterJSON(resp.Body, &transition, centerClientJSONLimit); err != nil {
		return CenterWorkflowStepTransitionResult{}, err
	}
	if strings.TrimSpace(transition.Step.ID) == "" {
		return CenterWorkflowStepTransitionResult{}, fmt.Errorf("iWorkerCenter workflow step transition response missing step")
	}
	if strings.TrimSpace(transition.Instance.ID) == "" {
		return CenterWorkflowStepTransitionResult{}, fmt.Errorf("iWorkerCenter workflow step transition response missing instance")
	}
	return transition, nil
}

// CenterRecommendation represents a colleague recommendation from iWorkerCenter.
type CenterRecommendation struct {
	ColleagueID string  `json:"colleague_id"`
	Name        string  `json:"name"`
	RoleCode    string  `json:"role_code"`
	Score       float64 `json:"score"`
	Reason      string  `json:"reason"`
}

// fetchRecommendations asks iWorkerCenter to recommend colleagues for a task.
func fetchRecommendations(centerBaseURL string, tenantID string, taskDesc string, topN int, timeoutSec int) []CenterRecommendation {
	recommendations, err := fetchRecommendationsResult(context.Background(), centerBaseURL, tenantID, taskDesc, topN, timeoutSec)
	if err != nil {
		return nil
	}
	return recommendations
}

func fetchRecommendationsResult(ctx context.Context, centerBaseURL string, tenantID string, taskDesc string, topN int, timeoutSec int) ([]CenterRecommendation, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	taskDesc = strings.TrimSpace(taskDesc)
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if taskDesc == "" {
		return nil, fmt.Errorf("task description is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	if topN <= 0 {
		topN = 3
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/recommend"

	payload, _ := json.Marshal(map[string]any{
		"task_description": taskDesc,
		"top_n":            topN,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter recommendation failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Recommendations []CenterRecommendation `json:"recommendations"`
	}
	if err := decodeCenterJSON(resp.Body, &result, 1*1024*1024); err != nil {
		return nil, err
	}
	if result.Recommendations == nil {
		result.Recommendations = []CenterRecommendation{}
	}
	return result.Recommendations, nil
}

// CenterGoalPush represents a GoalWatch push fetched from iWorkerCenter.
type CenterGoalPush struct {
	EventID                     string `json:"event_id,omitempty"`
	TaskID                      string `json:"task_id"`
	WorkflowStepInstanceID      string `json:"workflow_step_instance_id,omitempty"`
	Title                       string `json:"title"`
	ToColleagueID               string `json:"to_colleague_id"`
	ToRoleCode                  string `json:"to_role_code"`
	Status                      string `json:"status"`
	Reason                      string `json:"reason"`
	RecommendedAction           string `json:"recommended_action"`
	RecoveryAction              string `json:"recovery_action,omitempty"`
	RecoveryMethod              string `json:"recovery_method,omitempty"`
	RecoveryPath                string `json:"recovery_path,omitempty"`
	AgeSeconds                  int64  `json:"age_seconds"`
	ExecutorStatus              string `json:"executor_status,omitempty"`
	ExecutorHeartbeatAgeSeconds int64  `json:"executor_heartbeat_age_seconds,omitempty"`
	CreatedAt                   string `json:"created_at"`
	Source                      string `json:"source,omitempty"`
	CachedAt                    string `json:"cached_at,omitempty"`
	Stale                       bool   `json:"stale,omitempty"`
}

type CenterGoalPushAckRequest struct {
	EventID     string `json:"event_id"`
	ColleagueID string `json:"colleague_id"`
	Status      string `json:"status"`
	Note        string `json:"note"`
}

type CenterGoalPushAckResult struct {
	EventID    string `json:"event_id"`
	TaskID     string `json:"task_id"`
	AckEventID string `json:"ack_event_id"`
	Status     string `json:"status"`
	Note       string `json:"note,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type CenterGoalPushRecoverResult struct {
	Push           CenterGoalPush          `json:"push"`
	Ack            CenterGoalPushAckResult `json:"ack"`
	RecoveryAction string                  `json:"recovery_action"`
	RecoveryMethod string                  `json:"recovery_method"`
	RecoveryPath   string                  `json:"recovery_path"`
	Status         string                  `json:"status"`
}

func fetchCenterGoalPushes(centerBaseURL, tenantID, colleagueID string, limit int, timeoutSec int) ([]CenterGoalPush, error) {
	return fetchCenterGoalPushesContext(context.Background(), centerBaseURL, tenantID, colleagueID, limit, timeoutSec)
}

func fetchCenterGoalPushesContext(ctx context.Context, centerBaseURL, tenantID, colleagueID string, limit int, timeoutSec int) ([]CenterGoalPush, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	colleagueID = strings.TrimSpace(colleagueID)
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if colleagueID == "" {
		return nil, fmt.Errorf("colleague_id is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	values := url.Values{}
	values.Set("colleague_id", colleagueID)
	values.Set("limit", fmt.Sprintf("%d", limit))
	endpoint := centerBaseURL + "/client/goalwatch/pushes?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter goalwatch pushes failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Pushes []CenterGoalPush `json:"pushes"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	if result.Pushes == nil {
		result.Pushes = []CenterGoalPush{}
	}
	return result.Pushes, nil
}

func ackCenterGoalPush(centerBaseURL, tenantID string, reqBody CenterGoalPushAckRequest, timeoutSec int) (CenterGoalPushAckResult, error) {
	return ackCenterGoalPushContext(context.Background(), centerBaseURL, tenantID, reqBody, timeoutSec)
}

func recoverCenterGoalPush(centerBaseURL, tenantID string, reqBody CenterGoalPushAckRequest, timeoutSec int) (CenterGoalPushRecoverResult, error) {
	return recoverCenterGoalPushContext(context.Background(), centerBaseURL, tenantID, reqBody, timeoutSec)
}

func ackCenterGoalPushContext(ctx context.Context, centerBaseURL, tenantID string, reqBody CenterGoalPushAckRequest, timeoutSec int) (CenterGoalPushAckResult, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	reqBody.EventID = strings.TrimSpace(reqBody.EventID)
	reqBody.ColleagueID = strings.TrimSpace(reqBody.ColleagueID)
	reqBody.Status = strings.TrimSpace(reqBody.Status)
	if centerBaseURL == "" {
		return CenterGoalPushAckResult{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if reqBody.EventID == "" {
		return CenterGoalPushAckResult{}, fmt.Errorf("event_id is required")
	}
	if reqBody.ColleagueID == "" {
		return CenterGoalPushAckResult{}, fmt.Errorf("colleague_id is required")
	}
	if reqBody.Status == "" {
		reqBody.Status = "accepted"
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	payload, err := json.Marshal(map[string]string{
		"colleague_id": reqBody.ColleagueID,
		"status":       reqBody.Status,
		"note":         strings.TrimSpace(reqBody.Note),
	})
	if err != nil {
		return CenterGoalPushAckResult{}, err
	}
	endpoint := centerBaseURL + "/client/goalwatch/pushes/" + url.PathEscape(reqBody.EventID) + "/ack"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return CenterGoalPushAckResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(httpReq, tenantID)

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CenterGoalPushAckResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CenterGoalPushAckResult{}, fmt.Errorf("iWorkerCenter goalwatch ack failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result CenterGoalPushAckResult
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return CenterGoalPushAckResult{}, err
	}
	if strings.TrimSpace(result.EventID) == "" || strings.TrimSpace(result.AckEventID) == "" || strings.TrimSpace(result.Status) == "" {
		return CenterGoalPushAckResult{}, fmt.Errorf("iWorkerCenter goalwatch ack response missing ack result")
	}
	return result, nil
}

func recoverCenterGoalPushContext(ctx context.Context, centerBaseURL, tenantID string, reqBody CenterGoalPushAckRequest, timeoutSec int) (CenterGoalPushRecoverResult, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	reqBody.EventID = strings.TrimSpace(reqBody.EventID)
	reqBody.ColleagueID = strings.TrimSpace(reqBody.ColleagueID)
	if centerBaseURL == "" {
		return CenterGoalPushRecoverResult{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if reqBody.EventID == "" {
		return CenterGoalPushRecoverResult{}, fmt.Errorf("event_id is required")
	}
	if reqBody.ColleagueID == "" {
		return CenterGoalPushRecoverResult{}, fmt.Errorf("colleague_id is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	payload, err := json.Marshal(map[string]string{
		"colleague_id": reqBody.ColleagueID,
		"status":       "recovered",
		"note":         strings.TrimSpace(reqBody.Note),
	})
	if err != nil {
		return CenterGoalPushRecoverResult{}, err
	}
	endpoint := centerBaseURL + "/client/goalwatch/pushes/" + url.PathEscape(reqBody.EventID) + "/recover"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return CenterGoalPushRecoverResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(httpReq, tenantID)

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CenterGoalPushRecoverResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CenterGoalPushRecoverResult{}, fmt.Errorf("iWorkerCenter goalwatch recover failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result CenterGoalPushRecoverResult
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return CenterGoalPushRecoverResult{}, err
	}
	if strings.TrimSpace(result.Status) == "" || strings.TrimSpace(result.Ack.EventID) == "" || strings.TrimSpace(result.Ack.AckEventID) == "" || strings.TrimSpace(result.Ack.Status) == "" {
		return CenterGoalPushRecoverResult{}, fmt.Errorf("iWorkerCenter goalwatch recover response missing ack result")
	}
	if strings.TrimSpace(result.Push.EventID) == "" || strings.TrimSpace(result.Push.WorkflowStepInstanceID) == "" || strings.TrimSpace(result.RecoveryAction) == "" || strings.TrimSpace(result.RecoveryPath) == "" {
		return CenterGoalPushRecoverResult{}, fmt.Errorf("iWorkerCenter goalwatch recover response missing recovery result")
	}
	return result, nil
}

// CenterTenantOption represents a tenant discovered from iWorkerCenter.
type CenterTenantOption struct {
	ID          string `json:"id"`
	CompanyName string `json:"company_name"`
}

// CenterAuthMethodStatus describes an iWorkerCenter authentication provider.
type CenterAuthMethodStatus struct {
	Method      string `json:"method"`
	Label       string `json:"label"`
	Enabled     bool   `json:"enabled"`
	Implemented bool   `json:"implemented"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

// CenterEnrollmentDiscovery is the data a fresh iWorker needs to bind itself to a Center.
type CenterEnrollmentDiscovery struct {
	BaseURL          string                   `json:"base_url"`
	SelectedTenantID string                   `json:"selected_tenant_id"`
	Tenants          []CenterTenantOption     `json:"tenants"`
	Roles            []CenterRole             `json:"roles"`
	Colleagues       []CenterColleague        `json:"colleagues"`
	AuthMethods      []CenterAuthMethodStatus `json:"auth_methods"`
}

func discoverCenterEnrollment(centerBaseURL, preferredTenantID string, timeoutSec int) (CenterEnrollmentDiscovery, error) {
	return discoverCenterEnrollmentContext(context.Background(), centerBaseURL, preferredTenantID, timeoutSec)
}

func discoverCenterEnrollmentContext(ctx context.Context, centerBaseURL, preferredTenantID string, timeoutSec int) (CenterEnrollmentDiscovery, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return CenterEnrollmentDiscovery{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	tenants, err := fetchCenterTenantsContext(ctx, centerBaseURL, timeoutSec)
	if err != nil {
		return CenterEnrollmentDiscovery{}, err
	}
	tenantID := strings.TrimSpace(preferredTenantID)
	if tenantID == "" && len(tenants) > 0 {
		tenantID = tenants[0].ID
	}
	if tenantID == "" {
		tenantID = "default"
	}

	roles, err := fetchCenterRolesContext(ctx, centerBaseURL, tenantID, timeoutSec)
	if err != nil {
		return CenterEnrollmentDiscovery{}, err
	}
	colleagues, err := fetchCenterColleaguesContext(ctx, centerBaseURL, tenantID, timeoutSec)
	if err != nil {
		return CenterEnrollmentDiscovery{}, err
	}
	authMethods, err := fetchCenterAuthMethodsContext(ctx, centerBaseURL, timeoutSec)
	if err != nil {
		authMethods = []CenterAuthMethodStatus{{Method: "local", Label: "Local account", Enabled: true, Implemented: true, Status: "ready"}}
	}
	return CenterEnrollmentDiscovery{BaseURL: centerBaseURL, SelectedTenantID: tenantID, Tenants: tenants, Roles: roles, Colleagues: colleagues, AuthMethods: authMethods}, nil
}

func fetchCenterAuthMethodsContext(ctx context.Context, centerBaseURL string, timeoutSec int) ([]CenterAuthMethodStatus, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, centerBaseURL+"/diworker-auth/methods", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter auth methods discovery failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Methods []CenterAuthMethodStatus `json:"methods"`
	}
	if err := decodeCenterJSON(resp.Body, &result, 1*1024*1024); err != nil {
		return nil, err
	}
	if result.Methods == nil {
		result.Methods = []CenterAuthMethodStatus{}
	}
	return result.Methods, nil
}
func fetchCenterTenantsContext(ctx context.Context, centerBaseURL string, timeoutSec int) ([]CenterTenantOption, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, centerBaseURL+"/auth/tenants", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter tenants discovery failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Tenants []CenterTenantOption `json:"tenants"`
	}
	if err := decodeCenterJSON(resp.Body, &result, 1*1024*1024); err != nil {
		return nil, err
	}
	if result.Tenants == nil {
		result.Tenants = []CenterTenantOption{}
	}
	return result.Tenants, nil
}

func fetchCenterRolesContext(ctx context.Context, centerBaseURL, tenantID string, timeoutSec int) ([]CenterRole, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, centerBaseURL+"/client/roles", nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter roles discovery failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Roles []CenterRole `json:"roles"`
	}
	if err := decodeCenterJSON(resp.Body, &result, 1*1024*1024); err != nil {
		return nil, err
	}
	if result.Roles == nil {
		result.Roles = []CenterRole{}
	}
	return result.Roles, nil
}

func fetchCenterColleaguesContext(ctx context.Context, centerBaseURL, tenantID string, timeoutSec int) ([]CenterColleague, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, centerBaseURL+"/client/colleagues", nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter colleagues discovery failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Colleagues []CenterColleague `json:"colleagues"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	if result.Colleagues == nil {
		result.Colleagues = []CenterColleague{}
	}
	return result.Colleagues, nil
}

type CenterEnrollmentVerifyRequest struct {
	Method   string `json:"method"`
	Username string `json:"username"`
	Password string `json:"password"`
	WorkerID string `json:"worker_id"`
}

type CenterEnrollmentVerifyResult struct {
	Verified      bool   `json:"verified"`
	Authenticated bool   `json:"authenticated"`
	Method        string `json:"method"`
	Username      string `json:"username"`
	WorkerID      string `json:"worker_id"`
	Error         string `json:"error,omitempty"`
}

func verifyCenterEnrollment(centerBaseURL, tenantID string, reqBody CenterEnrollmentVerifyRequest, timeoutSec int) (CenterEnrollmentVerifyResult, error) {
	return verifyCenterEnrollmentContext(context.Background(), centerBaseURL, tenantID, reqBody, timeoutSec)
}

func verifyCenterEnrollmentContext(ctx context.Context, centerBaseURL, tenantID string, reqBody CenterEnrollmentVerifyRequest, timeoutSec int) (CenterEnrollmentVerifyResult, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return CenterEnrollmentVerifyResult{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	reqBody.Method = strings.TrimSpace(reqBody.Method)
	if reqBody.Method == "" {
		reqBody.Method = "local"
	}
	reqBody.Username = strings.TrimSpace(reqBody.Username)
	reqBody.WorkerID = strings.TrimSpace(reqBody.WorkerID)
	if reqBody.Username == "" || strings.TrimSpace(reqBody.Password) == "" {
		return CenterEnrollmentVerifyResult{}, fmt.Errorf("human identity username and password are required")
	}
	if reqBody.WorkerID == "" {
		return CenterEnrollmentVerifyResult{}, fmt.Errorf("worker_id is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return CenterEnrollmentVerifyResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, centerBaseURL+"/diworker-auth/enrollment/verify", bytes.NewReader(payload))
	if err != nil {
		return CenterEnrollmentVerifyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(httpReq, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CenterEnrollmentVerifyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CenterEnrollmentVerifyResult{}, fmt.Errorf("iWorkerCenter enrollment verify failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result CenterEnrollmentVerifyResult
	if err := decodeCenterJSON(resp.Body, &result, 1*1024*1024); err != nil {
		return CenterEnrollmentVerifyResult{}, err
	}
	if !result.Verified {
		if result.Error == "" {
			result.Error = "human identity is not allowed to bind this iWorker"
		}
		return result, fmt.Errorf("%s", result.Error)
	}
	return result, nil
}

func setCenterTenantHeader(req *http.Request, tenantID string) {
	if req == nil {
		return
	}
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
}

type CenterWorkStatusSummary struct {
	CurrentTask    string `json:"current_task,omitempty"`
	CurrentDetail  string `json:"current_detail,omitempty"`
	ActiveCount    int    `json:"active_count"`
	CompletedCount int    `json:"completed_count"`
	ReviewCount    int    `json:"review_count"`
	BlockedCount   int    `json:"blocked_count"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

// CenterAgentInstanceHeartbeatRequest registers one local agent instance heartbeat with iWorkerCenter.
type CenterAgentInstanceHeartbeatRequest struct {
	WorkerID        string                   `json:"worker_id"`
	InstanceID      string                   `json:"instance_id"`
	Role            string                   `json:"role"`
	Status          string                   `json:"status"`
	OrgUnitID       string                   `json:"org_unit_id"`
	Capabilities    []string                 `json:"capabilities"`
	MemoryAuthority string                   `json:"memory_authority"`
	LocalCacheMode  string                   `json:"local_cache_mode"`
	WorkStatus      *CenterWorkStatusSummary `json:"work_status,omitempty"`
	HostID          string                   `json:"host_id"`
	ProcessID       int                      `json:"process_id"`
	StartedAt       string                   `json:"started_at"`
}

type CenterAgentInstance struct {
	TenantID            string                   `json:"tenant_id"`
	WorkerID            string                   `json:"worker_id"`
	InstanceID          string                   `json:"instance_id"`
	Role                string                   `json:"role"`
	Status              string                   `json:"status"`
	OrgUnitID           string                   `json:"org_unit_id,omitempty"`
	Capabilities        []string                 `json:"capabilities"`
	MemoryAuthority     string                   `json:"memory_authority"`
	LocalCacheMode      string                   `json:"local_cache_mode"`
	WorkStatus          *CenterWorkStatusSummary `json:"work_status,omitempty"`
	HostID              string                   `json:"host_id,omitempty"`
	ProcessID           int                      `json:"process_id,omitempty"`
	StartedAt           string                   `json:"started_at"`
	LastHeartbeatAt     string                   `json:"last_heartbeat_at"`
	HeartbeatAgeSeconds int64                    `json:"heartbeat_age_seconds"`
	EffectiveStatus     string                   `json:"effective_status"`
	RuntimeSkillError   string                   `json:"runtime_skill_error,omitempty"`
	Source              string                   `json:"source,omitempty"`
	CachedAt            string                   `json:"cached_at,omitempty"`
	Stale               bool                     `json:"stale,omitempty"`
}

type CenterAgentInstanceHeartbeatResult struct {
	Instance          CenterAgentInstance `json:"instance"`
	RuntimeSkillError string              `json:"runtime_skill_error,omitempty"`
}

func postAgentInstanceHeartbeat(centerBaseURL, tenantID string, heartbeat CenterAgentInstanceHeartbeatRequest, timeoutSec int) (CenterAgentInstanceHeartbeatResult, error) {
	return postAgentInstanceHeartbeatContext(context.Background(), centerBaseURL, tenantID, heartbeat, timeoutSec)
}

func postAgentInstanceHeartbeatContext(ctx context.Context, centerBaseURL, tenantID string, heartbeat CenterAgentInstanceHeartbeatRequest, timeoutSec int) (CenterAgentInstanceHeartbeatResult, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return CenterAgentInstanceHeartbeatResult{}, fmt.Errorf("iWorkerCenter base URL is required")
	}
	heartbeat.WorkerID = strings.TrimSpace(heartbeat.WorkerID)
	heartbeat.InstanceID = strings.TrimSpace(heartbeat.InstanceID)
	heartbeat.Role = strings.TrimSpace(heartbeat.Role)
	if heartbeat.WorkerID == "" {
		return CenterAgentInstanceHeartbeatResult{}, fmt.Errorf("worker_id is required")
	}
	if heartbeat.Role == "" {
		heartbeat.Role = "executor"
	}
	if heartbeat.InstanceID == "" {
		heartbeat.InstanceID = heartbeat.WorkerID + ":" + heartbeat.Role
	}
	if heartbeat.Status == "" {
		heartbeat.Status = "online"
	}
	if heartbeat.MemoryAuthority == "" {
		heartbeat.MemoryAuthority = "iWorkerCenter"
	}
	if heartbeat.LocalCacheMode == "" {
		heartbeat.LocalCacheMode = "cache_only"
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return CenterAgentInstanceHeartbeatResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, centerBaseURL+"/runtime/iworker/instances/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return CenterAgentInstanceHeartbeatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(httpReq, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return CenterAgentInstanceHeartbeatResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CenterAgentInstanceHeartbeatResult{}, fmt.Errorf("iWorkerCenter agent heartbeat failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result CenterAgentInstanceHeartbeatResult
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return CenterAgentInstanceHeartbeatResult{}, err
	}
	return result, nil
}

func fetchCenterAgentInstances(centerBaseURL, tenantID, workerID string, timeoutSec int) ([]CenterAgentInstance, error) {
	return fetchCenterAgentInstancesContext(context.Background(), centerBaseURL, tenantID, workerID, timeoutSec)
}

func fetchCenterAgentInstancesContext(ctx context.Context, centerBaseURL, tenantID, workerID string, timeoutSec int) ([]CenterAgentInstance, error) {
	centerBaseURL = strings.TrimRight(strings.TrimSpace(centerBaseURL), "/")
	if centerBaseURL == "" {
		return nil, fmt.Errorf("iWorkerCenter base URL is required")
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	values := url.Values{}
	if strings.TrimSpace(workerID) != "" {
		values.Set("worker_id", strings.TrimSpace(workerID))
	}
	endpoint := centerBaseURL + "/client/iworker/instances"
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setCenterTenantHeader(req, tenantID)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter agent instances failed: status=%d body=%s", resp.StatusCode, readCenterErrorBody(resp.Body))
	}
	var result struct {
		Instances []CenterAgentInstance `json:"instances"`
	}
	if err := decodeCenterJSON(resp.Body, &result, centerClientJSONLimit); err != nil {
		return nil, err
	}
	if result.Instances == nil {
		result.Instances = []CenterAgentInstance{}
	}
	return result.Instances, nil
}
