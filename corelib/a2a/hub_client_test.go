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
	var gotPath, gotAuth string
	var got GroupProfile
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]GroupProfile{"profile": got})
	}))
	defer server.Close()

	client, err := NewHubClient(server.URL, WithHubBearerToken("tok"))
	if err != nil {
		t.Fatalf("NewHubClient: %v", err)
	}
	profile, err := client.PublishExpertProfile(context.Background(), GroupProfile{AgentID: " maclaw-a ", ModelClass: " frontier ", Discoverable: true, Available: true})
	if err != nil {
		t.Fatalf("PublishExpertProfile: %v", err)
	}
	if gotPath != "/api/a2a/expert-profile" || gotAuth != "Bearer tok" {
		t.Fatalf("path/auth = %q %q", gotPath, gotAuth)
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
