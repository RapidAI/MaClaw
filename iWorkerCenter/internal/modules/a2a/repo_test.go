package a2a

import (
	"path/filepath"
	"testing"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	centerdb "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func TestServicePersistsSessionsAcrossInstances(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "center.db")
	provider, err := centerdb.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := centerdb.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepo(provider.Write, provider.Read)
	svc := NewService(repo)
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{
		Topic:        "inventory exception",
		Goal:         "agree on recovery plan",
		OrgUnitID:    "supply-domain",
		Participants: []corea2a.Participant{{ID: "ops"}, {ID: "finance"}},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	updated, err := svc.AddProposal("tenant-a", session.ID, AddProposalRequest{AuthorID: "ops", Title: "expedite", Content: "expedite replenishment"})
	if err != nil {
		t.Fatalf("add proposal: %v", err)
	}
	proposalID := updated.Proposals[0].ID
	for _, reviewer := range []string{"ops", "finance"} {
		if _, err := svc.AddReview("tenant-a", session.ID, AddReviewRequest{ProposalID: proposalID, ReviewerID: reviewer, Position: corea2a.ReviewApprove}); err != nil {
			t.Fatalf("add review %s: %v", reviewer, err)
		}
	}
	if _, err := svc.Decide("tenant-a", session.ID, DecideRequest{ProposalID: proposalID, Summary: "expedite approved"}); err != nil {
		t.Fatalf("decide: %v", err)
	}

	restarted := NewService(repo)
	loaded, err := restarted.GetSession("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("load persisted session: %v", err)
	}
	if loaded.Status != corea2a.SessionDecided || loaded.Decision == nil || loaded.OrgUnitID != "supply-domain" {
		t.Fatalf("expected persisted decision, got %+v", loaded)
	}
	if len(loaded.Proposals) != 1 || len(loaded.Reviews) != 2 {
		t.Fatalf("expected proposal and reviews to persist, got %+v", loaded)
	}
}

func TestRepoKeepsTenantSessionsIsolated(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "center.db")
	provider, err := centerdb.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := centerdb.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := NewService(NewRepo(provider.Write, provider.Read))
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "pricing", OrgUnitID: "sales-domain", Participants: []corea2a.Participant{{ID: "sales"}}})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := svc.GetSession("tenant-b", session.ID); err == nil {
		t.Fatal("expected tenant-b to be unable to read tenant-a session")
	}
}

func TestRepoListsSessionsByOrgUnit(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "center.db")
	provider, err := centerdb.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := centerdb.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := NewService(NewRepo(provider.Write, provider.Read))
	for _, req := range []CreateSessionRequest{
		{Topic: "quality incident", OrgUnitID: "quality-domain", Participants: []corea2a.Participant{{ID: "qa"}}},
		{Topic: "sales exception", OrgUnitID: "sales-domain", Participants: []corea2a.Participant{{ID: "sales"}}},
	} {
		if _, err := svc.CreateSession("tenant-a", req); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}
	items, err := svc.ListSessions("tenant-a", ListSessionsFilter{OrgUnitID: "quality-domain"})
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 || items[0].OrgUnitID != "quality-domain" {
		t.Fatalf("unexpected org-unit list: %+v", items)
	}
}
