package llmservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestSetDefaultServiceGroupAndDeleteGuard(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &Registry{
		ServiceGroups: []llmpool.ServiceGroup{
			{ID: "redeem", Name: "Redeem"},
			{ID: "coding-auto", Name: "Coding"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDefaultServiceGroup(t.Context(), "missing"); err == nil {
		t.Fatal("missing group must not become default")
	}
	if err := svc.SetDefaultServiceGroup(t.Context(), "redeem"); err != nil {
		t.Fatal(err)
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.DefaultServiceGroupID != "redeem" {
		t.Fatalf("default = %q", reg.DefaultServiceGroupID)
	}
	if err := svc.DeleteServiceGroup(t.Context(), "redeem"); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("deleting default group err = %v", err)
	}
	if err := svc.SetDefaultServiceGroup(t.Context(), "coding-auto"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteServiceGroup(t.Context(), "redeem"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteOfficialServiceGroupRejected(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &Registry{
		ServiceGroups: []llmpool.ServiceGroup{
			{ID: llmpool.OfficialGroupID, Name: "MaClaw official"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteServiceGroup(t.Context(), llmpool.OfficialGroupID); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
		t.Fatalf("official delete err = %v", err)
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.ServiceGroups) != 1 || reg.ServiceGroups[0].ID != llmpool.OfficialGroupID {
		t.Fatalf("official group must remain, got %#v", reg.ServiceGroups)
	}
}