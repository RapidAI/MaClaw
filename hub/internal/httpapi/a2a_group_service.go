package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
)

const (
	groupDiscussionPersistenceTimeout = 5 * time.Second
)

type GroupDiscussionService struct {
	mu       sync.RWMutex
	sessions map[string]map[string]*corea2a.Session
	profiles map[string]map[string]corea2a.GroupProfile
	invites  map[string]map[string]groupInviteRecord
	db       *sql.DB
}

type groupInviteRecord struct {
	ID          string
	TenantID    string
	SessionID   string
	Invite      corea2a.GroupInvitation
	Status      string
	CreatedAt   time.Time
	RespondedAt time.Time
}

func NewGroupDiscussionService(dbs ...*sql.DB) *GroupDiscussionService {
	s := &GroupDiscussionService{sessions: map[string]map[string]*corea2a.Session{}, profiles: map[string]map[string]corea2a.GroupProfile{}, invites: map[string]map[string]groupInviteRecord{}}
	if len(dbs) > 0 && dbs[0] != nil {
		s.db = dbs[0]
		if err := s.loadPersistentTables(); err != nil {
			log.Printf("[a2a-group] load persisted state failed: %v", err)
		}
	}
	return s
}

type CreateSessionRequest struct {
	Topic          string                 `json:"topic"`
	Goal           string                 `json:"goal"`
	OrgUnitID      string                 `json:"org_unit_id"`
	DepartmentID   string                 `json:"department_id"`
	Participants   []corea2a.Participant  `json:"participants"`
	DecisionPolicy corea2a.DecisionPolicy `json:"decision_policy"`
}

type ListSessionsFilter struct {
	OrgUnitID     string
	Status        corea2a.SessionStatus
	ParticipantID string
	Role          string
	Limit         int
	Offset        int
}

type ListInvitationsFilter struct {
	ToID   string
	Status string
	Limit  int
	Offset int
}

type AddMessageRequest struct {
	FromID           string                    `json:"from_id"`
	ToIDs            []string                  `json:"to_ids"`
	Kind             corea2a.MessageKind       `json:"kind"`
	Content          string                    `json:"content"`
	Evidence         []string                  `json:"evidence"`
	TextAttachments  []corea2a.TextAttachment  `json:"text_attachments,omitempty"`
	ImageAttachments []corea2a.ImageAttachment `json:"image_attachments,omitempty"`
	FileAttachments  []corea2a.FileAttachment  `json:"file_attachments,omitempty"`
}

type AddProposalRequest struct {
	AuthorID    string   `json:"author_id"`
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Goals       []string `json:"goals"`
	Constraints []string `json:"constraints"`
	Risks       []string `json:"risks"`
}

type AddReviewRequest struct {
	ProposalID string                 `json:"proposal_id"`
	ReviewerID string                 `json:"reviewer_id"`
	Position   corea2a.ReviewPosition `json:"position"`
	Comment    string                 `json:"comment"`
}

type DecideRequest struct {
	ProposalID string   `json:"proposal_id"`
	Summary    string   `json:"summary"`
	Rationale  string   `json:"rationale"`
	RollbackOn []string `json:"rollback_on"`
}

type EscalateRequest struct {
	RaisedBy string `json:"raised_by"`
	Reason   string `json:"reason"`
	Target   string `json:"target"`
}

type AdminGroupDiscussionSnapshot struct {
	Experts       []corea2a.GroupProfile         `json:"experts"`
	Discussions   []corea2a.HubDiscussionSummary `json:"discussions"`
	ActiveExperts int                            `json:"active_experts"`
	TotalExperts  int                            `json:"total_experts"`
}

func (s *GroupDiscussionService) UpsertExpertProfile(tenantID string, profile corea2a.GroupProfile) (corea2a.GroupProfile, error) {
	tenantID = normalizeTenantID(tenantID)
	profile = profile.DiscoveryView("")
	if strings.TrimSpace(profile.AgentID) == "" {
		return corea2a.GroupProfile{}, fmt.Errorf("agent_id is required")
	}
	if profile.UpdatedAt.IsZero() {
		profile.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	if s.profiles[tenantID] == nil {
		s.profiles[tenantID] = map[string]corea2a.GroupProfile{}
	}
	s.profiles[tenantID][profile.AgentID] = cloneGroupProfile(profile)
	stored := cloneGroupProfile(profile)
	s.mu.Unlock()
	s.persistProfile(tenantID, stored)
	return stored, nil
}

func (s *GroupDiscussionService) ListExpertProfiles(tenantID string, activeWindow time.Duration) []corea2a.GroupProfile {
	tenantID = normalizeTenantID(tenantID)
	now := time.Now().UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]corea2a.GroupProfile, 0, len(s.profiles[tenantID]))
	for _, profile := range s.profiles[tenantID] {
		if !profile.Discoverable {
			continue
		}
		if activeWindow > 0 && !profile.UpdatedAt.IsZero() && now.Sub(profile.UpdatedAt) > activeWindow {
			continue
		}
		items = append(items, cloneGroupProfile(profile))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Available != items[j].Available {
			return items[i].Available
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

func (s *GroupDiscussionService) AdminGroupDiscussionSnapshot(tenantID string) (AdminGroupDiscussionSnapshot, error) {
	discussions, err := s.ListDiscussionSummaries(tenantID, ListSessionsFilter{})
	if err != nil {
		return AdminGroupDiscussionSnapshot{}, err
	}
	activeExperts := s.ListExpertProfiles(tenantID, 10*time.Minute)
	totalExperts := len(s.ListExpertProfiles(tenantID, 0))
	return AdminGroupDiscussionSnapshot{Experts: activeExperts, Discussions: discussions, ActiveExperts: len(activeExperts), TotalExperts: totalExperts}, nil
}

func (s *GroupDiscussionService) ListDiscussionSummaries(tenantID string, filter ListSessionsFilter) ([]corea2a.HubDiscussionSummary, error) {
	filter.ParticipantID = strings.TrimSpace(filter.ParticipantID)
	sessions, err := s.ListSessions(tenantID, filter)
	if err != nil {
		return nil, err
	}
	out := make([]corea2a.HubDiscussionSummary, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		summary := discussionSummaryFromSession(session)
		if filter.ParticipantID != "" {
			decorateSummaryForParticipant(&summary, session, filter.ParticipantID)
		}
		out = append(out, summary)
	}
	return out, nil
}

func decorateSummaryForParticipant(summary *corea2a.HubDiscussionSummary, session *corea2a.Session, participantID string) {
	if summary == nil || session == nil {
		return
	}
	role := strings.TrimSpace(participantRole(session, participantID))
	summary.Role = role
	if role == "initiator" {
		summary.LocalRelation = "initiated_by_me"
	} else if role != "" {
		summary.LocalRelation = "owned_ve_invited"
	}
	summary.Readonly = summary.LocalRelation != "initiated_by_me" || session.Status != corea2a.SessionOpen
}

func discussionSummaryFromSession(session *corea2a.Session) corea2a.HubDiscussionSummary {
	participants := make([]string, 0, len(session.Participants))
	for _, p := range session.Participants {
		if strings.TrimSpace(p.ID) != "" {
			participants = append(participants, p.ID)
		}
	}
	summary := ""
	if session.Decision != nil {
		summary = session.Decision.Summary
	} else if len(session.Proposals) > 0 {
		summary = session.Proposals[len(session.Proposals)-1].Title
	} else if session.Escalation != nil {
		summary = session.Escalation.Reason
	}
	question := session.Goal
	if question == "" {
		question = session.Topic
	}
	answerCount := discussionAnswerCount(session)
	expectedAnswerCount := discussionExpectedAnswerCount(session)
	hasResult := strings.TrimSpace(summary) != "" || session.Decision != nil
	ready := hasResult || answerCount >= expectedAnswerCount || (session.Status != corea2a.SessionOpen && answerCount > 0)
	reason := "waiting for expert answers"
	if hasResult {
		reason = "result already exists"
	} else if answerCount >= expectedAnswerCount {
		reason = "expected expert answers received"
	} else if session.Status != corea2a.SessionOpen && answerCount > 0 {
		reason = "discussion is no longer open"
	} else if answerCount > 0 {
		reason = fmt.Sprintf("waiting for more expert answers (%d/%d)", answerCount, expectedAnswerCount)
	}
	return corea2a.HubDiscussionSummary{
		ID:                  session.ID,
		Status:              string(session.Status),
		Topic:               session.Topic,
		Question:            question,
		ResultSummary:       summary,
		ParticipantIDs:      participants,
		MessageCount:        len(session.Messages),
		AnswerCount:         answerCount,
		ExpectedAnswerCount: expectedAnswerCount,
		ReadyToSummarize:    ready,
		ReadinessReason:     reason,
		CreatedAt:           session.CreatedAt,
		UpdatedAt:           session.UpdatedAt,
	}
}

func discussionAnswerCount(session *corea2a.Session) int {
	if session == nil {
		return 0
	}
	roles := participantRoleMap(session)
	count := 0
	for _, msg := range session.Messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		if role, ok := roles[strings.TrimSpace(msg.FromID)]; ok && !groupDiscussionRoleContributesAnswer(role) {
			continue
		}
		switch msg.Kind {
		case corea2a.MessageAnswer, corea2a.MessageStatement, corea2a.MessageEvidence, corea2a.MessageObjection:
			count++
		}
	}
	return count
}

func discussionExpectedAnswerCount(session *corea2a.Session) int {
	if session == nil {
		return 1
	}
	count := 0
	for _, participant := range session.Participants {
		if groupDiscussionRoleContributesAnswer(participant.RoleCode) {
			count++
		}
	}
	if count < 1 {
		return 1
	}
	return count
}

func participantRoleMap(session *corea2a.Session) map[string]string {
	roles := make(map[string]string, len(session.Participants))
	for _, participant := range session.Participants {
		id := strings.TrimSpace(participant.ID)
		if id != "" {
			roles[id] = strings.ToLower(strings.TrimSpace(participant.RoleCode))
		}
	}
	return roles
}

func groupDiscussionRoleContributesAnswer(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "initiator", "observe", "observer", "readonly", "read_only":
		return false
	default:
		return true
	}
}

func (s *GroupDiscussionService) CreateConsultation(tenantID string, req corea2a.GroupConsultationRequest) (corea2a.ConsultationCreateResponse, error) {
	tenantID = normalizeTenantID(tenantID)
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return corea2a.ConsultationCreateResponse{}, fmt.Errorf("question is required")
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	if req.ID == "" {
		req.ID = newGroupDiscussionID("a2areq")
	}
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		topic = "Group consultation"
	}
	participants := []corea2a.Participant{}
	if strings.TrimSpace(req.FromID) != "" {
		participants = append(participants, corea2a.Participant{ID: strings.TrimSpace(req.FromID), RoleCode: "initiator"})
	}
	if len(participants) == 0 {
		participants = append(participants, corea2a.Participant{ID: "initiator", RoleCode: "initiator"})
	}
	initiatorID := strings.TrimSpace(req.FromID)
	if initiatorID == "" {
		initiatorID = participants[0].ID
	}
	session, err := s.CreateSession(tenantID, CreateSessionRequest{Topic: topic, Goal: req.Question, Participants: participants, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		return corea2a.ConsultationCreateResponse{}, err
	}
	if strings.TrimSpace(req.ContextSummary) != "" {
		_, _ = s.AddMessage(tenantID, session.ID, AddMessageRequest{FromID: initiatorID, Kind: corea2a.MessageQuestion, Content: req.ContextSummary})
		session, _ = s.GetSession(tenantID, session.ID)
	}
	return corea2a.ConsultationCreateResponse{Discussion: discussionSummaryFromSession(session), Request: req}, nil
}

func (s *GroupDiscussionService) GetDiscussionSummary(tenantID, sessionID string) (corea2a.HubDiscussionSummary, error) {
	session, err := s.GetSession(tenantID, sessionID)
	if err != nil {
		return corea2a.HubDiscussionSummary{}, err
	}
	return discussionSummaryFromSession(session), nil
}

func (s *GroupDiscussionService) IsSessionParticipant(sessionID, participantID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	participantID = strings.TrimSpace(participantID)
	if sessionID == "" || participantID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, tenantSessions := range s.sessions {
		if session := tenantSessions[sessionID]; session != nil && findGroupDiscussionParticipant(session, participantID) != nil {
			return true
		}
	}
	return false
}
func (s *GroupDiscussionService) GetDiscussionDetail(tenantID, sessionID string) (corea2a.HubDiscussionDetail, error) {
	session, err := s.GetSession(tenantID, sessionID)
	if err != nil {
		return corea2a.HubDiscussionDetail{}, err
	}
	return corea2a.HubDiscussionDetail{
		Discussion:      discussionSummaryFromSession(session),
		Session:         session,
		Messages:        append([]corea2a.Message(nil), session.Messages...),
		Proposals:       append([]corea2a.Proposal(nil), session.Proposals...),
		Reviews:         append([]corea2a.Review(nil), session.Reviews...),
		ReviewSummaries: reviewSummariesFromSession(session),
		Decision:        session.Decision,
	}, nil
}

func reviewSummariesFromSession(session *corea2a.Session) map[string]corea2a.ReviewSummary {
	if session == nil || len(session.Proposals) == 0 {
		return nil
	}
	summaries := make(map[string]corea2a.ReviewSummary, len(session.Proposals))
	for _, proposal := range session.Proposals {
		proposalID := strings.TrimSpace(proposal.ID)
		if proposalID == "" {
			continue
		}
		summary := session.ReviewSummary(proposalID)
		if summary.Approvals == 0 && summary.Rejections == 0 && summary.Concerns == 0 && summary.Abstains == 0 && len(summary.ReviewedBy) == 0 {
			continue
		}
		summaries[proposalID] = summary
	}
	if len(summaries) == 0 {
		return nil
	}
	return summaries
}

func (s *GroupDiscussionService) ListInvitations(tenantID, toID, status string, filters ...ListInvitationsFilter) []corea2a.GroupInviteSummary {
	tenantID = normalizeTenantID(tenantID)
	filter := firstInvitationFilter(filters)
	if filter.ToID != "" {
		toID = filter.ToID
	}
	if filter.Status != "" {
		status = filter.Status
	}
	toID = strings.TrimSpace(toID)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "pending"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]corea2a.GroupInviteSummary, 0, len(s.invites[tenantID]))
	for _, record := range s.invites[tenantID] {
		if toID != "" && strings.TrimSpace(record.Invite.ToID) != toID {
			continue
		}
		recordStatus := strings.TrimSpace(record.Status)
		if recordStatus == "" {
			recordStatus = "pending"
		}
		if status != "all" && recordStatus != status {
			continue
		}
		var summary corea2a.HubDiscussionSummary
		if session := s.sessions[tenantID][record.SessionID]; session != nil {
			summary = discussionSummaryFromSession(session)
		}
		items = append(items, corea2a.GroupInviteSummary{
			ID:              record.ID,
			SessionID:       record.SessionID,
			RequestID:       record.Invite.RequestID,
			FromID:          strings.TrimSpace(record.Invite.FromID),
			ToID:            strings.TrimSpace(record.Invite.ToID),
			Role:            record.Invite.Role,
			Trusted:         record.Invite.Trusted,
			SecurityGroupID: record.Invite.SecurityGroupID,
			ContextPolicy:   record.Invite.ContextPolicy,
			Status:          recordStatus,
			Topic:           summary.Topic,
			Question:        summary.Question,
			CreatedAt:       record.CreatedAt,
			RespondedAt:     record.RespondedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return paginateInviteSummaries(items, filter.Offset, filter.Limit)
}

func (s *GroupDiscussionService) AddInvitation(tenantID, sessionID string, inv corea2a.GroupInvitation) (string, error) {
	tenantID = normalizeTenantID(tenantID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("consultation id is required")
	}
	if strings.TrimSpace(inv.ToID) == "" {
		return "", fmt.Errorf("invitation target is required")
	}
	if inv.Role == "" {
		inv.Role = corea2a.GroupRoleSpeak
	}
	if inv.RequestID == "" {
		inv.RequestID = sessionID
	}
	inviterID := strings.TrimSpace(inv.FromID)
	if inviterID == "" {
		return "", fmt.Errorf("invitation sender is required")
	}
	inviteID := newGroupDiscussionID("a2ainv")
	s.mu.Lock()
	session, err := s.loadSessionLocked(tenantID, sessionID)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	if session.Status != corea2a.SessionOpen {
		s.mu.Unlock()
		return "", fmt.Errorf("session %s is %s", session.ID, session.Status)
	}
	if err := requireGroupDiscussionWritableParticipant(session, inviterID); err != nil {
		s.mu.Unlock()
		return "", err
	}
	if s.invites[tenantID] == nil {
		s.invites[tenantID] = map[string]groupInviteRecord{}
	}
	record := groupInviteRecord{ID: inviteID, TenantID: tenantID, SessionID: sessionID, Invite: inv, Status: "pending", CreatedAt: time.Now().UTC()}
	s.invites[tenantID][inviteID] = record
	content := fmt.Sprintf("invited %s as %s", strings.TrimSpace(inv.ToID), inv.Role)
	_ = session.AddMessage(corea2a.Message{ID: newGroupDiscussionID("a2amsg"), FromID: strings.TrimSpace(inv.FromID), ToIDs: []string{strings.TrimSpace(inv.ToID)}, Kind: corea2a.MessageHandoff, Content: content, CreatedAt: time.Now().UTC()})
	sessionCopy := cloneSession(session)
	recordCopy := cloneInviteRecord(record)
	s.mu.Unlock()
	s.persistSession(tenantID, sessionCopy)
	s.persistInvite(tenantID, recordCopy)
	return inviteID, nil
}

func (s *GroupDiscussionService) RespondInvitation(tenantID, inviteID string, resp corea2a.GroupInvitationResponse) error {
	tenantID = normalizeTenantID(tenantID)
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return fmt.Errorf("invite id is required")
	}
	s.mu.Lock()
	record, ok := s.invites[tenantID][inviteID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("invite not found: %s", inviteID)
	}
	session, err := s.loadSessionLocked(tenantID, record.SessionID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	inviteeID := strings.TrimSpace(record.Invite.ToID)
	fromID := strings.TrimSpace(resp.FromID)
	if fromID == "" {
		fromID = inviteeID
	}
	if !strings.EqualFold(fromID, inviteeID) {
		s.mu.Unlock()
		return fmt.Errorf("invite response sender %s does not match invite target %s", fromID, inviteeID)
	}
	decision := resp.Decision
	if decision == "" {
		decision = corea2a.GroupInvitationReject
	}
	if decision == corea2a.GroupInvitationAccept && session.Status != corea2a.SessionOpen {
		s.mu.Unlock()
		return fmt.Errorf("session %s is %s", session.ID, session.Status)
	}
	if decision == corea2a.GroupInvitationAccept {
		addParticipantIfMissing(session, corea2a.Participant{ID: fromID, RoleCode: string(record.Invite.Role)})
	}
	record.Status = string(decision)
	record.RespondedAt = time.Now().UTC()
	s.invites[tenantID][inviteID] = record
	content := fmt.Sprintf("invitation %s: %s", inviteID, decision)
	if strings.TrimSpace(resp.Reason) != "" {
		content += " - " + strings.TrimSpace(resp.Reason)
	}
	_ = session.AddMessage(corea2a.Message{ID: newGroupDiscussionID("a2amsg"), FromID: fromID, Kind: corea2a.MessageAnswer, Content: content, CreatedAt: time.Now().UTC()})
	sessionCopy := cloneSession(session)
	recordCopy := cloneInviteRecord(record)
	s.mu.Unlock()
	s.persistSession(tenantID, sessionCopy)
	s.persistInvite(tenantID, recordCopy)
	return nil
}

func (s *GroupDiscussionService) AddDiscussionMessage(tenantID, sessionID string, msg corea2a.GroupDiscussionMessage) (*corea2a.Session, error) {
	kind := msg.Kind
	if kind == "" {
		kind = corea2a.MessageStatement
	}
	fromID := strings.TrimSpace(msg.FromID)
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if err := requireGroupDiscussionWritableParticipant(session, fromID); err != nil {
			return err
		}
		return session.AddMessage(corea2a.Message{ID: newGroupDiscussionID("a2amsg"), FromID: fromID, Kind: kind, Content: msg.Content, TextAttachments: msg.TextAttachments, ImageAttachments: msg.ImageAttachments, FileAttachments: msg.FileAttachments, CreatedAt: time.Now().UTC()})
	})
}

func findGroupDiscussionParticipant(session *corea2a.Session, participantID string) *corea2a.Participant {
	participantID = strings.TrimSpace(participantID)
	if session == nil || participantID == "" {
		return nil
	}
	for i := range session.Participants {
		if strings.EqualFold(strings.TrimSpace(session.Participants[i].ID), participantID) {
			return &session.Participants[i]
		}
	}
	return nil
}

func groupDiscussionParticipantCanMessage(roleCode string) bool {
	switch strings.ToLower(strings.TrimSpace(roleCode)) {
	case "", "initiator", "review", "speak", "speaker", "participant":
		return true
	case "observe", "observer", "readonly", "read_only":
		return false
	default:
		return false
	}
}

func requireGroupDiscussionWritableParticipant(session *corea2a.Session, participantID string) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	if session.Status != corea2a.SessionOpen {
		return fmt.Errorf("session %s is %s", session.ID, session.Status)
	}
	participantID = strings.TrimSpace(participantID)
	participant := findGroupDiscussionParticipant(session, participantID)
	if participant == nil {
		return fmt.Errorf("participant %s is not in discussion", participantID)
	}
	if !groupDiscussionParticipantCanMessage(participant.RoleCode) {
		return fmt.Errorf("participant %s is read-only in discussion", participantID)
	}
	return nil
}

func groupDiscussionSystemSender(participantID string) bool {
	switch strings.ToLower(strings.TrimSpace(participantID)) {
	case "hub", "system":
		return true
	default:
		return false
	}
}

func (s *GroupDiscussionService) SubmitDiscussionResult(tenantID, sessionID string, result corea2a.GroupDiscussionResult) (*corea2a.Session, error) {
	if strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("discussion result summary is required")
	}
	content := strings.TrimSpace(result.Rationale)
	if content == "" {
		content = result.Summary
	}
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if session.Status != corea2a.SessionOpen {
			return fmt.Errorf("session %s is %s", session.ID, session.Status)
		}
		now := time.Now().UTC()
		proposalID := newGroupDiscussionID("a2aprop")
		if err := session.AddProposal(corea2a.Proposal{ID: proposalID, AuthorID: "hub", Title: strings.TrimSpace(result.Summary), Content: content, Risks: result.Risks, CreatedAt: now}); err != nil {
			return err
		}
		for i := range session.Proposals {
			if session.Proposals[i].ID == proposalID {
				session.Proposals[i].Status = corea2a.ProposalAccepted
				session.Proposals[i].UpdatedAt = now
				break
			}
		}
		decidedBy := make([]string, 0, len(session.Participants))
		for _, participant := range session.Participants {
			if strings.TrimSpace(participant.ID) != "" {
				decidedBy = append(decidedBy, participant.ID)
			}
		}
		if len(decidedBy) == 0 {
			decidedBy = []string{"hub"}
		}
		session.Decision = &corea2a.Decision{ID: newGroupDiscussionID("a2adec"), SessionID: session.ID, ProposalID: proposalID, Summary: strings.TrimSpace(result.Summary), Rationale: content, DecidedBy: decidedBy, CreatedAt: now}
		session.Status = corea2a.SessionDecided
		session.UpdatedAt = now
		return nil
	})
}

func (s *GroupDiscussionService) SetDiscussionState(tenantID, sessionID, action string) (*corea2a.Session, error) {
	action = strings.TrimSpace(action)
	switch action {
	case "pause":
		return s.AddMessage(tenantID, sessionID, AddMessageRequest{FromID: "hub", Kind: corea2a.MessageStatement, Content: "discussion paused"})
	case "resume":
		return s.AddMessage(tenantID, sessionID, AddMessageRequest{FromID: "hub", Kind: corea2a.MessageStatement, Content: "discussion resumed"})
	case "cancel":
		return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
			session.Status = corea2a.SessionClosed
			session.UpdatedAt = time.Now().UTC()
			return nil
		})
	default:
		return nil, fmt.Errorf("unsupported consultation action %q", action)
	}
}

func (s *GroupDiscussionService) CreateSession(tenantID string, req CreateSessionRequest) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	now := time.Now().UTC()
	session, err := corea2a.NewSession(newGroupDiscussionID("a2a"), req.Topic, req.Goal, req.Participants, req.DecisionPolicy, now)
	if err != nil {
		return nil, err
	}
	session.TenantID = tenantID
	session.OrgUnitID = normalizeOrgUnitID(req.OrgUnitID, req.DepartmentID)

	s.mu.Lock()
	if s.sessions[tenantID] == nil {
		s.sessions[tenantID] = map[string]*corea2a.Session{}
	}
	s.sessions[tenantID][session.ID] = session
	created := cloneSession(session)
	s.mu.Unlock()
	s.persistSession(tenantID, created)
	return created, nil
}

func (s *GroupDiscussionService) ListSessions(tenantID string, filters ...ListSessionsFilter) ([]*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	filter := firstListFilter(filters)
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*corea2a.Session, 0, len(s.sessions[tenantID]))
	for _, session := range s.sessions[tenantID] {
		if !matchesListFilter(session, filter) {
			continue
		}
		items = append(items, cloneSession(session))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return paginateSessions(items, filter.Offset, filter.Limit), nil
}

func (s *GroupDiscussionService) GetSession(tenantID, sessionID string) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	sessionID = strings.TrimSpace(sessionID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[tenantID][sessionID]
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return cloneSession(session), nil
}

func (s *GroupDiscussionService) AddMessage(tenantID, sessionID string, req AddMessageRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if !groupDiscussionSystemSender(req.FromID) {
			if err := requireGroupDiscussionWritableParticipant(session, req.FromID); err != nil {
				return err
			}
		}
		return session.AddMessage(corea2a.Message{ID: newGroupDiscussionID("a2amsg"), FromID: req.FromID, ToIDs: req.ToIDs, Kind: req.Kind, Content: req.Content, Evidence: req.Evidence, TextAttachments: req.TextAttachments, ImageAttachments: req.ImageAttachments, FileAttachments: req.FileAttachments, CreatedAt: time.Now().UTC()})
	})
}

func (s *GroupDiscussionService) AddProposal(tenantID, sessionID string, req AddProposalRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if err := requireGroupDiscussionWritableParticipant(session, req.AuthorID); err != nil {
			return err
		}
		return session.AddProposal(corea2a.Proposal{ID: newGroupDiscussionID("a2aprop"), AuthorID: req.AuthorID, Title: req.Title, Content: req.Content, Goals: req.Goals, Constraints: req.Constraints, Risks: req.Risks, CreatedAt: time.Now().UTC()})
	})
}

func (s *GroupDiscussionService) AddReview(tenantID, sessionID string, req AddReviewRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if err := requireGroupDiscussionWritableParticipant(session, req.ReviewerID); err != nil {
			return err
		}
		return session.AddReview(corea2a.Review{ID: newGroupDiscussionID("a2arev"), ProposalID: req.ProposalID, ReviewerID: req.ReviewerID, Position: req.Position, Comment: req.Comment, CreatedAt: time.Now().UTC()})
	})
}

func (s *GroupDiscussionService) Decide(tenantID, sessionID string, req DecideRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		_, err := session.TryDecide(newGroupDiscussionID("a2adec"), req.ProposalID, req.Summary, time.Now().UTC())
		if err != nil {
			return err
		}
		if session.Decision != nil {
			session.Decision.Rationale = strings.TrimSpace(req.Rationale)
			session.Decision.RollbackOn = compactGroupDiscussionStrings(req.RollbackOn)
		}
		return nil
	})
}

func (s *GroupDiscussionService) Escalate(tenantID, sessionID string, req EscalateRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if err := requireGroupDiscussionWritableParticipant(session, req.RaisedBy); err != nil {
			return err
		}
		return session.Escalate(corea2a.Escalation{ID: newGroupDiscussionID("a2aesc"), RaisedBy: req.RaisedBy, Reason: req.Reason, Target: req.Target, CreatedAt: time.Now().UTC()})
	})
}

func (s *GroupDiscussionService) mutate(tenantID, sessionID string, fn func(*corea2a.Session) error) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	sessionID = strings.TrimSpace(sessionID)
	s.mu.Lock()

	session := s.sessions[tenantID][sessionID]
	if session == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	if err := fn(session); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	updated := cloneSession(session)
	s.mu.Unlock()
	s.persistSession(tenantID, updated)
	return updated, nil
}

func firstListFilter(filters []ListSessionsFilter) ListSessionsFilter {
	if len(filters) == 0 {
		return ListSessionsFilter{}
	}
	filter := filters[0]
	filter.OrgUnitID = normalizeOrgUnitID(filter.OrgUnitID)
	filter.Limit = normalizeListLimit(filter.Limit)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func firstInvitationFilter(filters []ListInvitationsFilter) ListInvitationsFilter {
	if len(filters) == 0 {
		return ListInvitationsFilter{}
	}
	filter := filters[0]
	filter.ToID = strings.TrimSpace(filter.ToID)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Limit = normalizeListLimit(filter.Limit)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func normalizeListLimit(limit int) int {
	if limit < 0 {
		return 0
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func paginateSessions(items []*corea2a.Session, offset, limit int) []*corea2a.Session {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []*corea2a.Session{}
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		return items[:limit]
	}
	return items
}

func paginateInviteSummaries(items []corea2a.GroupInviteSummary, offset, limit int) []corea2a.GroupInviteSummary {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []corea2a.GroupInviteSummary{}
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		return items[:limit]
	}
	return items
}

func matchesListFilter(session *corea2a.Session, filter ListSessionsFilter) bool {
	if session == nil {
		return false
	}
	if filter.OrgUnitID != "" && session.OrgUnitID != filter.OrgUnitID {
		return false
	}
	if filter.Status != "" && session.Status != filter.Status {
		return false
	}
	if filter.ParticipantID != "" {
		role := participantRole(session, filter.ParticipantID)
		if role == "" {
			return false
		}
		if filter.Role != "" && !strings.EqualFold(filter.Role, "all") && role != filter.Role {
			return false
		}
	}
	return true
}

func participantRole(session *corea2a.Session, participantID string) string {
	if session == nil {
		return ""
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return ""
	}
	for _, participant := range session.Participants {
		if strings.TrimSpace(participant.ID) == participantID {
			return strings.TrimSpace(participant.RoleCode)
		}
	}
	return ""
}

func normalizeTenantID(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return "default"
	}
	return strings.TrimSpace(tenantID)
}

func normalizeOrgUnitID(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *GroupDiscussionService) loadSessionLocked(tenantID, sessionID string) (*corea2a.Session, error) {
	session := s.sessions[tenantID][sessionID]
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return session, nil
}

func addParticipantIfMissing(session *corea2a.Session, participant corea2a.Participant) {
	if session == nil || strings.TrimSpace(participant.ID) == "" {
		return
	}
	participant.ID = strings.TrimSpace(participant.ID)
	for _, existing := range session.Participants {
		if existing.ID == participant.ID {
			return
		}
	}
	session.Participants = append(session.Participants, participant)
	session.UpdatedAt = time.Now().UTC()
}

func cloneSession(in *corea2a.Session) *corea2a.Session {
	if in == nil {
		return nil
	}
	out := *in
	out.Participants = append([]corea2a.Participant(nil), in.Participants...)
	out.Messages = append([]corea2a.Message(nil), in.Messages...)
	out.Proposals = append([]corea2a.Proposal(nil), in.Proposals...)
	out.Reviews = append([]corea2a.Review(nil), in.Reviews...)
	if in.Decision != nil {
		decision := *in.Decision
		decision.DecidedBy = append([]string(nil), in.Decision.DecidedBy...)
		decision.RollbackOn = append([]string(nil), in.Decision.RollbackOn...)
		out.Decision = &decision
	}
	if in.Escalation != nil {
		escalation := *in.Escalation
		out.Escalation = &escalation
	}
	return &out
}

func cloneGroupProfile(in corea2a.GroupProfile) corea2a.GroupProfile {
	out := in
	out.Skills = append([]string(nil), in.Skills...)
	out.Languages = append([]string(nil), in.Languages...)
	return out
}

func (s *GroupDiscussionService) loadPersistentTables() error {
	if s == nil || s.db == nil {
		return nil
	}
	profiles, err := s.loadProfilesFromDB()
	if err != nil {
		return err
	}
	sessions, err := s.loadSessionsFromDB()
	if err != nil {
		return err
	}
	invites, err := s.loadInvitesFromDB()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = profiles
	s.sessions = sessions
	s.invites = invites
	return nil
}

func (s *GroupDiscussionService) loadProfilesFromDB() (map[string]map[string]corea2a.GroupProfile, error) {
	out := map[string]map[string]corea2a.GroupProfile{}
	ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, agent_id, profile_json FROM a2a_group_profiles ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, agentID, raw string
		if err := rows.Scan(&tenantID, &agentID, &raw); err != nil {
			return nil, err
		}
		var profile corea2a.GroupProfile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			log.Printf("[a2a-group] skip corrupt profile %s/%s: %v", tenantID, agentID, err)
			continue
		}
		if out[tenantID] == nil {
			out[tenantID] = map[string]corea2a.GroupProfile{}
		}
		out[tenantID][agentID] = cloneGroupProfile(profile)
	}
	return out, rows.Err()
}

func (s *GroupDiscussionService) loadSessionsFromDB() (map[string]map[string]*corea2a.Session, error) {
	out := map[string]map[string]*corea2a.Session{}
	ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, session_id, session_json FROM a2a_group_sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, sessionID, raw string
		if err := rows.Scan(&tenantID, &sessionID, &raw); err != nil {
			return nil, err
		}
		var session corea2a.Session
		if err := json.Unmarshal([]byte(raw), &session); err != nil {
			log.Printf("[a2a-group] skip corrupt session %s/%s: %v", tenantID, sessionID, err)
			continue
		}
		if out[tenantID] == nil {
			out[tenantID] = map[string]*corea2a.Session{}
		}
		out[tenantID][sessionID] = cloneSession(&session)
	}
	return out, rows.Err()
}

func (s *GroupDiscussionService) loadInvitesFromDB() (map[string]map[string]groupInviteRecord, error) {
	out := map[string]map[string]groupInviteRecord{}
	ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, invite_id, invite_json FROM a2a_group_invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, inviteID, raw string
		if err := rows.Scan(&tenantID, &inviteID, &raw); err != nil {
			return nil, err
		}
		var record groupInviteRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			log.Printf("[a2a-group] skip corrupt invite %s/%s: %v", tenantID, inviteID, err)
			continue
		}
		if out[tenantID] == nil {
			out[tenantID] = map[string]groupInviteRecord{}
		}
		out[tenantID][inviteID] = cloneInviteRecord(record)
	}
	return out, rows.Err()
}

func (s *GroupDiscussionService) persistProfile(tenantID string, profile corea2a.GroupProfile) {
	if s == nil || s.db == nil {
		return
	}
	data, err := json.Marshal(profile)
	if err != nil {
		log.Printf("[a2a-group] encode profile failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `INSERT INTO a2a_group_profiles (tenant_id, agent_id, display_name, discoverable, available, updated_at, profile_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, agent_id) DO UPDATE SET display_name=excluded.display_name, discoverable=excluded.discoverable, available=excluded.available, updated_at=excluded.updated_at, profile_json=excluded.profile_json`,
		tenantID, profile.AgentID, profile.DisplayName, boolInt(profile.Discoverable), boolInt(profile.Available), formatGroupDiscussionTime(profile.UpdatedAt), string(data))
	if err != nil {
		log.Printf("[a2a-group] persist profile failed: %v", err)
	}
}

func (s *GroupDiscussionService) persistSession(tenantID string, session *corea2a.Session) {
	if s == nil || s.db == nil || session == nil {
		return
	}
	data, err := json.Marshal(session)
	if err != nil {
		log.Printf("[a2a-group] encode session failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `INSERT INTO a2a_group_sessions (tenant_id, session_id, status, topic, created_at, updated_at, session_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, session_id) DO UPDATE SET status=excluded.status, topic=excluded.topic, updated_at=excluded.updated_at, session_json=excluded.session_json`,
		tenantID, session.ID, string(session.Status), session.Topic, formatGroupDiscussionTime(session.CreatedAt), formatGroupDiscussionTime(session.UpdatedAt), string(data))
	if err != nil {
		log.Printf("[a2a-group] persist session failed: %v", err)
	}
}

func (s *GroupDiscussionService) persistInvite(tenantID string, record groupInviteRecord) {
	if s == nil || s.db == nil {
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		log.Printf("[a2a-group] encode invite failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `INSERT INTO a2a_group_invites (tenant_id, invite_id, session_id, to_id, from_id, role, status, created_at, responded_at, invite_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, invite_id) DO UPDATE SET session_id=excluded.session_id, to_id=excluded.to_id, from_id=excluded.from_id, role=excluded.role, status=excluded.status, responded_at=excluded.responded_at, invite_json=excluded.invite_json`,
		tenantID, record.ID, record.SessionID, record.Invite.ToID, record.Invite.FromID, string(record.Invite.Role), firstNonEmptyGroupStatus(record.Status, "pending"), formatGroupDiscussionTime(record.CreatedAt), formatGroupDiscussionOptionalTime(record.RespondedAt), string(data))
	if err != nil {
		log.Printf("[a2a-group] persist invite failed: %v", err)
	}
}

func cloneInviteRecord(in groupInviteRecord) groupInviteRecord {
	out := in
	return out
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatGroupDiscussionTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatGroupDiscussionOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func compactGroupDiscussionStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func firstNonEmptyGroupStatus(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func newGroupDiscussionID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
