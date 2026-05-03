package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

const groupDiscussionHubTimeout = 30 * time.Second

func (a *App) groupDiscussionClient() (*a2a.HubClient, corelib.AppConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, corelib.AppConfig{}, err
	}
	if !cfg.GroupDiscussion.Enabled {
		return nil, cfg, fmt.Errorf("group discussion is disabled")
	}
	client, err := a2a.NewHubClientFromConfig(cfg)
	if err != nil {
		return nil, cfg, err
	}
	return client, cfg, nil
}

func groupDiscussionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), groupDiscussionHubTimeout)
}

func (a *App) GroupDiscussionPublishProfile() (a2a.GroupProfile, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return a2a.GroupProfile{}, err
	}
	if !cfg.GroupDiscussion.Discoverable {
		return a2a.GroupProfile{}, fmt.Errorf("group discussion discovery is disabled")
	}
	profile, err := a2a.BuildGroupProfileFromConfig(cfg, time.Now())
	if err != nil {
		return a2a.GroupProfile{}, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.PublishExpertProfile(ctx, profile)
}

func (a *App) GroupDiscussionListExperts() ([]a2a.GroupProfile, error) {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.ListExperts(ctx)
}

func (a *App) GroupDiscussionListMine(role string) ([]a2a.HubDiscussionSummary, error) {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.ListDiscussions(ctx, role)
}

func (a *App) GroupDiscussionListInvites() ([]a2a.GroupInviteSummary, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(cfg.RemoteMachineID)
	if agentID == "" {
		return nil, fmt.Errorf("remote machine id is required")
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.ListInvites(ctx, agentID)
}

func (a *App) GroupDiscussionProcessPendingInvites() ([]a2a.GroupInviteSummary, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(cfg.RemoteMachineID)
	if agentID == "" {
		return nil, fmt.Errorf("remote machine id is required")
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	invites, err := client.ListInvites(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, invite := range invites {
		if strings.TrimSpace(invite.ID) == "" {
			continue
		}
		if !groupDiscussionRoleAllowed(cfg, invite.Role) {
			_ = client.RejectInvite(ctx, invite.ID, a2a.GroupInvitationResponse{FromID: agentID, Reason: "local policy: role is not allowed"})
			continue
		}
		if cfg.GroupDiscussion.RejectWhenDND && strings.EqualFold(strings.TrimSpace(cfg.GroupDiscussion.Availability), "dnd") {
			_ = client.RejectInvite(ctx, invite.ID, a2a.GroupInvitationResponse{FromID: agentID, Reason: "local policy: do not disturb"})
			continue
		}
		shouldAccept := a2a.ShouldAutoAcceptGroupInvitation(
			cfg.GroupDiscussion.InvitePolicy,
			cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion,
			cfg.GroupDiscussion.SecurityGroupID,
			a2a.GroupInvitation{ToID: invite.ToID, Role: invite.Role, Trusted: invite.Trusted, SecurityGroupID: invite.SecurityGroupID, ContextPolicy: invite.ContextPolicy},
		)
		if shouldAccept {
			if err := client.AcceptInvite(ctx, invite.ID, a2a.GroupInvitationResponse{FromID: agentID, Reason: "local policy: auto accepted"}); err == nil {
				go a.groupDiscussionContributeToInvite(invite)
			}
		}
	}
	return client.ListInvites(ctx, agentID)
}

type GroupDiscussionAuthorizedStartRequest struct {
	Request    a2a.GroupConsultationRequest `json:"request"`
	InviteeIDs []string                     `json:"invitee_ids,omitempty"`
	Role       a2a.GroupRole                `json:"role,omitempty"`
	Trusted    bool                         `json:"trusted,omitempty"`
}

type GroupDiscussionAuthorizedStartResult struct {
	Consultation a2a.ConsultationCreateResponse `json:"consultation"`
	InviteIDs    []string                       `json:"invite_ids,omitempty"`
	Experts      []a2a.GroupProfile             `json:"experts,omitempty"` // invited experts only
}

func (a *App) GroupDiscussionCreateConsultation(req a2a.GroupConsultationRequest) (a2a.ConsultationCreateResponse, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return a2a.ConsultationCreateResponse{}, err
	}
	if cfg.GroupDiscussion.ConfirmBeforeStart {
		return a2a.ConsultationCreateResponse{}, fmt.Errorf("group discussion requires user confirmation before start")
	}
	return createGroupDiscussionConsultation(client, cfg, req)
}

func (a *App) GroupDiscussionStartAuthorizedConsultation(start GroupDiscussionAuthorizedStartRequest) (GroupDiscussionAuthorizedStartResult, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return GroupDiscussionAuthorizedStartResult{}, err
	}
	consultation, err := createGroupDiscussionConsultation(client, cfg, start.Request)
	if err != nil {
		return GroupDiscussionAuthorizedStartResult{}, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	experts, _ := client.ListExperts(ctx)
	inviteeIDs := normalizeGroupDiscussionInvitees(start.InviteeIDs)
	if len(inviteeIDs) == 0 {
		inviteeIDs = selectGroupDiscussionInvitees(experts, cfg, start.Request)
	}
	role := start.Role
	if role == "" {
		role = a2a.GroupRoleSpeak
	}
	expertByID := make(map[string]a2a.GroupProfile, len(experts))
	for _, expert := range experts {
		expertByID[strings.TrimSpace(expert.AgentID)] = expert
	}
	inviteIDs := make([]string, 0, len(inviteeIDs))
	invitedExperts := make([]a2a.GroupProfile, 0, len(inviteeIDs))
	for _, toID := range inviteeIDs {
		if toID == "" || toID == cfg.RemoteMachineID {
			continue
		}
		inviteID, err := client.SendInvitation(ctx, consultation.Discussion.ID, a2a.GroupInvitation{
			RequestID:       consultation.Request.ID,
			FromID:          cfg.RemoteMachineID,
			ToID:            toID,
			Role:            role,
			Trusted:         start.Trusted,
			SecurityGroupID: cfg.GroupDiscussion.SecurityGroupID,
			ContextPolicy:   cfg.GroupDiscussion.ContextPolicy,
		})
		if err != nil {
			return GroupDiscussionAuthorizedStartResult{}, err
		}
		if inviteID != "" {
			inviteIDs = append(inviteIDs, inviteID)
			if expert, ok := expertByID[toID]; ok {
				invitedExperts = append(invitedExperts, expert)
			}
		}
	}
	return GroupDiscussionAuthorizedStartResult{Consultation: consultation, InviteIDs: inviteIDs, Experts: invitedExperts}, nil
}

func createGroupDiscussionConsultation(client *a2a.HubClient, cfg corelib.AppConfig, req a2a.GroupConsultationRequest) (a2a.ConsultationCreateResponse, error) {
	if req.FromID == "" {
		req.FromID = cfg.RemoteMachineID
	}
	if req.MaxRounds <= 0 {
		req.MaxRounds = cfg.GroupDiscussion.MaxRounds
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = cfg.GroupDiscussion.TimeoutSeconds
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.CreateConsultation(ctx, req)
}

func normalizeGroupDiscussionInvitees(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func selectGroupDiscussionInvitees(experts []a2a.GroupProfile, cfg corelib.AppConfig, req a2a.GroupConsultationRequest) []string {
	localID := strings.TrimSpace(cfg.RemoteMachineID)
	localSecurityGroup := strings.TrimSpace(cfg.GroupDiscussion.SecurityGroupID)
	wanted := lowerStringSet(req.SkillsWanted)
	type candidate struct {
		id    string
		score int
	}
	candidates := make([]candidate, 0, len(experts))
	for _, expert := range experts {
		id := strings.TrimSpace(expert.AgentID)
		if id == "" || id == localID || !expert.Discoverable || !expert.Available {
			continue
		}
		expertGroup := strings.TrimSpace(expert.SecurityGroupID)
		if cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion && localSecurityGroup != "" && expertGroup != localSecurityGroup {
			continue
		}
		score := 0
		if expertGroup != "" && expertGroup == localSecurityGroup {
			score += 4
		}
		for _, skill := range expert.Skills {
			if _, ok := wanted[strings.ToLower(strings.TrimSpace(skill))]; ok {
				score += 3
			}
		}
		if strings.TrimSpace(expert.Description) != "" {
			score++
		}
		candidates = append(candidates, candidate{id: id, score: score})
	}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	limit := 3
	out := make([]string, 0, limit)
	for _, item := range candidates {
		out = append(out, item.id)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func lowerStringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}
func (a *App) GroupDiscussionGetConsultation(consultationID string) (a2a.HubDiscussionSummary, error) {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return a2a.HubDiscussionSummary{}, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.GetConsultation(ctx, consultationID)
}

func (a *App) GroupDiscussionGetConsultationDetail(consultationID string) (a2a.HubDiscussionDetail, error) {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return a2a.HubDiscussionDetail{}, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.GetConsultationDetail(ctx, consultationID)
}

func (a *App) GroupDiscussionGetReadiness(consultationID string) (GroupDiscussionReadiness, error) {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionReadiness{}, err
	}
	readiness := groupDiscussionReadiness(detail)
	readiness.ConsultationID = strings.TrimSpace(consultationID)
	return readiness, nil
}
func (a *App) GroupDiscussionSendInvitation(consultationID string, inv a2a.GroupInvitation) (string, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return "", err
	}
	if inv.FromID == "" {
		inv.FromID = cfg.RemoteMachineID
	}
	if inv.ContextPolicy == "" {
		inv.ContextPolicy = cfg.GroupDiscussion.ContextPolicy
	}
	if inv.SecurityGroupID == "" {
		inv.SecurityGroupID = cfg.GroupDiscussion.SecurityGroupID
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.SendInvitation(ctx, consultationID, inv)
}

func (a *App) GroupDiscussionAcceptInvite(inviteID string, resp a2a.GroupInvitationResponse) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	if resp.FromID == "" {
		resp.FromID = cfg.RemoteMachineID
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	var acceptedInvite *a2a.GroupInviteSummary
	if invites, err := client.ListInvites(ctx, strings.TrimSpace(cfg.RemoteMachineID)); err == nil {
		for i := range invites {
			if strings.TrimSpace(invites[i].ID) == strings.TrimSpace(inviteID) {
				item := invites[i]
				acceptedInvite = &item
				break
			}
		}
	}
	if acceptedInvite != nil && !groupDiscussionRoleAllowed(cfg, acceptedInvite.Role) {
		return fmt.Errorf("group discussion invite role %q is not allowed by local policy", acceptedInvite.Role)
	}
	if err := client.AcceptInvite(ctx, inviteID, resp); err != nil {
		return err
	}
	if acceptedInvite != nil {
		go a.groupDiscussionContributeToInvite(*acceptedInvite)
	}
	return nil
}

func (a *App) groupDiscussionContributeToInvite(invite a2a.GroupInviteSummary) {
	if strings.TrimSpace(invite.SessionID) == "" {
		return
	}
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.GroupDiscussion.Enabled {
		return
	}
	if !groupDiscussionShouldAutoContribute(cfg, invite) {
		return
	}
	answer, err := a.groupDiscussionGenerateContribution(cfg, invite)
	if err != nil || strings.TrimSpace(answer) == "" {
		return
	}
	_ = a.GroupDiscussionSendMessage(invite.SessionID, a2a.GroupDiscussionMessage{
		FromID:    firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID),
		Kind:      a2a.MessageAnswer,
		Content:   answer,
		CreatedAt: time.Now(),
	})
}

func groupDiscussionShouldAutoContribute(cfg corelib.AppConfig, invite a2a.GroupInviteSummary) bool {
	if !groupDiscussionRoleAllowed(cfg, invite.Role) {
		return false
	}
	if cfg.GroupDiscussion.RejectWhenDND && strings.EqualFold(strings.TrimSpace(cfg.GroupDiscussion.Availability), "dnd") {
		return false
	}
	if invite.Role == a2a.GroupRoleObserve {
		return false
	}
	return true
}

func groupDiscussionRoleAllowed(cfg corelib.AppConfig, role a2a.GroupRole) bool {
	roleText := strings.TrimSpace(string(role))
	if roleText == "" {
		roleText = string(a2a.GroupRoleSpeak)
	}
	allowed := cfg.GroupDiscussion.AllowedRoles
	if len(allowed) == 0 {
		allowed = []string{string(a2a.GroupRoleObserve), string(a2a.GroupRoleSpeak), string(a2a.GroupRoleReview)}
	}
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), roleText) {
			return true
		}
	}
	return false
}

func (a *App) groupDiscussionGenerateContribution(cfg corelib.AppConfig, invite a2a.GroupInviteSummary) (string, error) {
	llmCfg := a.GetMaclawLLMConfig()
	if strings.TrimSpace(llmCfg.URL) == "" || strings.TrimSpace(llmCfg.Model) == "" {
		return "", fmt.Errorf("MaClaw LLM is not configured")
	}
	profile, _ := a2a.BuildGroupProfileFromConfig(cfg, time.Now())
	messages := []interface{}{
		map[string]string{"role": "system", "content": groupDiscussionContributionPrompt},
		map[string]string{"role": "user", "content": buildGroupDiscussionContributionInput(profile, cfg, invite)},
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := doSimpleLLMRequest(context.Background(), llmCfg, messages, client, 45*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

func buildGroupDiscussionContributionInput(profile a2a.GroupProfile, cfg corelib.AppConfig, invite a2a.GroupInviteSummary) string {
	var b strings.Builder
	b.WriteString("Expert name: ")
	b.WriteString(firstNonEmptyGroupString(profile.DisplayName, cfg.MaclawRoleName, cfg.RemoteNickname, "MaClaw"))
	b.WriteString("\nSkills: ")
	b.WriteString(strings.Join(profile.Skills, ", "))
	b.WriteString("\nDescription: ")
	b.WriteString(profile.Description)
	b.WriteString("\nDiscussion topic: ")
	b.WriteString(invite.Topic)
	b.WriteString("\nQuestion: ")
	b.WriteString(invite.Question)
	b.WriteString("\nRole: ")
	b.WriteString(string(invite.Role))
	b.WriteString("\nContext policy: ")
	b.WriteString(firstNonEmptyGroupString(invite.ContextPolicy, cfg.GroupDiscussion.ContextPolicy, "summary_only"))
	return b.String()
}

func firstNonEmptyGroupString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const groupDiscussionContributionPrompt = `You are a MaClaw expert invited into a current-Hub group discussion.
Respond as an expert peer, not as the user's main assistant.
Use only the provided topic/question/profile summary. Do not invent private context, credentials, or file contents.
Keep the answer concise and actionable.
Format:
1. Your key judgment.
2. Reasoning or evidence.
3. Risks/caveats.
4. Suggested next step.`

type GroupDiscussionSummarizeRequest struct {
	ConsultationID string `json:"consultation_id"`
	Submit         bool   `json:"submit,omitempty"`
	Inject         bool   `json:"inject,omitempty"`
	Force          bool   `json:"force,omitempty"`
}

type GroupDiscussionReadiness struct {
	ConsultationID      string `json:"consultation_id"`
	Status              string `json:"status,omitempty"`
	ParticipantCount    int    `json:"participant_count"`
	ExpectedAnswerCount int    `json:"expected_answer_count"`
	AnswerCount         int    `json:"answer_count"`
	HasResult           bool   `json:"has_result"`
	Ready               bool   `json:"ready"`
	Reason              string `json:"reason,omitempty"`
}
type GroupDiscussionSummarizeResult struct {
	ConsultationID string   `json:"consultation_id"`
	Summary        string   `json:"summary"`
	Rationale      string   `json:"rationale,omitempty"`
	Risks          []string `json:"risks,omitempty"`
	AnswerCount    int      `json:"answer_count"`
	UsedLLM        bool     `json:"used_llm"`
	Submitted      bool     `json:"submitted"`
	Injected       bool     `json:"injected"`
}

func (a *App) GroupDiscussionSummarizeResult(req GroupDiscussionSummarizeRequest) (GroupDiscussionSummarizeResult, error) {
	consultationID := strings.TrimSpace(req.ConsultationID)
	if consultationID == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("consultation id is required")
	}
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionSummarizeResult{}, err
	}
	readiness := groupDiscussionReadiness(detail)
	if !req.Force && !readiness.Ready {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("discussion is not ready to summarize: %s", readiness.Reason)
	}
	result := summarizeGroupDiscussionDetail(detail)
	result.ConsultationID = consultationID
	if llmResult, err := a.groupDiscussionGenerateResultSummary(detail); err == nil && strings.TrimSpace(llmResult.Summary) != "" {
		llmResult.ConsultationID = consultationID
		llmResult.AnswerCount = result.AnswerCount
		llmResult.UsedLLM = true
		result = llmResult
	}
	if strings.TrimSpace(result.Summary) == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("discussion has no expert answers to summarize")
	}
	if req.Submit && strings.TrimSpace(detail.Discussion.ResultSummary) == "" && detail.Decision == nil {
		if err := a.GroupDiscussionSubmitResult(consultationID, a2a.GroupDiscussionResult{Summary: result.Summary, Rationale: result.Rationale, Risks: result.Risks, CreatedAt: time.Now()}); err != nil {
			return result, err
		}
		result.Submitted = true
	}
	if req.Inject {
		injected, err := a.InjectAIAssistantSupplementary(formatGroupDiscussionSupplement(result))
		if err != nil {
			return result, err
		}
		result.Injected = injected
	}
	return result, nil
}

func (a *App) groupDiscussionGenerateResultSummary(detail a2a.HubDiscussionDetail) (GroupDiscussionSummarizeResult, error) {
	llmCfg := a.GetMaclawLLMConfig()
	if strings.TrimSpace(llmCfg.URL) == "" || strings.TrimSpace(llmCfg.Model) == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("MaClaw LLM is not configured")
	}
	input := buildGroupDiscussionResultSummaryInput(detail)
	if strings.TrimSpace(input) == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("discussion has no content")
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": groupDiscussionResultSummaryPrompt},
		map[string]string{"role": "user", "content": input},
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := doSimpleLLMRequest(context.Background(), llmCfg, messages, client, 45*time.Second)
	if err != nil {
		return GroupDiscussionSummarizeResult{}, err
	}
	return decodeGroupDiscussionResultSummary(resp.Content)
}

func groupDiscussionReadiness(detail a2a.HubDiscussionDetail) GroupDiscussionReadiness {
	answerCount := countGroupDiscussionAnswers(detail.Messages)
	participantCount := len(detail.Discussion.ParticipantIDs)
	if participantCount == 0 && detail.Session != nil {
		participantCount = len(detail.Session.Participants)
	}
	expected := participantCount - 1
	if expected < 1 {
		expected = 1
	}
	status := strings.TrimSpace(detail.Discussion.Status)
	if status == "" && detail.Session != nil {
		status = string(detail.Session.Status)
	}
	hasResult := strings.TrimSpace(detail.Discussion.ResultSummary) != "" || detail.Decision != nil
	ready := hasResult || answerCount >= expected || (status != "" && status != string(a2a.SessionOpen) && answerCount > 0)
	reason := "waiting for expert answers"
	if hasResult {
		reason = "result already exists"
	} else if answerCount >= expected {
		reason = "expected expert answers received"
	} else if status != "" && status != string(a2a.SessionOpen) && answerCount > 0 {
		reason = "discussion is no longer open"
	} else if answerCount > 0 {
		reason = fmt.Sprintf("waiting for more expert answers (%d/%d)", answerCount, expected)
	}
	return GroupDiscussionReadiness{Status: status, ParticipantCount: participantCount, ExpectedAnswerCount: expected, AnswerCount: answerCount, HasResult: hasResult, Ready: ready, Reason: reason}
}
func summarizeGroupDiscussionDetail(detail a2a.HubDiscussionDetail) GroupDiscussionSummarizeResult {
	if detail.Decision != nil && strings.TrimSpace(detail.Decision.Summary) != "" {
		return GroupDiscussionSummarizeResult{Summary: strings.TrimSpace(detail.Decision.Summary), Rationale: strings.TrimSpace(detail.Decision.Rationale), AnswerCount: countGroupDiscussionAnswers(detail.Messages)}
	}
	answers := groupDiscussionAnswerMessages(detail.Messages)
	if len(answers) == 0 {
		return GroupDiscussionSummarizeResult{}
	}
	var b strings.Builder
	for i, msg := range answers {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(msg.FromID))
		if strings.TrimSpace(msg.FromID) == "" {
			b.WriteString("expert")
		}
		b.WriteString(": ")
		b.WriteString(truncateGroupDiscussionText(msg.Content, 700))
	}
	summary := strings.TrimSpace(detail.Discussion.ResultSummary)
	if summary == "" {
		summary = "Group discussion produced " + fmt.Sprint(len(answers)) + " expert answer(s)."
	}
	return GroupDiscussionSummarizeResult{Summary: summary, Rationale: b.String(), AnswerCount: len(answers)}
}

func groupDiscussionAnswerMessages(messages []a2a.Message) []a2a.Message {
	out := make([]a2a.Message, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		if msg.Kind == a2a.MessageAnswer || msg.Kind == a2a.MessageStatement || msg.Kind == a2a.MessageEvidence || msg.Kind == a2a.MessageObjection {
			out = append(out, msg)
		}
	}
	return out
}

func countGroupDiscussionAnswers(messages []a2a.Message) int {
	return len(groupDiscussionAnswerMessages(messages))
}

func buildGroupDiscussionResultSummaryInput(detail a2a.HubDiscussionDetail) string {
	answers := groupDiscussionAnswerMessages(detail.Messages)
	if len(answers) == 0 && detail.Decision == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Topic: ")
	b.WriteString(detail.Discussion.Topic)
	b.WriteString("\nQuestion: ")
	b.WriteString(detail.Discussion.Question)
	if detail.Decision != nil {
		b.WriteString("\nExisting decision: ")
		b.WriteString(detail.Decision.Summary)
		b.WriteString("\nExisting rationale: ")
		b.WriteString(detail.Decision.Rationale)
	}
	b.WriteString("\nExpert messages:")
	for _, msg := range answers {
		b.WriteString("\n- From ")
		b.WriteString(firstNonEmptyGroupString(msg.FromID, "expert"))
		b.WriteString(" [")
		b.WriteString(string(msg.Kind))
		b.WriteString("]: ")
		b.WriteString(truncateGroupDiscussionText(msg.Content, 1200))
	}
	return b.String()
}

func decodeGroupDiscussionResultSummary(content string) (GroupDiscussionSummarizeResult, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}
	var parsed GroupDiscussionSummarizeResult
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return GroupDiscussionSummarizeResult{}, err
	}
	parsed.Summary = strings.TrimSpace(parsed.Summary)
	parsed.Rationale = strings.TrimSpace(parsed.Rationale)
	cleanRisks := make([]string, 0, len(parsed.Risks))
	for _, risk := range parsed.Risks {
		if risk = strings.TrimSpace(risk); risk != "" {
			cleanRisks = append(cleanRisks, risk)
		}
	}
	parsed.Risks = cleanRisks
	if parsed.Summary == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("empty summary")
	}
	return parsed, nil
}

func truncateGroupDiscussionText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len([]rune(value)) <= max {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:max])) + "..."
}

func formatGroupDiscussionSupplement(result GroupDiscussionSummarizeResult) string {
	var b strings.Builder
	b.WriteString("MaClaw group discussion result")
	if result.ConsultationID != "" {
		b.WriteString(" (")
		b.WriteString(result.ConsultationID)
		b.WriteString(")")
	}
	b.WriteString(":\nSummary: ")
	b.WriteString(result.Summary)
	if result.Rationale != "" {
		b.WriteString("\nRationale:\n")
		b.WriteString(result.Rationale)
	}
	if len(result.Risks) > 0 {
		b.WriteString("\nRisks:\n")
		for _, risk := range result.Risks {
			b.WriteString("- ")
			b.WriteString(risk)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

const groupDiscussionResultSummaryPrompt = `You synthesize a current-Hub MaClaw group discussion for the initiating MaClaw.
Return only JSON with this shape: {"summary":"...","rationale":"...","risks":["..."]}.
Use only the provided topic, question, existing decision, and expert messages.
Do not invent private context, files, secrets, or external facts.
Keep the summary actionable and preserve important disagreements or caveats in rationale/risks.`

func (a *App) GroupDiscussionRejectInvite(inviteID string, resp a2a.GroupInvitationResponse) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	if resp.FromID == "" {
		resp.FromID = cfg.RemoteMachineID
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.RejectInvite(ctx, inviteID, resp)
}

func (a *App) GroupDiscussionSendMessage(consultationID string, msg a2a.GroupDiscussionMessage) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	if msg.FromID == "" {
		msg.FromID = cfg.RemoteMachineID
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.SendDiscussionMessage(ctx, consultationID, msg)
}

func (a *App) GroupDiscussionSubmitResult(consultationID string, result a2a.GroupDiscussionResult) error {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.SubmitDiscussionResult(ctx, consultationID, result)
}

type GroupDiscussionStaleCleanupRequest struct {
	DryRun bool `json:"dry_run,omitempty"`
}

type GroupDiscussionStaleCleanupResult struct {
	TimeoutSeconds int                        `json:"timeout_seconds"`
	DryRun         bool                       `json:"dry_run"`
	Stale          []a2a.HubDiscussionSummary `json:"stale,omitempty"`
	CancelledIDs   []string                   `json:"cancelled_ids,omitempty"`
	Errors         map[string]string          `json:"errors,omitempty"`
}

func (a *App) GroupDiscussionCleanupStale(req GroupDiscussionStaleCleanupRequest) (GroupDiscussionStaleCleanupResult, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return GroupDiscussionStaleCleanupResult{}, err
	}
	timeoutSeconds := cfg.GroupDiscussion.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	discussions, err := client.ListDiscussions(ctx, "")
	if err != nil {
		return GroupDiscussionStaleCleanupResult{}, err
	}
	stale := staleGroupDiscussions(discussions, timeoutSeconds, time.Now())
	result := GroupDiscussionStaleCleanupResult{TimeoutSeconds: timeoutSeconds, DryRun: req.DryRun, Stale: stale}
	if req.DryRun {
		return result, nil
	}
	for _, discussion := range stale {
		id := strings.TrimSpace(discussion.ID)
		if id == "" {
			continue
		}
		if err := client.SetConsultationState(ctx, id, "cancel"); err != nil {
			if result.Errors == nil {
				result.Errors = map[string]string{}
			}
			result.Errors[id] = err.Error()
			continue
		}
		result.CancelledIDs = append(result.CancelledIDs, id)
	}
	return result, nil
}

func staleGroupDiscussions(discussions []a2a.HubDiscussionSummary, timeoutSeconds int, now time.Time) []a2a.HubDiscussionSummary {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	if now.IsZero() {
		now = time.Now()
	}
	deadline := time.Duration(timeoutSeconds) * time.Second
	out := make([]a2a.HubDiscussionSummary, 0)
	for _, discussion := range discussions {
		if !strings.EqualFold(strings.TrimSpace(discussion.Status), string(a2a.SessionOpen)) {
			continue
		}
		base := discussion.CreatedAt
		if base.IsZero() || (!discussion.UpdatedAt.IsZero() && discussion.UpdatedAt.Before(base)) {
			base = discussion.UpdatedAt
		}
		if base.IsZero() {
			continue
		}
		if now.Sub(base) > deadline {
			out = append(out, discussion)
		}
	}
	return out
}
func (a *App) GroupDiscussionSetState(consultationID, action string) error {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.SetConsultationState(ctx, consultationID, action)
}

type GroupDiscussionStatus struct {
	Enabled                          bool                       `json:"enabled"`
	Discoverable                     bool                       `json:"discoverable"`
	ConfirmBeforeStart               bool                       `json:"confirm_before_start"`
	AllowSecurityGroupFreeDiscussion bool                       `json:"allow_security_group_free_discussion"`
	InvitePolicy                     string                     `json:"invite_policy,omitempty"`
	SecurityGroupID                  string                     `json:"security_group_id,omitempty"`
	ContextPolicy                    string                     `json:"context_policy,omitempty"`
	Profile                          *a2a.GroupProfile          `json:"profile,omitempty"`
	Experts                          []a2a.GroupProfile         `json:"experts,omitempty"`
	Discussions                      []a2a.HubDiscussionSummary `json:"discussions,omitempty"`
	ActiveDiscussionCount            int                        `json:"active_discussion_count"`
	ReadyDiscussionCount             int                        `json:"ready_discussion_count"`
	WaitingDiscussionCount           int                        `json:"waiting_discussion_count"`
	StaleDiscussionCount             int                        `json:"stale_discussion_count"`
	PendingInvites                   []a2a.GroupInviteSummary   `json:"pending_invites,omitempty"`
	Error                            string                     `json:"error,omitempty"`
}

func (a *App) GroupDiscussionStatus() GroupDiscussionStatus {
	cfg, err := a.LoadConfig()
	if err != nil {
		return GroupDiscussionStatus{Error: err.Error()}
	}
	status := GroupDiscussionStatus{Enabled: cfg.GroupDiscussion.Enabled, Discoverable: cfg.GroupDiscussion.Discoverable, ConfirmBeforeStart: cfg.GroupDiscussion.ConfirmBeforeStart, AllowSecurityGroupFreeDiscussion: cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion, InvitePolicy: cfg.GroupDiscussion.InvitePolicy, SecurityGroupID: cfg.GroupDiscussion.SecurityGroupID, ContextPolicy: cfg.GroupDiscussion.ContextPolicy}
	profile, profileErr := a2a.BuildGroupProfileFromConfig(cfg, time.Now())
	if profileErr == nil {
		status.Profile = &profile
	}
	if !cfg.GroupDiscussion.Enabled {
		return status
	}
	client, err := a2a.NewHubClientFromConfig(cfg)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if experts, err := client.ListExperts(ctx); err == nil {
		status.Experts = experts
	} else if status.Error == "" {
		status.Error = err.Error()
	}
	if discussions, err := client.ListDiscussions(ctx, ""); err == nil {
		status.Discussions = discussions
		for _, discussion := range discussions {
			isOpen := strings.EqualFold(strings.TrimSpace(discussion.Status), string(a2a.SessionOpen))
			if isOpen {
				status.ActiveDiscussionCount++
			}
			if discussion.ReadyToSummarize {
				status.ReadyDiscussionCount++
			} else if isOpen {
				status.WaitingDiscussionCount++
			}
		}
		status.StaleDiscussionCount = len(staleGroupDiscussions(discussions, cfg.GroupDiscussion.TimeoutSeconds, time.Now()))
	} else if status.Error == "" {
		status.Error = err.Error()
	}
	if invites, err := client.ListInvites(ctx, cfg.RemoteMachineID); err == nil {
		status.PendingInvites = invites
	} else if status.Error == "" {
		status.Error = err.Error()
	}
	return status
}
