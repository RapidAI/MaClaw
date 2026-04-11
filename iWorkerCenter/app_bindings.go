package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// centerAPIClient makes HTTP calls to the local center server for Wails bindings.
var centerAPIClient = &http.Client{Timeout: 5 * time.Second}

func centerAPIGet(path string) ([]byte, error) {
	resp, err := centerAPIClient.Get("http://127.0.0.1:9377" + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func centerAPIPost(path string, payload interface{}) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := centerAPIClient.Post("http://127.0.0.1:9377"+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func centerAPIPut(path string, payload interface{}) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9377"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := centerAPIClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// --- Role bindings ---

type WailsRole struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      string   `json:"description"`
	DefaultStrengths []string `json:"default_strengths"`
	ApplicableTasks  []string `json:"applicable_tasks"`
	Status           string   `json:"status"`
	SortOrder        int      `json:"sort_order"`
}

func (a *App) ListRoles() ([]WailsRole, error) {
	body, err := centerAPIGet("/admin/roles")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Roles []WailsRole `json:"roles"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Roles, nil
}

func (a *App) CreateRole(req map[string]interface{}) (WailsRole, error) {
	body, err := centerAPIPost("/admin/roles", req)
	if err != nil {
		return WailsRole{}, err
	}
	var role WailsRole
	if err := json.Unmarshal(body, &role); err != nil {
		return WailsRole{}, err
	}
	return role, nil
}

// --- Colleague bindings ---

type WailsColleague struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	RoleID      string   `json:"role_id"`
	RoleName    string   `json:"role_name"`
	RoleCode    string   `json:"role_code"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
	Status      string   `json:"status"`
}

func (a *App) ListColleagues() ([]WailsColleague, error) {
	body, err := centerAPIGet("/admin/colleagues")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Colleagues []WailsColleague `json:"colleagues"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Colleagues, nil
}

func (a *App) CreateColleague(req map[string]interface{}) (WailsColleague, error) {
	body, err := centerAPIPost("/admin/colleagues", req)
	if err != nil {
		return WailsColleague{}, err
	}
	var col WailsColleague
	if err := json.Unmarshal(body, &col); err != nil {
		return WailsColleague{}, err
	}
	return col, nil
}

func (a *App) AssignColleagueRole(colleagueID string, roleID string, reason string) error {
	_, err := centerAPIPost("/admin/colleagues/"+colleagueID+"/assign-role", map[string]string{
		"role_id": roleID,
		"reason":  reason,
	})
	return err
}

// --- Memory bindings ---

type WailsMemory struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Level     string   `json:"level"`
	Scope     string   `json:"scope"`
	Tags      []string `json:"tags"`
	Version   int      `json:"version"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func (a *App) ListMemories() ([]WailsMemory, error) {
	body, err := centerAPIGet("/admin/memories")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Memories []WailsMemory `json:"memories"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Memories, nil
}

func (a *App) CreateMemory(req map[string]interface{}) (WailsMemory, error) {
	body, err := centerAPIPost("/admin/memories", req)
	if err != nil {
		return WailsMemory{}, err
	}
	var mem WailsMemory
	if err := json.Unmarshal(body, &mem); err != nil {
		return WailsMemory{}, err
	}
	return mem, nil
}

// --- Capability bindings ---

type WailsCapability struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	RiskLevel   string `json:"risk_level"`
	Status      string `json:"status"`
}

func (a *App) ListCapabilities() ([]WailsCapability, error) {
	body, err := centerAPIGet("/admin/capabilities")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Capabilities []WailsCapability `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Capabilities, nil
}

func (a *App) CreateCapability(req map[string]interface{}) (WailsCapability, error) {
	body, err := centerAPIPost("/admin/capabilities", req)
	if err != nil {
		return WailsCapability{}, err
	}
	var cap WailsCapability
	if err := json.Unmarshal(body, &cap); err != nil {
		return WailsCapability{}, err
	}
	return cap, nil
}

func (a *App) BindCapability(capabilityID string, colleagueID string) error {
	_, err := centerAPIPost("/admin/capabilities/"+capabilityID+"/bind", map[string]string{
		"colleague_id": colleagueID,
	})
	return err
}


// --- Collaboration bindings ---

type WailsCollabTask struct {
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

func (a *App) ListCollaborations() ([]WailsCollabTask, error) {
	body, err := centerAPIGet("/admin/collaborations")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tasks []WailsCollabTask `json:"tasks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

func (a *App) CreateCollaboration(req map[string]interface{}) (WailsCollabTask, error) {
	body, err := centerAPIPost("/runtime/collaboration/create", req)
	if err != nil {
		return WailsCollabTask{}, err
	}
	var task WailsCollabTask
	if err := json.Unmarshal(body, &task); err != nil {
		return WailsCollabTask{}, err
	}
	return task, nil
}

// --- Workflow bindings ---

type WailsWorkflowDef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TriggerType string `json:"trigger_type"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type WailsWorkflowInstance struct {
	ID            string `json:"id"`
	DefinitionID  string `json:"definition_id"`
	Title         string `json:"title"`
	InitiatorID   string `json:"initiator_id"`
	CurrentStepID string `json:"current_step_id"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func (a *App) ListWorkflows() ([]WailsWorkflowDef, error) {
	body, err := centerAPIGet("/admin/workflows")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Workflows []WailsWorkflowDef `json:"workflows"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Workflows, nil
}

func (a *App) CreateWorkflow(req map[string]interface{}) (WailsWorkflowDef, error) {
	body, err := centerAPIPost("/admin/workflows", req)
	if err != nil {
		return WailsWorkflowDef{}, err
	}
	var wf WailsWorkflowDef
	if err := json.Unmarshal(body, &wf); err != nil {
		return WailsWorkflowDef{}, err
	}
	return wf, nil
}

func (a *App) PublishWorkflow(workflowID string) error {
	_, err := centerAPIPost("/admin/workflows/"+workflowID+"/publish", nil)
	return err
}

func (a *App) ListWorkflowInstances() ([]WailsWorkflowInstance, error) {
	body, err := centerAPIGet("/admin/workflow-instances")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Instances []WailsWorkflowInstance `json:"instances"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Instances, nil
}

func (a *App) StartWorkflowInstance(req map[string]interface{}) (WailsWorkflowInstance, error) {
	body, err := centerAPIPost("/runtime/workflows/start", req)
	if err != nil {
		return WailsWorkflowInstance{}, err
	}
	var inst WailsWorkflowInstance
	if err := json.Unmarshal(body, &inst); err != nil {
		return WailsWorkflowInstance{}, err
	}
	return inst, nil
}
