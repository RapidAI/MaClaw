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

// IWorkerCenterClient is the thin iWorker-side client for organization-owned
// state. It deliberately keeps memory, workflow and push authority in Center;
// the desktop process only polls, executes safe recovery hooks and caches views.
type IWorkerCenterClient struct {
	baseURL     string
	tenantID    string
	colleagueID string
	httpClient  *http.Client
}

type IWorkerCenterClientConfig struct {
	BaseURL     string
	TenantID    string
	ColleagueID string
	HTTPClient  *http.Client
}

type IWorkerCenterPush struct {
	EventID                     string    `json:"event_id,omitempty"`
	TaskID                      string    `json:"task_id"`
	WorkflowStepInstanceID      string    `json:"workflow_step_instance_id,omitempty"`
	Title                       string    `json:"title"`
	ToColleagueID               string    `json:"to_colleague_id"`
	ToRoleCode                  string    `json:"to_role_code"`
	Status                      string    `json:"status"`
	Reason                      string    `json:"reason"`
	RecommendedAction           string    `json:"recommended_action"`
	RecoveryAction              string    `json:"recovery_action,omitempty"`
	RecoveryMethod              string    `json:"recovery_method,omitempty"`
	RecoveryPath                string    `json:"recovery_path,omitempty"`
	AgeSeconds                  int64     `json:"age_seconds"`
	ExecutorStatus              string    `json:"executor_status,omitempty"`
	ExecutorHeartbeatAgeSeconds int64     `json:"executor_heartbeat_age_seconds,omitempty"`
	CreatedAt                   time.Time `json:"created_at"`
}

type IWorkerCenterInstance struct {
	TenantID            string    `json:"tenant_id"`
	WorkerID            string    `json:"worker_id"`
	InstanceID          string    `json:"instance_id"`
	Role                string    `json:"role"`
	Status              string    `json:"status"`
	OrgUnitID           string    `json:"org_unit_id,omitempty"`
	Capabilities        []string  `json:"capabilities"`
	MemoryAuthority     string    `json:"memory_authority"`
	LocalCacheMode      string    `json:"local_cache_mode"`
	HostID              string    `json:"host_id,omitempty"`
	ProcessID           int       `json:"process_id,omitempty"`
	StartedAt           time.Time `json:"started_at"`
	LastHeartbeatAt     time.Time `json:"last_heartbeat_at"`
	HeartbeatAgeSeconds int64     `json:"heartbeat_age_seconds"`
	EffectiveStatus     string    `json:"effective_status"`
}

type IWorkerCenterHeartbeatRequest struct {
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

type IWorkerCenterHeartbeatResult struct {
	Instance          IWorkerCenterInstance `json:"instance"`
	RuntimeSkills     []map[string]any      `json:"runtime_skills,omitempty"`
	RuntimeSkillError string                `json:"runtime_skill_error,omitempty"`
}
type IWorkerCenterAckResult struct {
	EventID    string    `json:"event_id"`
	TaskID     string    `json:"task_id"`
	AckEventID string    `json:"ack_event_id"`
	Status     string    `json:"status"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type IWorkerCenterRecoverResult struct {
	Push           IWorkerCenterPush      `json:"push"`
	Ack            IWorkerCenterAckResult `json:"ack"`
	RecoveryAction string                 `json:"recovery_action"`
	RecoveryMethod string                 `json:"recovery_method"`
	RecoveryPath   string                 `json:"recovery_path"`
	Status         string                 `json:"status"`
}

type IWorkerCenterRecoverySummary struct {
	Checked   int      `json:"checked"`
	Recovered int      `json:"recovered"`
	Skipped   int      `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}
type IWorkerCenterCloudHeartbeatStatus struct {
	Configured          bool      `json:"configured"`
	Status              string    `json:"status"`
	CenterID            string    `json:"center_id,omitempty"`
	LastAttemptAt       time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       time.Time `json:"last_success_at,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	RuntimeType         string    `json:"runtime_type"`
	ProductKind         string    `json:"product_kind"`
	AdminConsole        string    `json:"admin_console"`
}

type IWorkerCenterServiceStatus struct {
	Status         string                             `json:"status"`
	RuntimeType    string                             `json:"runtime_type"`
	ProductKind    string                             `json:"product_kind"`
	AdminConsole   string                             `json:"admin_console"`
	ProviderCount  int                                `json:"provider_count"`
	ConfigPath     string                             `json:"config_path,omitempty"`
	CloudHeartbeat *IWorkerCenterCloudHeartbeatStatus `json:"cloud_heartbeat,omitempty"`
}

func NewIWorkerCenterClient(cfg IWorkerCenterClientConfig) (*IWorkerCenterClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("iworkercenter base url is required")
	}
	tenantID := strings.TrimSpace(cfg.TenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("iworkercenter tenant id is required")
	}
	colleagueID := strings.TrimSpace(cfg.ColleagueID)
	if colleagueID == "" {
		return nil, fmt.Errorf("iworker colleague id is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = hubHTTPClient
	}
	return &IWorkerCenterClient{baseURL: baseURL, tenantID: tenantID, colleagueID: colleagueID, httpClient: client}, nil
}

func (c *IWorkerCenterClient) FetchServiceStatus() (IWorkerCenterServiceStatus, error) {
	return c.FetchServiceStatusContext(context.Background())
}

func (c *IWorkerCenterClient) FetchServiceStatusContext(ctx context.Context) (IWorkerCenterServiceStatus, error) {
	var status IWorkerCenterServiceStatus
	if c == nil {
		return status, fmt.Errorf("iworkercenter client is nil")
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/center/status", nil, &status); err != nil {
		return status, err
	}
	return status, nil
}

func (s IWorkerCenterServiceStatus) IsIWorkerCenterService() bool {
	return strings.EqualFold(strings.TrimSpace(s.RuntimeType), "service") &&
		strings.EqualFold(strings.TrimSpace(s.ProductKind), "iworkercenter") &&
		strings.EqualFold(strings.TrimSpace(s.AdminConsole), "web_console")
}

func (c *IWorkerCenterClient) ValidateServiceIdentity(ctx context.Context) (IWorkerCenterServiceStatus, error) {
	status, err := c.FetchServiceStatusContext(ctx)
	if err != nil {
		return status, err
	}
	if !status.IsIWorkerCenterService() {
		return status, fmt.Errorf("endpoint is not an iWorkerCenter service: runtime_type=%q product_kind=%q admin_console=%q", status.RuntimeType, status.ProductKind, status.AdminConsole)
	}
	return status, nil
}
func (c *IWorkerCenterClient) SendHeartbeat(req IWorkerCenterHeartbeatRequest) (IWorkerCenterHeartbeatResult, error) {
	return c.SendHeartbeatContext(context.Background(), req)
}

func (c *IWorkerCenterClient) SendHeartbeatContext(ctx context.Context, req IWorkerCenterHeartbeatRequest) (IWorkerCenterHeartbeatResult, error) {
	var result IWorkerCenterHeartbeatResult
	if c == nil {
		return result, fmt.Errorf("iworkercenter client is nil")
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		req.WorkerID = c.colleagueID
	}
	if strings.TrimSpace(req.Role) == "" {
		req.Role = "watcher"
	}
	if strings.TrimSpace(req.Status) == "" {
		req.Status = "online"
	}
	if strings.TrimSpace(req.MemoryAuthority) == "" {
		req.MemoryAuthority = "iWorkerCenter"
	}
	if strings.TrimSpace(req.LocalCacheMode) == "" {
		req.LocalCacheMode = "cache_only"
	}
	if strings.TrimSpace(req.InstanceID) == "" {
		req.InstanceID = strings.TrimSpace(req.WorkerID) + ":" + strings.TrimSpace(req.Role)
	}
	if err := c.doJSON(ctx, http.MethodPost, "/runtime/iworker/instances/heartbeat", req, &result); err != nil {
		return result, err
	}
	return result, nil
}
func (c *IWorkerCenterClient) ListGoalWatchPushes(limit int) ([]IWorkerCenterPush, error) {
	return c.ListGoalWatchPushesContext(context.Background(), limit)
}

func (c *IWorkerCenterClient) ListGoalWatchPushesContext(ctx context.Context, limit int) ([]IWorkerCenterPush, error) {
	if c == nil {
		return nil, fmt.Errorf("iworkercenter client is nil")
	}
	query := url.Values{}
	query.Set("colleague_id", c.colleagueID)
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	var body struct {
		Pushes []IWorkerCenterPush `json:"pushes"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/client/goalwatch/pushes?"+query.Encode(), nil, &body); err != nil {
		return nil, err
	}
	return body.Pushes, nil
}

func (c *IWorkerCenterClient) AckGoalWatchPush(eventID, status, note string) (IWorkerCenterAckResult, error) {
	return c.AckGoalWatchPushContext(context.Background(), eventID, status, note)
}

func (c *IWorkerCenterClient) AckGoalWatchPushContext(ctx context.Context, eventID, status, note string) (IWorkerCenterAckResult, error) {
	var result IWorkerCenterAckResult
	if strings.TrimSpace(eventID) == "" {
		return result, fmt.Errorf("goalwatch event id is required")
	}
	payload := map[string]string{"colleague_id": c.colleagueID, "status": strings.TrimSpace(status), "note": strings.TrimSpace(note)}
	if err := c.doJSON(ctx, http.MethodPost, "/client/goalwatch/pushes/"+url.PathEscape(eventID)+"/ack", payload, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *IWorkerCenterClient) RecoverGoalWatchPush(eventID, note string) (IWorkerCenterRecoverResult, error) {
	return c.RecoverGoalWatchPushContext(context.Background(), eventID, note)
}

func (c *IWorkerCenterClient) RecoverGoalWatchPushContext(ctx context.Context, eventID, note string) (IWorkerCenterRecoverResult, error) {
	var result IWorkerCenterRecoverResult
	if strings.TrimSpace(eventID) == "" {
		return result, fmt.Errorf("goalwatch event id is required")
	}
	payload := map[string]string{"colleague_id": c.colleagueID, "note": strings.TrimSpace(note)}
	if err := c.doJSON(ctx, http.MethodPost, "/client/goalwatch/pushes/"+url.PathEscape(eventID)+"/recover", payload, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *IWorkerCenterClient) RecoverEligibleGoalWatchPushes(limit int, note string) IWorkerCenterRecoverySummary {
	return c.RecoverEligibleGoalWatchPushesContext(context.Background(), limit, note)
}

func (c *IWorkerCenterClient) RecoverEligibleGoalWatchPushesContext(ctx context.Context, limit int, note string) IWorkerCenterRecoverySummary {
	summary := IWorkerCenterRecoverySummary{}
	pushes, err := c.ListGoalWatchPushesContext(ctx, limit)
	if err != nil {
		summary.Errors = append(summary.Errors, err.Error())
		return summary
	}
	summary.Checked = len(pushes)
	for _, push := range pushes {
		if err := ctx.Err(); err != nil {
			summary.Errors = append(summary.Errors, err.Error())
			return summary
		}
		if !IsIWorkerCenterRecoverablePush(push) {
			summary.Skipped++
			continue
		}
		if _, err := c.RecoverGoalWatchPushContext(ctx, push.EventID, note); err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", push.EventID, err))
			continue
		}
		summary.Recovered++
	}
	return summary
}

func IsIWorkerCenterRecoverablePush(push IWorkerCenterPush) bool {
	if strings.TrimSpace(push.EventID) == "" || strings.TrimSpace(push.WorkflowStepInstanceID) == "" {
		return false
	}
	switch strings.TrimSpace(push.RecoveryAction) {
	case "start_workflow_step", "resume_workflow_step":
		return true
	default:
		return false
	}
}

func (c *IWorkerCenterClient) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("iworkercenter client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Tenant-ID", c.tenantID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("iworkercenter %s %s failed: status=%d body=%s", method, path, res.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
