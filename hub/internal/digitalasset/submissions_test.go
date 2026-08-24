package digitalasset

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func testKnowledgePackage(title, kind, content string, labels []string, topicHint string) []byte {
	pkg := map[string]any{
		"manifest": map[string]any{
			"format":     "maclaw.knowledge.package",
			"title":      title,
			"package_id": "kxp_test",
		},
		"sources": []map[string]any{{
			"id":         "src_1",
			"kind":       kind,
			"title":      title,
			"content":    content,
			"labels":     labels,
			"topic_hint": topicHint,
		}},
	}
	b, _ := json.Marshal(pkg)
	return b
}

func TestSubmitApproveRejectExperience(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	biz, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "业务库", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers}, LibraryKind: LibraryKindBusiness,
	})
	if err != nil {
		t.Fatalf("create business lib: %v", err)
	}
	tech, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "技术库", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers}, LibraryKind: LibraryKindTechnical,
	})
	if err != nil {
		t.Fatalf("create technical lib: %v", err)
	}

	libs, err := svc.ListContributableLibraries(ctx, "tenant_a", "user@x.com", LibraryKindBusiness)
	if err != nil || len(libs) != 1 || libs[0].ID != biz.ID {
		t.Fatalf("contributable business=%+v err=%v", libs, err)
	}

	_, err = svc.SubmitExperience(ctx, SubmitExperienceInput{
		TenantID: "tenant_a", LibraryID: tech.ID, SubmitterUserID: "u1", SubmitterEmail: "user@x.com",
		Kind: SubmissionKindBusiness, Title: "SOP", Summary: "why org",
		PackageJSON: testKnowledgePackage("SOP", "document", "follow the process", nil, ""),
	})
	if err == nil {
		t.Fatal("expected kind mismatch")
	}

	row, err := svc.SubmitExperience(ctx, SubmitExperienceInput{
		TenantID: "tenant_a", LibraryID: biz.ID, SubmitterUserID: "u1", SubmitterEmail: "user@x.com",
		Kind: SubmissionKindBusiness, Title: "Onboarding SOP", Summary: "new hire checklist",
		PackageJSON: testKnowledgePackage("Onboarding SOP", "document", "Day 1 checklist for new hires.", nil, ""),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if row.Status != "submitted" || row.ItemCount != 1 {
		t.Fatalf("row=%+v", row)
	}

	if _, err := svc.WithdrawSubmission(ctx, "tenant_a", row.ID, "other"); err == nil {
		t.Fatal("expected withdraw forbidden")
	}

	codingHint, _ := json.Marshal(map[string]any{
		"category": "pattern", "scope": "universal", "trigger_condition": "nil pointer",
		"status": "candidate",
	})
	techRow, err := svc.SubmitExperience(ctx, SubmitExperienceInput{
		TenantID: "tenant_a", LibraryID: tech.ID, SubmitterUserID: "u1", SubmitterEmail: "user@x.com",
		Kind: SubmissionKindCoding, Title: "Nil check", Summary: "avoid panic",
		PackageJSON: testKnowledgePackage("Nil check", "coding_experience", "Always check err and nil before deref.", []string{"coding_experience", "experience_class=pattern"}, string(codingHint)),
	})
	if err != nil {
		t.Fatalf("submit coding: %v", err)
	}

	rejected, err := svc.RejectSubmission(ctx, ReviewSubmissionInput{
		TenantID: "tenant_a", SubmissionID: row.ID, Actor: "admin@x.com", Note: "too local",
	})
	if err != nil || rejected.Status != "rejected" {
		t.Fatalf("reject=%+v err=%v", rejected, err)
	}

	approved, err := svc.ApproveSubmission(ctx, ReviewSubmissionInput{
		TenantID: "tenant_a", SubmissionID: techRow.ID, Actor: "admin@x.com",
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != "approved" || approved.ImportJobID == "" {
		t.Fatalf("approved=%+v", approved)
	}
	got, err := svc.GetLibrary(ctx, "tenant_a", tech.ID)
	if err != nil || got.ContentRev < 1 {
		t.Fatalf("library after approve rev=%v err=%v", got, err)
	}
	hits, err := svc.SearchLibrary(ctx, "tenant_a", tech.ID, "nil pointer", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected imported coding experience to be searchable")
	}

	if _, err := svc.ApproveSubmission(ctx, ReviewSubmissionInput{
		TenantID: "tenant_a", SubmissionID: techRow.ID, Actor: "admin@x.com",
	}); err == nil {
		t.Fatal("expected conflict on second approve")
	}

	mine, total, err := svc.ListSubmissions(ctx, store.DigitalAssetSubmissionFilter{
		TenantID: "tenant_a", SubmitterUserID: "u1", Limit: 20,
	})
	if err != nil || total != 2 || len(mine) != 2 {
		t.Fatalf("list mine total=%d len=%d err=%v", total, len(mine), err)
	}
}

func TestSubmitRejectsEnterpriseEchoAndEmptySummary(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "业务库", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitExperience(ctx, SubmitExperienceInput{
		TenantID: "tenant_a", LibraryID: lib.ID, SubmitterUserID: "u1", SubmitterEmail: "user@x.com",
		Kind: SubmissionKindBusiness, Title: "x",
		PackageJSON: testKnowledgePackage("x", "document", "body", nil, ""),
	}); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("expected summary required, got %v", err)
	}
	if _, err := svc.SubmitExperience(ctx, SubmitExperienceInput{
		TenantID: "tenant_a", LibraryID: lib.ID, SubmitterUserID: "u1", SubmitterEmail: "user@x.com",
		Kind: SubmissionKindBusiness, Title: "echo", Summary: "no",
		PackageJSON: testKnowledgePackage("echo", "document", "body", []string{"enterprise_import_kind=knowledge_share"}, ""),
	}); err == nil {
		t.Fatal("expected enterprise echo rejection")
	}
}
