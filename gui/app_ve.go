package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// VirtualEmployeeEntry is the frontend-facing VE data structure.
type VirtualEmployeeEntry struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	SkillDescription string `json:"skill_description"`
	AccessPolicy     string `json:"access_policy"`
	Status           string `json:"status"`
	OnlineStatus     string `json:"online_status"`
	RegisteredAt     string `json:"registered_at,omitempty"`
}

// VEStatusResponse is returned by GetVEStatus.
type VEStatusResponse struct {
	Registered bool                  `json:"registered"`
	Employee   *VirtualEmployeeEntry `json:"employee,omitempty"`
}

// VESessionInfo is returned when initiating a VE conversation.
type VESessionInfo struct {
	SessionID string `json:"session_id"`
	VEID      string `json:"ve_id"`
	VEName    string `json:"ve_name"`
}

// RegisterVirtualEmployee submits a VE registration request to the Hub.
func (a *App) RegisterVirtualEmployee(name, skillDesc, policy string, list []string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":             strings.TrimSpace(name),
		"skill_description": strings.TrimSpace(skillDesc),
		"access_policy":    strings.TrimSpace(policy),
	}
	if policy == "whitelist" {
		body["whitelist"] = list
	} else if policy == "blacklist" {
		body["blacklist"] = list
	}

	_, err = a.postHubJSON(hubURL, token, "/api/ve/register", body)
	return err
}

// UpdateVESettings updates the VE's name, skill description, and access policy.
func (a *App) UpdateVESettings(name, skillDesc, policy string, list []string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":             strings.TrimSpace(name),
		"skill_description": strings.TrimSpace(skillDesc),
		"access_policy":    strings.TrimSpace(policy),
	}
	if policy == "whitelist" {
		body["whitelist"] = list
	} else if policy == "blacklist" {
		body["blacklist"] = list
	}

	_, err = a.putHubJSON(hubURL, token, "/api/ve/settings", body)
	return err
}

// GetVEStatus returns the current VE registration status for this machine.
func (a *App) GetVEStatus() (*VEStatusResponse, error) {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return &VEStatusResponse{Registered: false}, nil
	}

	data, err := a.getHubJSON(hubURL, token, "/api/ve/status")
	if err != nil {
		return &VEStatusResponse{Registered: false}, nil
	}

	var resp VEStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return &VEStatusResponse{Registered: false}, nil
	}
	return &resp, nil
}

// ListVirtualEmployees returns the list of discoverable virtual employees from the Hub.
func (a *App) ListVirtualEmployees() ([]VirtualEmployeeEntry, error) {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return nil, err
	}

	data, err := a.getHubJSON(hubURL, token, "/api/ve/discoverable")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Employees            []VirtualEmployeeEntry `json:"employees"`
		MaxGroupParticipants int                    `json:"max_group_participants"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode VE list: %w", err)
	}
	return resp.Employees, nil
}

// InitiateVEConversation starts a conversation with a virtual employee.
func (a *App) InitiateVEConversation(veID string) (*VESessionInfo, error) {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return nil, err
	}

	data, err := a.postHubJSON(hubURL, token, "/api/ve/"+veID+"/initiate", nil)
	if err != nil {
		return nil, err
	}

	var info VESessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("decode session info: %w", err)
	}
	return &info, nil
}

// SendVEMessage sends a message in a VE conversation.
func (a *App) SendVEMessage(sessionID, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("message content is empty")
	}
	if len([]rune(content)) > 32000 {
		return fmt.Errorf("message exceeds 32,000 character limit")
	}

	// Use existing A2A infrastructure to send the message
	return a.GroupDiscussionSendMessage(sessionID, a2a.GroupDiscussionMessage{
		Kind:      a2a.MessageStatement,
		Content:   content,
		CreatedAt: time.Now(),
	})
}

// CloseVESession ends a VE conversation session.
func (a *App) CloseVESession(sessionID string) error {
	// Cancel the A2A session via Hub
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}
	_, err = a.postHubJSON(hubURL, token, "/api/a2a/consultations/"+sessionID+"/cancel", nil)
	return err
}

// AddVEToGroup adds a virtual employee to an existing group chat.
func (a *App) AddVEToGroup(sessionID, veID string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}

	body := map[string]any{
		"to_id": veID,
		"role":  "speak",
	}
	_, err = a.postHubJSON(hubURL, token, "/api/a2a/consultations/"+sessionID+"/invites", body)
	return err
}

// RespondAuthRequest responds to a per-request authorization request.
func (a *App) RespondAuthRequest(requestID, decision string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}

	body := map[string]any{
		"request_id": requestID,
		"decision":   decision,
	}
	_, err = a.postHubJSON(hubURL, token, "/api/ve/auth/respond", body)
	return err
}

// --- Hub HTTP helpers ---

func (a *App) getHubCredentials() (hubURL, token string, err error) {
	cfg, loadErr := a.LoadConfig()
	if loadErr != nil {
		return "", "", fmt.Errorf("load config: %w", loadErr)
	}
	hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	token = strings.TrimSpace(cfg.RemoteMachineToken)
	if hubURL == "" {
		return "", "", fmt.Errorf("Hub URL not configured")
	}
	if token == "" {
		return "", "", fmt.Errorf("Hub token not configured")
	}
	return hubURL, token, nil
}

func (a *App) getHubJSON(hubURL, token, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	cfg, _ := a.LoadConfig()
	if cfg.RemoteMachineID != "" {
		req.Header.Set("X-Machine-ID", cfg.RemoteMachineID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hub returned %d: %s", resp.StatusCode, truncateVEStr(string(data), 200))
	}
	return data, nil
}

func (a *App) postHubJSON(hubURL, token, path string, body any) ([]byte, error) {
	return a.doHubJSON(hubURL, token, http.MethodPost, path, body)
}

func (a *App) putHubJSON(hubURL, token, path string, body any) ([]byte, error) {
	return a.doHubJSON(hubURL, token, http.MethodPut, path, body)
}

func (a *App) doHubJSON(hubURL, token, method, path string, body any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, hubURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	cfg, _ := a.LoadConfig()
	if cfg.RemoteMachineID != "" {
		req.Header.Set("X-Machine-ID", cfg.RemoteMachineID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hub returned %d: %s", resp.StatusCode, truncateVEStr(string(data), 200))
	}
	return data, nil
}

func truncateVEStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
