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
