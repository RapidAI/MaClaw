package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/wailsapp/wails/v2/pkg/runtime"
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
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		return nil, fmt.Errorf("load config: %w", cfgErr)
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
	return filterOwnVirtualEmployees(resp.Employees, groupDiscussionAgentID(cfg)), nil
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
	if info := a.findActiveVESession(veID); info != nil {
		return info, nil
	}

	// No active session found; create a new one.
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

	// Try to find an existing active group session with this participant set.
	if info := a.findActiveGroupSession(normalized); info != nil {
		return info, nil
	}

	// No active session; create via group consultation.
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
	client, _, err := a.veA2AHubClient()
	if err != nil {
		return nil, fmt.Errorf("create group session: %w", err)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	resp, err := client.CreateConsultation(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create group session: %w", err)
	}

	sessionID := resp.Discussion.ID
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("create group session: missing session id")
	}

	// Invite all VEs to the group.
	for _, veID := range normalized {
		if err := a.AddVEToGroup(sessionID, veID); err != nil {
			return nil, fmt.Errorf("invite digital employee %q: %w", veID, err)
		}
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
// When the local AI agent is enabled for this session, the message is dispatched
// directly to the local agent (zero network latency) while syncing
// targeted history to Hub. Otherwise, the message goes through Hub normally.
func (a *App) SendVEMessage(sessionID, content string) error {
	// Delegate to SendVEGroupMessage with no explicit mentions (single-responder routing)
	return a.SendVEGroupMessage(sessionID, content, nil)
}

// SendVEGroupMessage sends a message in a VE group conversation with @mention-based routing.
// mentionedIds controls which participant handles the message:
//   - nil or empty: prefer the local AI executor, then fall back to Hub
//   - contains the local AI participant or legacy alias: route to local AI only
//   - contains remote VE id: route to remote VE via Hub only
//   - contains both: use the same priority path as an unmentioned message
func (a *App) SendVEGroupMessage(sessionID, content string, mentionedIds []string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("message content is empty")
	}
	if len([]rune(content)) > 32000 {
		return fmt.Errorf("message exceeds 32,000 character limit")
	}

	msg := a2a.GroupDiscussionMessage{
		Kind:      a2a.MessageStatement,
		Content:   content,
		CreatedAt: time.Now(),
	}

	targets, err := a.resolveVEGroupMentionTargets(sessionID, content, mentionedIds)
	if err != nil {
		return err
	}

	// No explicit mentions, or mixed local+remote mentions, use the same
	// single-responder path as a normal chat message.
	if !targets.Explicit || (targets.Local && len(targets.RemoteToIDs) > 0) {
		if a.tryLocalExecutorDispatch(sessionID, msg) {
			return nil
		}
		return a.sendVEA2AMessage(sessionID, msg)
	}

	if targets.Local {
		if !a.tryLocalExecutorDispatch(sessionID, msg) {
			if _, err := a.RegisterLocalExecutorInGroup(sessionID); err != nil {
				return fmt.Errorf("local AI is not ready in this group: %w", err)
			}
			if !a.tryLocalExecutorDispatch(sessionID, msg) {
				return fmt.Errorf("local AI is not ready in this group; please add it again")
			}
		}
		return nil
	}

	if len(targets.RemoteToIDs) > 0 {
		msg.ToIDs = targets.RemoteToIDs
		return a.sendVEA2AMessage(sessionID, msg)
	}

	return fmt.Errorf("no valid mention target found")
}

type LocalGroupExecutorRegistration struct {
	SessionID     string `json:"session_id"`
	ParticipantID string `json:"participant_id"`
	DisplayName   string `json:"display_name"`
}

type veGroupMentionTargets struct {
	Explicit    bool
	Local       bool
	RemoteToIDs []string
}

func (a *App) resolveVEGroupMentionTargets(sessionID, content string, mentionedIds []string) (veGroupMentionTargets, error) {
	var targets veGroupMentionTargets
	localID := ""
	if cfg, err := a.LoadConfig(); err == nil {
		localID = strings.TrimSpace(groupDiscussionAgentID(cfg))
	}
	participantIDs := a.groupDiscussionParticipantIDs(sessionID, localID)
	var employeeIDs map[string]string
	seenRemote := map[string]struct{}{}
	for _, rawID := range mentionedIds {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		targets.Explicit = true
		if isLocalGroupMentionID(id, localID) {
			targets.Local = true
			continue
		}
		if employeeIDs == nil {
			employeeIDs = a.discoverableVEParticipantIDs()
		}
		canonical := canonicalGroupMentionTargetID(id, employeeIDs, participantIDs)
		if isLocalGroupMentionID(canonical, localID) {
			targets.Local = true
			continue
		}
		if canonical == "" {
			return veGroupMentionTargets{}, fmt.Errorf("mention target %s is not available in this discussion", id)
		}
		key := strings.ToLower(canonical)
		if _, ok := seenRemote[key]; ok {
			continue
		}
		seenRemote[key] = struct{}{}
		targets.RemoteToIDs = append(targets.RemoteToIDs, canonical)
	}
	if !targets.Explicit && contentMentionsLocalGroupAI(content, localID) {
		targets.Explicit = true
		targets.Local = true
	}
	return targets, nil
}

func contentMentionsLocalGroupAI(content, localID string) bool {
	compactContent := strings.ToLower(strings.Join(strings.Fields(content), ""))
	if compactContent == "" {
		return false
	}
	labels := []string{"local-maclaw", "localai", "local-ai", "\u672c\u673aAI", "\u672c\u6a5fAI"}
	if strings.TrimSpace(localID) != "" {
		labels = append(labels, strings.TrimSpace(localID))
	}
	for _, label := range labels {
		compactLabel := strings.ToLower(strings.Join(strings.Fields(label), ""))
		if compactLabel != "" && compactGroupMentionLabelMatches(compactContent, compactLabel) {
			return true
		}
	}
	return false
}

func compactGroupMentionLabelMatches(compactContent, compactLabel string) bool {
	needle := "@" + strings.ToLower(compactLabel)
	for offset := 0; offset < len(compactContent); {
		idx := strings.Index(compactContent[offset:], needle)
		if idx < 0 {
			return false
		}
		end := offset + idx + len(needle)
		if end >= len(compactContent) || !isASCIIGroupMentionContinuation(compactContent[end]) {
			return true
		}
		offset = end
	}
	return false
}

func isASCIIGroupMentionContinuation(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.'
}

func (a *App) groupDiscussionParticipantIDs(sessionID, localID string) map[string]string {
	ids := map[string]string{}
	client, cfg, err := a.veA2AHubClient()
	if err != nil {
		return ids
	}
	requesterID := strings.TrimSpace(firstNonEmptyGroupString(localID, groupDiscussionAgentID(cfg)))
	if requesterID == "" {
		return ids
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, strings.TrimSpace(sessionID), requesterID)
	if err != nil || detail.Session == nil {
		return ids
	}
	for _, participant := range detail.Session.Participants {
		id := strings.TrimSpace(participant.ID)
		if id != "" {
			ids[strings.ToLower(id)] = id
		}
	}
	return ids
}

func (a *App) discoverableVEParticipantIDs() map[string]string {
	ids := map[string]string{}
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return ids
	}
	employees, err := a.loadDiscoverableVEEntries(hubURL, token)
	if err != nil {
		return ids
	}
	for _, employee := range employees {
		machineID := strings.TrimSpace(employee.MachineID)
		profileID := strings.TrimSpace(employee.ID)
		canonical := firstNonEmptyGroupString(machineID, profileID)
		if canonical == "" {
			continue
		}
		ids[strings.ToLower(canonical)] = canonical
		if profileID != "" {
			ids[strings.ToLower(profileID)] = canonical
		}
		if machineID != "" {
			ids[strings.ToLower(machineID)] = canonical
		}
	}
	return ids
}

func canonicalGroupMentionTargetID(id string, employeeIDs, participantIDs map[string]string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	key := strings.ToLower(id)
	if canonical := strings.TrimSpace(employeeIDs[key]); canonical != "" {
		id = canonical
		key = strings.ToLower(id)
	}
	if canonical := strings.TrimSpace(participantIDs[key]); canonical != "" {
		return canonical
	}
	if len(participantIDs) == 0 {
		return id
	}
	return ""
}

func isLocalGroupMentionID(id, localID string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if localID != "" && strings.EqualFold(id, strings.TrimSpace(localID)) {
		return true
	}
	switch strings.ToLower(id) {
	case "local-maclaw", "local ai", "local-ai", "localai":
		return true
	}
	switch strings.Join(strings.Fields(id), "") {
	case "本机AI", "本機AI":
		return true
	default:
		return false
	}
}

func (a *App) localTargetedGroupMessage(msg a2a.GroupDiscussionMessage) (a2a.GroupDiscussionMessage, bool) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return a2a.GroupDiscussionMessage{}, false
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" {
		return a2a.GroupDiscussionMessage{}, false
	}
	msg.ToIDs = []string{localID}
	return msg, true
}

func (a *App) syncLocalDispatchInputToHub(sessionID string, msg a2a.GroupDiscussionMessage) {
	msg, ok := a.localTargetedGroupMessage(msg)
	if !ok {
		return
	}
	if err := a.sendVEA2AMessage(sessionID, msg); err != nil {
		log.Printf("[ve-group] failed to sync local dispatch input for session %s: %v", sessionID, err)
	}
}

func (a *App) sendVEA2AMessage(sessionID string, msg a2a.GroupDiscussionMessage) error {
	client, cfg, err := a.veA2AHubClient()
	if err != nil {
		return err
	}
	if msg.FromID == "" {
		msg.FromID = groupDiscussionAgentID(cfg)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := client.SendDiscussionMessage(ctx, sessionID, msg); err != nil {
		return err
	}
	if detail, err := client.GetConsultationDetailForAgent(ctx, sessionID, groupDiscussionAgentID(cfg)); err == nil {
		if store, storeErr := a.openGroupDiscussionHistoryStore(); storeErr == nil {
			_ = store.CacheDetail(ctx, detail, a.groupDiscussionAttachmentRoot)
			_ = store.Close()
		}
	}
	return nil
}

// tryLocalExecutorDispatch checks if local AI is enabled for this session
// and dispatches the message directly to the local agent if so.
// Returns true if local dispatch was performed, false otherwise.
func (a *App) tryLocalExecutorDispatch(sessionID string, msg a2a.GroupDiscussionMessage) bool {
	hubClient := a.hubClient()
	if hubClient == nil {
		return false
	}
	dispatcher := hubClient.groupChatDispatcher()
	if dispatcher == nil || !dispatcher.IsRegistered(sessionID) {
		return false
	}
	// Dispatch directly to local agent — bypasses Hub network round-trip
	dispatcher.HandleGroupMessage(sessionID, msg, true)
	return true
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

	client, _, err := a.veA2AHubClient()
	if err != nil {
		return err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.SetConsultationState(ctx, sessionID, "cancel")
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

	client, cfg, err := a.veA2AHubClient()
	if err != nil {
		return err
	}
	inviteeID := a.resolveVEInviteMachineID(hubURL, token, veID)
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	_, err = client.SendInvitation(ctx, sessionID, a2a.GroupInvitation{
		FromID: strings.TrimSpace(groupDiscussionAgentID(cfg)),
		ToID:   inviteeID,
		Role:   a2a.GroupRoleSpeak,
	})
	return err
}

// RegisterLocalExecutorInGroup adds the local machine to the Hub discussion
// and starts the local executor dispatcher for this session. This enables the
// local AI assistant to receive and respond with full tool access.
func (a *App) RegisterLocalExecutorInGroup(sessionID string) (*LocalGroupExecutorRegistration, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID is required")
	}

	// Register with Hub first so local responses can be synced back into
	// discussion history, then register the in-process dispatcher.
	client, cfg, err := a.veA2AHubClient()
	if err != nil {
		return nil, err
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" {
		return nil, fmt.Errorf("machine not registered")
	}

	if a.localParticipantCanAnswerInHubDiscussion(sessionID, localID, cfg) {
		a.registerLocalGroupDispatcher(sessionID)
		return localGroupExecutorRegistration(sessionID, localID), nil
	}

	ctx, cancel := groupDiscussionContext()
	defer cancel()
	inviteID, err := client.SendInvitation(ctx, sessionID, a2a.GroupInvitation{
		FromID: localID,
		ToID:   localID,
		Role:   a2a.GroupRoleSpeak,
	})
	if err != nil {
		return nil, fmt.Errorf("register local AI with Hub: %w", err)
	}
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return nil, fmt.Errorf("register local AI with Hub: missing invite id")
	}
	if err := client.AcceptInvite(ctx, inviteID, a2a.GroupInvitationResponse{FromID: localID, Reason: "local AI ready"}); err != nil {
		return nil, fmt.Errorf("accept local AI invite: %w", err)
	}

	a.registerLocalGroupDispatcher(sessionID)
	return localGroupExecutorRegistration(sessionID, localID), nil
}

func localGroupExecutorRegistration(sessionID, participantID string) *LocalGroupExecutorRegistration {
	return &LocalGroupExecutorRegistration{SessionID: strings.TrimSpace(sessionID), ParticipantID: strings.TrimSpace(participantID), DisplayName: "Local AI"}
}

func (a *App) localParticipantCanAnswerInHubDiscussion(sessionID, localID string, cfg corelib.AppConfig) bool {
	client, err := a2a.NewHubClientFromConfig(cfg)
	if err != nil {
		return false
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, sessionID, localID)
	if err != nil || detail.Session == nil {
		return false
	}
	for _, participant := range detail.Session.Participants {
		if !strings.EqualFold(strings.TrimSpace(participant.ID), localID) {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(participant.RoleCode))
		switch role {
		case "", "initiator", "speak", "speaker", "review", "participant", "executor":
			return true
		default:
			return false
		}
	}
	return false
}

func (a *App) registerLocalGroupDispatcher(sessionID string) {
	hubClient := a.hubClient()
	if hubClient == nil {
		return
	}
	dispatcher := hubClient.groupChatDispatcher()
	if dispatcher != nil {
		dispatcher.RegisterSession(sessionID)
	}
}

// resolveVEInviteMachineID maps a frontend VE id to the discussion participant machine id.
func (a *App) resolveVEInviteMachineID(hubURL, token, veID string) string {
	veID = strings.TrimSpace(veID)
	if veID == "" {
		return ""
	}
	employees, err := a.loadDiscoverableVEEntries(hubURL, token)
	if err != nil {
		return veID
	}
	return resolveVEInviteMachineID(employees, veID)
}

func (a *App) loadDiscoverableVEEntries(hubURL, token string) ([]VirtualEmployeeEntry, error) {
	data, err := a.getHubJSON(hubURL, token, "/api/ve/discoverable")
	if err != nil {
		return nil, err
	}
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		return nil, cfgErr
	}
	var resp struct {
		Employees []VirtualEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return filterOwnVirtualEmployees(resp.Employees, groupDiscussionAgentID(cfg)), nil
}

func filterOwnVirtualEmployees(employees []VirtualEmployeeEntry, localMachineID string) []VirtualEmployeeEntry {
	localMachineID = strings.TrimSpace(localMachineID)
	if localMachineID == "" || len(employees) == 0 {
		return employees
	}
	localVEID := virtualEmployeeIDForMachine(localMachineID)
	filtered := employees[:0]
	for _, employee := range employees {
		id := strings.TrimSpace(employee.ID)
		machineID := strings.TrimSpace(employee.MachineID)
		if strings.EqualFold(machineID, localMachineID) || strings.EqualFold(id, localMachineID) || strings.EqualFold(id, localVEID) {
			continue
		}
		filtered = append(filtered, employee)
	}
	return filtered
}

func virtualEmployeeIDForMachine(machineID string) string {
	cleaned := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(strings.TrimSpace(machineID))
	if cleaned == "" {
		return ""
	}
	return "ve_" + cleaned
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

// --- VE Allowed Directories ---

// SelectVEAllowedDirectory opens the native OS directory picker dialog.
// Returns the selected directory path, or empty string if cancelled.
func (a *App) SelectVEAllowedDirectory() (string, error) {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择允许访问的目录 / Select Allowed Directory",
	})
	if err != nil {
		return "", fmt.Errorf("open directory picker: %w", err)
	}
	return selection, nil
}

// GetVEAllowedDirectories returns the current allowed directories list from AppConfig.
// If the config is missing or invalid, returns an empty list (Requirement 2.5).
func (a *App) GetVEAllowedDirectories() ([]string, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		// Treat config load failure as empty list — do not crash (Requirement 2.5).
		return []string{}, nil
	}
	if cfg.VEAllowedDirectories == nil {
		return []string{}, nil
	}
	return cfg.VEAllowedDirectories, nil
}

// SetVEAllowedDirectories persists the updated allowed directories list to the
// local config file. Paths that do not exist on the filesystem are still accepted
// (the owner may add directories for drives not currently connected — Requirement 2.6).
// Duplicate paths are silently removed (case-insensitive on Windows, Requirement 1.6).
func (a *App) SetVEAllowedDirectories(dirs []string) error {
	// Reload the latest config to avoid overwriting concurrent changes.
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Backend deduplication: defense-in-depth against bypassed frontend checks
	// or direct config.json edits. Case-insensitive on Windows, slash-normalized.
	cfg.VEAllowedDirectories = deduplicateVEDirs(dirs)
	if err := a.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// deduplicateVEDirs removes duplicate directory paths from the list.
// Comparison is case-insensitive and slash-normalized (forward/backward slashes
// treated as equivalent) to match Windows filesystem behavior.
func deduplicateVEDirs(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	result := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		// Normalize for comparison: lowercase + forward slashes
		key := strings.ToLower(strings.ReplaceAll(d, "\\", "/"))
		// Also trim trailing slash for consistent comparison
		key = strings.TrimRight(key, "/")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, d) // preserve original casing/format
	}
	return result
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
func (a *App) findActiveVESession(veID string) *VESessionInfo {
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
	if !a.isSessionActive(sessionID) {
		a.veSessionCache.Delete(veID)
		return nil
	}

	return &VESessionInfo{
		SessionID: sessionID,
		VEID:      veID,
	}
}

// findActiveGroupSession checks the local cache for a group session.
func (a *App) findActiveGroupSession(veIDs []string) *VESessionInfo {
	key := veGroupKey(veIDs)
	val, ok := a.groupSessionCache.Load(key)
	if !ok {
		return nil
	}
	sessionID, _ := val.(string)
	if sessionID == "" {
		return nil
	}

	if !a.isSessionActive(sessionID) {
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
func (a *App) isSessionActive(sessionID string) bool {
	client, _, err := a.veA2AHubClient()
	if err != nil {
		return false
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	discussion, err := client.GetConsultation(ctx, sessionID)
	if err != nil {
		return false
	}
	return normalizeGroupDiscussionSessionStatus(discussion.Status).IsOpen()
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
