package a2a

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

type Service struct {
	mu       sync.RWMutex
	repo     *Repo
	sessions map[string]map[string]*corea2a.Session
	profiles map[string]map[string]corea2a.GroupProfile
	invites  map[string]map[string]groupInviteRecord
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

func NewService(repos ...*Repo) *Service {
	var repo *Repo
	if len(repos) > 0 {
		repo = repos[0]
	}
	return &Service{repo: repo, sessions: map[string]map[string]*corea2a.Session{}, profiles: map[string]map[string]corea2a.GroupProfile{}, invites: map[string]map[string]groupInviteRecord{}}
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
	OrgUnitID string
	Status    corea2a.SessionStatus
}

type AddMessageRequest struct {
	FromID   string              `json:"from_id"`
	ToIDs    []string            `json:"to_ids"`
	Kind     corea2a.MessageKind `json:"kind"`
	Content  string              `json:"content"`
	Evidence []string            `json:"evidence"`
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
	ProposalID string `json:"proposal_id"`
	Summary    string `json:"summary"`
}

type EscalateRequest struct {
	RaisedBy string `json:"raised_by"`
	Reason   string `json:"reason"`
	Target   string `json:"target"`
}

type AdminGroupDiscussionSnapshot struct {
	Experts     []corea2a.GroupProfile         `json:"experts"`
	Discussions []corea2a.HubDiscussionSummary `json:"discussions"`
}

func (s *Service) UpsertExpertProfile(tenantID string, profile corea2a.GroupProfile) (corea2a.GroupProfile, error) {
	tenantID = normalizeTenantID(tenantID)
	profile = profile.DiscoveryView("")
	if strings.TrimSpace(profile.AgentID) == "" {
		return corea2a.GroupProfile{}, fmt.Errorf("agent_id is required")
	}
	if profile.UpdatedAt.IsZero() {
		profile.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profiles[tenantID] == nil {
		s.profiles[tenantID] = map[string]corea2a.GroupProfile{}
	}
	s.profiles[tenantID][profile.AgentID] = cloneGroupProfile(profile)
	return cloneGroupProfile(profile), nil
}

func (s *Service) ListExpertProfiles(tenantID string, activeWindow time.Duration) []corea2a.GroupProfile {
	tenantID = normalizeTenantID(tenantID)
	now := time.Now().UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]corea2a.GroupProfile, 0, len(s.profiles[tenantID]))
	for _, profile := range s.profiles[tenantID] {
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

func (s *Service) AdminGroupDiscussionSnapshot(tenantID string) (AdminGroupDiscussionSnapshot, error) {
	discussions, err := s.ListDiscussionSummaries(tenantID, ListSessionsFilter{})
	if err != nil {
		return AdminGroupDiscussionSnapshot{}, err
	}
	return AdminGroupDiscussionSnapshot{Experts: s.ListExpertProfiles(tenantID, 10*time.Minute), Discussions: discussions}, nil
}

func (s *Service) ListDiscussionSummaries(tenantID string, filter ListSessionsFilter) ([]corea2a.HubDiscussionSummary, error) {
	sessions, err := s.ListSessions(tenantID, filter)
	if err != nil {
		return nil, err
	}
	out := make([]corea2a.HubDiscussionSummary, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		out = append(out, discussionSummaryFromSession(session))
	}
	return out, nil
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
	answerCount := discussionAnswerCount(session.Messages)
	expectedAnswerCount := len(participants) - 1
	if expectedAnswerCount < 1 {
		expectedAnswerCount = 1
	}
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

func discussionAnswerCount(messages []corea2a.Message) int {
	count := 0
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		switch msg.Kind {
		case corea2a.MessageAnswer, corea2a.MessageStatement, corea2a.MessageEvidence, corea2a.MessageObjection:
			count++
		}
	}
	return count
}

func (s *Service) CreateConsultation(tenantID string, req corea2a.GroupConsultationRequest) (corea2a.ConsultationCreateResponse, error) {
	tenantID = normalizeTenantID(tenantID)
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return corea2a.ConsultationCreateResponse{}, fmt.Errorf("question is required")
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	if req.ID == "" {
		req.ID = idgen.New("a2areq")
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
	session, err := s.CreateSession(tenantID, CreateSessionRequest{Topic: topic, Goal: req.Question, Participants: participants, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		return corea2a.ConsultationCreateResponse{}, err
	}
	if strings.TrimSpace(req.ContextSummary) != "" {
		_, _ = s.AddMessage(tenantID, session.ID, AddMessageRequest{FromID: req.FromID, Kind: corea2a.MessageQuestion, Content: req.ContextSummary})
		session, _ = s.GetSession(tenantID, session.ID)
	}
	return corea2a.ConsultationCreateResponse{Discussion: discussionSummaryFromSession(session), Request: req}, nil
}

func (s *Service) GetDiscussionSummary(tenantID, sessionID string) (corea2a.HubDiscussionSummary, error) {
	session, err := s.GetSession(tenantID, sessionID)
	if err != nil {
		return corea2a.HubDiscussionSummary{}, err
	}
	return discussionSummaryFromSession(session), nil
}

func (s *Service) GetDiscussionDetail(tenantID, sessionID string) (corea2a.HubDiscussionDetail, error) {
	session, err := s.GetSession(tenantID, sessionID)
	if err != nil {
		return corea2a.HubDiscussionDetail{}, err
	}
	return corea2a.HubDiscussionDetail{
		Discussion: discussionSummaryFromSession(session),
		Session:    session,
		Messages:   append([]corea2a.Message(nil), session.Messages...),
		Proposals:  append([]corea2a.Proposal(nil), session.Proposals...),
		Reviews:    append([]corea2a.Review(nil), session.Reviews...),
		Decision:   session.Decision,
	}, nil
}

func (s *Service) ListInvitations(tenantID, toID, status string) []corea2a.GroupInviteSummary {
	tenantID = normalizeTenantID(tenantID)
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
	return items
}

func (s *Service) AddInvitation(tenantID, sessionID string, inv corea2a.GroupInvitation) (string, error) {
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
	inviteID := idgen.New("a2ainv")
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.loadSessionLocked(tenantID, sessionID)
	if err != nil {
		return "", err
	}
	if s.invites[tenantID] == nil {
		s.invites[tenantID] = map[string]groupInviteRecord{}
	}
	s.invites[tenantID][inviteID] = groupInviteRecord{ID: inviteID, TenantID: tenantID, SessionID: sessionID, Invite: inv, Status: "pending", CreatedAt: time.Now().UTC()}
	content := fmt.Sprintf("invited %s as %s", strings.TrimSpace(inv.ToID), inv.Role)
	_ = session.AddMessage(corea2a.Message{ID: idgen.New("a2amsg"), FromID: strings.TrimSpace(inv.FromID), ToIDs: []string{strings.TrimSpace(inv.ToID)}, Kind: corea2a.MessageHandoff, Content: content, CreatedAt: time.Now().UTC()})
	if s.repo != nil {
		if err := s.repo.UpsertSession(tenantID, session); err != nil {
			return "", err
		}
	}
	return inviteID, nil
}

func (s *Service) RespondInvitation(tenantID, inviteID string, resp corea2a.GroupInvitationResponse) error {
	tenantID = normalizeTenantID(tenantID)
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return fmt.Errorf("invite id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.invites[tenantID][inviteID]
	if !ok {
		return fmt.Errorf("invite not found: %s", inviteID)
	}
	session, err := s.loadSessionLocked(tenantID, record.SessionID)
	if err != nil {
		return err
	}
	fromID := strings.TrimSpace(resp.FromID)
	if fromID == "" {
		fromID = strings.TrimSpace(record.Invite.ToID)
	}
	decision := resp.Decision
	if decision == "" {
		decision = corea2a.GroupInvitationReject
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
	_ = session.AddMessage(corea2a.Message{ID: idgen.New("a2amsg"), FromID: fromID, Kind: corea2a.MessageAnswer, Content: content, CreatedAt: time.Now().UTC()})
	if s.repo != nil {
		if err := s.repo.UpsertSession(tenantID, session); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) AddDiscussionMessage(tenantID, sessionID string, msg corea2a.GroupDiscussionMessage) (*corea2a.Session, error) {
	kind := msg.Kind
	if kind == "" {
		kind = corea2a.MessageStatement
	}
	return s.AddMessage(tenantID, sessionID, AddMessageRequest{FromID: msg.FromID, Kind: kind, Content: msg.Content})
}

func (s *Service) SubmitDiscussionResult(tenantID, sessionID string, result corea2a.GroupDiscussionResult) (*corea2a.Session, error) {
	if strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("discussion result summary is required")
	}
	content := strings.TrimSpace(result.Rationale)
	if content == "" {
		content = result.Summary
	}
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		now := time.Now().UTC()
		proposalID := idgen.New("a2aprop")
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
		session.Decision = &corea2a.Decision{ID: idgen.New("a2adec"), SessionID: session.ID, ProposalID: proposalID, Summary: strings.TrimSpace(result.Summary), Rationale: content, DecidedBy: decidedBy, CreatedAt: now}
		session.Status = corea2a.SessionDecided
		session.UpdatedAt = now
		return nil
	})
}

func (s *Service) SetDiscussionState(tenantID, sessionID, action string) (*corea2a.Session, error) {
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

func (s *Service) CreateSession(tenantID string, req CreateSessionRequest) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	now := time.Now().UTC()
	session, err := corea2a.NewSession(idgen.New("a2a"), req.Topic, req.Goal, req.Participants, req.DecisionPolicy, now)
	if err != nil {
		return nil, err
	}
	session.TenantID = tenantID
	session.OrgUnitID = normalizeOrgUnitID(req.OrgUnitID, req.DepartmentID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repo != nil {
		if err := s.repo.UpsertSession(tenantID, session); err != nil {
			return nil, err
		}
		return cloneSession(session), nil
	}
	if s.sessions[tenantID] == nil {
		s.sessions[tenantID] = map[string]*corea2a.Session{}
	}
	s.sessions[tenantID][session.ID] = session
	return cloneSession(session), nil
}

func (s *Service) ListSessions(tenantID string, filters ...ListSessionsFilter) ([]*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	filter := firstListFilter(filters)
	if s.repo != nil {
		items, err := s.repo.ListSessions(tenantID, filter)
		if err != nil {
			return nil, err
		}
		for i := range items {
			items[i] = cloneSession(items[i])
		}
		return items, nil
	}
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
	return items, nil
}

func (s *Service) GetSession(tenantID, sessionID string) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	sessionID = strings.TrimSpace(sessionID)
	if s.repo != nil {
		session, err := s.repo.GetSession(tenantID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return cloneSession(session), nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[tenantID][sessionID]
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return cloneSession(session), nil
}

func (s *Service) AddMessage(tenantID, sessionID string, req AddMessageRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		return session.AddMessage(corea2a.Message{ID: idgen.New("a2amsg"), FromID: req.FromID, ToIDs: req.ToIDs, Kind: req.Kind, Content: req.Content, Evidence: req.Evidence, CreatedAt: time.Now().UTC()})
	})
}

func (s *Service) AddProposal(tenantID, sessionID string, req AddProposalRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		return session.AddProposal(corea2a.Proposal{ID: idgen.New("a2aprop"), AuthorID: req.AuthorID, Title: req.Title, Content: req.Content, Goals: req.Goals, Constraints: req.Constraints, Risks: req.Risks, CreatedAt: time.Now().UTC()})
	})
}

func (s *Service) AddReview(tenantID, sessionID string, req AddReviewRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		return session.AddReview(corea2a.Review{ID: idgen.New("a2arev"), ProposalID: req.ProposalID, ReviewerID: req.ReviewerID, Position: req.Position, Comment: req.Comment, CreatedAt: time.Now().UTC()})
	})
}

func (s *Service) Decide(tenantID, sessionID string, req DecideRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		_, err := session.TryDecide(idgen.New("a2adec"), req.ProposalID, req.Summary, time.Now().UTC())
		return err
	})
}

func (s *Service) Escalate(tenantID, sessionID string, req EscalateRequest) (*corea2a.Session, error) {
	return s.mutate(tenantID, sessionID, func(session *corea2a.Session) error {
		return session.Escalate(corea2a.Escalation{ID: idgen.New("a2aesc"), RaisedBy: req.RaisedBy, Reason: req.Reason, Target: req.Target, CreatedAt: time.Now().UTC()})
	})
}

func (s *Service) mutate(tenantID, sessionID string, fn func(*corea2a.Session) error) (*corea2a.Session, error) {
	tenantID = normalizeTenantID(tenantID)
	sessionID = strings.TrimSpace(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()

	var session *corea2a.Session
	if s.repo != nil {
		loaded, err := s.repo.GetSession(tenantID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		session = loaded
	} else {
		session = s.sessions[tenantID][sessionID]
		if session == nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
	}

	if err := fn(session); err != nil {
		return nil, err
	}
	if s.repo != nil {
		if err := s.repo.UpsertSession(tenantID, session); err != nil {
			return nil, err
		}
	}
	return cloneSession(session), nil
}

func firstListFilter(filters []ListSessionsFilter) ListSessionsFilter {
	if len(filters) == 0 {
		return ListSessionsFilter{}
	}
	filter := filters[0]
	filter.OrgUnitID = normalizeOrgUnitID(filter.OrgUnitID)
	return filter
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
	return true
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

func (s *Service) loadSessionLocked(tenantID, sessionID string) (*corea2a.Session, error) {
	if s.repo != nil {
		loaded, err := s.repo.GetSession(tenantID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return loaded, nil
	}
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
