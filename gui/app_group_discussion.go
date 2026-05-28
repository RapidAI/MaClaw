package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

const groupDiscussionHubTimeout = time.Duration(corelib.DefaultAgentTimeoutSec) * time.Second

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

func (a *App) veA2AHubClient() (*a2a.HubClient, corelib.AppConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, corelib.AppConfig{}, err
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

func groupDiscussionAgentID(cfg corelib.AppConfig) string {
	return firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID)
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
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if agentID == "" {
		return nil, fmt.Errorf("remote machine id is required")
	}
	role = strings.TrimSpace(role)
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	discussions, err := client.ListDiscussionsForAgent(ctx, agentID, role)
	store, storeErr := a.openGroupDiscussionHistoryStore()
	if storeErr == nil {
		defer store.Close()
	}
	if err != nil {
		if storeErr == nil {
			if cached, cacheErr := store.CachedSummaries(ctx, false); cacheErr == nil && len(cached) > 0 {
				cached = filterGroupDiscussionSummariesByRole(cached, role)
				if len(cached) > 0 {
					return cached, nil
				}
			}
		}
		return nil, err
	}
	if storeErr == nil {
		_ = store.CacheSummaries(ctx, discussions, a.groupDiscussionAttachmentRoot)
		discussions, _ = store.VisibleSummaries(ctx, discussions)
		if cached, cacheErr := store.CachedSummaries(ctx, false); cacheErr == nil && len(cached) > 0 {
			discussions = mergeGroupDiscussionSummaries(discussions, cached, role)
		}
	}
	return discussions, nil
}

func (a *App) GroupDiscussionSetLocalHidden(consultationID string, hidden bool) error {
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	store, err := a.openGroupDiscussionHistoryStore()
	if err != nil {
		return err
	}
	defer store.Close()
	return store.SetHidden(ctx, consultationID, hidden)
}

func (a *App) GroupDiscussionListLocalHidden() ([]a2a.HubDiscussionSummary, error) {
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	store, err := a.openGroupDiscussionHistoryStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.HiddenSummaries(ctx)
}

func (a *App) GroupDiscussionListInvites() ([]a2a.GroupInviteSummary, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(groupDiscussionAgentID(cfg))
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
	agentID := strings.TrimSpace(groupDiscussionAgentID(cfg))
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

type GroupDiscussionExpertRank struct {
	AgentID              string   `json:"agent_id"`
	DisplayName          string   `json:"display_name,omitempty"`
	Score                int      `json:"score"`
	Selected             bool     `json:"selected,omitempty"`
	Reasons              []string `json:"reasons,omitempty"`
	MatchedSkills        []string `json:"matched_skills,omitempty"`
	Skills               []string `json:"skills,omitempty"`
	SecurityGroupID      string   `json:"security_group_id,omitempty"`
	ContributionScore    float64  `json:"contribution_score,omitempty"`
	ContributionEvidence int      `json:"contribution_evidence,omitempty"`
}

type GroupDiscussionExpertRankingResult struct {
	InviteeIDs              []string                           `json:"invitee_ids,omitempty"`
	Ranked                  []GroupDiscussionExpertRank        `json:"ranked,omitempty"`
	Limit                   int                                `json:"limit"`
	UseCrossAgentExperience bool                               `json:"use_cross_agent_experience"`
	RecommendedFocusContext map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                             `json:"non_executing_boundary"`
}

func (a *App) GroupDiscussionCreateConsultation(req a2a.GroupConsultationRequest) (a2a.ConsultationCreateResponse, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return a2a.ConsultationCreateResponse{}, err
	}
	if cfg.GroupDiscussion.ConfirmBeforeStart {
		return a2a.ConsultationCreateResponse{}, fmt.Errorf("group discussion requires user confirmation before start")
	}
	out, err := createGroupDiscussionConsultation(client, cfg, req)
	if err == nil {
		cacheCreatedGroupDiscussion(a, out)
	}
	return out, err
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
	cacheCreatedGroupDiscussion(a, consultation)
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
		if toID == "" || toID == groupDiscussionAgentID(cfg) {
			continue
		}
		inviteID, err := client.SendInvitation(ctx, consultation.Discussion.ID, a2a.GroupInvitation{
			RequestID:       consultation.Request.ID,
			FromID:          groupDiscussionAgentID(cfg),
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

func cacheCreatedGroupDiscussion(a *App, out a2a.ConsultationCreateResponse) {
	if a == nil || strings.TrimSpace(out.Discussion.ID) == "" {
		return
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if store, err := a.openGroupDiscussionHistoryStore(); err == nil {
		out.Discussion.LocalRelation = firstNonEmptyGroupString(out.Discussion.LocalRelation, "initiated_by_me")
		out.Discussion.Role = firstNonEmptyGroupString(out.Discussion.Role, "initiator")
		out.Discussion.Readonly = !normalizeGroupDiscussionSessionStatus(out.Discussion.Status).IsOpen()
		_ = store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{out.Discussion}, a.groupDiscussionAttachmentRoot)
		_ = store.Close()
	}
}

func (a *App) GroupDiscussionRankExperts(req a2a.GroupConsultationRequest) (GroupDiscussionExpertRankingResult, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return GroupDiscussionExpertRankingResult{}, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	experts, err := client.ListExperts(ctx)
	if err != nil {
		return GroupDiscussionExpertRankingResult{}, err
	}
	return rankGroupDiscussionExperts(experts, cfg, req, 3), nil
}

func createGroupDiscussionConsultation(client *a2a.HubClient, cfg corelib.AppConfig, req a2a.GroupConsultationRequest) (a2a.ConsultationCreateResponse, error) {
	if req.FromID == "" {
		req.FromID = groupDiscussionAgentID(cfg)
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
	return rankGroupDiscussionExperts(experts, cfg, req, 3).InviteeIDs
}

func rankGroupDiscussionExperts(experts []a2a.GroupProfile, cfg corelib.AppConfig, req a2a.GroupConsultationRequest, limit int) GroupDiscussionExpertRankingResult {
	if limit <= 0 {
		limit = 3
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	localSecurityGroup := strings.TrimSpace(cfg.GroupDiscussion.SecurityGroupID)
	useCrossAgentExperience := cfg.GroupDiscussion.CrossAgentExperienceEnabled()
	wanted := lowerStringSet(req.SkillsWanted)
	type candidate struct {
		rank  GroupDiscussionExpertRank
		order int
	}
	candidates := make([]candidate, 0, len(experts))
	for index, expert := range experts {
		id := strings.TrimSpace(expert.AgentID)
		if id == "" || id == localID || !expert.Discoverable || !expert.Available {
			continue
		}
		expertGroup := strings.TrimSpace(expert.SecurityGroupID)
		if cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion && localSecurityGroup != "" && expertGroup != localSecurityGroup {
			continue
		}
		score := 0
		reasons := make([]string, 0, 4)
		matchedSkills := make([]string, 0, len(expert.Skills))
		if expertGroup != "" && expertGroup == localSecurityGroup {
			score += 4
			reasons = append(reasons, "same_security_group:+4")
		}
		for _, skill := range expert.Skills {
			skill = strings.TrimSpace(skill)
			if _, ok := wanted[strings.ToLower(skill)]; ok {
				score += 3
				matchedSkills = append(matchedSkills, skill)
				reasons = append(reasons, "skill:"+skill+":+3")
			}
		}
		if strings.TrimSpace(expert.Description) != "" {
			score++
			reasons = append(reasons, "profile_description:+1")
		}
		if useCrossAgentExperience {
			bonus := groupDiscussionContributionScoreBonus(expert)
			if bonus > 0 {
				score += bonus
				reasons = append(reasons, fmt.Sprintf("contribution_score:+%d", bonus))
			}
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "eligible_default")
		}
		candidates = append(candidates, candidate{
			rank: GroupDiscussionExpertRank{
				AgentID:              id,
				DisplayName:          strings.TrimSpace(expert.DisplayName),
				Score:                score,
				Reasons:              reasons,
				MatchedSkills:        matchedSkills,
				Skills:               dedupeGroupDiscussionStrings(expert.Skills),
				SecurityGroupID:      expertGroup,
				ContributionScore:    expert.ContributionScore,
				ContributionEvidence: expert.ContributionEvidence,
			},
			order: index,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank.Score == candidates[j].rank.Score {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].rank.Score > candidates[j].rank.Score
	})
	invitees := make([]string, 0, limit)
	ranked := make([]GroupDiscussionExpertRank, 0, len(candidates))
	for i := range candidates {
		if len(invitees) < limit {
			candidates[i].rank.Selected = true
			invitees = append(invitees, candidates[i].rank.AgentID)
		}
		ranked = append(ranked, candidates[i].rank)
	}
	result := GroupDiscussionExpertRankingResult{
		InviteeIDs:              invitees,
		Ranked:                  ranked,
		Limit:                   limit,
		UseCrossAgentExperience: useCrossAgentExperience,
		NonExecutingBoundary:    "read-only expert ranking preview; no discussion was started and no invitations were sent",
	}
	result.RecommendedFocusContext = groupDiscussionExpertRankingFocusContext(result, req)
	result.RecommendedToolCall = groupDiscussionExpertRankingToolCall(result, req)
	result.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(result.RecommendedToolCall, result.RecommendedFocusContext, result.NonExecutingBoundary)
	return result
}

func groupDiscussionExpertRankingFocusContext(result GroupDiscussionExpertRankingResult, req a2a.GroupConsultationRequest) map[string]interface{} {
	ctx := map[string]interface{}{
		"action_kind": "rank_experts",
		"reason":      "read-only A2A expert ranking preview before any discussion start or invitation",
		"limit":       result.Limit,
	}
	if topic := strings.TrimSpace(req.Topic); topic != "" {
		ctx["topic"] = topic
	}
	if question := strings.TrimSpace(req.Question); question != "" {
		ctx["question"] = question
	}
	if risk := strings.TrimSpace(req.RiskLevel); risk != "" {
		ctx["risk_level"] = risk
	}
	if len(req.SkillsWanted) > 0 {
		ctx["skills_wanted"] = dedupeGroupDiscussionStrings(req.SkillsWanted)
	}
	if len(result.InviteeIDs) > 0 {
		ctx["selected_invitee_ids"] = append([]string(nil), result.InviteeIDs...)
	}
	ctx["use_cross_agent_experience"] = result.UseCrossAgentExperience
	return ctx
}

func groupDiscussionExpertRankingToolCall(result GroupDiscussionExpertRankingResult, req a2a.GroupConsultationRequest) *GroupDiscussionToolCallSuggestion {
	args := map[string]interface{}{
		"action": "rank_experts",
	}
	if topic := strings.TrimSpace(req.Topic); topic != "" {
		args["topic"] = topic
	}
	if question := strings.TrimSpace(req.Question); question != "" {
		args["question"] = question
	}
	if contextSummary := strings.TrimSpace(req.ContextSummary); contextSummary != "" {
		args["context_summary"] = contextSummary
	}
	if risk := strings.TrimSpace(req.RiskLevel); risk != "" {
		args["risk_level"] = risk
	}
	if len(req.SkillsWanted) > 0 {
		args["skills_wanted"] = dedupeGroupDiscussionStrings(req.SkillsWanted)
	}
	focusContext := groupDiscussionExpertRankingFocusContext(result, req)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended group_discussion expert ranking preview only; it must not start a discussion, invite experts, send messages, mutate Hub state, mutate memory, or change routing",
	}
}

func groupDiscussionContributionScoreBonus(expert a2a.GroupProfile) int {
	if expert.ContributionEvidence < 2 || expert.ContributionScore <= 0 {
		return 0
	}
	score := expert.ContributionScore
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	bonus := int(score * 3)
	if bonus == 0 && score >= 0.5 {
		bonus = 1
	}
	if expert.ContributionEvidence >= 8 && bonus < 3 && score >= 0.75 {
		bonus++
	}
	if bonus > 3 {
		bonus = 3
	}
	return bonus
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
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return a2a.HubDiscussionDetail{}, err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, consultationID, groupDiscussionAgentID(cfg))
	store, storeErr := a.openGroupDiscussionHistoryStore()
	if storeErr == nil {
		defer store.Close()
	}
	if err != nil {
		if storeErr == nil {
			if cached, ok, cacheErr := store.CachedDetail(ctx, consultationID); cacheErr == nil && ok {
				return cached, nil
			}
		}
		return a2a.HubDiscussionDetail{}, err
	}
	if storeErr == nil {
		_ = store.CacheDetail(ctx, detail, a.groupDiscussionAttachmentRoot)
		detail = store.EnrichDetailAttachments(ctx, detail)
	}
	return detail, nil
}

func (a *App) GroupDiscussionGetReadiness(consultationID string) (GroupDiscussionReadiness, error) {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionReadiness{}, err
	}
	readiness := groupDiscussionReadiness(detail)
	readiness.ConsultationID = strings.TrimSpace(consultationID)
	readiness = finalizeGroupDiscussionReadiness(readiness)
	return readiness, nil
}

func (a *App) GroupDiscussionGetWorkflowState(consultationID string) (GroupDiscussionWorkflowState, error) {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionWorkflowState{}, err
	}
	state := groupDiscussionWorkflowState(detail)
	if strings.TrimSpace(state.ConsultationID) == "" {
		state.ConsultationID = strings.TrimSpace(consultationID)
	}
	return state, nil
}

func (a *App) GroupDiscussionSuggestEscalationRoute(consultationID string) (GroupDiscussionEscalationRouteSuggestion, error) {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionEscalationRouteSuggestion{}, err
	}
	suggestion := groupDiscussionEscalationRouteSuggestion(detail)
	if strings.TrimSpace(suggestion.ConsultationID) == "" {
		suggestion.ConsultationID = strings.TrimSpace(consultationID)
	}
	return suggestion, nil
}

func (a *App) GroupDiscussionBuildWorkflowActionDraft(consultationID string) (GroupDiscussionWorkflowActionDraft, error) {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionWorkflowActionDraft{}, err
	}
	state := groupDiscussionWorkflowState(detail)
	if strings.TrimSpace(state.ConsultationID) == "" {
		state.ConsultationID = strings.TrimSpace(consultationID)
	}
	return groupDiscussionWorkflowActionDraft(detail, state), nil
}

func (a *App) GroupDiscussionGetRollbackReadiness(consultationID string, evidence string) (GroupDiscussionRollbackReadiness, error) {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return GroupDiscussionRollbackReadiness{}, err
	}
	readiness := groupDiscussionRollbackReadiness(detail, evidence)
	if strings.TrimSpace(readiness.ConsultationID) == "" {
		readiness.ConsultationID = strings.TrimSpace(consultationID)
	}
	return readiness, nil
}

func (a *App) GroupDiscussionSendInvitation(consultationID string, inv a2a.GroupInvitation) (string, error) {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return "", err
	}
	if inv.FromID == "" {
		inv.FromID = groupDiscussionAgentID(cfg)
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
		resp.FromID = groupDiscussionAgentID(cfg)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	var acceptedInvite *a2a.GroupInviteSummary
	if invites, err := client.ListInvites(ctx, strings.TrimSpace(groupDiscussionAgentID(cfg))); err == nil {
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
		FromID:    groupDiscussionAgentID(cfg),
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
	Preview        bool   `json:"preview,omitempty"`
}

type GroupDiscussionReadiness struct {
	ConsultationID          string                             `json:"consultation_id"`
	Status                  string                             `json:"status,omitempty"`
	ParticipantCount        int                                `json:"participant_count"`
	ExpectedAnswerCount     int                                `json:"expected_answer_count"`
	AnswerCount             int                                `json:"answer_count"`
	HasResult               bool                               `json:"has_result"`
	Ready                   bool                               `json:"ready"`
	Reason                  string                             `json:"reason,omitempty"`
	RecommendedFocusContext map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall     *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary    string                             `json:"non_executing_boundary,omitempty"`
}
type GroupDiscussionSummarizeResult struct {
	ConsultationID           string                             `json:"consultation_id"`
	Summary                  string                             `json:"summary"`
	Rationale                string                             `json:"rationale,omitempty"`
	Risks                    []string                           `json:"risks,omitempty"`
	Disagreements            []string                           `json:"disagreements,omitempty"`
	OpenQuestions            []string                           `json:"open_questions,omitempty"`
	ParticipantContributions map[string]string                  `json:"participant_contributions,omitempty"`
	Confidence               float64                            `json:"confidence,omitempty"`
	AnswerCount              int                                `json:"answer_count"`
	UsedLLM                  bool                               `json:"used_llm"`
	Submitted                bool                               `json:"submitted"`
	Injected                 bool                               `json:"injected"`
	RecommendedFocusContext  map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall      *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary     string                             `json:"non_executing_boundary,omitempty"`
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
	if req.Preview {
		return finalizeGroupDiscussionSummaryPreview(result), nil
	}
	a.recordLocalGroupDiscussionContribution(detail, result)
	a.promoteGroupDiscussionResultToMemory(detail, result)
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
	client := &http.Client{Timeout: 45 * time.Second}
	if shouldUseLayeredGroupDiscussionSummary(detail) {
		if result, err := a.groupDiscussionGenerateLayeredResultSummary(detail, llmCfg, client); err == nil && strings.TrimSpace(result.Summary) != "" {
			return result, nil
		}
	}
	input := buildGroupDiscussionResultSummaryInput(detail)
	if strings.TrimSpace(input) == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("discussion has no content")
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": groupDiscussionResultSummaryPrompt},
		map[string]string{"role": "user", "content": input},
	}
	resp, err := doSimpleLLMRequest(context.Background(), llmCfg, messages, client, 45*time.Second)
	if err != nil {
		return GroupDiscussionSummarizeResult{}, err
	}
	return decodeGroupDiscussionResultSummary(resp.Content)
}

func groupDiscussionReadiness(detail a2a.HubDiscussionDetail) GroupDiscussionReadiness {
	answerCount := countGroupDiscussionAnswersForDetail(detail)
	participantCount := len(detail.Discussion.ParticipantIDs)
	if participantCount == 0 && detail.Session != nil {
		participantCount = len(detail.Session.Participants)
	}
	expected := expectedGroupDiscussionAnswers(detail)
	status := strings.TrimSpace(detail.Discussion.Status)
	if status == "" && detail.Session != nil {
		status = string(detail.Session.Status)
	}
	hasResult := strings.TrimSpace(detail.Discussion.ResultSummary) != "" || detail.Decision != nil
	statusKind := normalizeGroupDiscussionSessionStatus(status)
	ready := hasResult || answerCount >= expected || (statusKind.IsSetAndNotOpen() && answerCount > 0)
	reason := "waiting for expert answers"
	if hasResult {
		reason = "result already exists"
	} else if answerCount >= expected {
		reason = "expected expert answers received"
	} else if statusKind.IsSetAndNotOpen() && answerCount > 0 {
		reason = "discussion is no longer open"
	} else if answerCount > 0 {
		reason = fmt.Sprintf("waiting for more expert answers (%d/%d)", answerCount, expected)
	}
	return GroupDiscussionReadiness{Status: status, ParticipantCount: participantCount, ExpectedAnswerCount: expected, AnswerCount: answerCount, HasResult: hasResult, Ready: ready, Reason: reason}
}

func finalizeGroupDiscussionReadiness(readiness GroupDiscussionReadiness) GroupDiscussionReadiness {
	readiness.NonExecutingBoundary = "read-only readiness inspection; no discussion summary was submitted, no chat was injected, no message was sent, and no Hub state changed"
	readiness.RecommendedFocusContext = groupDiscussionReadinessFocusContext(readiness)
	readiness.RecommendedToolCall = groupDiscussionReadinessToolCall(readiness)
	readiness.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(readiness.RecommendedToolCall, readiness.RecommendedFocusContext, readiness.NonExecutingBoundary)
	return readiness
}

func groupDiscussionReadinessFocusContext(readiness GroupDiscussionReadiness) map[string]interface{} {
	consultationID := strings.TrimSpace(readiness.ConsultationID)
	if consultationID == "" {
		return nil
	}
	actionKind := "inspect_workflow_state"
	if readiness.Ready {
		actionKind = "preview_summary"
	}
	return map[string]interface{}{
		"consultation_id":       consultationID,
		"action_kind":           actionKind,
		"ready":                 readiness.Ready,
		"reason":                strings.TrimSpace(readiness.Reason),
		"answer_count":          readiness.AnswerCount,
		"expected_answer_count": readiness.ExpectedAnswerCount,
		"has_result":            readiness.HasResult,
	}
}

func groupDiscussionReadinessToolCall(readiness GroupDiscussionReadiness) *GroupDiscussionToolCallSuggestion {
	consultationID := strings.TrimSpace(readiness.ConsultationID)
	if consultationID == "" {
		return nil
	}
	args := map[string]interface{}{"action": "workflow_state", "consultation_id": consultationID}
	if readiness.Ready {
		args = map[string]interface{}{"action": "summarize_result", "consultation_id": consultationID, "preview": true}
	}
	focusContext := groupDiscussionReadinessFocusContext(readiness)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    args,
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended group_discussion readiness follow-up only; it may inspect workflow state or preview a summary, and must not submit results, inject chat, send messages, invite experts, change discussion state, mutate memory, or change routing",
	}
}

func finalizeGroupDiscussionSummaryPreview(result GroupDiscussionSummarizeResult) GroupDiscussionSummarizeResult {
	result.NonExecutingBoundary = "preview-only discussion summary; no result was submitted, no chat was injected, no memory was promoted, no messages were sent, and no Hub state changed"
	result.RecommendedFocusContext = groupDiscussionSummaryPreviewFocusContext(result)
	result.RecommendedToolCall = groupDiscussionSummaryPreviewToolCall(result)
	result.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(result.RecommendedToolCall, result.RecommendedFocusContext, result.NonExecutingBoundary)
	return result
}

func groupDiscussionSummaryPreviewFocusContext(result GroupDiscussionSummarizeResult) map[string]interface{} {
	consultationID := strings.TrimSpace(result.ConsultationID)
	if consultationID == "" {
		return nil
	}
	return map[string]interface{}{
		"consultation_id": consultationID,
		"action_kind":     "summary_preview",
		"answer_count":    result.AnswerCount,
		"used_llm":        result.UsedLLM,
		"reason":          "preview-only A2A discussion summary; inspect before submit/inject",
	}
}

func groupDiscussionSummaryPreviewToolCall(result GroupDiscussionSummarizeResult) *GroupDiscussionToolCallSuggestion {
	consultationID := strings.TrimSpace(result.ConsultationID)
	if consultationID == "" {
		return nil
	}
	focusContext := groupDiscussionSummaryPreviewFocusContext(result)
	return &GroupDiscussionToolCallSuggestion{
		Tool:                    "group_discussion",
		Args:                    map[string]interface{}{"action": "get_detail", "consultation_id": consultationID},
		RecommendedFocusContext: focusContext,
		DiscussionFocusContext:  focusContext,
		NonExecuting:            true,
		NonExecutingBoundary:    "recommended summary-preview inspection only; it may fetch discussion detail and must not submit results, inject chat, promote memory, send messages, invite experts, change Hub state, or change routing",
	}
}

func summarizeGroupDiscussionDetail(detail a2a.HubDiscussionDetail) GroupDiscussionSummarizeResult {
	if detail.Decision != nil && strings.TrimSpace(detail.Decision.Summary) != "" {
		return GroupDiscussionSummarizeResult{Summary: strings.TrimSpace(detail.Decision.Summary), Rationale: strings.TrimSpace(detail.Decision.Rationale), AnswerCount: countGroupDiscussionAnswersForDetail(detail)}
	}
	if escalation := groupDiscussionEscalation(detail); escalation != nil && strings.TrimSpace(escalation.Reason) != "" {
		rationale := "Escalated"
		if strings.TrimSpace(escalation.Target) != "" {
			rationale += " to " + strings.TrimSpace(escalation.Target)
		}
		if strings.TrimSpace(escalation.RaisedBy) != "" {
			rationale += " by " + strings.TrimSpace(escalation.RaisedBy)
		}
		rationale += ": " + strings.TrimSpace(escalation.Reason)
		return GroupDiscussionSummarizeResult{Summary: "Escalated: " + strings.TrimSpace(escalation.Reason), Rationale: rationale, AnswerCount: countGroupDiscussionAnswersForDetail(detail)}
	}
	answers := groupDiscussionAnswerMessagesForDetail(detail)
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
	return GroupDiscussionSummarizeResult{Summary: summary, Rationale: b.String(), Risks: groupDiscussionRiskSnippets(answers), Disagreements: groupDiscussionDisagreementSnippets(answers), AnswerCount: len(answers)}
}

func (a *App) recordLocalGroupDiscussionContribution(detail a2a.HubDiscussionDetail, result GroupDiscussionSummarizeResult) {
	if a == nil || result.AnswerCount == 0 {
		return
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.GroupDiscussion.CrossAgentExperienceEnabled() {
		return
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" {
		localID = strings.TrimSpace(cfg.RemoteClientID)
	}
	if localID == "" || !groupDiscussionHasAnswerFromDetail(detail, localID) {
		return
	}
	score := groupDiscussionContributionQuality(detail, result, localID)
	if score <= 0 {
		return
	}
	_ = a.PatchConfig(func(cfg *corelib.AppConfig) {
		evidence := cfg.GroupDiscussion.ContributionEvidence
		if evidence < 0 {
			evidence = 0
		}
		current := cfg.GroupDiscussion.ContributionScore
		if current < 0 || current > 1 || evidence == 0 {
			current = score
		} else {
			current = ((current * float64(evidence)) + score) / float64(evidence+1)
		}
		cfg.GroupDiscussion.ContributionEvidence = evidence + 1
		cfg.GroupDiscussion.ContributionScore = clampGroupDiscussionFloat(current, 0, 1)
	})
}

func groupDiscussionHasAnswerFromDetail(detail a2a.HubDiscussionDetail, participant string) bool {
	participant = strings.TrimSpace(participant)
	if participant == "" {
		return false
	}
	for _, msg := range groupDiscussionAnswerMessagesForDetail(detail) {
		if strings.TrimSpace(msg.FromID) == participant {
			return true
		}
	}
	return false
}

func groupDiscussionContributionQuality(detail a2a.HubDiscussionDetail, result GroupDiscussionSummarizeResult, participant string) float64 {
	participant = strings.TrimSpace(participant)
	if participant == "" || result.AnswerCount == 0 {
		return 0
	}
	answerCount := 0
	kindBonus := 0.0
	for _, msg := range groupDiscussionAnswerMessagesForDetail(detail) {
		if strings.TrimSpace(msg.FromID) != participant {
			continue
		}
		answerCount++
		switch msg.Kind {
		case a2a.MessageEvidence, a2a.MessageObjection:
			kindBonus += 0.08
		case a2a.MessageAnswer:
			kindBonus += 0.04
		}
	}
	if answerCount == 0 {
		return 0
	}
	score := 0.45
	if result.Confidence > 0 {
		score += result.Confidence * 0.25
	}
	if len(result.ParticipantContributions) > 0 {
		if contribution := strings.TrimSpace(result.ParticipantContributions[participant]); contribution != "" {
			score += 0.15
		}
	}
	if len(result.Risks) > 0 || len(result.Disagreements) > 0 || len(result.OpenQuestions) > 0 {
		score += 0.05
	}
	score += kindBonus
	return clampGroupDiscussionFloat(score, 0.1, 1)
}

func clampGroupDiscussionFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func groupDiscussionRiskSnippets(messages []a2a.Message) []string {
	var risks []string
	for _, msg := range messages {
		if msg.Kind == a2a.MessageObjection || msg.Kind == a2a.MessageEvidence {
			risks = append(risks, truncateGroupDiscussionText(msg.Content, 240))
		}
	}
	return dedupeGroupDiscussionStrings(risks)
}

func groupDiscussionDisagreementSnippets(messages []a2a.Message) []string {
	var disagreements []string
	for _, msg := range messages {
		if msg.Kind == a2a.MessageObjection {
			disagreements = append(disagreements, truncateGroupDiscussionText(msg.Content, 240))
		}
	}
	return dedupeGroupDiscussionStrings(disagreements)
}
func groupDiscussionAnswerMessagesForDetail(detail a2a.HubDiscussionDetail) []a2a.Message {
	return groupDiscussionAnswerMessagesWithRoles(detail.Messages, groupDiscussionParticipantRoleMap(detail))
}

func groupDiscussionAnswerMessages(messages []a2a.Message) []a2a.Message {
	return groupDiscussionAnswerMessagesWithRoles(messages, nil)
}

func groupDiscussionAnswerMessagesWithRoles(messages []a2a.Message, roles map[string]string) []a2a.Message {
	out := make([]a2a.Message, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		if role, ok := roles[strings.TrimSpace(msg.FromID)]; ok && !groupDiscussionRoleContributesAnswer(role) {
			continue
		}
		if msg.Kind == a2a.MessageAnswer || msg.Kind == a2a.MessageStatement || msg.Kind == a2a.MessageEvidence || msg.Kind == a2a.MessageObjection {
			out = append(out, msg)
		}
	}
	return out
}

func countGroupDiscussionAnswersForDetail(detail a2a.HubDiscussionDetail) int {
	return len(groupDiscussionAnswerMessagesForDetail(detail))
}

func countGroupDiscussionAnswers(messages []a2a.Message) int {
	return len(groupDiscussionAnswerMessages(messages))
}

func expectedGroupDiscussionAnswers(detail a2a.HubDiscussionDetail) int {
	if detail.Session != nil && len(detail.Session.Participants) > 0 {
		count := 0
		hasRoles := false
		for _, participant := range detail.Session.Participants {
			if strings.TrimSpace(participant.RoleCode) != "" {
				hasRoles = true
			}
			if groupDiscussionRoleContributesAnswer(participant.RoleCode) {
				count++
			}
		}
		if hasRoles && count > 0 {
			return count
		}
	}
	participantCount := len(detail.Discussion.ParticipantIDs)
	if participantCount == 0 && detail.Session != nil {
		participantCount = len(detail.Session.Participants)
	}
	expected := participantCount - 1
	if expected < 1 {
		return 1
	}
	return expected
}

func groupDiscussionParticipantRoleMap(detail a2a.HubDiscussionDetail) map[string]string {
	if detail.Session == nil || len(detail.Session.Participants) == 0 {
		return nil
	}
	roles := make(map[string]string, len(detail.Session.Participants))
	for _, participant := range detail.Session.Participants {
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

const (
	groupDiscussionLayeredAnswerThreshold  = 4
	groupDiscussionLayeredTokenThreshold   = 3500
	groupDiscussionSummaryShardMaxMessages = 3
	groupDiscussionSummaryShardMaxRunes    = 2600
)

type groupDiscussionSummaryShard struct {
	Label    string
	Messages []a2a.Message
}

type groupDiscussionShardResult struct {
	Label        string
	MessageCount int
	Result       GroupDiscussionSummarizeResult
}

func shouldUseLayeredGroupDiscussionSummary(detail a2a.HubDiscussionDetail) bool {
	answers := groupDiscussionAnswerMessagesForDetail(detail)
	if len(answers) <= 1 {
		return false
	}
	if len(answers) > groupDiscussionLayeredAnswerThreshold {
		return true
	}
	input := buildGroupDiscussionResultSummaryInput(detail)
	return corelib.EstimateTextTokens(input) > groupDiscussionLayeredTokenThreshold
}

func (a *App) groupDiscussionGenerateLayeredResultSummary(detail a2a.HubDiscussionDetail, llmCfg corelib.MaclawLLMConfig, client *http.Client) (GroupDiscussionSummarizeResult, error) {
	shards := buildGroupDiscussionSummaryShards(detail, groupDiscussionSummaryShardMaxMessages, groupDiscussionSummaryShardMaxRunes)
	if len(shards) <= 1 {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("discussion does not need layered summary")
	}
	shardResults := make([]groupDiscussionShardResult, 0, len(shards))
	for _, shard := range shards {
		messages := []interface{}{
			map[string]string{"role": "system", "content": groupDiscussionShardSummaryPrompt},
			map[string]string{"role": "user", "content": buildGroupDiscussionShardSummaryInput(detail, shard)},
		}
		resp, err := doSimpleLLMRequest(context.Background(), llmCfg, messages, client, 45*time.Second)
		var result GroupDiscussionSummarizeResult
		if err == nil {
			result, err = decodeGroupDiscussionResultSummary(resp.Content)
		}
		if err != nil || strings.TrimSpace(result.Summary) == "" {
			result = fallbackGroupDiscussionShardSummary(shard)
		}
		shardResults = append(shardResults, groupDiscussionShardResult{Label: shard.Label, MessageCount: len(shard.Messages), Result: result})
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": groupDiscussionLayeredReducePrompt},
		map[string]string{"role": "user", "content": buildGroupDiscussionLayeredReduceInput(detail, shardResults)},
	}
	resp, err := doSimpleLLMRequest(context.Background(), llmCfg, messages, client, 45*time.Second)
	if err != nil {
		return GroupDiscussionSummarizeResult{}, err
	}
	result, err := decodeGroupDiscussionResultSummary(resp.Content)
	if err != nil {
		return GroupDiscussionSummarizeResult{}, err
	}
	result.AnswerCount = countGroupDiscussionAnswersForDetail(detail)
	if len(result.ParticipantContributions) == 0 {
		result.ParticipantContributions = participantContributionsFromShardResults(shardResults)
	}
	return result, nil
}

func buildGroupDiscussionSummaryShards(detail a2a.HubDiscussionDetail, maxMessages, maxRunes int) []groupDiscussionSummaryShard {
	if maxMessages <= 0 {
		maxMessages = groupDiscussionSummaryShardMaxMessages
	}
	if maxRunes <= 0 {
		maxRunes = groupDiscussionSummaryShardMaxRunes
	}
	answers := groupDiscussionAnswerMessagesForDetail(detail)
	shards := make([]groupDiscussionSummaryShard, 0)
	currentByLabel := map[string]int{}
	for _, msg := range answers {
		label := firstNonEmptyGroupString(msg.FromID, "expert") + "/" + string(msg.Kind)
		idx, ok := currentByLabel[label]
		if !ok || len(shards[idx].Messages) >= maxMessages || groupDiscussionShardRunes(shards[idx])+len([]rune(msg.Content)) > maxRunes {
			shards = append(shards, groupDiscussionSummaryShard{Label: label})
			idx = len(shards) - 1
			currentByLabel[label] = idx
		}
		shards[idx].Messages = append(shards[idx].Messages, msg)
	}
	return shards
}

func groupDiscussionShardRunes(shard groupDiscussionSummaryShard) int {
	total := 0
	for _, msg := range shard.Messages {
		total += len([]rune(msg.Content))
	}
	return total
}

func buildGroupDiscussionShardSummaryInput(detail a2a.HubDiscussionDetail, shard groupDiscussionSummaryShard) string {
	var b strings.Builder
	b.WriteString("Topic: ")
	b.WriteString(detail.Discussion.Topic)
	b.WriteString("\nQuestion: ")
	b.WriteString(detail.Discussion.Question)
	b.WriteString("\nShard: ")
	b.WriteString(shard.Label)
	b.WriteString("\nMessages:")
	for _, msg := range shard.Messages {
		b.WriteString("\n- From ")
		b.WriteString(firstNonEmptyGroupString(msg.FromID, "expert"))
		b.WriteString(" [")
		b.WriteString(string(msg.Kind))
		b.WriteString("]: ")
		b.WriteString(truncateGroupDiscussionText(msg.Content, 1200))
	}
	return b.String()
}

func buildGroupDiscussionLayeredReduceInput(detail a2a.HubDiscussionDetail, shards []groupDiscussionShardResult) string {
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
	b.WriteString("\nShard summaries:")
	for _, shard := range shards {
		b.WriteString("\n- Shard ")
		b.WriteString(shard.Label)
		b.WriteString(" (")
		b.WriteString(fmt.Sprint(shard.MessageCount))
		b.WriteString(" message(s))")
		b.WriteString("\n  Summary: ")
		b.WriteString(shard.Result.Summary)
		if shard.Result.Rationale != "" {
			b.WriteString("\n  Rationale: ")
			b.WriteString(shard.Result.Rationale)
		}
		if len(shard.Result.Risks) > 0 {
			b.WriteString("\n  Risks: ")
			b.WriteString(strings.Join(shard.Result.Risks, "; "))
		}
		if len(shard.Result.Disagreements) > 0 {
			b.WriteString("\n  Disagreements: ")
			b.WriteString(strings.Join(shard.Result.Disagreements, "; "))
		}
		if len(shard.Result.OpenQuestions) > 0 {
			b.WriteString("\n  Open questions: ")
			b.WriteString(strings.Join(shard.Result.OpenQuestions, "; "))
		}
	}
	return b.String()
}

func fallbackGroupDiscussionShardSummary(shard groupDiscussionSummaryShard) GroupDiscussionSummarizeResult {
	var rationale strings.Builder
	risks := make([]string, 0)
	disagreements := make([]string, 0)
	for i, msg := range shard.Messages {
		if i > 0 {
			rationale.WriteString("\n")
		}
		rationale.WriteString(firstNonEmptyGroupString(msg.FromID, "expert"))
		rationale.WriteString(" [")
		rationale.WriteString(string(msg.Kind))
		rationale.WriteString("]: ")
		rationale.WriteString(truncateGroupDiscussionText(msg.Content, 500))
		if msg.Kind == a2a.MessageObjection {
			disagreements = append(disagreements, truncateGroupDiscussionText(msg.Content, 240))
			risks = append(risks, truncateGroupDiscussionText(msg.Content, 240))
		}
		if msg.Kind == a2a.MessageEvidence {
			risks = append(risks, truncateGroupDiscussionText(msg.Content, 240))
		}
	}
	return GroupDiscussionSummarizeResult{
		Summary:       "Shard " + shard.Label + " contains " + fmt.Sprint(len(shard.Messages)) + " expert message(s).",
		Rationale:     rationale.String(),
		Risks:         dedupeGroupDiscussionStrings(risks),
		Disagreements: dedupeGroupDiscussionStrings(disagreements),
		Confidence:    0.4,
	}
}

func participantContributionsFromShardResults(shards []groupDiscussionShardResult) map[string]string {
	out := map[string]string{}
	for _, shard := range shards {
		participant := shard.Label
		if slash := strings.Index(participant, "/"); slash > 0 {
			participant = participant[:slash]
		}
		if participant == "" {
			participant = "expert"
		}
		text := strings.TrimSpace(shard.Result.Summary)
		if text == "" {
			continue
		}
		if existing := out[participant]; existing != "" {
			out[participant] = existing + "\n" + text
		} else {
			out[participant] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func buildGroupDiscussionResultSummaryInput(detail a2a.HubDiscussionDetail) string {
	answers := groupDiscussionAnswerMessagesForDetail(detail)
	escalation := groupDiscussionEscalation(detail)
	if len(answers) == 0 && detail.Decision == nil && escalation == nil {
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
	if escalation != nil {
		b.WriteString("\nEscalation reason: ")
		b.WriteString(escalation.Reason)
		if strings.TrimSpace(escalation.Target) != "" {
			b.WriteString("\nEscalation target: ")
			b.WriteString(escalation.Target)
		}
		if strings.TrimSpace(escalation.RaisedBy) != "" {
			b.WriteString("\nEscalation raised by: ")
			b.WriteString(escalation.RaisedBy)
		}
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
	parsed.Risks = dedupeGroupDiscussionStrings(parsed.Risks)
	parsed.Disagreements = dedupeGroupDiscussionStrings(parsed.Disagreements)
	parsed.OpenQuestions = dedupeGroupDiscussionStrings(parsed.OpenQuestions)
	if len(parsed.ParticipantContributions) > 0 {
		clean := make(map[string]string, len(parsed.ParticipantContributions))
		for participant, contribution := range parsed.ParticipantContributions {
			participant = strings.TrimSpace(participant)
			contribution = strings.TrimSpace(contribution)
			if participant != "" && contribution != "" {
				clean[participant] = contribution
			}
		}
		parsed.ParticipantContributions = clean
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	} else if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	if parsed.Summary == "" {
		return GroupDiscussionSummarizeResult{}, fmt.Errorf("empty summary")
	}
	return parsed, nil
}

func dedupeGroupDiscussionStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
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
	if len(result.Disagreements) > 0 {
		b.WriteString("\nDisagreements:\n")
		for _, item := range result.Disagreements {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	if len(result.OpenQuestions) > 0 {
		b.WriteString("\nOpen questions:\n")
		for _, item := range result.OpenQuestions {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	if len(result.ParticipantContributions) > 0 {
		b.WriteString("\nParticipant contributions:\n")
		for participant, contribution := range result.ParticipantContributions {
			b.WriteString("- ")
			b.WriteString(participant)
			b.WriteString(": ")
			b.WriteString(contribution)
			b.WriteString("\n")
		}
	}
	if result.Confidence > 0 {
		b.WriteString("\nConfidence: ")
		b.WriteString(fmt.Sprintf("%.2f", result.Confidence))
	}
	return strings.TrimSpace(b.String())
}

const groupDiscussionResultSummaryPrompt = `You synthesize a current-Hub MaClaw group discussion for the initiating MaClaw.
Return only JSON with this shape: {"summary":"...","rationale":"...","risks":["..."],"disagreements":["..."],"open_questions":["..."],"participant_contributions":{"agent_id":"..."},"confidence":0.0}.
Use only the provided topic, question, existing decision, and expert messages.
Do not invent private context, files, secrets, or external facts.
Keep the summary actionable and preserve important disagreements or caveats in rationale/risks.`

const groupDiscussionShardSummaryPrompt = `You summarize one shard of a current-Hub MaClaw group discussion.
Return only JSON with this shape: {"summary":"...","rationale":"...","risks":["..."],"disagreements":["..."],"open_questions":["..."],"confidence":0.0}.
Use only this shard. Preserve concrete objections, evidence, caveats, paths, commands, numbers, and rollback constraints.`

const groupDiscussionLayeredReducePrompt = `You synthesize shard summaries from a current-Hub MaClaw group discussion.
Return only JSON with this shape: {"summary":"...","rationale":"...","risks":["..."],"disagreements":["..."],"open_questions":["..."],"participant_contributions":{"agent_id":"..."},"confidence":0.0}.
Do not smooth away minority objections. If experts disagree, put the disagreement in disagreements and explain the practical consequence in rationale or risks.`

func (a *App) GroupDiscussionRejectInvite(inviteID string, resp a2a.GroupInvitationResponse) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	if resp.FromID == "" {
		resp.FromID = groupDiscussionAgentID(cfg)
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
		msg.FromID = groupDiscussionAgentID(cfg)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := client.SendDiscussionMessage(ctx, consultationID, msg); err != nil {
		return err
	}
	if shouldRefreshVEA2ADetailAfterSend(msg) {
		a.cacheVEA2ADetailAsync(client, consultationID, groupDiscussionAgentID(cfg))
	}
	return nil
}

func (a *App) GroupDiscussionSendHistoryMessage(consultationID string, msg a2a.GroupDiscussionMessage) error {
	detail, err := a.GroupDiscussionGetConsultationDetail(consultationID)
	if err != nil {
		return err
	}
	if !isWritableHistoryDiscussionSummary(detail.Discussion) {
		return fmt.Errorf("history discussion is read-only")
	}
	if cfg, cfgErr := a.LoadConfig(); cfgErr == nil {
		msg.ToIDs = normalizeGroupDiscussionHistoryTargetIDs(msg.ToIDs, groupDiscussionAgentID(cfg))
	}
	msg.FromID = ""
	msg.SessionID = consultationID
	return a.GroupDiscussionSendMessage(consultationID, msg)
}

func normalizeGroupDiscussionHistoryTargetIDs(toIDs []string, localID string) []string {
	if len(toIDs) == 0 {
		return nil
	}
	localID = strings.TrimSpace(localID)
	out := make([]string, 0, len(toIDs))
	seen := map[string]struct{}{}
	for _, rawID := range toIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if strings.EqualFold(id, "local-maclaw") && localID != "" {
			id = localID
		}
		key := strings.ToLower(id)
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

func isWritableHistoryDiscussionSummary(summary a2a.HubDiscussionSummary) bool {
	if summary.Readonly || !normalizeGroupDiscussionSessionStatus(summary.Status).IsOpen() {
		return false
	}
	relation := strings.ToLower(strings.TrimSpace(summary.LocalRelation))
	return relation == "initiated_by_me"
}

func (a *App) GroupDiscussionAddProposal(consultationID string, proposal a2a.Proposal) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	if strings.TrimSpace(proposal.AuthorID) == "" {
		proposal.AuthorID = groupDiscussionAgentID(cfg)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.AddDiscussionProposal(ctx, consultationID, proposal)
}

func (a *App) GroupDiscussionAddReview(consultationID string, review a2a.Review) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	if strings.TrimSpace(review.ReviewerID) == "" {
		review.ReviewerID = groupDiscussionAgentID(cfg)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.AddDiscussionReview(ctx, consultationID, review)
}

func (a *App) GroupDiscussionEscalate(consultationID string, escalation a2a.Escalation) error {
	client, cfg, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	escalation = normalizeGroupDiscussionEscalation(escalation, groupDiscussionAgentID(cfg))
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.EscalateDiscussion(ctx, consultationID, escalation)
}

func normalizeGroupDiscussionEscalation(escalation a2a.Escalation, fallbackRaisedBy string) a2a.Escalation {
	escalation.RaisedBy = strings.TrimSpace(firstNonEmptyGroupString(escalation.RaisedBy, fallbackRaisedBy))
	escalation.Reason = strings.TrimSpace(escalation.Reason)
	escalation.Target = strings.TrimSpace(escalation.Target)
	if escalation.Target == "" {
		escalation.Target = defaultGroupDiscussionEscalationTarget()
	}
	return escalation
}

func defaultGroupDiscussionEscalationTarget() string {
	return "human_owner"
}

func (a *App) GroupDiscussionDecide(consultationID string, decision a2a.Decision) error {
	client, _, err := a.groupDiscussionClient()
	if err != nil {
		return err
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	return client.DecideDiscussion(ctx, consultationID, decision)
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
		if !normalizeGroupDiscussionSessionStatus(discussion.Status).IsOpen() {
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
	Enabled                          bool                               `json:"enabled"`
	Discoverable                     bool                               `json:"discoverable"`
	ConfirmBeforeStart               bool                               `json:"confirm_before_start"`
	AllowSecurityGroupFreeDiscussion bool                               `json:"allow_security_group_free_discussion"`
	InvitePolicy                     string                             `json:"invite_policy,omitempty"`
	SecurityGroupID                  string                             `json:"security_group_id,omitempty"`
	ContextPolicy                    string                             `json:"context_policy,omitempty"`
	Profile                          *a2a.GroupProfile                  `json:"profile,omitempty"`
	Experts                          []a2a.GroupProfile                 `json:"experts,omitempty"`
	Discussions                      []a2a.HubDiscussionSummary         `json:"discussions,omitempty"`
	ActiveDiscussionCount            int                                `json:"active_discussion_count"`
	ReadyDiscussionCount             int                                `json:"ready_discussion_count"`
	WaitingDiscussionCount           int                                `json:"waiting_discussion_count"`
	StaleDiscussionCount             int                                `json:"stale_discussion_count"`
	PendingInvites                   []a2a.GroupInviteSummary           `json:"pending_invites,omitempty"`
	RecommendedFocusContext          map[string]interface{}             `json:"recommended_focus_context,omitempty"`
	RecommendedToolCall              *GroupDiscussionToolCallSuggestion `json:"recommended_tool_call,omitempty"`
	NonExecutingBoundary             string                             `json:"non_executing_boundary,omitempty"`
	Error                            string                             `json:"error,omitempty"`
}

func (a *App) GroupDiscussionStatus() GroupDiscussionStatus {
	cfg, err := a.LoadConfig()
	if err != nil {
		return groupDiscussionStatusWithSafeHandoff(GroupDiscussionStatus{Error: err.Error()})
	}
	status := GroupDiscussionStatus{Enabled: cfg.GroupDiscussion.Enabled, Discoverable: cfg.GroupDiscussion.Discoverable, ConfirmBeforeStart: cfg.GroupDiscussion.ConfirmBeforeStart, AllowSecurityGroupFreeDiscussion: cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion, InvitePolicy: cfg.GroupDiscussion.InvitePolicy, SecurityGroupID: cfg.GroupDiscussion.SecurityGroupID, ContextPolicy: cfg.GroupDiscussion.ContextPolicy}
	profile, profileErr := a2a.BuildGroupProfileFromConfig(cfg, time.Now())
	if profileErr == nil {
		status.Profile = &profile
	}
	if !cfg.GroupDiscussion.Enabled {
		return groupDiscussionStatusWithSafeHandoff(status)
	}
	client, err := a2a.NewHubClientFromConfig(cfg)
	if err != nil {
		status.Error = err.Error()
		return groupDiscussionStatusWithSafeHandoff(status)
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
			isOpen := normalizeGroupDiscussionSessionStatus(discussion.Status).IsOpen()
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
	if invites, err := client.ListInvites(ctx, groupDiscussionAgentID(cfg)); err == nil {
		status.PendingInvites = invites
	} else if status.Error == "" {
		status.Error = err.Error()
	}
	return groupDiscussionStatusWithSafeHandoff(status)
}

func groupDiscussionStatusWithSafeHandoff(status GroupDiscussionStatus) GroupDiscussionStatus {
	focusContext := groupDiscussionStatusFocusContext(status)
	status.RecommendedFocusContext = focusContext
	status.RecommendedToolCall = groupDiscussionStatusToolCall(focusContext)
	status.NonExecutingBoundary = groupDiscussionStatusNonExecutingBoundary
	status.RecommendedToolCall = normalizeGroupDiscussionSafeToolCall(status.RecommendedToolCall, status.RecommendedFocusContext, status.NonExecutingBoundary)
	return status
}
