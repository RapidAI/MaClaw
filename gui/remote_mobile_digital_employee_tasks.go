package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const mobileDigitalEmployeeTaskPollInterval = 12 * time.Second

type mobileDigitalEmployeeTask struct {
	TaskID     string            `json:"task_id"`
	EmployeeID string            `json:"employee_id"`
	Prompt     string            `json:"prompt"`
	TaskType   string            `json:"task_type"`
	Context    map[string]string `json:"context"`
	Status     string            `json:"status"`
	Result     string            `json:"result"`
	ClaimedBy  string            `json:"claimed_by"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

type mobileDigitalEmployeeTaskClaimResponse struct {
	Status string                     `json:"status"`
	Task   *mobileDigitalEmployeeTask `json:"task"`
}

func (c *RemoteHubClient) mobileDigitalEmployeeTaskLoop() {
	if c == nil || c.app == nil {
		return
	}
	if !c.mobileTaskActive.CompareAndSwap(false, true) {
		return
	}
	defer c.mobileTaskActive.Store(false)

	c.pollMobileDigitalEmployeeTasksOnce()
	c.pollMobileDocumentUploadTasksOnce()
	ticker := time.NewTicker(mobileDigitalEmployeeTaskPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !c.IsConnected() {
				return
			}
			c.pollMobileDocumentUploadTasksOnce()
			c.pollMobileDigitalEmployeeTasksOnce()
		}
	}
}

func (c *RemoteHubClient) pollMobileDigitalEmployeeTasksOnce() {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return
	}
	for _, employeeID := range mobileDigitalEmployeeCandidateIDs(cfg.RemoteMachineID, cfg.RemoteClientID) {
		claim, err := c.claimMobileDigitalEmployeeTask(employeeID)
		if err != nil {
			log.Printf("[hub-client] mobile digital employee claim failed employee=%s: %v", employeeID, err)
			continue
		}
		if claim == nil || claim.Task == nil || strings.TrimSpace(claim.Task.TaskID) == "" {
			continue
		}
		c.processMobileDigitalEmployeeTask(*claim.Task)
		return
	}
}

func mobileDigitalEmployeeCandidateIDs(machineID, clientID string) []string {
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if !strings.HasPrefix(strings.ToLower(value), "ve_") {
			ve := "ve_" + value
			if _, ok := seen[strings.ToLower(ve)]; !ok {
				seen[strings.ToLower(ve)] = struct{}{}
				out = append(out, ve)
			}
		}
	}
	add(machineID)
	add(clientID)
	return out
}

func (c *RemoteHubClient) claimMobileDigitalEmployeeTask(employeeID string) (*mobileDigitalEmployeeTaskClaimResponse, error) {
	var out mobileDigitalEmployeeTaskClaimResponse
	path := "/api/mobile/digital-employees/" + url.PathEscape(strings.TrimSpace(employeeID)) + "/tasks/claim"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) updateMobileDigitalEmployeeTask(taskID, status, result string) (*mobileDigitalEmployeeTask, error) {
	payload := map[string]string{
		"status": strings.TrimSpace(status),
		"result": strings.TrimSpace(result),
	}
	var out mobileDigitalEmployeeTask
	path := "/api/mobile/digital-employees/tasks/" + url.PathEscape(strings.TrimSpace(taskID))
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPatch, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) doMobileDigitalEmployeeTaskRequest(ctx context.Context, method, path string, payload any, out any) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("remote hub client is not initialized")
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	if base == "" || machineID == "" || token == "" {
		return fmt.Errorf("remote hub machine identity is incomplete")
	}

	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *RemoteHubClient) processMobileDigitalEmployeeTask(task mobileDigitalEmployeeTask) {
	taskID := strings.TrimSpace(task.TaskID)
	prompt := strings.TrimSpace(task.Prompt)
	if taskID == "" || prompt == "" {
		return
	}
	_, _ = c.updateMobileDigitalEmployeeTask(taskID, "in_progress", "远程数字员工正在处理手机任务。")

	handler := c.digitalEmployeeMessageHandler()
	if handler == nil {
		_, _ = c.updateMobileDigitalEmployeeTask(taskID, "failed", "远程数字员工不可用。")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sessionID := "mobile-digital-employee:" + taskID
	result, err := handler.runAgentForVE(ctx, sessionID, buildMobileDigitalEmployeeExecutionPrompt(task), taskID, func(string) {})
	if err != nil {
		_, _ = c.updateMobileDigitalEmployeeTask(taskID, "failed", err.Error())
		return
	}
	result = strings.TrimSpace(result)
	if result == "" {
		result = "任务已完成，但没有生成可展示结果。"
	}
	_, _ = c.updateMobileDigitalEmployeeTask(taskID, "done", result)
}

func buildMobileDigitalEmployeeExecutionPrompt(task mobileDigitalEmployeeTask) string {
	prompt := strings.TrimSpace(task.Prompt)
	taskType := strings.TrimSpace(task.TaskType)
	if taskType == "" {
		taskType = "general"
	}
	var b strings.Builder
	b.WriteString("[MaClaw Mobile emergency task]\n")
	b.WriteString("Task type: ")
	b.WriteString(taskType)
	b.WriteString("\n")
	if len(task.Context) > 0 {
		b.WriteString("Context:\n")
		keys := make([]string, 0, len(task.Context))
		contextValues := make(map[string]string, len(task.Context))
		for rawKey, rawValue := range task.Context {
			key := strings.TrimSpace(rawKey)
			value := strings.TrimSpace(rawValue)
			if key == "" || value == "" {
				continue
			}
			contextValues[key] = value
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := contextValues[key]
			b.WriteString("- ")
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(value)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nUser request:\n")
	b.WriteString(prompt)
	b.WriteString("\n\nMobile response requirements:\n")
	b.WriteString("- Start with a concise conclusion suitable for phone reading.\n")
	b.WriteString("- Include evidence, impact, and next steps.\n")
	b.WriteString("- For high-risk server or desktop operations, provide command drafts and ask for manual confirmation instead of executing automatically.\n")
	return strings.TrimSpace(b.String())
}
