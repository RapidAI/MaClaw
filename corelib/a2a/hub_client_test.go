package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHubClientPublishExpertProfile(t *testing.T) {
	var gotPath, gotAuth, gotMachineID string
	var got GroupProfile
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotMachineID = r.Header.Get("X-Machine-ID")
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]GroupProfile{"profile": got})
	}))
	defer server.Close()

	client, err := NewHubClient(server.URL, WithHubBearerToken("tok"), WithHubMachineID(" machine-a "))
	if err != nil {
		t.Fatalf("NewHubClient: %v", err)
	}
	profile, err := client.PublishExpertProfile(context.Background(), GroupProfile{AgentID: " maclaw-a ", ModelClass: " frontier ", Discoverable: true, Available: true})
	if err != nil {
		t.Fatalf("PublishExpertProfile: %v", err)
	}
	if gotPath != "/api/a2a/expert-profile" || gotAuth != "Bearer tok" || gotMachineID != "machine-a" {
		t.Fatalf("path/auth/machine = %q %q %q", gotPath, gotAuth, gotMachineID)
	}
	if got.AgentID != "maclaw-a" || got.ModelClass != "frontier" || profile.AgentID != "maclaw-a" {
		t.Fatalf("profile not normalized: got=%+v returned=%+v", got, profile)
	}
}

func TestHubClientCreateConsultation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/consultations" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("X-Hub-Tenant-ID") != "" {
			t.Fatalf("A2A client must not send tenant headers: X-Tenant-ID=%q X-Hub-Tenant-ID=%q", r.Header.Get("X-Tenant-ID"), r.Header.Get("X-Hub-Tenant-ID"))
		}
		var req GroupConsultationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Question != "Which design is safer?" {
			t.Fatalf("question = %q", req.Question)
		}
		_ = json.NewEncoder(w).Encode(ConsultationCreateResponse{Discussion: HubDiscussionSummary{ID: "disc-1", Question: req.Question}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	out, err := client.CreateConsultation(context.Background(), GroupConsultationRequest{FromID: "maclaw-a", Question: "Which design is safer?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	if out.Discussion.ID != "disc-1" {
		t.Fatalf("discussion = %+v", out.Discussion)
	}
}

func TestHubClientListDiscussionsRoleQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/discussions/mine" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("role") != "participated" {
			t.Fatalf("role query = %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(DiscussionListResponse{Discussions: []HubDiscussionSummary{{ID: "disc-1", Role: "participated"}}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	items, err := client.ListDiscussions(context.Background(), "participated")
	if err != nil {
		t.Fatalf("ListDiscussions: %v", err)
	}
	if len(items) != 1 || items[0].ID != "disc-1" {
		t.Fatalf("items = %+v", items)
	}
}

func TestHubClientListDiscussionsAllOmitsRoleQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/discussions/mine" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
			t.Fatalf("participant_id = %q", got)
		}
		if got := r.URL.Query().Get("role"); got != "" {
			t.Fatalf("role query should be omitted for all, got %q raw=%q", got, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(DiscussionListResponse{Discussions: []HubDiscussionSummary{{ID: "disc-all"}}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	items, err := client.ListDiscussionsForAgent(context.Background(), "machine-1", "all")
	if err != nil {
		t.Fatalf("ListDiscussionsForAgent: %v", err)
	}
	if len(items) != 1 || items[0].ID != "disc-all" {
		t.Fatalf("items = %+v", items)
	}
}

func TestHubClientGetConsultationDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/consultations/disc-1/detail" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(HubDiscussionDetail{
			Discussion: HubDiscussionSummary{ID: "disc-1", Status: "open"},
			Messages:   []Message{{ID: "msg-1", SessionID: "disc-1", FromID: "maclaw-b", Kind: MessageAnswer, Content: "Prefer staged rollout"}},
		})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	detail, err := client.GetConsultationDetail(context.Background(), "disc-1")
	if err != nil {
		t.Fatalf("GetConsultationDetail: %v", err)
	}
	if detail.Discussion.ID != "disc-1" || len(detail.Messages) != 1 || detail.Messages[0].Content != "Prefer staged rollout" {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestHubClientGetConsultationDetailForAgentAddsParticipantQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/consultations/disc-1/detail" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
			t.Fatalf("participant_id = %q raw=%q", got, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(HubDiscussionDetail{Discussion: HubDiscussionSummary{ID: "disc-1", LocalRelation: "owned_ve_invited", Readonly: true}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	detail, err := client.GetConsultationDetailForAgent(context.Background(), "disc-1", "machine-1")
	if err != nil {
		t.Fatalf("GetConsultationDetailForAgent: %v", err)
	}
	if detail.Discussion.LocalRelation != "owned_ve_invited" || !detail.Discussion.Readonly {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestHubClientListInvitesByStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/invites/mine" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("to_id"); got != "maclaw-b" {
			t.Fatalf("to_id = %q raw=%q", got, r.URL.RawQuery)
		}
		if got := r.URL.Query().Get("status"); got != "all" {
			t.Fatalf("status = %q raw=%q", got, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(InviteListResponse{Invites: []GroupInviteSummary{{ID: "invite-1", Status: "reject", Reason: "offline"}}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	invites, err := client.ListInvitesByStatus(context.Background(), "maclaw-b", "all")
	if err != nil {
		t.Fatalf("ListInvitesByStatus: %v", err)
	}
	if len(invites) != 1 || invites[0].Status != "reject" || invites[0].Reason != "offline" {
		t.Fatalf("invites = %+v", invites)
	}
}

func TestHubClientListSentInvitesByStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/invites/mine" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("from_id"); got != "maclaw-a" {
			t.Fatalf("from_id = %q raw=%q", got, r.URL.RawQuery)
		}
		if got := r.URL.Query().Get("status"); got != "all" {
			t.Fatalf("status = %q raw=%q", got, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(InviteListResponse{Invites: []GroupInviteSummary{{ID: "invite-1", Status: "reject", Reason: "offline"}}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	invites, err := client.ListSentInvitesByStatus(context.Background(), "maclaw-a", "all")
	if err != nil {
		t.Fatalf("ListSentInvitesByStatus: %v", err)
	}
	if len(invites) != 1 || invites[0].Status != "reject" || invites[0].Reason != "offline" {
		t.Fatalf("invites = %+v", invites)
	}
}

func TestHubClientGetSentInviteFiltersByInviteID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/invites/mine" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("from_id"); got != "maclaw-a" {
			t.Fatalf("from_id = %q raw=%q", got, r.URL.RawQuery)
		}
		if got := r.URL.Query().Get("invite_id"); got != "invite-2" {
			t.Fatalf("invite_id = %q raw=%q", got, r.URL.RawQuery)
		}
		if got := r.URL.Query().Get("status"); got != "all" {
			t.Fatalf("status = %q raw=%q", got, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(InviteListResponse{Invites: []GroupInviteSummary{{ID: "invite-2", Status: "reject", Reason: "offline"}}})
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	invite, ok, err := client.GetSentInvite(context.Background(), "maclaw-a", "invite-2")
	if err != nil {
		t.Fatalf("GetSentInvite: %v", err)
	}
	if !ok || invite.ID != "invite-2" || invite.Status != "reject" || invite.Reason != "offline" {
		t.Fatalf("invite = %+v ok=%v", invite, ok)
	}
}

func TestHubClientInviteAndResultEndpoints(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/a2a/consultations/disc-1/invites" {
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-123"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	ctx := context.Background()
	inviteID, err := client.SendInvitation(ctx, "disc-1", GroupInvitation{ToID: "maclaw-b", Role: GroupRoleReview})
	if err != nil {
		t.Fatalf("SendInvitation: %v", err)
	}
	if inviteID != "invite-123" {
		t.Fatalf("inviteID = %q", inviteID)
	}
	if err := client.AcceptInvite(ctx, "invite-1", GroupInvitationResponse{FromID: "maclaw-b"}); err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	if err := client.SendDiscussionMessage(ctx, "disc-1", GroupDiscussionMessage{Content: "Prefer staged rollout", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SendDiscussionMessage: %v", err)
	}
	if err := client.SendDiscussionMessage(ctx, "disc-1", GroupDiscussionMessage{Kind: MessageStreamEnd, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SendDiscussionMessage stream_end: %v", err)
	}
	if err := client.SendDiscussionMessage(ctx, "disc-1", GroupDiscussionMessage{FileAttachments: []FileAttachment{{FileURL: "https://hub.local/files/doc", Filename: "doc.pdf"}}, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SendDiscussionMessage attachment-only: %v", err)
	}
	if err := client.AddDiscussionProposal(ctx, "disc-1", Proposal{AuthorID: "maclaw-a", Title: "Use staged rollout", Content: "Ship behind gates"}); err != nil {
		t.Fatalf("AddDiscussionProposal: %v", err)
	}
	if err := client.AddDiscussionReview(ctx, "disc-1", Review{ProposalID: "prop-1", ReviewerID: "maclaw-a", Position: ReviewApprove}); err != nil {
		t.Fatalf("AddDiscussionReview: %v", err)
	}
	if err := client.DecideDiscussion(ctx, "disc-1", Decision{ProposalID: "prop-1", Summary: "Use staged rollout"}); err != nil {
		t.Fatalf("DecideDiscussion: %v", err)
	}
	if err := client.EscalateDiscussion(ctx, "disc-1", Escalation{RaisedBy: "maclaw-a", Reason: "Needs owner decision"}); err != nil {
		t.Fatalf("EscalateDiscussion: %v", err)
	}
	if err := client.SubmitDiscussionResult(ctx, "disc-1", GroupDiscussionResult{Summary: "Use staged rollout"}); err != nil {
		t.Fatalf("SubmitDiscussionResult: %v", err)
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{
		"POST /api/a2a/consultations/disc-1/invites",
		"POST /api/a2a/invites/invite-1/accept",
		"POST /api/a2a/consultations/disc-1/messages",
		"POST /api/a2a/consultations/disc-1/proposals",
		"POST /api/a2a/consultations/disc-1/reviews",
		"POST /api/a2a/consultations/disc-1/decide",
		"POST /api/a2a/consultations/disc-1/escalate",
		"POST /api/a2a/consultations/disc-1/result",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in paths:\n%s", want, joined)
		}
	}
}

func TestHubClientDecodesHubError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"current hub only"}`))
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	_, err := client.ListExperts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "current hub only") {
		t.Fatalf("expected hub message, got %v", err)
	}
}

func TestHubClientDecodesPlainTextHubError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "participant employee-1 is not in discussion", http.StatusBadRequest)
	}))
	defer server.Close()

	client, _ := NewHubClient(server.URL)
	_, err := client.ListExperts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "participant employee-1 is not in discussion") {
		t.Fatalf("expected plain-text hub message, got %v", err)
	}
}
