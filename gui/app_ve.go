package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	veSessionActiveValidationTTL = 30 * time.Second
	veDiscoverableCacheTTL       = 10 * time.Second
	veAvatarImageMaxBytes        = 1024 * 1024
	veAvatarDataURLMaxLength     = len("data:image/jpeg;base64,") + ((veAvatarImageMaxBytes+2)/3)*4
)

var (
	veGroupInviteJoinTimeout   = 8 * time.Second
	veGroupInviteJoinPollDelay = 250 * time.Millisecond
)

type veDiscoverableCacheEntry struct {
	expiresAt time.Time
	employees []VirtualEmployeeEntry
}

// VirtualEmployeeEntry is the frontend-facing VE data structure.
type VirtualEmployeeEntry struct {
	ID               string   `json:"id"`
	MachineID        string   `json:"machine_id,omitempty"`
	Name             string   `json:"name"`
	SkillDescription string   `json:"skill_description"`
	AvatarDataURL    string   `json:"avatar_data_url,omitempty"`
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
func (a *App) RegisterVirtualEmployee(name, skillDesc, policy string, list []string, avatarDataURL string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}
	avatarDataURL = strings.TrimSpace(avatarDataURL)
	if err := validateVEAvatarDataURL(avatarDataURL); err != nil {
		return err
	}

	body := map[string]any{
		"name":              strings.TrimSpace(name),
		"skill_description": strings.TrimSpace(skillDesc),
		"access_policy":     strings.TrimSpace(policy),
		"avatar_data_url":   avatarDataURL,
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
func (a *App) UpdateVESettings(name, skillDesc, policy string, list []string, avatarDataURL string) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return err
	}
	avatarDataURL = strings.TrimSpace(avatarDataURL)
	if err := validateVEAvatarDataURL(avatarDataURL); err != nil {
		return err
	}

	body := map[string]any{
		"name":              strings.TrimSpace(name),
		"skill_description": strings.TrimSpace(skillDesc),
		"access_policy":     strings.TrimSpace(policy),
		"avatar_data_url":   avatarDataURL,
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

func validateVEAvatarDataURL(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > veAvatarDataURLMaxLength {
		return fmt.Errorf("avatar image is too large")
	}
	lower := strings.ToLower(value)
	declaredType := ""
	switch {
	case strings.HasPrefix(lower, "data:image/png;base64,"):
		declaredType = "image/png"
	case strings.HasPrefix(lower, "data:image/jpeg;base64,"), strings.HasPrefix(lower, "data:image/jpg;base64,"):
		declaredType = "image/jpeg"
	case strings.HasPrefix(lower, "data:image/webp;base64,"):
		declaredType = "image/webp"
	default:
		return fmt.Errorf("avatar image must be a PNG, JPEG, or WebP data URL")
	}
	comma := strings.Index(value, ",")
	if comma < 0 || comma == len(value)-1 {
		return fmt.Errorf("avatar image must be a PNG, JPEG, or WebP data URL")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value[comma+1:])
	if err != nil || len(decoded) > veAvatarImageMaxBytes || http.DetectContentType(decoded) != declaredType {
		return fmt.Errorf("avatar image must be a valid PNG, JPEG, or WebP data URL")
	}
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

	// Try to find an existing active session with this VE first.
	if info := a.findActiveVESession(veID); info != nil {
		return info, nil
	}
	if info := a.findCachedVEDirectSession(veID); info != nil {
		return info, nil
	}

	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return nil, err
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
	if strings.TrimSpace(info.SessionID) == "" {
		var pending struct {
			Status    string `json:"status"`
			RequestID string `json:"request_id"`
			ExpiresAt string `json:"expires_at"`
		}
		_ = json.Unmarshal(data, &pending)
		if strings.TrimSpace(pending.Status) == "pending_confirmation" {
			detail := "pending_confirmation"
			if requestID := strings.TrimSpace(pending.RequestID); requestID != "" {
				detail += " request_id=" + requestID
			}
			if expiresAt := strings.TrimSpace(pending.ExpiresAt); expiresAt != "" {
				detail += " expires_at=" + expiresAt
			}
			return nil, fmt.Errorf("%s", detail)
		}
		return nil, fmt.Errorf("create session: missing session id")
	}

	// Cache the new session for future lookups.
	a.cacheVESession(veID, info.SessionID)
	a.cacheVEGroupDefaultResponder(info.SessionID, a.resolveVEInviteMachineID(hubURL, token, firstNonEmptyGroupString(info.VEID, veID)))

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

	// Invite all VEs to the group. Resolve invite IDs once, then send the
	// independent invitations in parallel so group startup does not scale linearly.
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	employees, _ := a.loadDiscoverableVEEntries(hubURL, token)
	inviteErrs := make(chan error, len(normalized))
	var wg sync.WaitGroup
	for _, veID := range normalized {
		veID := veID
		inviteeID := resolveVEInviteMachineID(employees, veID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.sendVEGroupInvitation(client, sessionID, agentID, inviteeID); err != nil {
				inviteErrs <- fmt.Errorf("invite digital employee %q: %w", veID, err)
			}
		}()
	}
	wg.Wait()
	close(inviteErrs)
	for err := range inviteErrs {
		if err != nil {
			return nil, err
		}
	}

	// Cache the group session.
	a.cacheGroupSession(normalized, sessionID)
	if len(normalized) > 0 {
		a.cacheVEGroupDefaultResponder(sessionID, resolveVEInviteMachineID(employees, normalized[0]))
	}

	return &VESessionInfo{
		SessionID: sessionID,
		VEID:      strings.Join(normalized, ","),
		VEName:    "Group",
	}, nil
}

// SendVEMessage sends a message in a VE conversation.
// Unmentioned messages go through Hub with explicit remote targets so the
// discussion owner/default VE keeps the floor and local AI stays quiet unless
// mentioned.
func (a *App) SendVEMessage(sessionID, content string) error {
	// Delegate to SendVEGroupMessage with no explicit mentions (single-responder routing)
	return a.SendVEGroupMessage(sessionID, content, nil)
}

// SendVEGroupMessage sends a message in a VE group conversation with @mention-based routing.
// mentionedIds controls which participant handles the message:
//   - nil or empty: send to Hub so the discussion owner/default VE answers
//   - contains the local AI participant or legacy alias: route to local AI only
//   - contains remote VE id: route to remote VE via Hub only
//   - contains both: route to local AI and the mentioned remote VE participants
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

	// No explicit mentions preserve 1:1 default responder semantics. Once more
	// than one remote speaking participant is present, it becomes a real group
	// turn targeted to all writable digital employees while the local AI stays
	// quiet unless explicitly targeted.
	if !targets.Explicit {
		msg.ToIDs = a.groupDiscussionUnmentionedTargetIDs(sessionID)
		return a.sendVEA2AMessage(sessionID, msg)
	}

	if targets.Local && len(targets.RemoteToIDs) > 0 {
		if !a.tryLocalExecutorDispatch(sessionID, msg) {
			if _, err := a.RegisterLocalExecutorInGroup(sessionID); err != nil {
				return fmt.Errorf("local AI is not ready in this group: %w", err)
			}
			if !a.tryLocalExecutorDispatch(sessionID, msg) {
				return fmt.Errorf("local AI is not ready in this group; please add it again")
			}
		}
		msg.ToIDs = targets.RemoteToIDs
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
	var employeeIDs map[string]string
	var participantIDs map[string]string
	loadParticipantIDs := func() map[string]string {
		if participantIDs == nil {
			participantIDs = a.groupDiscussionParticipantIDs(sessionID, localID)
		}
		return participantIDs
	}
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
		if participantIDs == nil {
			participantIDs = a.groupDiscussionParticipantIDs(sessionID, localID)
		}
		if canonical := strings.TrimSpace(participantIDs[strings.ToLower(id)]); canonical != "" {
			if !veGroupParticipantSeen(seenRemote, canonical) {
				targets.RemoteToIDs = append(targets.RemoteToIDs, canonical)
			}
			continue
		}
		if canonical := canonicalParticipantMentionByGeneratedVEID(id, participantIDs); canonical != "" {
			if !veGroupParticipantSeen(seenRemote, canonical) {
				targets.RemoteToIDs = append(targets.RemoteToIDs, canonical)
			}
			continue
		}
		if employeeIDs == nil {
			employeeIDs = a.discoverableVEParticipantIDs()
		}
		canonical := canonicalGroupMentionTargetID(id, employeeIDs, loadParticipantIDs())
		if isLocalGroupMentionID(canonical, localID) {
			targets.Local = true
			continue
		}
		if canonical == "" {
			return veGroupMentionTargets{}, fmt.Errorf("mention target %s is not available in this discussion", id)
		}
		if veGroupParticipantSeen(seenRemote, canonical) {
			continue
		}
		targets.RemoteToIDs = append(targets.RemoteToIDs, canonical)
	}
	if !targets.Explicit && contentMentionsLocalGroupAI(content, localID) {
		targets.Explicit = true
		targets.Local = true
	}
	return targets, nil
}

func veGroupParticipantSeen(seen map[string]struct{}, participantID string) bool {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return true
	}
	aliases := map[string]string{}
	addGroupDiscussionHistoryParticipantAliases(aliases, participantID)
	for key := range aliases {
		if _, ok := seen[key]; ok {
			return true
		}
	}
	for key := range aliases {
		seen[key] = struct{}{}
	}
	return false
}

func canonicalParticipantMentionByGeneratedVEID(id string, participantIDs map[string]string) string {
	id = strings.TrimSpace(id)
	if id == "" || len(participantIDs) == 0 {
		return ""
	}
	for _, participantID := range participantIDs {
		participantID = strings.TrimSpace(participantID)
		if participantID != "" && isVEGroupDefaultResponderMatch(participantID, id) {
			return participantID
		}
	}
	return ""
}

func contentMentionsLocalGroupAI(content, localID string) bool {
	compactContent := strings.ToLower(strings.Join(strings.Fields(content), ""))
	if compactContent == "" {
		return false
	}
	labels := []string{"local-maclaw", "localai", "local-ai", "\u672c\u673aAI", "\u672c\u6a5fAI", "\u672c\u5730AI"}
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
			addGroupDiscussionHistoryParticipantAliases(ids, id)
		}
	}
	return ids
}

func (a *App) groupDiscussionUnmentionedTargetIDs(sessionID string) []string {
	preferredID := a.cachedVEGroupDefaultResponderID(sessionID)
	client, cfg, err := a.veA2AHubClient()
	if err != nil {
		return singleGroupDiscussionTarget(preferredID)
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" {
		return singleGroupDiscussionTarget(preferredID)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, strings.TrimSpace(sessionID), localID)
	if err != nil || detail.Session == nil {
		return singleGroupDiscussionTarget(preferredID)
	}
	candidates := make([]string, 0, len(detail.Session.Participants))
	for _, participant := range detail.Session.Participants {
		id := strings.TrimSpace(participant.ID)
		if !isVEGroupDefaultResponderCandidate(id, participant.RoleCode, localID) {
			continue
		}
		candidates = append(candidates, id)
	}
	candidates = dedupeVEGroupParticipantIDs(candidates)
	if len(candidates) > 0 {
		if len(candidates) == 1 {
			a.cacheVEGroupDefaultResponder(sessionID, candidates[0])
		}
		return candidates
	}
	return singleGroupDiscussionTarget(preferredID)
}

func dedupeVEGroupParticipantIDs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			continue
		}
		duplicate := false
		for _, existing := range out {
			if veGroupParticipantIdentityMatches(existing, id) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		out = append(out, id)
	}
	return out
}

func singleGroupDiscussionTarget(id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return []string{id}
}

func isVEGroupDefaultResponderCandidate(id, roleCode, localID string) bool {
	id = strings.TrimSpace(id)
	localID = strings.TrimSpace(localID)
	if id == "" || isVEGroupDefaultResponderMatch(id, localID) || isVEGroupLocalHumanID(id) || isVEGroupLocalAIID(id) {
		return false
	}
	role := strings.ToLower(strings.TrimSpace(roleCode))
	switch role {
	case "", "speak", "speaker", "participant", "review":
		return true
	default:
		return false
	}
}

func isVEGroupLocalAIID(id string) bool {
	id = strings.TrimSpace(id)
	return veGroupParticipantIdentityMatches(id, "local-maclaw")
}

func isVEGroupLocalHumanID(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "me", "user", "local", "local-user", "operator", "desktop-user", "initiator":
		return true
	default:
		return false
	}
}

func isVEGroupDefaultResponderMatch(participantID, preferredID string) bool {
	return veGroupParticipantIdentityMatches(participantID, preferredID)
}

func veGroupParticipantIdentityMatches(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	aKeys := map[string]string{}
	addGroupDiscussionHistoryParticipantAliases(aKeys, a)
	bKeys := map[string]string{}
	addGroupDiscussionHistoryParticipantAliases(bKeys, b)
	for key := range bKeys {
		if _, ok := aKeys[key]; ok {
			return true
		}
	}
	return false
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
			addGroupDiscussionHistoryParticipantAliases(ids, profileID)
			for key, value := range ids {
				if veGroupParticipantIdentityMatches(value, profileID) {
					ids[key] = canonical
				}
			}
		}
		if machineID != "" {
			addGroupDiscussionHistoryParticipantAliases(ids, machineID)
			for key, value := range ids {
				if veGroupParticipantIdentityMatches(value, machineID) {
					ids[key] = canonical
				}
			}
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
	if localID != "" && veGroupParticipantIdentityMatches(id, localID) {
		return true
	}
	switch strings.ToLower(id) {
	case "local-maclaw", "local ai", "local-ai", "localai":
		return true
	}
	switch strings.Join(strings.Fields(id), "") {
	case "本机AI", "本機AI", "本地AI":
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
	msg = sanitizeLocalDispatchMessageForHub(msg)
	if strings.TrimSpace(msg.Content) == "" && !HasAttachments(msg) {
		return a2a.GroupDiscussionMessage{}, false
	}
	return msg, true
}

func sanitizeLocalDispatchMessageForHub(msg a2a.GroupDiscussionMessage) a2a.GroupDiscussionMessage {
	if len(msg.TextAttachments) > 0 {
		textAttachments := make([]a2a.TextAttachment, 0, len(msg.TextAttachments))
		for _, att := range msg.TextAttachments {
			att.LocalPath = ""
			textAttachments = append(textAttachments, att)
		}
		msg.TextAttachments = textAttachments
	}
	if len(msg.ImageAttachments) > 0 {
		imageAttachments := make([]a2a.ImageAttachment, 0, len(msg.ImageAttachments))
		for _, att := range msg.ImageAttachments {
			att.LocalPath = ""
			if strings.TrimSpace(att.FileURL) == "" {
				continue
			}
			imageAttachments = append(imageAttachments, att)
		}
		msg.ImageAttachments = imageAttachments
	}
	if len(msg.FileAttachments) > 0 {
		fileAttachments := make([]a2a.FileAttachment, 0, len(msg.FileAttachments))
		for _, att := range msg.FileAttachments {
			att.LocalPath = ""
			if strings.TrimSpace(att.FileURL) == "" {
				continue
			}
			fileAttachments = append(fileAttachments, att)
		}
		msg.FileAttachments = fileAttachments
	}
	return msg
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
	started := time.Now()
	log.Printf("[ve] send discussion message start session=%s from=%s to=%v kind=%s content_chars=%d", sessionID, msg.FromID, msg.ToIDs, msg.Kind, len([]rune(msg.Content)))
	if err := client.SendDiscussionMessage(ctx, sessionID, msg); err != nil {
		log.Printf("[ve] send discussion message failed session=%s from=%s to=%v kind=%s duration=%s: %v", sessionID, msg.FromID, msg.ToIDs, msg.Kind, time.Since(started), err)
		return fmt.Errorf("send digital employee message: %w", err)
	}
	log.Printf("[ve] send discussion message ok session=%s from=%s to=%v kind=%s duration=%s", sessionID, msg.FromID, msg.ToIDs, msg.Kind, time.Since(started))
	if shouldRefreshVEA2ADetailAfterSend(msg) {
		a.cacheVEA2ADetailAsync(client, sessionID, groupDiscussionAgentID(cfg))
	}
	return nil
}

func shouldRefreshVEA2ADetailAfterSend(msg a2a.GroupDiscussionMessage) bool {
	return msg.Kind != a2a.MessageStreamChunk
}

func (a *App) cacheVEA2ADetailAsync(client *a2a.HubClient, sessionID, agentID string) {
	if a == nil || client == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	state := &veDetailRefreshState{}
	if existing, loaded := a.veDetailRefreshCache.LoadOrStore(sessionID, state); loaded {
		if refreshState, ok := existing.(*veDetailRefreshState); ok && refreshState != nil {
			refreshState.mu.Lock()
			refreshState.dirty = true
			refreshState.mu.Unlock()
		}
		return
	}
	go func() {
		defer a.veDetailRefreshCache.Delete(sessionID)
		for {
			a.cacheVEA2ADetailSnapshot(client, sessionID, agentID)
			state.mu.Lock()
			if !state.dirty {
				state.mu.Unlock()
				return
			}
			state.dirty = false
			state.mu.Unlock()
		}
	}()
}

func (a *App) cacheVEA2ADetailSnapshot(client *a2a.HubClient, sessionID, agentID string) {
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, sessionID, agentID)
	if err != nil {
		log.Printf("[ve] async detail refresh failed for session %s: %v", sessionID, err)
		return
	}
	store, storeErr := a.openGroupDiscussionHistoryStore()
	if storeErr != nil {
		log.Printf("[ve] async detail cache unavailable for session %s: %v", sessionID, storeErr)
		return
	}
	defer store.Close()
	if err := store.CacheDetail(ctx, detail, a.groupDiscussionAttachmentRoot); err != nil {
		log.Printf("[ve] async detail cache failed for session %s: %v", sessionID, err)
	}
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
	a.veDefaultResponder.Delete(sessionID)
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
	fromID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if a.veGroupParticipantAvailable(client, sessionID, fromID, inviteeID) {
		a.cacheVESession(veID, sessionID)
		a.cacheVESession(inviteeID, sessionID)
		a.cacheVESession(virtualEmployeeIDForMachine(inviteeID), sessionID)
		return nil
	}
	inviteID, err := a.sendVEGroupInvitation(client, sessionID, fromID, inviteeID)
	if err != nil {
		return err
	}
	if err := a.waitForVEGroupParticipant(client, sessionID, fromID, inviteeID, inviteID, veGroupInviteJoinTimeout); err != nil {
		return err
	}
	a.cacheVESession(veID, sessionID)
	a.cacheVESession(inviteeID, sessionID)
	a.cacheVESession(virtualEmployeeIDForMachine(inviteeID), sessionID)
	return nil
}

func (a *App) sendVEGroupInvitation(client *a2a.HubClient, sessionID, fromID, inviteeID string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("Hub client is required")
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	inviteID, err := client.SendInvitation(ctx, sessionID, a2a.GroupInvitation{
		FromID:  strings.TrimSpace(fromID),
		ToID:    inviteeID,
		Role:    a2a.GroupRoleSpeak,
		Trusted: true,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(inviteID) == "" {
		return "", fmt.Errorf("create invitation: missing invite id")
	}
	return inviteID, nil
}

func (a *App) waitForVEGroupParticipant(client *a2a.HubClient, sessionID, requesterID, participantID, inviteID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if a.veGroupParticipantAvailable(client, sessionID, requesterID, participantID) {
			return nil
		}
		if status, reason := a.veGroupInvitationStatus(client, inviteID, requesterID); strings.EqualFold(status, "reject") {
			if reason != "" {
				return fmt.Errorf("invitation %s rejected: %s", inviteID, reason)
			}
			return fmt.Errorf("invitation %s rejected", inviteID)
		}
		if time.Now().Add(veGroupInviteJoinPollDelay).After(deadline) {
			return fmt.Errorf("invitation sent but participant %s has not joined discussion %s yet", participantID, sessionID)
		}
		time.Sleep(veGroupInviteJoinPollDelay)
	}
}

func (a *App) veGroupInvitationStatus(client *a2a.HubClient, inviteID, requesterID string) (string, string) {
	if client == nil || strings.TrimSpace(inviteID) == "" || strings.TrimSpace(requesterID) == "" {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	invite, ok, err := client.GetSentInvite(ctx, requesterID, inviteID)
	if err != nil {
		return "", ""
	}
	if ok {
		return strings.TrimSpace(invite.Status), strings.TrimSpace(invite.Reason)
	}
	return "", ""
}

func (a *App) veGroupParticipantAvailable(client *a2a.HubClient, sessionID, requesterID, participantID string) bool {
	if client == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	requesterID = strings.TrimSpace(requesterID)
	participantID = strings.TrimSpace(participantID)
	if sessionID == "" || requesterID == "" || participantID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, sessionID, requesterID)
	if err != nil || detail.Session == nil {
		return false
	}
	for _, participant := range detail.Session.Participants {
		if isVEGroupDefaultResponderMatch(participant.ID, participantID) {
			return true
		}
	}
	return false
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
	if a.localGroupDispatcherRegistered(sessionID) {
		return localGroupExecutorRegistration(sessionID, localID), nil
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
		if !veGroupParticipantIdentityMatches(participant.ID, localID) {
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

func (a *App) localGroupDispatcherRegistered(sessionID string) bool {
	hubClient := a.hubClient()
	if hubClient == nil {
		return false
	}
	dispatcher := hubClient.groupChatDispatcher()
	return dispatcher != nil && dispatcher.IsRegistered(sessionID)
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
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		return nil, cfgErr
	}
	localID := groupDiscussionAgentID(cfg)
	cacheKey := strings.Join([]string{strings.TrimRight(strings.TrimSpace(hubURL), "/"), strings.TrimSpace(token), strings.TrimSpace(localID)}, "\x00")
	if cacheKey != "\x00\x00" {
		if val, ok := a.veDiscoverableCache.Load(cacheKey); ok {
			entry, ok := val.(veDiscoverableCacheEntry)
			if ok && time.Now().Before(entry.expiresAt) {
				return cloneVirtualEmployeeEntries(entry.employees), nil
			}
			a.veDiscoverableCache.Delete(cacheKey)
		}
	}
	data, err := a.getHubJSON(hubURL, token, "/api/ve/discoverable")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Employees []VirtualEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	employees := filterOwnVirtualEmployees(resp.Employees, localID)
	if cacheKey != "\x00\x00" {
		a.veDiscoverableCache.Store(cacheKey, veDiscoverableCacheEntry{expiresAt: time.Now().Add(veDiscoverableCacheTTL), employees: cloneVirtualEmployeeEntries(employees)})
	}
	return employees, nil
}

func cloneVirtualEmployeeEntries(employees []VirtualEmployeeEntry) []VirtualEmployeeEntry {
	if len(employees) == 0 {
		return nil
	}
	out := make([]VirtualEmployeeEntry, len(employees))
	copy(out, employees)
	for i := range out {
		out[i].Whitelist = append([]string(nil), out[i].Whitelist...)
		out[i].Blacklist = append([]string(nil), out[i].Blacklist...)
	}
	return out
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
		if veGroupParticipantIdentityMatches(machineID, localMachineID) || veGroupParticipantIdentityMatches(id, localMachineID) || veGroupParticipantIdentityMatches(id, localVEID) {
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
		if veGroupParticipantIdentityMatches(employee.ID, value) || veGroupParticipantIdentityMatches(employee.MachineID, value) {
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
	switch decision {
	case "allow", "allow_once", "allow_long", "deny", "block":
	default:
		return fmt.Errorf("decision must be 'allow_once', 'allow_long', 'allow', 'deny', or 'block'")
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
	a.markVESessionActive(sessionID)
}

func (a *App) cacheGroupSession(veIDs []string, sessionID string) {
	key := veGroupKey(veIDs)
	sessionID = strings.TrimSpace(sessionID)
	if key == "" || sessionID == "" {
		return
	}
	a.groupSessionCache.Store(key, sessionID)
	a.markVESessionActive(sessionID)
}

func (a *App) cacheVEGroupDefaultResponder(sessionID, responderID string) {
	sessionID = strings.TrimSpace(sessionID)
	responderID = strings.TrimSpace(responderID)
	if sessionID == "" || responderID == "" {
		return
	}
	a.veDefaultResponder.Store(sessionID, responderID)
}

func (a *App) cachedVEGroupDefaultResponderID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	val, ok := a.veDefaultResponder.Load(sessionID)
	if !ok {
		return ""
	}
	responderID, _ := val.(string)
	return strings.TrimSpace(responderID)
}

func (a *App) groupDiscussionDefaultResponderID(sessionID string) string {
	targets := a.groupDiscussionUnmentionedTargetIDs(sessionID)
	if len(targets) == 1 {
		return strings.TrimSpace(targets[0])
	}
	return ""
}

// ArchiveVESession removes a session from the sticky cache, allowing a fresh
// session to be created on next conversation initiation.
func (a *App) ArchiveVESession(veID string) {
	veID = strings.TrimSpace(veID)
	if val, ok := a.veSessionCache.Load(veID); ok {
		if sessionID, _ := val.(string); sessionID != "" {
			a.veSessionActiveCache.Delete(sessionID)
		}
	}
	a.veSessionCache.Delete(veID)
}

// ArchiveGroupSession removes a group session from the sticky cache.
func (a *App) ArchiveGroupSession(veIDs []string) {
	key := veGroupKey(veIDs)
	if val, ok := a.groupSessionCache.Load(key); ok {
		if sessionID, _ := val.(string); sessionID != "" {
			a.veSessionActiveCache.Delete(sessionID)
		}
	}
	a.groupSessionCache.Delete(key)
}

func (a *App) markVESessionActive(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	a.veSessionActiveCache.Store(sessionID, time.Now())
}

func (a *App) hasFreshVESessionActiveValidation(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	val, ok := a.veSessionActiveCache.Load(sessionID)
	if !ok {
		return false
	}
	checkedAt, ok := val.(time.Time)
	if !ok || time.Since(checkedAt) > veSessionActiveValidationTTL {
		a.veSessionActiveCache.Delete(sessionID)
		return false
	}
	return true
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

func (a *App) findCachedVEDirectSession(veID string) *VESessionInfo {
	veID = strings.TrimSpace(veID)
	if veID == "" {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" {
		return nil
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	store, err := a.openGroupDiscussionHistoryStore()
	if err != nil {
		return nil
	}
	defer store.Close()
	summaries, err := store.CachedSummaries(ctx, false)
	if err != nil || len(summaries) == 0 {
		return nil
	}
	targetIDs := baseVEDirectSessionCandidateIDs(veID)
	if info := a.findCachedVEDirectSessionWithCandidates(summaries, localID, veID, targetIDs); info != nil {
		return info
	}
	if a.addDiscoverableVEDirectSessionCandidateIDs(cfg, veID, targetIDs) {
		if info := a.findCachedVEDirectSessionWithCandidates(summaries, localID, veID, targetIDs); info != nil {
			return info
		}
	}
	return nil
}

func (a *App) findCachedVEDirectSessionWithCandidates(summaries []a2a.HubDiscussionSummary, localID, veID string, targetIDs map[string]struct{}) *VESessionInfo {
	for _, summary := range summaries {
		if !isCachedVEDirectSessionMatch(summary, localID, targetIDs) {
			continue
		}
		a.cacheVESession(veID, summary.ID)
		for _, participantID := range summary.ParticipantIDs {
			participantID = strings.TrimSpace(participantID)
			if participantID != "" && isVEGroupDefaultResponderCandidate(participantID, "", localID) {
				a.cacheVEGroupDefaultResponder(summary.ID, participantID)
				a.cacheVESession(participantID, summary.ID)
				a.cacheVESession(virtualEmployeeIDForMachine(participantID), summary.ID)
			}
		}
		return &VESessionInfo{SessionID: summary.ID, VEID: veID}
	}
	return nil
}

func baseVEDirectSessionCandidateIDs(veID string) map[string]struct{} {
	candidates := map[string]struct{}{}
	addVEGroupParticipantIdentityKeys(candidates, veID)
	return candidates
}

func (a *App) addDiscoverableVEDirectSessionCandidateIDs(cfg corelib.AppConfig, veID string, candidates map[string]struct{}) bool {
	before := len(candidates)
	if hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/"); hubURL != "" {
		if employees, err := a.loadDiscoverableVEEntries(hubURL, strings.TrimSpace(cfg.RemoteMachineToken)); err == nil {
			for _, employee := range employees {
				if veGroupParticipantIdentityMatches(employee.ID, veID) || veGroupParticipantIdentityMatches(employee.MachineID, veID) {
					for _, id := range []string{employee.ID, employee.MachineID, virtualEmployeeIDForMachine(employee.MachineID)} {
						addVEGroupParticipantIdentityKeys(candidates, id)
					}
				}
			}
		}
	}
	return len(candidates) > before
}

func isCachedVEDirectSessionMatch(summary a2a.HubDiscussionSummary, localID string, targetIDs map[string]struct{}) bool {
	if strings.TrimSpace(summary.ID) == "" || !normalizeGroupDiscussionSessionStatus(summary.Status).IsOpen() {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(summary.LocalRelation), "initiated_by_me") && strings.TrimSpace(summary.LocalRelation) != "" {
		return false
	}
	hasLocal := false
	hasTarget := false
	participants := dedupeVEGroupParticipantIDs(summary.ParticipantIDs)
	for _, participantID := range participants {
		participantID = strings.TrimSpace(participantID)
		if veGroupParticipantIdentityMatches(participantID, localID) {
			hasLocal = true
			continue
		}
		if veGroupParticipantIDInSet(participantID, targetIDs) {
			hasTarget = true
		}
	}
	return len(participants) == 2 && hasLocal && hasTarget
}

func addVEGroupParticipantIdentityKeys(target map[string]struct{}, id string) {
	aliases := map[string]string{}
	addGroupDiscussionHistoryParticipantAliases(aliases, id)
	for key := range aliases {
		target[key] = struct{}{}
	}
}

func veGroupParticipantIDInSet(id string, candidates map[string]struct{}) bool {
	aliases := map[string]string{}
	addGroupDiscussionHistoryParticipantAliases(aliases, id)
	for key := range aliases {
		if _, ok := candidates[key]; ok {
			return true
		}
	}
	return false
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
	if a.hasFreshVESessionActiveValidation(sessionID) {
		return true
	}
	client, _, err := a.veA2AHubClient()
	if err != nil {
		return false
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	discussion, err := client.GetConsultation(ctx, sessionID)
	if err != nil {
		a.veSessionActiveCache.Delete(strings.TrimSpace(sessionID))
		return false
	}
	active := normalizeGroupDiscussionSessionStatus(discussion.Status).IsOpen()
	if active {
		a.markVESessionActive(sessionID)
	} else {
		a.veSessionActiveCache.Delete(strings.TrimSpace(sessionID))
	}
	return active
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
