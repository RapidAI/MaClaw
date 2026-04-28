package a2a

import (
	"testing"
	"time"
)

func TestSessionMajorityDecision(t *testing.T) {
	now := time.Date(2026, 4, 28, 1, 2, 3, 0, time.UTC)
	s, err := NewSession("a2a-1", "delivery exception", "pick a response plan", []Participant{{ID: "ops"}, {ID: "quality"}, {ID: "sales"}}, PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if err := s.AddProposal(Proposal{ID: "prop-1", AuthorID: "ops", Title: "fast repair", Content: "repair first, notify customer second", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal returned error: %v", err)
	}
	_ = s.AddReview(Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "ops", Position: ReviewApprove, CreatedAt: now})
	if s.PolicySatisfied("prop-1") {
		t.Fatal("one approval should not satisfy majority of three")
	}
	_ = s.AddReview(Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "quality", Position: ReviewApprove, CreatedAt: now})
	decision, err := s.TryDecide("dec-1", "prop-1", "repair first", now)
	if err != nil {
		t.Fatalf("TryDecide returned error: %v", err)
	}
	if s.Status != SessionDecided || decision.ProposalID != "prop-1" {
		t.Fatalf("decision state = %s %+v", s.Status, decision)
	}
}

func TestConcernBlocksDecision(t *testing.T) {
	now := time.Now()
	s, err := NewSession("a2a-2", "pricing", "choose discount", []Participant{{ID: "sales"}, {ID: "finance"}}, PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	_ = s.AddProposal(Proposal{ID: "prop-1", AuthorID: "sales", Title: "discount", Content: "offer 10 percent", CreatedAt: now})
	_ = s.AddReview(Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "sales", Position: ReviewApprove, CreatedAt: now})
	_ = s.AddReview(Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "finance", Position: ReviewConcern, CreatedAt: now})
	if _, err := s.TryDecide("dec-1", "prop-1", "discount", now); err == nil {
		t.Fatal("expected concern to block decision")
	}
}

func TestEscalationClosesLocalDecisionPath(t *testing.T) {
	s, err := NewSession("a2a-3", "budget", "approve spend", []Participant{{ID: "ops"}}, PolicyMajority, time.Now())
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if err := s.Escalate(Escalation{ID: "esc-1", RaisedBy: "ops", Reason: "budget threshold exceeded"}); err != nil {
		t.Fatalf("Escalate returned error: %v", err)
	}
	if s.Status != SessionEscalated || s.Escalation.Target != "iworkercenter" {
		t.Fatalf("unexpected escalation state: %+v", s.Escalation)
	}
	if err := s.AddMessage(Message{ID: "msg-1", FromID: "ops", Content: "late note"}); err == nil {
		t.Fatal("expected escalated session to reject new messages")
	}
}
