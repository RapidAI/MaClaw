package main

import (
	"encoding/json"
	"io"
	"net/http"
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
func fetchCenterColleagues(centerBaseURL string, timeoutSec int) []CenterColleague {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/colleagues"

	resp, err := client.Get(url)
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
func fetchCenterRoles(centerBaseURL string, timeoutSec int) []CenterRole {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/roles"

	resp, err := client.Get(url)
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
func fetchCenterCapabilities(centerBaseURL string, colleagueID string, timeoutSec int) []CenterCapability {
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

	resp, err := client.Get(url)
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
func fetchCenterCollaborations(centerBaseURL string, colleagueID string, timeoutSec int) []CenterCollabTask {
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

	resp, err := client.Get(url)
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
func fetchCenterWorkflowInstances(centerBaseURL string, timeoutSec int) []CenterWorkflowInstance {
	if centerBaseURL == "" {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	url := strings.TrimRight(centerBaseURL, "/") + "/client/workflow-instances"

	resp, err := client.Get(url)
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
func fetchRecommendations(centerBaseURL string, taskDesc string, topN int, timeoutSec int) []CenterRecommendation {
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

	resp, err := client.Post(url, "application/json", strings.NewReader(string(payload)))
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
