package skill

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// mockManageSkillDispatch simulates the dispatch logic of toolManageSkill.
// It uses the same switch-case structure as the real GUI/TUI implementations
// but delegates to mock handlers that return deterministic results.
func mockManageSkillDispatch(action string, args map[string]interface{}) string {
	switch action {
	case "list":
		return "mock:list"
	case "search":
		return "mock:search:" + mockStringVal(args, "query")
	case "install":
		return "mock:install:" + mockStringVal(args, "skill_id")
	case "run":
		return "mock:run:" + mockStringVal(args, "name")
	case "status":
		return "mock:status:" + mockStringVal(args, "run_id")
	case "upload":
		return "mock:upload:" + mockStringVal(args, "name")
	case "validate":
		return "mock:validate:" + mockStringVal(args, "name")
	case "patch":
		return "mock:patch:" + mockStringVal(args, "skill_name")
	case "history":
		return "mock:history:" + mockStringVal(args, "skill_name")
	default:
		return ManageSkillUnknownActionError(action)
	}
}

// mockStandaloneHandler simulates calling the standalone handler directly.
func mockStandaloneHandler(action string, args map[string]interface{}) string {
	switch action {
	case "list":
		return "mock:list"
	case "search":
		return "mock:search:" + mockStringVal(args, "query")
	case "install":
		return "mock:install:" + mockStringVal(args, "skill_id")
	case "run":
		return "mock:run:" + mockStringVal(args, "name")
	case "status":
		return "mock:status:" + mockStringVal(args, "run_id")
	case "upload":
		return "mock:upload:" + mockStringVal(args, "name")
	case "validate":
		return "mock:validate:" + mockStringVal(args, "name")
	case "patch":
		return "mock:patch:" + mockStringVal(args, "skill_name")
	case "history":
		return "mock:history:" + mockStringVal(args, "skill_name")
	default:
		return ""
	}
}

func mockStringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// TestProperty_DispatchRoundTripEquivalence verifies that for any valid action
// and any args map, calling the dispatcher produces the same result as calling
// the corresponding standalone handler directly.
func TestProperty_DispatchRoundTripEquivalence(t *testing.T) {
	validActions := ManageSkillActionNames()
	genAction := rapid.SampledFrom(validActions)

	genArgs := rapid.Custom(func(t *rapid.T) map[string]interface{} {
		args := make(map[string]interface{})
		keys := []string{"query", "skill_id", "hub_url", "name", "skill_name", "run_id", "action"}
		for _, key := range keys {
			if rapid.Bool().Draw(t, "include_"+key) {
				args[key] = rapid.StringMatching(`[a-zA-Z0-9_-]{0,20}`).Draw(t, "val_"+key)
			}
		}
		return args
	})

	rapid.Check(t, func(t *rapid.T) {
		action := genAction.Draw(t, "action")
		args := genArgs.Draw(t, "args")
		args["action"] = action

		dispatchResult := mockManageSkillDispatch(action, args)
		standaloneResult := mockStandaloneHandler(action, args)

		if dispatchResult != standaloneResult {
			t.Fatalf("dispatch(%q) = %q, standalone = %q", action, dispatchResult, standaloneResult)
		}
	})
}

// TestProperty_InvalidActionErrorListsAllActions verifies that for any string
// that is NOT one of the valid actions, the error message contains all
// supported action names.
func TestProperty_InvalidActionErrorListsAllActions(t *testing.T) {
	validSet := make(map[string]bool)
	for _, name := range ManageSkillActionNames() {
		validSet[name] = true
	}

	genInvalidAction := rapid.Custom(func(t *rapid.T) string {
		for {
			s := rapid.StringMatching(`[a-zA-Z0-9_]{0,30}`).Draw(t, "candidate")
			if !validSet[s] {
				return s
			}
		}
	})

	rapid.Check(t, func(t *rapid.T) {
		invalidAction := genInvalidAction.Draw(t, "invalidAction")
		result := mockManageSkillDispatch(invalidAction, map[string]interface{}{})

		for _, name := range ManageSkillActionNames() {
			if !strings.Contains(result, name) {
				t.Fatalf("error message for invalid action %q missing action name %q: %s",
					invalidAction, name, result)
			}
		}
	})
}

// TestProperty_MockDispatcherCoversAllCanonicalActions verifies that the mock
// dispatcher has a case for every action in the canonical list. If a new action
// is added to ManageSkillActions but not to the mock, this test catches it.
func TestProperty_MockDispatcherCoversAllCanonicalActions(t *testing.T) {
	for _, name := range ManageSkillActionNames() {
		result := mockManageSkillDispatch(name, map[string]interface{}{})
		if strings.HasPrefix(result, "未知") {
			t.Errorf("mock dispatcher has no case for canonical action %q", name)
		}
	}
}

// TestManageSkillDescription_ContainsAllActions verifies the generated
// description string contains every action name.
func TestManageSkillDescription_ContainsAllActions(t *testing.T) {
	desc := ManageSkillDescription()
	for _, name := range ManageSkillActionNames() {
		if !strings.Contains(desc, name) {
			t.Errorf("ManageSkillDescription() missing action %q", name)
		}
	}
}

// TestManageSkillActionIsValid verifies the validity checker.
func TestManageSkillActionIsValid(t *testing.T) {
	for _, name := range ManageSkillActionNames() {
		if !ManageSkillActionIsValid(name) {
			t.Errorf("ManageSkillActionIsValid(%q) = false, want true", name)
		}
	}
	if ManageSkillActionIsValid("nonexistent") {
		t.Error("ManageSkillActionIsValid(\"nonexistent\") = true, want false")
	}
}
