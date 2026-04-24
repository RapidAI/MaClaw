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
