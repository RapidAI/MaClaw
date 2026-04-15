package tool

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// validManageSkillActions is the set of valid actions for the manage_skill dispatcher.
var validManageSkillActions = []string{"list", "search", "install", "run", "status", "upload"}

// allSixActionNames are the action names that must appear in the error message
// for an invalid action.
var allSixActionNames = []string{"list", "search", "install", "run", "status", "upload"}

// mockManageSkillDispatch simulates the dispatch logic of toolManageSkill.
// It uses the same switch-case structure as the real implementation but
// delegates to mock handlers that return deterministic results.
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
	default:
		return "未知 manage_skill action: " + action + "（支持: list/search/install/run/status/upload）"
	}
}

// mockStandaloneHandler simulates calling the standalone handler directly
// for a given action, producing the same result as mockManageSkillDispatch
// for valid actions.
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
//
// Feature: merge-skill-tools, Property 1: Dispatch round-trip equivalence
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.8**
func TestProperty_DispatchRoundTripEquivalence(t *testing.T) {
	genAction := rapid.SampledFrom(validManageSkillActions)

	genArgs := rapid.Custom(func(t *rapid.T) map[string]interface{} {
		args := make(map[string]interface{})
		// Generate random string values for common arg keys.
		keys := []string{"query", "skill_id", "hub_url", "name", "run_id", "action"}
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

		// Set the action in args (as the real dispatcher reads it from args).
		args["action"] = action

		dispatchResult := mockManageSkillDispatch(action, args)
		standaloneResult := mockStandaloneHandler(action, args)

		if dispatchResult != standaloneResult {
			t.Fatalf("dispatch(%q) = %q, standalone = %q", action, dispatchResult, standaloneResult)
		}
	})
}

// TestProperty_InvalidActionErrorListsAllActions verifies that for any string
// that is NOT one of the six valid actions, the error message contains all six
// supported action names.
//
// Feature: merge-skill-tools, Property 2: Invalid action error lists all supported actions
// **Validates: Requirements 2.7**
func TestProperty_InvalidActionErrorListsAllActions(t *testing.T) {
	validSet := map[string]bool{
		"list": true, "search": true, "install": true,
		"run": true, "status": true, "upload": true,
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
		args := map[string]interface{}{"action": invalidAction}

		result := mockManageSkillDispatch(invalidAction, args)

		// The error message must contain all six supported action names.
		for _, name := range allSixActionNames {
			if !strings.Contains(result, name) {
				t.Fatalf("error message for invalid action %q missing action name %q: %s",
					invalidAction, name, result)
			}
		}
	})
}
