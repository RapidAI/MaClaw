package skill

import (
	"strings"
	"testing"
)

func TestManageSkillDescription_ContainsAllActions(t *testing.T) {
	desc := ManageSkillDescription()
	for _, a := range ManageSkillActions {
		if !strings.Contains(desc, a.Name) {
			t.Errorf("ManageSkillDescription() missing action %q", a.Name)
		}
		if !strings.Contains(desc, a.Brief) {
			t.Errorf("ManageSkillDescription() missing brief for %q", a.Name)
		}
	}
}

func TestManageSkillActionSlash_ContainsAllActions(t *testing.T) {
	slash := ManageSkillActionSlash()
	for _, a := range ManageSkillActions {
		if !strings.Contains(slash, a.Name) {
			t.Errorf("ManageSkillActionSlash() missing %q", a.Name)
		}
	}
}

func TestManageSkillUnknownActionError_ContainsAllActions(t *testing.T) {
	msg := ManageSkillUnknownActionError("bogus")
	if !strings.Contains(msg, "bogus") {
		t.Error("error message should contain the invalid action name")
	}
	for _, a := range ManageSkillActions {
		if !strings.Contains(msg, a.Name) {
			t.Errorf("error message missing action %q", a.Name)
		}
	}
}

func TestManageSkillActionIsValid(t *testing.T) {
	for _, a := range ManageSkillActions {
		if !ManageSkillActionIsValid(a.Name) {
			t.Errorf("ManageSkillActionIsValid(%q) = false, want true", a.Name)
		}
	}
	if ManageSkillActionIsValid("nonexistent") {
		t.Error("ManageSkillActionIsValid(\"nonexistent\") = true, want false")
	}
	if ManageSkillActionIsValid("") {
		t.Error("ManageSkillActionIsValid(\"\") = true, want false")
	}
}

func TestManageSkillActionNames_Length(t *testing.T) {
	names := ManageSkillActionNames()
	if len(names) != len(ManageSkillActions) {
		t.Errorf("ManageSkillActionNames() returned %d names, want %d", len(names), len(ManageSkillActions))
	}
}

func TestManageSkillUploadDescriptionDocumentsAliases(t *testing.T) {
	desc := ManageSkillDescription()
	for _, want := range []string{
		`action="upload"`,
		"SkillMarket",
		"HubCenter",
		"publish",
		"pub",
		"submit",
		"发布",
		"上架",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("ManageSkillDescription() missing upload alias %q: %s", want, desc)
		}
	}
}

func TestNormalizeManageSkillActionUploadAliases(t *testing.T) {
	for _, action := range []string{"publish", "pub", "submit", "release", "发布", "發布", "上架", "提交"} {
		if got := NormalizeManageSkillAction(action); got != "upload" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want upload", action, got)
		}
	}
	if got := NormalizeManageSkillAction(" RUN "); got != "run" {
		t.Fatalf("NormalizeManageSkillAction trims/lowercases = %q, want run", got)
	}
	if got := NormalizeManageSkillAction("custom"); got != "custom" {
		t.Fatalf("NormalizeManageSkillAction unknown = %q, want custom", got)
	}
}

func TestNormalizeManageSkillActionInfoAliases(t *testing.T) {
	for _, action := range []string{"info", "inspect", "show", "describe", "get", "detail", "schema", "params"} {
		if got := NormalizeManageSkillAction(action); got != "info" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want info", action, got)
		}
	}
}

func TestNormalizeManageSkillActionEvolutionAliases(t *testing.T) {
	for _, action := range []string{"evolution", "evol_status", "self_repair_status", "optimize_status"} {
		if got := NormalizeManageSkillAction(action); got != "evolution_status" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want evolution_status", action, got)
		}
	}
	for _, action := range []string{"set_evolution", "evolution_enable", "enable_evolution", "disable_evolution", "set_skill_evolution"} {
		if got := NormalizeManageSkillAction(action); got != "set_evolution_enabled" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want set_evolution_enabled", action, got)
		}
	}
	for _, action := range []string{"evolution_audit", "evolution_log", "audit", "audit_log", "evolution_history", "skill_evolution_audit"} {
		if got := NormalizeManageSkillAction(action); got != "evolution_audit" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want evolution_audit", action, got)
		}
	}
	for _, action := range []string{"maintenance_drafts", "list_drafts", "review_drafts", "patch_drafts", "governance_drafts"} {
		if got := NormalizeManageSkillAction(action); got != "maintenance_drafts" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want maintenance_drafts", action, got)
		}
	}
	for _, action := range []string{"repair", "repair_now", "self_repair", "attempt_repair", "fix_skill"} {
		if got := NormalizeManageSkillAction(action); got != "trigger_repair" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want trigger_repair", action, got)
		}
	}
	for _, action := range []string{"optimize", "optimize_now", "trigger_opt", "improve_skill"} {
		if got := NormalizeManageSkillAction(action); got != "trigger_optimize" {
			t.Fatalf("NormalizeManageSkillAction(%q)=%q, want trigger_optimize", action, got)
		}
	}
}
