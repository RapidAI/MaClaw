package a2a

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const GroupScopeCurrentHub = "current_hub"

type GroupMessageType string

const (
	GroupMessageProfile             GroupMessageType = "profile"
	GroupMessageConsultationRequest GroupMessageType = "consultation_request"
	GroupMessageInvitation          GroupMessageType = "invitation"
	GroupMessageInvitationResponse  GroupMessageType = "invitation_response"
	GroupMessageDiscussionMessage   GroupMessageType = "discussion_message"
	GroupMessageDiscussionResult    GroupMessageType = "discussion_result"
	GroupMessageApprovalRequest     GroupMessageType = "approval_request"
	GroupMessageApprovalResponse    GroupMessageType = "approval_response"
)

// Envelope type aliases for approval workflow messages.
const (
	EnvelopeTypeApprovalRequest  = "approval_request"
	EnvelopeTypeApprovalResponse = "approval_response"
)

type GroupRole string

const (
	GroupRoleObserve GroupRole = "observe"
	GroupRoleSpeak   GroupRole = "speak"
	GroupRoleReview  GroupRole = "review"
)

type GroupInvitationDecision string

const (
	GroupInvitationAccept GroupInvitationDecision = "accept"
	GroupInvitationReject GroupInvitationDecision = "reject"
)

type GroupProfile struct {
	AgentID              string    `json:"agent_id"`
	DisplayName          string    `json:"display_name,omitempty"`
	Skills               []string  `json:"skills,omitempty"`
	Description          string    `json:"description,omitempty"`
	ModelClass           string    `json:"model_class,omitempty"`
	Languages            []string  `json:"languages,omitempty"`
	SecurityGroupID      string    `json:"security_group_id,omitempty"`
	ContributionScore    float64   `json:"contribution_score,omitempty"`
	ContributionEvidence int       `json:"contribution_evidence,omitempty"`
	Discoverable         bool      `json:"discoverable"`
	Available            bool      `json:"available"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type GroupConsultationRequest struct {
	ID             string    `json:"id"`
	FromID         string    `json:"from_id"`
	Topic          string    `json:"topic,omitempty"`
	Question       string    `json:"question"`
	ContextSummary string    `json:"context_summary,omitempty"`
	SkillsWanted   []string  `json:"skills_wanted,omitempty"`
	RiskLevel      string    `json:"risk_level,omitempty"`
	MaxRounds      int       `json:"max_rounds,omitempty"`
	TimeoutSeconds int       `json:"timeout_seconds,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type GroupInvitation struct {
	RequestID       string    `json:"request_id"`
	FromID          string    `json:"from_id"`
	ToID            string    `json:"to_id"`
	Role            GroupRole `json:"role"`
	Trusted         bool      `json:"trusted,omitempty"`
	SecurityGroupID string    `json:"security_group_id,omitempty"`
	ContextPolicy   string    `json:"context_policy,omitempty"`
}

type GroupInvitationResponse struct {
	RequestID string                  `json:"request_id"`
	FromID    string                  `json:"from_id"`
	ToID      string                  `json:"to_id"`
	Decision  GroupInvitationDecision `json:"decision"`
	Reason    string                  `json:"reason,omitempty"`
}

type GroupDiscussionMessage struct {
	ID               string            `json:"id"`
	SessionID        string            `json:"session_id"`
	FromID           string            `json:"from_id"`
	ToIDs            []string          `json:"to_ids,omitempty"`
	Kind             MessageKind       `json:"kind"`
	Content          string            `json:"content"`
	TextAttachments  []TextAttachment  `json:"text_attachments,omitempty"`
	ImageAttachments []ImageAttachment `json:"image_attachments,omitempty"`
	FileAttachments  []FileAttachment  `json:"file_attachments,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

type GroupDiscussionResult struct {
	SessionID string    `json:"session_id"`
	Summary   string    `json:"summary"`
	Rationale string    `json:"rationale,omitempty"`
	Risks     []string  `json:"risks,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupEnvelope struct {
	ID        string           `json:"id"`
	Type      GroupMessageType `json:"type"`
	Scope     string           `json:"scope"`
	FromID    string           `json:"from_id"`
	ToIDs     []string         `json:"to_ids,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	CreatedAt time.Time        `json:"created_at"`

	Profile            *GroupProfile             `json:"profile,omitempty"`
	Request            *GroupConsultationRequest `json:"request,omitempty"`
	Invitation         *GroupInvitation          `json:"invitation,omitempty"`
	InvitationResponse *GroupInvitationResponse  `json:"invitation_response,omitempty"`
	Message            *GroupDiscussionMessage   `json:"message,omitempty"`
	Result             *GroupDiscussionResult    `json:"result,omitempty"`

	// Payload carries serialized approval workflow messages.
	// For Type=approval_request: JSON-serialized ApprovalRequest (from hub/internal/workflow).
	// For Type=approval_response: JSON-serialized ApprovalResponse (from hub/internal/workflow).
	Payload json.RawMessage `json:"payload,omitempty"`
}

func NewGroupEnvelope(id string, typ GroupMessageType, fromID string, now time.Time) GroupEnvelope {
	if now.IsZero() {
		now = time.Now()
	}
	return GroupEnvelope{
		ID:        strings.TrimSpace(id),
		Type:      typ,
		Scope:     GroupScopeCurrentHub,
		FromID:    strings.TrimSpace(fromID),
		CreatedAt: now,
	}
}

func (e GroupEnvelope) ValidateCurrentHub() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("envelope id is required")
	}
	if strings.TrimSpace(e.FromID) == "" {
		return fmt.Errorf("from_id is required")
	}
	if e.Type == "" {
		return fmt.Errorf("message type is required")
	}
	if e.Scope != "" && e.Scope != GroupScopeCurrentHub {
		return fmt.Errorf("group discussion scope must be %s", GroupScopeCurrentHub)
	}
	switch e.Type {
	case GroupMessageProfile:
		if e.Profile == nil {
			return fmt.Errorf("profile payload is required")
		}
	case GroupMessageConsultationRequest:
		if e.Request == nil || strings.TrimSpace(e.Request.Question) == "" {
			return fmt.Errorf("consultation request question is required")
		}
	case GroupMessageInvitation:
		if e.Invitation == nil || strings.TrimSpace(e.Invitation.ToID) == "" {
			return fmt.Errorf("invitation target is required")
		}
	case GroupMessageInvitationResponse:
		if e.InvitationResponse == nil || e.InvitationResponse.Decision == "" {
			return fmt.Errorf("invitation response decision is required")
		}
	case GroupMessageDiscussionMessage:
		if e.Message == nil || !GroupDiscussionMessageHasPayload(*e.Message) {
			return fmt.Errorf("discussion message content or attachment payload is required")
		}
	case GroupMessageDiscussionResult:
		if e.Result == nil || strings.TrimSpace(e.Result.Summary) == "" {
			return fmt.Errorf("discussion result summary is required")
		}
	case GroupMessageApprovalRequest:
		if len(e.Payload) == 0 {
			return fmt.Errorf("approval request payload is required")
		}
	case GroupMessageApprovalResponse:
		if len(e.Payload) == 0 {
			return fmt.Errorf("approval response payload is required")
		}
	default:
		return fmt.Errorf("unknown group message type %q", e.Type)
	}
	return nil
}

func GroupDiscussionMessageHasPayload(msg GroupDiscussionMessage) bool {
	return strings.TrimSpace(msg.Content) != "" ||
		msg.Kind == MessageStreamEnd ||
		len(msg.TextAttachments) > 0 ||
		len(msg.ImageAttachments) > 0 ||
		len(msg.FileAttachments) > 0
}

func (p GroupProfile) DiscoveryView(modelVisibility string) GroupProfile {
	out := p
	out.AgentID = strings.TrimSpace(out.AgentID)
	out.DisplayName = strings.TrimSpace(out.DisplayName)
	out.Description = strings.TrimSpace(out.Description)
	out.ModelClass = strings.TrimSpace(out.ModelClass)
	out.SecurityGroupID = strings.TrimSpace(out.SecurityGroupID)
	if out.ContributionScore < 0 {
		out.ContributionScore = 0
	} else if out.ContributionScore > 1 {
		out.ContributionScore = 1
	}
	if out.ContributionEvidence < 0 {
		out.ContributionEvidence = 0
	}
	if modelVisibility == "hidden" {
		out.ModelClass = ""
	}
	return out
}

func ShouldAutoAcceptGroupInvitation(invitePolicy string, allowSameSecurityGroup bool, localSecurityGroupID string, inv GroupInvitation) bool {
	policy := strings.TrimSpace(invitePolicy)
	if policy == "" {
		policy = "ask_always"
	}
	if policy == "reject_all" {
		return false
	}
	localGroup := strings.TrimSpace(localSecurityGroupID)
	inviteGroup := strings.TrimSpace(inv.SecurityGroupID)
	if allowSameSecurityGroup && localGroup != "" && localGroup == inviteGroup {
		return true
	}
	switch policy {
	case "trusted_auto", "auto_trusted":
		return inv.Trusted
	case "observe_auto", "observe_only_auto":
		return inv.Role == GroupRoleObserve
	default:
		return false
	}
}
