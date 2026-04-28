package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil
	}
	var result struct {
		Colleagues []CenterColleague `json:"colleagues"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return nil
	}
	var result struct {
		Roles []CenterRole `json:"roles"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Roles
}

// centerColleagueToLocal converts a CenterColleague to the local Colleague type
// used by the frontend (Wails binding).
func centerColleagueToLocal(c CenterColleague) Colleague {
	role := c.RoleName
	if role == "" && c.RoleCode != "" {
		role = "你的" + c.RoleCode + "同事"
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
	url := strings.TrimRight(centerBaseURL, "/") + "/client/capabilities"
	if colleagueID != "" {
		url += "?colleague_id=" + colleagueID
	}

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil
	}
	var result struct {
		Capabilities []CenterCapability `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Capabilities
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
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// fetchCenterCollaborations retrieves collaboration tasks from iWorkerCenter.
// If colleagueID is provided, returns only tasks assigned to that colleague.
func fetchCenterCollaborations(centerBaseURL string, tenantID string, colleagueID string, timeoutSec int) []CenterCollabTask {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/collaborations"
	if colleagueID != "" {
		url += "?colleague_id=" + colleagueID
	}

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil
	}
	var result struct {
		Tasks []CenterCollabTask `json:"tasks"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Tasks
}

// CenterWorkflowInstance represents a workflow instance from iWorkerCenter.
type CenterWorkflowInstance struct {
	ID            string `json:"id"`
	DefinitionID  string `json:"definition_id"`
	Title         string `json:"title"`
	InitiatorID   string `json:"initiator_id"`
	CurrentStepID string `json:"current_step_id"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// fetchCenterWorkflowInstances retrieves workflow instances from iWorkerCenter.
func fetchCenterWorkflowInstances(centerBaseURL string, tenantID string, timeoutSec int) []CenterWorkflowInstance {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/workflow-instances"

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil
	}
	var result struct {
		Instances []CenterWorkflowInstance `json:"instances"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Instances
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
	if centerBaseURL == "" || taskDesc == "" {
		return nil
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

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	setCenterTenantHeader(req, tenantID)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return nil
	}
	var result struct {
		Recommendations []CenterRecommendation `json:"recommendations"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Recommendations
}

// CenterGoalPush represents a GoalWatch push fetched from iWorkerCenter.
type CenterGoalPush struct {
	EventID                     string `json:"event_id,omitempty"`
	TaskID                      string `json:"task_id"`
	Title                       string `json:"title"`
	ToColleagueID               string `json:"to_colleague_id"`
	ToRoleCode                  string `json:"to_role_code"`
	Status                      string `json:"status"`
	Reason                      string `json:"reason"`
	RecommendedAction           string `json:"recommended_action"`
	AgeSeconds                  int64  `json:"age_seconds"`
	ExecutorStatus              string `json:"executor_status,omitempty"`
	ExecutorHeartbeatAgeSeconds int64  `json:"executor_heartbeat_age_seconds,omitempty"`
	CreatedAt                   string `json:"created_at"`
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter goalwatch pushes failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Pushes []CenterGoalPush `json:"pushes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return CenterGoalPushAckResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return CenterGoalPushAckResult{}, fmt.Errorf("iWorkerCenter goalwatch ack failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result CenterGoalPushAckResult
	if err := json.Unmarshal(body, &result); err != nil {
		return CenterGoalPushAckResult{}, err
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

// CenterAgentInstanceHeartbeatRequest registers one local agent instance heartbeat with iWorkerCenter.
type CenterAgentInstanceHeartbeatRequest struct {
	WorkerID        string   `json:"worker_id"`
	InstanceID      string   `json:"instance_id"`
	Role            string   `json:"role"`
	Status          string   `json:"status"`
	OrgUnitID       string   `json:"org_unit_id"`
	Capabilities    []string `json:"capabilities"`
	MemoryAuthority string   `json:"memory_authority"`
	LocalCacheMode  string   `json:"local_cache_mode"`
	HostID          string   `json:"host_id"`
	ProcessID       int      `json:"process_id"`
	StartedAt       string   `json:"started_at"`
}

type CenterAgentInstance struct {
	TenantID            string   `json:"tenant_id"`
	WorkerID            string   `json:"worker_id"`
	InstanceID          string   `json:"instance_id"`
	Role                string   `json:"role"`
	Status              string   `json:"status"`
	OrgUnitID           string   `json:"org_unit_id,omitempty"`
	Capabilities        []string `json:"capabilities"`
	MemoryAuthority     string   `json:"memory_authority"`
	LocalCacheMode      string   `json:"local_cache_mode"`
	HostID              string   `json:"host_id,omitempty"`
	ProcessID           int      `json:"process_id,omitempty"`
	StartedAt           string   `json:"started_at"`
	LastHeartbeatAt     string   `json:"last_heartbeat_at"`
	HeartbeatAgeSeconds int64    `json:"heartbeat_age_seconds"`
	EffectiveStatus     string   `json:"effective_status"`
}

type CenterAgentInstanceHeartbeatResult struct {
	Instance CenterAgentInstance `json:"instance"`
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return CenterAgentInstanceHeartbeatResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return CenterAgentInstanceHeartbeatResult{}, fmt.Errorf("iWorkerCenter agent heartbeat failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result CenterAgentInstanceHeartbeatResult
	if err := json.Unmarshal(body, &result); err != nil {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iWorkerCenter agent instances failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Instances []CenterAgentInstance `json:"instances"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Instances == nil {
		result.Instances = []CenterAgentInstance{}
	}
	return result.Instances, nil
}
