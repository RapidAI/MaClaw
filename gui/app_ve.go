package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// VirtualEmployeeEntry is the frontend-facing VE data structure.
type VirtualEmployeeEntry struct {
	ID               string   `json:"id"`
	MachineID        string   `json:"machine_id,omitempty"`
	Name             string   `json:"name"`
	SkillDescription string   `json:"skill_description"`
	AccessPolicy     string   `json:"access_policy"`
	Status           string   `json:"status"`
	OnlineStatus     string   `json:"online_status"`
	RegisteredAt     string   `json:"registered_at,omitempty"`
	Whitelist        []string `json:"whitelist,omitempty"`
	Blacklist        []string `json:"blacklist,omitempty"`
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
		"name":              strings.TrimSpace(name),
		"skill_description": strings.TrimSpace(skillDesc),
		"access_policy":     strings.TrimSpace(policy),
	}
	if policy == "whitelist" {
		body["whitelist"] = list
	} else if policy == "blacklist" {
		body["blacklist"] = list
	}

	_, err = a.postHubJSON(hubURL, token, "/api/ve/register", body)
	if err != nil {
		return err
	}
	a.emitEvent("ve:status_change", nil)
	a.emitDigitalEmployeeFeatureStatusChanged()
	return nil
}

// UpdateVESettings updates the VE's name, skill description, and access policy.
func (a *App) UpdateVESettings(name, skillDesc, policy string, list []string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":              strings.TrimSpace(name),
		"skill_description": strings.TrimSpace(skillDesc),
		"access_policy":     strings.TrimSpace(policy),
	}
	if policy == "whitelist" {
		body["whitelist"] = list
	} else if policy == "blacklist" {
		body["blacklist"] = list
	}

	_, err = a.putHubJSON(hubURL, token, "/api/ve/settings", body)
	if err != nil {
		return err
	}
	a.emitEvent("ve:list_update", nil)
	a.emitEvent("ve:status_change", nil)
	a.emitDigitalEmployeeFeatureStatusChanged()
	return nil
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

// ListVirtualEmployees returns the list of discoverable digital employees from the Hub.
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
		return nil, fmt.Errorf("decode digital employee list: %w", err)
	}
	return resp.Employees, nil
}

// InitiateVEConversation starts or resumes a conversation with a digital employee.
// It implements sticky sessions: if an active (non-archived) session with the same
// VE already exists, it returns that session instead of creating a new one.
func (a *App) InitiateVEConversation(veID string) (*VESessionInfo, error) {
	veID = strings.TrimSpace(veID)
	if veID == "" {
		return nil, fmt.Errorf("veID is required")
	}

	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return nil, err
	}

	// Try to find an existing active session with this VE first.
	if info := a.findActiveVESession(hubURL, token, veID); info != nil {
		return info, nil
	}

	// No active session found — create a new one.
	data, err := a.postHubJSON(hubURL, token, "/api/ve/"+veID+"/initiate", nil)
	if err != nil {
		return nil, err
	}

	var info VESessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("decode session info: %w", err)
	}

	// Cache the new session for future lookups.
	a.cacheVESession(veID, info.SessionID)

	return &info, nil
}

// InitiateGroupConversation starts or resumes a group conversation with the given
// set of VE participants. Sticky session: same participant set → same session.
func (a *App) InitiateGroupConversation(veIDs []string) (*VESessionInfo, error) {
	if len(veIDs) == 0 {
		return nil, fmt.Errorf("at least one veID is required")
	}
	// Normalize and sort for consistent lookup key.
	normalized := make([]string, 0, len(veIDs))
	for _, id := range veIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			normalized = append(normalized, id)
		}
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one veID is required")
	}

	// For single VE, delegate to the 1:1 path.
	if len(normalized) == 1 {
		return a.InitiateVEConversation(normalized[0])
	}

	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return nil, err
	}

	// Try to find an existing active group session with this participant set.
	if info := a.findActiveGroupSession(hubURL, token, normalized); info != nil {
		return info, nil
	}

	// No active session — create via group consultation.
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		return nil, fmt.Errorf("load config: %w", cfgErr)
	}
	agentID := strings.TrimSpace(groupDiscussionAgentID(cfg))

	req := a2a.GroupConsultationRequest{
		FromID:    agentID,
		Question:  "Group conversation",
		CreatedAt: time.Now(),
	}
	resp, err := a.GroupDiscussionCreateConsultation(req)
	if err != nil {
		return nil, fmt.Errorf("create group session: %w", err)
	}

	sessionID := resp.Discussion.ID

	// Invite all VEs to the group.
	for _, veID := range normalized {
		_ = a.AddVEToGroup(sessionID, veID)
	}

	// Cache the group session.
	a.cacheGroupSession(normalized, sessionID)

	return &VESessionInfo{
		SessionID: sessionID,
		VEID:      strings.Join(normalized, ","),
		VEName:    "Group",
	}, nil
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

// CloseVESession ends a VE conversation session and removes it from the sticky cache.
func (a *App) CloseVESession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("sessionID is required")
	}

	// Remove from sticky caches — any entry pointing to this session.
	a.veSessionCache.Range(func(key, value any) bool {
		if cachedSessionID, ok := value.(string); ok && cachedSessionID == sessionID {
			a.veSessionCache.Delete(key)
		}
		return true
	})
	a.groupSessionCache.Range(func(key, value any) bool {
		if cachedSessionID, ok := value.(string); ok && cachedSessionID == sessionID {
			a.groupSessionCache.Delete(key)
		}
		return true
	})

	// Cancel the A2A session via Hub
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}
	_, err = a.postHubJSON(hubURL, token, "/api/a2a/consultations/"+sessionID+"/cancel", nil)
	return err
}

// AddVEToGroup adds a digital employee to an existing group chat.
func (a *App) AddVEToGroup(sessionID, veID string) error {
	sessionID = strings.TrimSpace(sessionID)
	veID = strings.TrimSpace(veID)
	if sessionID == "" {
		return fmt.Errorf("sessionID is required")
	}
	if veID == "" {
		return fmt.Errorf("veID is required")
	}

	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}

	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		return fmt.Errorf("load config: %w", cfgErr)
	}
	inviteeID := a.resolveVEInviteMachineID(hubURL, token, veID)
	body := map[string]any{
		"from_id": strings.TrimSpace(groupDiscussionAgentID(cfg)),
		"to_id":   inviteeID,
		"role":    "speak",
	}
	_, err = a.postHubJSON(hubURL, token, "/api/a2a/consultations/"+sessionID+"/invites", body)
	return err
}

// resolveVEInviteMachineID maps a frontend VE id to the discussion participant machine id.
func (a *App) resolveVEInviteMachineID(hubURL, token, veID string) string {
	veID = strings.TrimSpace(veID)
	if veID == "" {
		return ""
	}
	data, err := a.getHubJSON(hubURL, token, "/api/ve/discoverable")
	if err != nil {
		return veID
	}
	var resp struct {
		Employees []VirtualEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return veID
	}
	return resolveVEInviteMachineID(resp.Employees, veID)
}

func resolveVEInviteMachineID(employees []VirtualEmployeeEntry, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, employee := range employees {
		if strings.EqualFold(strings.TrimSpace(employee.ID), value) || strings.EqualFold(strings.TrimSpace(employee.MachineID), value) {
			if machineID := strings.TrimSpace(employee.MachineID); machineID != "" {
				return machineID
			}
			return value
		}
	}
	return value
}

// RespondAuthRequest responds to a per-request authorization request.
func (a *App) RespondAuthRequest(requestID, decision string) error {
	requestID = strings.TrimSpace(requestID)
	decision = strings.TrimSpace(decision)
	if requestID == "" {
		return fmt.Errorf("requestID is required")
	}
	if decision != "allow" && decision != "deny" {
		return fmt.Errorf("decision must be 'allow' or 'deny'")
	}

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
	if machineID := strings.TrimSpace(groupDiscussionAgentID(cfg)); machineID != "" {
		req.Header.Set("X-Machine-ID", machineID)
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
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, hubURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	cfg, _ := a.LoadConfig()
	if machineID := strings.TrimSpace(groupDiscussionAgentID(cfg)); machineID != "" {
		req.Header.Set("X-Machine-ID", machineID)
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

// --- Sticky Session Cache ---
//
// Maintains a local mapping of (participant set) → session ID so that
// conversations with the same VE (or same group) always reuse the same session
// unless explicitly archived. The cache is in-memory (lost on restart) but
// sessions are also looked up from the Hub's active session list as fallback.
//
// Uses sync.Map for concurrent safety — Wails bindings and event callbacks
// execute on different goroutines.

func veGroupKey(veIDs []string) string {
	seen := make(map[string]struct{}, len(veIDs))
	sorted := make([]string, 0, len(veIDs))
	for _, id := range veIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	return strings.Join(sorted, "|")
}

func (a *App) cacheVESession(veID, sessionID string) {
	veID = strings.TrimSpace(veID)
	sessionID = strings.TrimSpace(sessionID)
	if veID == "" || sessionID == "" {
		return
	}
	a.veSessionCache.Store(veID, sessionID)
}

func (a *App) cacheGroupSession(veIDs []string, sessionID string) {
	key := veGroupKey(veIDs)
	sessionID = strings.TrimSpace(sessionID)
	if key == "" || sessionID == "" {
		return
	}
	a.groupSessionCache.Store(key, sessionID)
}

// ArchiveVESession removes a session from the sticky cache, allowing a fresh
// session to be created on next conversation initiation.
func (a *App) ArchiveVESession(veID string) {
	a.veSessionCache.Delete(strings.TrimSpace(veID))
}

// ArchiveGroupSession removes a group session from the sticky cache.
func (a *App) ArchiveGroupSession(veIDs []string) {
	a.groupSessionCache.Delete(veGroupKey(veIDs))
}

// findActiveVESession checks the local cache and validates the session is still
// active on the Hub. Returns nil if no active session exists.
func (a *App) findActiveVESession(hubURL, token, veID string) *VESessionInfo {
	veID = strings.TrimSpace(veID)
	val, ok := a.veSessionCache.Load(veID)
	if !ok {
		return nil
	}
	sessionID, _ := val.(string)
	if sessionID == "" {
		return nil
	}

	// Validate the session is still active on the Hub.
	if !a.isSessionActive(hubURL, token, sessionID) {
		a.veSessionCache.Delete(veID)
		return nil
	}

	return &VESessionInfo{
		SessionID: sessionID,
		VEID:      veID,
	}
}

// findActiveGroupSession checks the local cache for a group session.
func (a *App) findActiveGroupSession(hubURL, token string, veIDs []string) *VESessionInfo {
	key := veGroupKey(veIDs)
	val, ok := a.groupSessionCache.Load(key)
	if !ok {
		return nil
	}
	sessionID, _ := val.(string)
	if sessionID == "" {
		return nil
	}

	if !a.isSessionActive(hubURL, token, sessionID) {
		a.groupSessionCache.Delete(key)
		return nil
	}

	return &VESessionInfo{
		SessionID: sessionID,
		VEID:      strings.Join(veIDs, ","),
		VEName:    "Group",
	}
}

// isSessionActive checks if a session is still active (not archived/cancelled) on the Hub.
func (a *App) isSessionActive(hubURL, token, sessionID string) bool {
	data, err := a.getHubJSON(hubURL, token, "/api/a2a/consultations/"+sessionID)
	if err != nil {
		return false
	}
	return isVEConsultationActiveJSON(data)
}

func isVEConsultationActiveJSON(data []byte) bool {
	var resp struct {
		Status     string `json:"status"`
		Discussion struct {
			Status string `json:"status"`
		} `json:"discussion"`
		Session *struct {
			Status string `json:"status"`
		} `json:"session"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return false
	}
	status := firstNonEmptyGroupString(resp.Status, resp.Discussion.Status)
	if resp.Session != nil {
		status = firstNonEmptyGroupString(status, resp.Session.Status)
	}
	return normalizeGroupDiscussionSessionStatus(status).IsOpen()
}
