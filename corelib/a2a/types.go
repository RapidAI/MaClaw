package a2a

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type SessionStatus string

const (
	SessionOpen      SessionStatus = "open"
	SessionDecided   SessionStatus = "decided"
	SessionEscalated SessionStatus = "escalated"
	SessionClosed    SessionStatus = "closed"
)

type MessageKind string

const (
	MessageStatement   MessageKind = "statement"
	MessageQuestion    MessageKind = "question"
	MessageAnswer      MessageKind = "answer"
	MessageEvidence    MessageKind = "evidence"
	MessageObjection   MessageKind = "objection"
	MessageHandoff     MessageKind = "handoff"
	MessageEscalation  MessageKind = "escalation"
	MessageStreamChunk MessageKind = "stream_chunk" // VE streaming response fragment
	MessageStreamEnd   MessageKind = "stream_end"   // VE streaming response complete
)

type ProposalStatus string

const (
	ProposalOpen       ProposalStatus = "open"
	ProposalAccepted   ProposalStatus = "accepted"
	ProposalRejected   ProposalStatus = "rejected"
	ProposalSuperseded ProposalStatus = "superseded"
)

type ReviewPosition string

const (
	ReviewApprove ReviewPosition = "approve"
	ReviewReject  ReviewPosition = "reject"
	ReviewConcern ReviewPosition = "concern"
	ReviewAbstain ReviewPosition = "abstain"
)

type DecisionPolicy string

const (
	PolicyMajority  DecisionPolicy = "majority"
	PolicyUnanimous DecisionPolicy = "unanimous"
)

type Participant struct {
	ID       string   `json:"id"`
	RoleCode string   `json:"role_code,omitempty"`
	Name     string   `json:"name,omitempty"`
	Skills   []string `json:"skills,omitempty"`
}

type Session struct {
	ID                  string              `json:"id"`
	TenantID            string              `json:"tenant_id,omitempty"`
	OrgUnitID           string              `json:"org_unit_id,omitempty"`
	Topic               string              `json:"topic"`
	Goal                string              `json:"goal,omitempty"`
	Status              SessionStatus       `json:"status"`
	DecisionPolicy      DecisionPolicy      `json:"decision_policy"`
	Participants        []Participant       `json:"participants"`
	ContextSummary      string              `json:"context_summary,omitempty"`
	SummaryUpToID       string              `json:"summary_up_to_id,omitempty"`
	SummaryUpdatedAt    time.Time           `json:"summary_updated_at,omitempty"`
	DefaultReplyTargets map[string][]string `json:"default_reply_targets,omitempty"`
	Messages            []Message           `json:"messages,omitempty"`
	Proposals           []Proposal          `json:"proposals,omitempty"`
	Reviews             []Review            `json:"reviews,omitempty"`
	Decision            *Decision           `json:"decision,omitempty"`
	Escalation          *Escalation         `json:"escalation,omitempty"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
}

type Message struct {
	ID               string            `json:"id"`
	SessionID        string            `json:"session_id"`
	FromID           string            `json:"from_id"`
	ToIDs            []string          `json:"to_ids,omitempty"`
	Kind             MessageKind       `json:"kind"`
	Content          string            `json:"content"`
	Evidence         []string          `json:"evidence,omitempty"`
	TextAttachments  []TextAttachment  `json:"text_attachments,omitempty"`
	ImageAttachments []ImageAttachment `json:"image_attachments,omitempty"`
	FileAttachments  []FileAttachment  `json:"file_attachments,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

type Proposal struct {
	ID          string         `json:"id"`
	SessionID   string         `json:"session_id"`
	AuthorID    string         `json:"author_id"`
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	Goals       []string       `json:"goals,omitempty"`
	Constraints []string       `json:"constraints,omitempty"`
	Risks       []string       `json:"risks,omitempty"`
	Status      ProposalStatus `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type Review struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	ProposalID string         `json:"proposal_id"`
	ReviewerID string         `json:"reviewer_id"`
	Position   ReviewPosition `json:"position"`
	Comment    string         `json:"comment,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Decision struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id"`
	ProposalID string     `json:"proposal_id"`
	Summary    string     `json:"summary"`
	Rationale  string     `json:"rationale,omitempty"`
	DecidedBy  []string   `json:"decided_by"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
	RollbackOn []string   `json:"rollback_on,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Handoff struct {
	FromID         string   `json:"from_id"`
	ToID           string   `json:"to_id"`
	Context        string   `json:"context"`
	ExpectedOutput string   `json:"expected_output"`
	Constraints    []string `json:"constraints,omitempty"`
}

type Escalation struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	RaisedBy  string    `json:"raised_by"`
	Reason    string    `json:"reason"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
}

// TextAttachment represents an inline text file attachment (base64-encoded content).
type TextAttachment struct {
	Content   string `json:"content"`              // base64-encoded text content
	Filename  string `json:"filename"`             // original filename
	MimeType  string `json:"mime_type,omitempty"`  // e.g. "text/plain", "text/markdown"
	LocalPath string `json:"local_path,omitempty"` // local cached copy path, when downloaded
}

// ImageAttachment represents an image file attachment referenced by URL.
type ImageAttachment struct {
	FileURL   string `json:"file_url"`             // Hub file relay URL
	Filename  string `json:"filename"`             // original filename
	MimeType  string `json:"mime_type,omitempty"`  // e.g. "image/png", "image/jpeg"
	Width     int    `json:"width,omitempty"`      // image width in pixels
	Height    int    `json:"height,omitempty"`     // image height in pixels
	LocalPath string `json:"local_path,omitempty"` // local cached copy path, when downloaded
}

// FileAttachment represents a document/file attachment referenced by URL.
type FileAttachment struct {
	FileURL   string `json:"file_url"`             // Hub file relay URL
	Filename  string `json:"filename"`             // original filename
	MimeType  string `json:"mime_type,omitempty"`  // e.g. "application/pdf"
	SizeBytes int64  `json:"size_bytes,omitempty"` // file size in bytes
	LocalPath string `json:"local_path,omitempty"` // local cached copy path, when downloaded
}

type ReviewSummary struct {
	Approvals  int      `json:"approvals"`
	Rejections int      `json:"rejections"`
	Concerns   int      `json:"concerns"`
	Abstains   int      `json:"abstains"`
	ReviewedBy []string `json:"reviewed_by"`
}

func NewSession(id, topic, goal string, participants []Participant, policy DecisionPolicy, now time.Time) (*Session, error) {
	id = strings.TrimSpace(id)
	topic = strings.TrimSpace(topic)
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("at least one participant is required")
	}
	if policy == "" {
		policy = PolicyMajority
	}
	if now.IsZero() {
		now = time.Now()
	}
	return &Session{ID: id, Topic: topic, Goal: strings.TrimSpace(goal), Status: SessionOpen, DecisionPolicy: policy, Participants: participants, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Session) AddMessage(msg Message) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	msg.Content = strings.TrimSpace(msg.Content)
	if msg.Kind == "" {
		msg.Kind = MessageStatement
	}
	if strings.TrimSpace(msg.ID) == "" || strings.TrimSpace(msg.FromID) == "" || !messageHasPayload(msg) {
		return fmt.Errorf("message id, from_id and content or attachment payload are required")
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	msg.SessionID = s.ID
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = msg.CreatedAt
	return nil
}

func messageHasPayload(msg Message) bool {
	return strings.TrimSpace(msg.Content) != "" ||
		msg.Kind == MessageStreamEnd ||
		len(msg.TextAttachments) > 0 ||
		len(msg.ImageAttachments) > 0 ||
		len(msg.FileAttachments) > 0
}

func (s *Session) AddProposal(p Proposal) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	p.Title = strings.TrimSpace(p.Title)
	p.Content = strings.TrimSpace(p.Content)
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.AuthorID) == "" || p.Title == "" || p.Content == "" {
		return fmt.Errorf("proposal id, author_id, title and content are required")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	p.SessionID = s.ID
	p.Status = ProposalOpen
	p.UpdatedAt = p.CreatedAt
	s.Proposals = append(s.Proposals, p)
	s.UpdatedAt = p.CreatedAt
	return nil
}

func (s *Session) AddReview(r Review) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.ProposalID) == "" || strings.TrimSpace(r.ReviewerID) == "" {
		return fmt.Errorf("review id, proposal_id and reviewer_id are required")
	}
	if r.Position == "" {
		r.Position = ReviewAbstain
	}
	if !s.hasProposal(r.ProposalID) {
		return fmt.Errorf("proposal %s not found", r.ProposalID)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	r.SessionID = s.ID
	s.Reviews = append(s.Reviews, r)
	s.UpdatedAt = r.CreatedAt
	return nil
}

func (s *Session) TryDecide(decisionID, proposalID, summary string, now time.Time) (*Decision, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	proposal := s.findProposal(proposalID)
	if proposal == nil {
		return nil, fmt.Errorf("proposal %s not found", proposalID)
	}
	if strings.TrimSpace(summary) == "" {
		summary = proposal.Title
	}
	if !s.PolicySatisfied(proposalID) {
		return nil, fmt.Errorf("decision policy %s is not satisfied", s.DecisionPolicy)
	}
	if now.IsZero() {
		now = time.Now()
	}
	decision := &Decision{ID: strings.TrimSpace(decisionID), SessionID: s.ID, ProposalID: proposalID, Summary: strings.TrimSpace(summary), DecidedBy: s.approvers(proposalID), CreatedAt: now}
	if decision.ID == "" {
		return nil, fmt.Errorf("decision id is required")
	}
	proposal.Status = ProposalAccepted
	proposal.UpdatedAt = now
	s.Decision = decision
	s.Status = SessionDecided
	s.UpdatedAt = now
	return decision, nil
}

func (s *Session) Escalate(e Escalation) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.RaisedBy) == "" || strings.TrimSpace(e.Reason) == "" {
		return fmt.Errorf("escalation id, raised_by and reason are required")
	}
	if e.Target == "" {
		e.Target = "human_owner"
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.SessionID = s.ID
	s.Escalation = &e
	s.Status = SessionEscalated
	s.UpdatedAt = e.CreatedAt
	return nil
}

func (s *Session) ReviewSummary(proposalID string) ReviewSummary {
	latest := map[string]Review{}
	for _, r := range s.Reviews {
		if r.ProposalID == proposalID {
			latest[participantIdentityKey(r.ReviewerID)] = r
		}
	}
	out := ReviewSummary{ReviewedBy: make([]string, 0, len(latest))}
	for _, review := range latest {
		reviewer := strings.TrimSpace(review.ReviewerID)
		pos := review.Position
		out.ReviewedBy = append(out.ReviewedBy, reviewer)
		switch pos {
		case ReviewApprove:
			out.Approvals++
		case ReviewReject:
			out.Rejections++
		case ReviewConcern:
			out.Concerns++
		default:
			out.Abstains++
		}
	}
	slices.Sort(out.ReviewedBy)
	return out
}

func (s *Session) PolicySatisfied(proposalID string) bool {
	summary := s.ReviewSummary(proposalID)
	if summary.Rejections > 0 || summary.Concerns > 0 {
		return false
	}
	participants := s.distinctParticipantCount()
	if participants == 0 {
		return false
	}
	if s.DecisionPolicy == PolicyUnanimous {
		return summary.Approvals == participants
	}
	return summary.Approvals > participants/2
}

func (s *Session) distinctParticipantCount() int {
	if s == nil {
		return 0
	}
	seen := map[string]struct{}{}
	for _, participant := range s.Participants {
		key := participantIdentityKey(participant.ID)
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	return len(seen)
}

func (s *Session) ensureOpen() error {
	if s == nil {
		return fmt.Errorf("session is nil")
	}
	if s.Status != SessionOpen {
		return fmt.Errorf("session %s is %s", s.ID, s.Status)
	}
	return nil
}

func (s *Session) hasProposal(id string) bool { return s.findProposal(id) != nil }

func (s *Session) findProposal(id string) *Proposal {
	for i := range s.Proposals {
		if s.Proposals[i].ID == id {
			return &s.Proposals[i]
		}
	}
	return nil
}

func (s *Session) approvers(proposalID string) []string {
	latest := map[string]Review{}
	for _, r := range s.Reviews {
		if r.ProposalID == proposalID {
			latest[participantIdentityKey(r.ReviewerID)] = r
		}
	}
	out := []string{}
	for _, review := range latest {
		if review.Position == ReviewApprove {
			out = append(out, strings.TrimSpace(review.ReviewerID))
		}
	}
	slices.Sort(out)
	return out
}

func participantIdentityKey(participantID string) string {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return ""
	}
	cleaned := strings.NewReplacer("/", "_", "\\", "_", " ", "_", "-", "_").Replace(participantID)
	if len(cleaned) > 3 && strings.EqualFold(cleaned[:3], "ve_") {
		cleaned = cleaned[3:]
	}
	return strings.ToLower(strings.TrimSpace(cleaned))
}
