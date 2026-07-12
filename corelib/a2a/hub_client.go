package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HubClient struct {
	baseURL    string
	token      string
	machineID  string
	httpClient *http.Client
}

type HubClientOption func(*HubClient)

func WithHubHTTPClient(client *http.Client) HubClientOption {
	return func(c *HubClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithHubBearerToken(token string) HubClientOption {
	return func(c *HubClient) {
		c.token = strings.TrimSpace(token)
	}
}

func WithHubMachineID(machineID string) HubClientOption {
	return func(c *HubClient) {
		c.machineID = strings.TrimSpace(machineID)
	}
}

func NewHubClient(baseURL string, opts ...HubClientOption) (*HubClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("hub URL is required")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid hub URL: %w", err)
	}
	c := &HubClient{baseURL: baseURL, httpClient: &http.Client{Timeout: 30 * time.Second}}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c, nil
}

type ExpertListResponse struct {
	Experts []GroupProfile `json:"experts"`
}

type DiscussionListResponse struct {
	Discussions []HubDiscussionSummary `json:"discussions"`
}

type InviteListResponse struct {
	Invites []GroupInviteSummary `json:"invites"`
}

type HubDiscussionSummary struct {
	ID                  string    `json:"id"`
	Role                string    `json:"role,omitempty"`
	LocalRelation       string    `json:"local_relation,omitempty"`
	Readonly            bool      `json:"readonly"`
	Status              string    `json:"status,omitempty"`
	Topic               string    `json:"topic,omitempty"`
	Question            string    `json:"question,omitempty"`
	ResultSummary       string    `json:"result_summary,omitempty"`
	ParticipantIDs      []string  `json:"participant_ids,omitempty"`
	MessageCount        int       `json:"message_count,omitempty"`
	AnswerCount         int       `json:"answer_count,omitempty"`
	ExpectedAnswerCount int       `json:"expected_answer_count,omitempty"`
	ReadyToSummarize    bool      `json:"ready_to_summarize,omitempty"`
	ReadinessReason     string    `json:"readiness_reason,omitempty"`
	CreatedAt           time.Time `json:"created_at,omitempty"`
	UpdatedAt           time.Time `json:"updated_at,omitempty"`
}

type HubDiscussionDetail struct {
	Discussion      HubDiscussionSummary     `json:"discussion"`
	Session         *Session                 `json:"session,omitempty"`
	Messages        []Message                `json:"messages,omitempty"`
	Proposals       []Proposal               `json:"proposals,omitempty"`
	Reviews         []Review                 `json:"reviews,omitempty"`
	ReviewSummaries map[string]ReviewSummary `json:"review_summaries,omitempty"`
	Decision        *Decision                `json:"decision,omitempty"`
}

type GroupInviteSummary struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id"`
	RequestID       string    `json:"request_id,omitempty"`
	FromID          string    `json:"from_id"`
	ToID            string    `json:"to_id"`
	Role            GroupRole `json:"role"`
	Trusted         bool      `json:"trusted,omitempty"`
	SecurityGroupID string    `json:"security_group_id,omitempty"`
	ContextPolicy   string    `json:"context_policy,omitempty"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason,omitempty"`
	Topic           string    `json:"topic,omitempty"`
	Question        string    `json:"question,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	RespondedAt     time.Time `json:"responded_at,omitempty"`
}

type ConsultationCreateResponse struct {
	Discussion HubDiscussionSummary     `json:"discussion"`
	Request    GroupConsultationRequest `json:"request,omitempty"`
}

func (c *HubClient) ListExperts(ctx context.Context) ([]GroupProfile, error) {
	var out ExpertListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/a2a/experts", nil, &out); err != nil {
		return nil, err
	}
	return out.Experts, nil
}

func (c *HubClient) PublishExpertProfile(ctx context.Context, profile GroupProfile) (GroupProfile, error) {
	profile = profile.DiscoveryView("")
	if strings.TrimSpace(profile.AgentID) == "" {
		return GroupProfile{}, fmt.Errorf("agent_id is required")
	}
	var out struct {
		Profile GroupProfile `json:"profile"`
	}
	if err := c.doJSON(ctx, http.MethodPut, "/api/a2a/expert-profile", profile, &out); err != nil {
		return GroupProfile{}, err
	}
	if strings.TrimSpace(out.Profile.AgentID) == "" {
		return profile, nil
	}
	return out.Profile, nil
}

func (c *HubClient) ListDiscussions(ctx context.Context, role string) ([]HubDiscussionSummary, error) {
	return c.listDiscussions(ctx, "", role)
}

func (c *HubClient) ListDiscussionsForAgent(ctx context.Context, agentID, role string) ([]HubDiscussionSummary, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	return c.listDiscussions(ctx, agentID, role)
}

func (c *HubClient) listDiscussions(ctx context.Context, agentID, role string) ([]HubDiscussionSummary, error) {
	values := url.Values{}
	if agentID = strings.TrimSpace(agentID); agentID != "" {
		values.Set("participant_id", agentID)
	}
	if role = strings.TrimSpace(role); role != "" && !strings.EqualFold(role, "all") {
		values.Set("role", role)
	}
	path := "/api/a2a/discussions/mine"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out DiscussionListResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Discussions, nil
}

func (c *HubClient) ListInvites(ctx context.Context, agentID string) ([]GroupInviteSummary, error) {
	return c.ListInvitesByStatus(ctx, agentID, "pending")
}

func (c *HubClient) ListInvitesByStatus(ctx context.Context, agentID, status string) ([]GroupInviteSummary, error) {
	return c.listInvitesByStatus(ctx, "to_id", agentID, status, "")
}

func (c *HubClient) ListSentInvitesByStatus(ctx context.Context, agentID, status string) ([]GroupInviteSummary, error) {
	return c.listInvitesByStatus(ctx, "from_id", agentID, status, "")
}

func (c *HubClient) GetSentInvite(ctx context.Context, agentID, inviteID string) (GroupInviteSummary, bool, error) {
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return GroupInviteSummary{}, false, fmt.Errorf("invite id is required")
	}
	invites, err := c.listInvitesByStatus(ctx, "from_id", agentID, "all", inviteID)
	if err != nil {
		return GroupInviteSummary{}, false, err
	}
	for _, invite := range invites {
		if strings.EqualFold(strings.TrimSpace(invite.ID), inviteID) {
			return invite, true, nil
		}
	}
	return GroupInviteSummary{}, false, nil
}

func (c *HubClient) listInvitesByStatus(ctx context.Context, idParam, agentID, status, inviteID string) ([]GroupInviteSummary, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	idParam = strings.TrimSpace(idParam)
	if idParam == "" {
		idParam = "to_id"
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "pending"
	}
	values := url.Values{}
	values.Set(idParam, agentID)
	values.Set("status", status)
	if inviteID = strings.TrimSpace(inviteID); inviteID != "" {
		values.Set("invite_id", inviteID)
	}
	path := "/api/a2a/invites/mine?" + values.Encode()
	var out InviteListResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Invites, nil
}

func (c *HubClient) CreateConsultation(ctx context.Context, req GroupConsultationRequest) (ConsultationCreateResponse, error) {
	if strings.TrimSpace(req.Question) == "" {
		return ConsultationCreateResponse{}, fmt.Errorf("question is required")
	}
	var out ConsultationCreateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations", req, &out); err != nil {
		return ConsultationCreateResponse{}, err
	}
	return out, nil
}

func (c *HubClient) GetConsultation(ctx context.Context, id string) (HubDiscussionSummary, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return HubDiscussionSummary{}, fmt.Errorf("consultation id is required")
	}
	var out struct {
		Discussion HubDiscussionSummary `json:"discussion"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/a2a/consultations/"+url.PathEscape(id), nil, &out); err != nil {
		return HubDiscussionSummary{}, err
	}
	return out.Discussion, nil
}

func (c *HubClient) GetConsultationDetail(ctx context.Context, id string) (HubDiscussionDetail, error) {
	return c.GetConsultationDetailForAgent(ctx, id, "")
}

func (c *HubClient) GetConsultationDetailForAgent(ctx context.Context, id, agentID string) (HubDiscussionDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return HubDiscussionDetail{}, fmt.Errorf("consultation id is required")
	}
	path := "/api/a2a/consultations/" + url.PathEscape(id) + "/detail"
	if agentID = strings.TrimSpace(agentID); agentID != "" {
		path += "?participant_id=" + url.QueryEscape(agentID)
	}
	var out HubDiscussionDetail
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return HubDiscussionDetail{}, err
	}
	if strings.TrimSpace(out.Discussion.ID) == "" && out.Session != nil {
		out.Discussion = HubDiscussionSummary{ID: out.Session.ID, Status: string(out.Session.Status), Topic: out.Session.Topic, Question: out.Session.Goal, ParticipantIDs: participantIDsFromSession(out.Session), MessageCount: len(out.Session.Messages), CreatedAt: out.Session.CreatedAt, UpdatedAt: out.Session.UpdatedAt}
	}
	return out, nil
}

func participantIDsFromSession(session *Session) []string {
	if session == nil {
		return nil
	}
	out := make([]string, 0, len(session.Participants))
	for _, participant := range session.Participants {
		if id := strings.TrimSpace(participant.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}
func (c *HubClient) SendInvitation(ctx context.Context, consultationID string, inv GroupInvitation) (string, error) {
	consultationID = strings.TrimSpace(consultationID)
	if consultationID == "" {
		return "", fmt.Errorf("consultation id is required")
	}
	if strings.TrimSpace(inv.ToID) == "" {
		return "", fmt.Errorf("invitation target is required")
	}
	var out struct {
		InviteID string `json:"invite_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/invites", inv, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.InviteID), nil
}

func (c *HubClient) AcceptInvite(ctx context.Context, inviteID string, resp GroupInvitationResponse) error {
	resp.Decision = GroupInvitationAccept
	return c.respondInvite(ctx, inviteID, "accept", resp)
}

func (c *HubClient) RejectInvite(ctx context.Context, inviteID string, resp GroupInvitationResponse) error {
	resp.Decision = GroupInvitationReject
	return c.respondInvite(ctx, inviteID, "reject", resp)
}

func (c *HubClient) SendDiscussionMessage(ctx context.Context, consultationID string, msg GroupDiscussionMessage) error {
	consultationID = strings.TrimSpace(consultationID)
	if consultationID == "" {
		return fmt.Errorf("consultation id is required")
	}
	if !GroupDiscussionMessageHasPayload(msg) {
		return fmt.Errorf("discussion message content or attachment payload is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/messages", msg, nil)
}

func (c *HubClient) SubmitDiscussionResult(ctx context.Context, consultationID string, result GroupDiscussionResult) error {
	consultationID = strings.TrimSpace(consultationID)
	if consultationID == "" {
		return fmt.Errorf("consultation id is required")
	}
	if strings.TrimSpace(result.Summary) == "" {
		return fmt.Errorf("discussion result summary is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/result", result, nil)
}

func (c *HubClient) AddDiscussionProposal(ctx context.Context, consultationID string, proposal Proposal) error {
	consultationID = strings.TrimSpace(consultationID)
	proposal.AuthorID = strings.TrimSpace(proposal.AuthorID)
	proposal.Title = strings.TrimSpace(proposal.Title)
	proposal.Content = strings.TrimSpace(proposal.Content)
	if consultationID == "" {
		return fmt.Errorf("consultation id is required")
	}
	if proposal.AuthorID == "" || proposal.Title == "" || proposal.Content == "" {
		return fmt.Errorf("proposal author_id, title and content are required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/proposals", proposal, nil)
}

func (c *HubClient) AddDiscussionReview(ctx context.Context, consultationID string, review Review) error {
	consultationID = strings.TrimSpace(consultationID)
	review.ProposalID = strings.TrimSpace(review.ProposalID)
	review.ReviewerID = strings.TrimSpace(review.ReviewerID)
	if consultationID == "" {
		return fmt.Errorf("consultation id is required")
	}
	if review.ProposalID == "" || review.ReviewerID == "" {
		return fmt.Errorf("review proposal_id and reviewer_id are required")
	}
	if review.Position == "" {
		review.Position = ReviewAbstain
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/reviews", review, nil)
}

func (c *HubClient) DecideDiscussion(ctx context.Context, consultationID string, decision Decision) error {
	consultationID = strings.TrimSpace(consultationID)
	decision.ProposalID = strings.TrimSpace(decision.ProposalID)
	decision.Summary = strings.TrimSpace(decision.Summary)
	if consultationID == "" {
		return fmt.Errorf("consultation id is required")
	}
	if decision.ProposalID == "" {
		return fmt.Errorf("decision proposal_id is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/decide", decision, nil)
}

func (c *HubClient) EscalateDiscussion(ctx context.Context, consultationID string, escalation Escalation) error {
	consultationID = strings.TrimSpace(consultationID)
	escalation.RaisedBy = strings.TrimSpace(escalation.RaisedBy)
	escalation.Reason = strings.TrimSpace(escalation.Reason)
	escalation.Target = strings.TrimSpace(escalation.Target)
	if consultationID == "" {
		return fmt.Errorf("consultation id is required")
	}
	if escalation.RaisedBy == "" || escalation.Reason == "" {
		return fmt.Errorf("escalation raised_by and reason are required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/escalate", escalation, nil)
}

func (c *HubClient) RenameConsultationTopic(ctx context.Context, consultationID, fromID, topic string) (HubDiscussionSummary, error) {
	consultationID = strings.TrimSpace(consultationID)
	fromID = strings.TrimSpace(fromID)
	topic = strings.TrimSpace(topic)
	if consultationID == "" {
		return HubDiscussionSummary{}, fmt.Errorf("consultation id is required")
	}
	if fromID == "" {
		return HubDiscussionSummary{}, fmt.Errorf("from_id is required")
	}
	if topic == "" {
		return HubDiscussionSummary{}, fmt.Errorf("topic is required")
	}
	var out struct {
		Discussion HubDiscussionSummary `json:"discussion"`
	}
	payload := map[string]string{"from_id": fromID, "topic": topic}
	if err := c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/rename", payload, &out); err != nil {
		return HubDiscussionSummary{}, err
	}
	return out.Discussion, nil
}

func (c *HubClient) SetConsultationState(ctx context.Context, consultationID, action string) error {
	consultationID = strings.TrimSpace(consultationID)
	action = strings.TrimSpace(action)
	if consultationID == "" || action == "" {
		return fmt.Errorf("consultation id and action are required")
	}
	switch action {
	case "pause", "resume", "cancel":
		return c.doJSON(ctx, http.MethodPost, "/api/a2a/consultations/"+url.PathEscape(consultationID)+"/"+action, nil, nil)
	default:
		return fmt.Errorf("unsupported consultation action %q", action)
	}
}

func (c *HubClient) respondInvite(ctx context.Context, inviteID, action string, resp GroupInvitationResponse) error {
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return fmt.Errorf("invite id is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/a2a/invites/"+url.PathEscape(inviteID)+"/"+action, resp, nil)
}

func (c *HubClient) doJSON(ctx context.Context, method, path string, in any, out any) error {
	if c == nil {
		return fmt.Errorf("hub client is nil")
	}
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.machineID != "" {
		req.Header.Set("X-Machine-ID", c.machineID)
	}
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeHubError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode hub response: %w", err)
	}
	return nil
}

func decodeHubError(resp *http.Response) error {
	const maxHubErrorBodyBytes = 8 << 10
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHubErrorBodyBytes))
	if readErr != nil {
		return fmt.Errorf("read hub error response: %w", readErr)
	}
	// Hub error responses use two formats:
	// 1. {"message":"...", "error":"string"}  (legacy)
	// 2. {"message":"...", "error":{"message":"...", "code":"..."}}  (current)
	// Use json.RawMessage to handle both without type mismatch failures. Some
	// reverse proxies and older Hub builds return plain text, which is still
	// useful to callers deciding whether a stale session can be recovered.
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return fmt.Errorf("hub returned %d: %s", resp.StatusCode, msg)
		}
		if len(payload.Error) > 0 {
			// Try as string first.
			var errStr string
			if json.Unmarshal(payload.Error, &errStr) == nil && strings.TrimSpace(errStr) != "" {
				return fmt.Errorf("hub returned %d: %s", resp.StatusCode, errStr)
			}
			// Try as object with "message" field.
			var errObj struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(payload.Error, &errObj) == nil && strings.TrimSpace(errObj.Message) != "" {
				return fmt.Errorf("hub returned %d: %s", resp.StatusCode, errObj.Message)
			}
		}
	}
	if message := strings.TrimSpace(string(body)); message != "" {
		return fmt.Errorf("hub returned %d: %s", resp.StatusCode, message)
	}
	return fmt.Errorf("hub returned %s", resp.Status)
}
