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
}

func NewService(repos ...*Repo) *Service {
	var repo *Repo
	if len(repos) > 0 {
		repo = repos[0]
	}
	return &Service{repo: repo, sessions: map[string]map[string]*corea2a.Session{}}
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
