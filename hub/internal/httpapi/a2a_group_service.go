package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	groupDiscussionPersistenceTimeout    = 5 * time.Second
	groupDiscussionSummaryMinMessages    = 40
	groupDiscussionSummaryRecentMessages = 16
	groupDiscussionSummaryMaxChars       = 6000
	groupDiscussionSummaryLineMaxChars   = 420
)

var groupDiscussionUnresolvedMentionPattern = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])@[^\s@]+`)

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
	Reason      string
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
	ID     string
	FromID string
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

type RenameDiscussionRequest struct {
	FromID string `json:"from_id"`
	Topic  string `json:"topic"`
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
	seenParticipants := map[string]struct{}{}
	for _, p := range session.Participants {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		key := groupDiscussionCanonicalParticipantIdentityKey(id)
		if key == "" {
			continue
		}
		if _, ok := seenParticipants[key]; ok {
			continue
		}
		seenParticipants[key] = struct{}{}
		participants = append(participants, id)
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
	answered := map[string]struct{}{}
	for _, msg := range session.Messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		if groupDiscussionMessageTargetsOnlySender(msg) {
			continue
		}
		if role, ok := groupDiscussionParticipantRoleForID(roles, msg.FromID); ok && !groupDiscussionRoleContributesAnswer(role) {
			continue
		}
		switch msg.Kind {
		case corea2a.MessageAnswer, corea2a.MessageStatement, corea2a.MessageEvidence, corea2a.MessageObjection:
			key := groupDiscussionCanonicalParticipantIdentityKey(msg.FromID)
			if key != "" {
				answered[key] = struct{}{}
			}
		}
	}
	return len(answered)
}

func groupDiscussionCanonicalParticipantIdentityKey(participantID string) string {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return ""
	}
	cleaned := strings.NewReplacer("/", "_", "\\", "_", " ", "_", "-", "_").Replace(participantID)
	if len(cleaned) > 3 && (strings.EqualFold(cleaned[:3], "ve_") || strings.EqualFold(cleaned[:3], "ve-")) {
		cleaned = cleaned[3:]
	}
	return strings.ToLower(strings.TrimSpace(cleaned))
}

func groupDiscussionMessageTargetsOnlySender(msg corea2a.Message) bool {
	fromID := strings.TrimSpace(msg.FromID)
	if fromID == "" || len(msg.ToIDs) != 1 {
		return false
	}
	return groupDiscussionParticipantIdentityMatches(msg.ToIDs[0], fromID)
}

func discussionExpectedAnswerCount(session *corea2a.Session) int {
	if session == nil {
		return 1
	}
	participants := map[string]struct{}{}
	for _, participant := range session.Participants {
		if !groupDiscussionRoleContributesAnswer(participant.RoleCode) {
			continue
		}
		key := groupDiscussionCanonicalParticipantIdentityKey(participant.ID)
		if key != "" {
			participants[key] = struct{}{}
		}
	}
	count := len(participants)
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
			role := strings.ToLower(strings.TrimSpace(participant.RoleCode))
			for _, key := range groupDiscussionParticipantIdentityKeys(id) {
				roles[key] = role
			}
		}
	}
	return roles
}

func groupDiscussionParticipantRoleForID(roles map[string]string, participantID string) (string, bool) {
	if len(roles) == 0 {
		return "", false
	}
	for _, key := range groupDiscussionParticipantIdentityKeys(participantID) {
		if role, ok := roles[key]; ok {
			return role, true
		}
	}
	return "", false
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

func (s *GroupDiscussionService) RenameDiscussionTopic(tenantID, sessionID string, req RenameDiscussionRequest) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	sessionID = strings.TrimSpace(sessionID)
	fromID := strings.TrimSpace(req.FromID)
	topic := strings.TrimSpace(req.Topic)
	if sessionID == "" {
		return nil, fmt.Errorf("consultation id is required")
	}
	if fromID == "" {
		return nil, fmt.Errorf("from_id is required")
	}
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if len([]rune(topic)) > 60 {
		return nil, fmt.Errorf("topic must be 60 characters or fewer")
	}
	s.mu.Lock()
	session, err := s.loadSessionLocked(tenantID, sessionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	participant := findGroupDiscussionParticipant(session, fromID)
	if participant == nil || !strings.EqualFold(strings.TrimSpace(participant.RoleCode), "initiator") {
		s.mu.Unlock()
		return nil, fmt.Errorf("only the discussion initiator can rename the discussion")
	}
	session.Topic = topic
	session.UpdatedAt = time.Now().UTC()
	sessionCopy := cloneSession(session)
	s.mu.Unlock()
	s.persistSession(tenantID, sessionCopy)
	return sessionCopy, nil
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
	fromID := strings.TrimSpace(filter.FromID)
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
		if filter.ID != "" && !strings.EqualFold(strings.TrimSpace(record.ID), strings.TrimSpace(filter.ID)) {
			continue
		}
		if fromID != "" && !groupDiscussionParticipantIdentityMatches(record.Invite.FromID, fromID) {
			continue
		}
		if toID != "" && !groupDiscussionParticipantIdentityMatches(record.Invite.ToID, toID) {
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
			Reason:          strings.TrimSpace(record.Reason),
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
	if !groupDiscussionParticipantIdentityMatches(inviteeID, fromID) {
		s.mu.Unlock()
		return fmt.Errorf("invite response sender %s does not match invite target %s", fromID, inviteeID)
	}
	if groupDiscussionGeneratedVEAlias(fromID) {
		fromID = inviteeID
	}
	decision := resp.Decision
	if decision == "" {
		decision = corea2a.GroupInvitationReject
	}
	switch decision {
	case corea2a.GroupInvitationAccept, corea2a.GroupInvitationReject:
	default:
		s.mu.Unlock()
		return fmt.Errorf("unsupported invite decision %q", decision)
	}
	recordStatus := strings.TrimSpace(record.Status)
	if recordStatus == "" {
		recordStatus = "pending"
	}
	if recordStatus != "pending" {
		if recordStatus == string(decision) {
			if decision == corea2a.GroupInvitationAccept && session.Status == corea2a.SessionOpen {
				if addParticipantIfMissing(session, corea2a.Participant{ID: fromID, RoleCode: string(record.Invite.Role)}) {
					sessionCopy := cloneSession(session)
					s.mu.Unlock()
					s.persistSession(tenantID, sessionCopy)
					return nil
				}
			}
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		return fmt.Errorf("invite %s is already %s", inviteID, recordStatus)
	}
	if decision == corea2a.GroupInvitationAccept && session.Status != corea2a.SessionOpen {
		s.mu.Unlock()
		return fmt.Errorf("session %s is %s", session.ID, session.Status)
	}
	if decision == corea2a.GroupInvitationAccept {
		addParticipantIfMissing(session, corea2a.Participant{ID: fromID, RoleCode: string(record.Invite.Role)})
	}
	record.Status = string(decision)
	record.Reason = strings.TrimSpace(resp.Reason)
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
	messageID := strings.TrimSpace(msg.ID)
	if messageID == "" {
		messageID = newGroupDiscussionID("a2amsg")
	}
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		if err := requireGroupDiscussionWritableParticipant(session, fromID); err != nil {
			return err
		}
		for _, existing := range session.Messages {
			if strings.TrimSpace(existing.ID) == messageID {
				return nil
			}
		}
		toIDs, err := normalizeGroupDiscussionMessageTargetIDs(session, fromID, msg.ToIDs, msg.Content, kind)
		if err != nil {
			return err
		}
		if err := session.AddMessage(corea2a.Message{ID: messageID, FromID: fromID, ToIDs: toIDs, Kind: kind, Content: msg.Content, TextAttachments: msg.TextAttachments, ImageAttachments: msg.ImageAttachments, FileAttachments: msg.FileAttachments, CreatedAt: time.Now().UTC()}); err != nil {
			return err
		}
		applyGroupDiscussionMessageTargetEffect(session, fromID, msg.ToIDs, msg.Content, kind)
		refreshGroupDiscussionContextSummary(session)
		return nil
	})
}

func (s *GroupDiscussionService) HasDiscussionMessage(tenantID, sessionID, messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || s == nil {
		return false
	}
	tenantID = normalizeTenantID(tenantID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[tenantID][strings.TrimSpace(sessionID)]
	if session == nil {
		return false
	}
	for _, existing := range session.Messages {
		if strings.TrimSpace(existing.ID) == messageID {
			return true
		}
	}
	return false
}

func refreshGroupDiscussionContextSummary(session *corea2a.Session) {
	if session == nil || len(session.Messages) < groupDiscussionSummaryMinMessages {
		return
	}
	cutoff := len(session.Messages) - groupDiscussionSummaryRecentMessages
	if cutoff <= 0 {
		return
	}
	upToID := strings.TrimSpace(session.Messages[cutoff-1].ID)
	if upToID != "" && strings.EqualFold(strings.TrimSpace(session.SummaryUpToID), upToID) && strings.TrimSpace(session.ContextSummary) != "" {
		return
	}
	summary := buildGroupDiscussionContextSummary(session, cutoff)
	if strings.TrimSpace(summary) == "" {
		return
	}
	session.ContextSummary = summary
	session.SummaryUpToID = upToID
	session.SummaryUpdatedAt = time.Now().UTC()
}

func buildGroupDiscussionContextSummary(session *corea2a.Session, cutoff int) string {
	if session == nil || cutoff <= 0 {
		return ""
	}
	if cutoff > len(session.Messages) {
		cutoff = len(session.Messages)
	}
	start := groupDiscussionSummaryStartIndex(session.Messages[:cutoff], session.SummaryUpToID)
	lines := make([]string, 0, cutoff-start)
	if start > 0 {
		lines = append(lines, groupDiscussionExistingSummaryLines(session.ContextSummary)...)
	}
	lines = append(lines, groupDiscussionSummaryMessageLines(session.Messages[start:cutoff])...)
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[compressed shared group memory]\n")
	b.WriteString("This memory is shared by all participants. @ mentions and to_ids are reply targets, not private visibility.\n")
	if topic := strings.TrimSpace(session.Topic); topic != "" {
		b.WriteString("Topic: ")
		b.WriteString(truncateGroupDiscussionSummaryText(topic, groupDiscussionSummaryLineMaxChars))
		b.WriteString("\n")
	}
	if goal := strings.TrimSpace(session.Goal); goal != "" {
		b.WriteString("Goal: ")
		b.WriteString(truncateGroupDiscussionSummaryText(goal, groupDiscussionSummaryLineMaxChars))
		b.WriteString("\n")
	}
	b.WriteString("Earlier messages through ")
	b.WriteString(strings.TrimSpace(session.Messages[cutoff-1].ID))
	b.WriteString(":\n")
	for _, line := range newestGroupDiscussionSummaryLines(lines, groupDiscussionSummaryMaxChars-b.Len()) {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("[/compressed shared group memory]")
	return b.String()
}

func groupDiscussionSummaryStartIndex(messages []corea2a.Message, summaryUpToID string) int {
	summaryUpToID = strings.TrimSpace(summaryUpToID)
	if summaryUpToID == "" {
		return 0
	}
	for i, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.ID), summaryUpToID) {
			return i + 1
		}
	}
	return 0
}

func groupDiscussionExistingSummaryLines(summary string) []string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}
	lines := make([]string, 0)
	for _, raw := range strings.Split(summary, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func newestGroupDiscussionSummaryLines(lines []string, maxChars int) []string {
	if len(lines) == 0 || maxChars <= 0 {
		return nil
	}
	selected := make([]string, 0, len(lines))
	used := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		cost := len(line) + 4
		if used > 0 && used+cost > maxChars {
			break
		}
		if used == 0 && cost > maxChars {
			lineBudget := maxChars - 4
			if lineBudget <= 0 {
				break
			}
			line = truncateGroupDiscussionSummaryTextToBudget(line, lineBudget)
			cost = len(line) + 4
		}
		used += cost
		selected = append(selected, line)
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}

func groupDiscussionSummaryMessageLines(messages []corea2a.Message) []string {
	lines := make([]string, 0, len(messages))
	var streamFrom string
	var streamContent strings.Builder
	flushStream := func() {
		content := strings.TrimSpace(streamContent.String())
		if content != "" {
			fromID := strings.TrimSpace(streamFrom)
			if fromID == "" {
				fromID = "unknown"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s", fromID, truncateGroupDiscussionSummaryText(content, groupDiscussionSummaryLineMaxChars)))
		}
		streamFrom = ""
		streamContent.Reset()
	}
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		switch msg.Kind {
		case corea2a.MessageStreamEnd, corea2a.MessageHandoff:
			flushStream()
			continue
		case corea2a.MessageStreamChunk:
			if msg.Content == "" {
				continue
			}
			fromID := strings.TrimSpace(msg.FromID)
			if streamFrom != "" && !groupDiscussionParticipantIdentityMatches(streamFrom, fromID) {
				flushStream()
			}
			if streamFrom == "" {
				streamFrom = fromID
			}
			streamContent.WriteString(msg.Content)
			continue
		default:
			flushStream()
		}
		if content == "" || strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		fromID := strings.TrimSpace(msg.FromID)
		if fromID == "" {
			fromID = "unknown"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", fromID, truncateGroupDiscussionSummaryText(content, groupDiscussionSummaryLineMaxChars)))
	}
	flushStream()
	return lines
}

func truncateGroupDiscussionSummaryText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func truncateGroupDiscussionSummaryTextToBudget(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return ""
	}
	if len(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	if maxRunes <= 3 {
		var b strings.Builder
		for _, r := range runes {
			part := string(r)
			if b.Len()+len(part) > maxRunes {
				break
			}
			b.WriteString(part)
		}
		return b.String()
	}
	var b strings.Builder
	limit := maxRunes - 3
	for _, r := range runes {
		part := string(r)
		if b.Len()+len(part) > limit {
			break
		}
		b.WriteString(part)
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("...")
	return b.String()
}

func (s *GroupDiscussionService) RemoveDiscussionMessage(tenantID, sessionID, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	_, err := s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if strings.TrimSpace(session.Messages[i].ID) != messageID {
				continue
			}
			session.Messages = append(session.Messages[:i], session.Messages[i+1:]...)
			rebuildGroupDiscussionDerivedMessageState(session)
			return nil
		}
		return nil
	})
	return err
}

func (s *GroupDiscussionService) DeleteSessionsByParticipants(tenantID string, participantIDs []string) (int, error) {
	if s == nil {
		return 0, nil
	}
	tenantID = store.NormalizeTenantID(tenantID)
	ids := normalizeVEStringList(participantIDs)
	if tenantID == "" || len(ids) == 0 {
		return 0, nil
	}
	sessionIDs := make([]string, 0)
	s.mu.RLock()
	if tenantSessions := s.sessions[tenantID]; tenantSessions != nil {
		for sessionID, session := range tenantSessions {
			if groupDiscussionSessionHasAnyParticipant(session, ids) {
				sessionIDs = append(sessionIDs, sessionID)
			}
		}
	}
	s.mu.RUnlock()
	deletedSessionIDs := map[string]struct{}{}
	for _, sessionID := range sessionIDs {
		deletedSessionIDs[sessionID] = struct{}{}
	}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), groupDiscussionPersistenceTimeout)
		defer cancel()
		for _, sessionID := range sessionIDs {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM a2a_group_sessions WHERE tenant_id = ? AND session_id = ?`, tenantID, sessionID); err != nil {
				return len(sessionIDs), err
			}
			if _, err := s.db.ExecContext(ctx, `DELETE FROM a2a_group_invites WHERE tenant_id = ? AND session_id = ?`, tenantID, sessionID); err != nil {
				return len(sessionIDs), err
			}
		}
		for _, id := range ids {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM a2a_group_invites WHERE tenant_id = ? AND (to_id = ? OR from_id = ?)`, tenantID, id, id); err != nil {
				return len(sessionIDs), err
			}
		}
	}
	s.mu.Lock()
	if tenantSessions := s.sessions[tenantID]; tenantSessions != nil {
		for _, sessionID := range sessionIDs {
			delete(tenantSessions, sessionID)
		}
	}
	if tenantInvites := s.invites[tenantID]; tenantInvites != nil {
		for inviteID, record := range tenantInvites {
			_, sessionDeleted := deletedSessionIDs[record.SessionID]
			if sessionDeleted || groupDiscussionInviteMatchesAnyParticipant(record, ids) {
				delete(tenantInvites, inviteID)
			}
		}
	}
	s.mu.Unlock()
	return len(sessionIDs), nil
}

func groupDiscussionSessionHasAnyParticipant(session *corea2a.Session, participantIDs []string) bool {
	if session == nil {
		return false
	}
	for _, participant := range session.Participants {
		for _, id := range participantIDs {
			if groupDiscussionParticipantIdentityMatches(participant.ID, id) {
				return true
			}
		}
	}
	return false
}

func groupDiscussionInviteMatchesAnyParticipant(record groupInviteRecord, participantIDs []string) bool {
	for _, id := range participantIDs {
		if groupDiscussionParticipantIdentityMatches(record.Invite.ToID, id) || groupDiscussionParticipantIdentityMatches(record.Invite.FromID, id) {
			return true
		}
	}
	return false
}

func rebuildGroupDiscussionDerivedMessageState(session *corea2a.Session) {
	if session == nil {
		return
	}
	session.ContextSummary = ""
	session.SummaryUpToID = ""
	session.SummaryUpdatedAt = time.Time{}
	session.DefaultReplyTargets = nil
	for _, msg := range session.Messages {
		fromID := strings.TrimSpace(msg.FromID)
		if fromID == "" {
			continue
		}
		if len(msg.ToIDs) > 0 && groupDiscussionMessageKindUpdatesDefaultReplyTarget(msg.Kind) {
			setGroupDiscussionDefaultReplyTargets(session, fromID, msg.ToIDs)
			continue
		}
		if groupDiscussionMessageKindUpdatesDefaultReplyTarget(msg.Kind) && groupDiscussionUnresolvedMentionPattern.MatchString(msg.Content) {
			clearGroupDiscussionDefaultReplyTargets(session, fromID)
		}
	}
	refreshGroupDiscussionContextSummary(session)
}

func normalizeGroupDiscussionMessageTargetIDs(session *corea2a.Session, fromID string, toIDs []string, content string, kind corea2a.MessageKind) ([]string, error) {
	if len(toIDs) > 0 {
		return normalizeGroupDiscussionTargetIDs(session, toIDs)
	}
	if !groupDiscussionMessageKindUpdatesDefaultReplyTarget(kind) {
		return nil, nil
	}
	if groupDiscussionUnresolvedMentionPattern.MatchString(content) {
		return nil, nil
	}
	if targets := groupDiscussionDefaultReplyTargetsForSender(session, fromID); len(targets) > 0 {
		return targets, nil
	}
	return groupDiscussionDefaultMessageTargetIDs(session, fromID), nil
}

func applyGroupDiscussionMessageTargetEffect(session *corea2a.Session, fromID string, rawToIDs []string, content string, kind corea2a.MessageKind) {
	if !groupDiscussionMessageKindUpdatesDefaultReplyTarget(kind) {
		return
	}
	if len(rawToIDs) > 0 {
		if normalized, err := normalizeGroupDiscussionTargetIDs(session, rawToIDs); err == nil && len(normalized) > 0 {
			setGroupDiscussionDefaultReplyTargets(session, fromID, normalized)
		}
		return
	}
	if groupDiscussionUnresolvedMentionPattern.MatchString(content) {
		clearGroupDiscussionDefaultReplyTargets(session, fromID)
	}
}

func groupDiscussionMessageKindUpdatesDefaultReplyTarget(kind corea2a.MessageKind) bool {
	switch kind {
	case "", corea2a.MessageStatement, corea2a.MessageQuestion, corea2a.MessageAnswer, corea2a.MessageEvidence, corea2a.MessageObjection, corea2a.MessageEscalation:
		return true
	default:
		return false
	}
}

func setGroupDiscussionDefaultReplyTargets(session *corea2a.Session, fromID string, toIDs []string) {
	fromKey := groupDiscussionCanonicalParticipantIdentityKey(fromID)
	if session == nil || fromKey == "" || len(toIDs) == 0 {
		return
	}
	if session.DefaultReplyTargets == nil {
		session.DefaultReplyTargets = map[string][]string{}
	}
	session.DefaultReplyTargets[fromKey] = append([]string(nil), toIDs...)
}

func clearGroupDiscussionDefaultReplyTargets(session *corea2a.Session, fromID string) {
	fromKey := groupDiscussionCanonicalParticipantIdentityKey(fromID)
	if session == nil || fromKey == "" || len(session.DefaultReplyTargets) == 0 {
		return
	}
	delete(session.DefaultReplyTargets, fromKey)
}

func groupDiscussionDefaultReplyTargetsForSender(session *corea2a.Session, fromID string) []string {
	fromKey := groupDiscussionCanonicalParticipantIdentityKey(fromID)
	if session == nil || fromKey == "" || len(session.DefaultReplyTargets) == 0 {
		return nil
	}
	stored, ok := session.DefaultReplyTargets[fromKey]
	if !ok || len(stored) == 0 {
		return nil
	}
	normalized, err := normalizeGroupDiscussionTargetIDs(session, stored)
	if err != nil || len(normalized) == 0 {
		return nil
	}
	return normalized
}

func groupDiscussionDefaultMessageTargetIDs(session *corea2a.Session, fromID string) []string {
	if session == nil {
		return nil
	}
	senderRole := participantRole(session, fromID)
	out := make([]string, 0, len(session.Participants))
	seen := map[string]struct{}{}
	for _, participant := range session.Participants {
		id := strings.TrimSpace(participant.ID)
		if id == "" || groupDiscussionParticipantIdentityMatches(id, fromID) || groupDiscussionParticipantIdentityMatches(id, "local-maclaw") {
			continue
		}
		if groupDiscussionRoleIsInitiator(senderRole) {
			if !groupDiscussionRoleContributesAnswer(participant.RoleCode) {
				continue
			}
		} else if groupDiscussionRoleContributesAnswer(senderRole) {
			if !groupDiscussionRoleIsInitiator(participant.RoleCode) {
				continue
			}
		} else {
			continue
		}
		key := groupDiscussionCanonicalParticipantIdentityKey(id)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func groupDiscussionRoleIsInitiator(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "initiator")
}

func normalizeGroupDiscussionTargetIDs(session *corea2a.Session, toIDs []string) ([]string, error) {
	if len(toIDs) == 0 {
		return nil, nil
	}
	participants := make(map[string]string, len(session.Participants))
	for _, participant := range session.Participants {
		id := strings.TrimSpace(participant.ID)
		if id != "" {
			addGroupDiscussionParticipantAliases(participants, id)
		}
	}
	out := make([]string, 0, len(toIDs))
	seen := map[string]struct{}{}
	for _, rawID := range toIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		canonical, ok := participants[strings.ToLower(id)]
		if !ok {
			return nil, fmt.Errorf("target participant %s is not in discussion", id)
		}
		key := groupDiscussionCanonicalParticipantIdentityKey(canonical)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, canonical)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("target participant id is required")
	}
	return out, nil
}

func addGroupDiscussionParticipantAliases(target map[string]string, participantID string) {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" || target == nil {
		return
	}
	for _, alias := range groupDiscussionParticipantIdentityKeys(participantID) {
		if _, ok := target[alias]; ok {
			continue
		}
		target[alias] = participantID
	}
}

func groupDiscussionParticipantIdentityKeys(participantID string) []string {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return nil
	}
	out := make([]string, 0, 6)
	seen := map[string]struct{}{}
	add := func(value string) {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	add(participantID)
	cleaned := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(participantID)
	withoutPrefix := cleaned
	if len(withoutPrefix) > 3 && (strings.EqualFold(withoutPrefix[:3], "ve_") || strings.EqualFold(withoutPrefix[:3], "ve-")) {
		withoutPrefix = withoutPrefix[3:]
	}
	for _, base := range []string{withoutPrefix, strings.ReplaceAll(withoutPrefix, "-", "_")} {
		add(base)
		add("ve_" + base)
		add("ve-" + base)
	}
	return out
}

func groupDiscussionParticipantIdentityMatches(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	aKeys := map[string]struct{}{}
	for _, key := range groupDiscussionParticipantIdentityKeys(a) {
		aKeys[key] = struct{}{}
	}
	for _, key := range groupDiscussionParticipantIdentityKeys(b) {
		if _, ok := aKeys[key]; ok {
			return true
		}
	}
	return false
}

func groupDiscussionGeneratedVEAlias(id string) bool {
	id = strings.TrimSpace(id)
	return len(id) > 3 && (strings.EqualFold(id[:3], "ve_") || strings.EqualFold(id[:3], "ve-"))
}

func canonicalGroupDiscussionParticipantID(session *corea2a.Session, participantID string) string {
	participant := findGroupDiscussionParticipant(session, participantID)
	if participant == nil {
		return ""
	}
	return strings.TrimSpace(participant.ID)
}

func findGroupDiscussionParticipant(session *corea2a.Session, participantID string) *corea2a.Participant {
	participantID = strings.TrimSpace(participantID)
	if session == nil || participantID == "" {
		return nil
	}
	for i := range session.Participants {
		if groupDiscussionParticipantIdentityMatches(session.Participants[i].ID, participantID) {
			return &session.Participants[i]
		}
	}
	return nil
}

func groupDiscussionParticipantCanMessage(roleCode string) bool {
	switch strings.ToLower(strings.TrimSpace(roleCode)) {
	case "", "initiator", "review", "speak", "speaker", "participant", "executor":
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
		seenDeciders := map[string]struct{}{}
		for _, participant := range session.Participants {
			id := strings.TrimSpace(participant.ID)
			key := groupDiscussionCanonicalParticipantIdentityKey(id)
			if id != "" && key != "" {
				if _, ok := seenDeciders[key]; ok {
					continue
				}
				seenDeciders[key] = struct{}{}
				decidedBy = append(decidedBy, id)
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
		toIDs, err := normalizeGroupDiscussionMessageTargetIDs(session, req.FromID, req.ToIDs, req.Content, req.Kind)
		if err != nil {
			return err
		}
		if err := session.AddMessage(corea2a.Message{ID: newGroupDiscussionID("a2amsg"), FromID: req.FromID, ToIDs: toIDs, Kind: req.Kind, Content: req.Content, Evidence: req.Evidence, TextAttachments: req.TextAttachments, ImageAttachments: req.ImageAttachments, FileAttachments: req.FileAttachments, CreatedAt: time.Now().UTC()}); err != nil {
			return err
		}
		applyGroupDiscussionMessageTargetEffect(session, req.FromID, req.ToIDs, req.Content, req.Kind)
		refreshGroupDiscussionContextSummary(session)
		return nil
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
		if groupDiscussionParticipantIdentityMatches(participant.ID, participantID) {
			return strings.TrimSpace(participant.RoleCode)
		}
	}
	return ""
}

func normalizeTenantID(tenantID string) string {
	return store.NormalizeTenantID(tenantID)
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

func addParticipantIfMissing(session *corea2a.Session, participant corea2a.Participant) bool {
	if session == nil || strings.TrimSpace(participant.ID) == "" {
		return false
	}
	participant.ID = strings.TrimSpace(participant.ID)
	for i := range session.Participants {
		if !groupDiscussionParticipantIdentityMatches(session.Participants[i].ID, participant.ID) {
			continue
		}
		changed := false
		if role := strings.TrimSpace(participant.RoleCode); role != "" {
			existingRole := strings.TrimSpace(session.Participants[i].RoleCode)
			if existingRole == "" || (!groupDiscussionParticipantCanMessage(existingRole) && groupDiscussionParticipantCanMessage(role)) {
				session.Participants[i].RoleCode = role
				changed = true
			}
		}
		if name := strings.TrimSpace(participant.Name); name != "" && strings.TrimSpace(session.Participants[i].Name) == "" {
			session.Participants[i].Name = name
			changed = true
		}
		if len(participant.Skills) > 0 && len(session.Participants[i].Skills) == 0 {
			session.Participants[i].Skills = append([]string(nil), participant.Skills...)
			changed = true
		}
		if changed {
			session.UpdatedAt = time.Now().UTC()
		}
		return changed
	}
	session.Participants = append(session.Participants, participant)
	session.UpdatedAt = time.Now().UTC()
	return true
}

func cloneSession(in *corea2a.Session) *corea2a.Session {
	if in == nil {
		return nil
	}
	out := *in
	out.Participants = append([]corea2a.Participant(nil), in.Participants...)
	if len(in.DefaultReplyTargets) > 0 {
		out.DefaultReplyTargets = make(map[string][]string, len(in.DefaultReplyTargets))
		for key, targets := range in.DefaultReplyTargets {
			out.DefaultReplyTargets[key] = append([]string(nil), targets...)
		}
	}
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
