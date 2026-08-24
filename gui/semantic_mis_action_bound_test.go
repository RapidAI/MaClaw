package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func misSelection() tool.PlannedSelection {
	return tool.PlannedSelection{AdapterName: "mis_data"}
}

func misQuerySelection() tool.PlannedSelection {
	return tool.PlannedSelection{AdapterName: "mis_query"}
}

func misRequest(argsJSON string) tool.CanonicalRequest {
	return tool.CanonicalRequest{CanonicalJSON: []byte(argsJSON)}
}

// The finding this closes: the audit froze five identifier fields and an open
// payload, but `action` was a free string over ~130 actions, dispatched under
// one shared service token. The identifiers were bounded arguments of an
// unbounded verb.
func TestTheManagedBusinessDataSurfaceRefusesDestructiveActions(t *testing.T) {
	destructive := []string{
		"delete_record", "delete_dataset", "bulk_delete_records", "bulk_update_records",
		"restore_backup", "create_backup", "run_maintenance", "apply_schema_proposal",
		"upsert_fields", "batch_import_records", "export_records", "restore_record",
		"review_record_approval", "upsert_connector",
	}
	for _, action := range destructive {
		reason, refused := semanticManagedInvocationRefusal(misSelection(), misRequest(`{"action":"`+action+`","dataset_id":"any"}`))
		if !refused {
			t.Errorf("managed business-data surface admitted %q", action)
			continue
		}
		if reason != "mis_action_outside_managed_surface" {
			t.Errorf("action %q refused as %q", action, reason)
		}
	}
}

// The bound must not gut the family. These are what a business-data turn is
// invoked to do.
func TestTheManagedBusinessDataSurfaceStillServesItsFamily(t *testing.T) {
	for _, action := range []string{
		"resolve_intent", "query_records", "get_record", "list_business_objects",
		"validate_record", "upsert_record", "execute_business_action", "create_record_approval",
		"list_agent_transactions",
	} {
		if _, refused := semanticManagedInvocationRefusal(misSelection(), misRequest(`{"action":"`+action+`"}`)); refused {
			t.Errorf("managed business-data surface refused %q, which the family exists to do", action)
		}
	}
}

// Fail closed: an absent, empty, misspelled or unreadable action is not
// evidence that the call is inside the bound.
func TestAnUnprovableBusinessDataActionIsRefused(t *testing.T) {
	cases := map[string]string{
		"missing":    `{"dataset_id":"d1"}`,
		"empty":      `{"action":""}`,
		"misspelled": `{"action":"query_record"}`,
		"unreadable": `{"action":`,
	}
	for name, args := range cases {
		if _, refused := semanticManagedInvocationRefusal(misSelection(), misRequest(args)); !refused {
			t.Errorf("%s action was admitted", name)
		}
	}
}

// Casing and padding are not a way around the bound, and not a way to be
// refused for something the family does allow.
func TestTheBusinessDataBoundIgnoresCasingAndPadding(t *testing.T) {
	if _, refused := semanticManagedInvocationRefusal(misSelection(), misRequest(`{"action":"  Query_Records "}`)); refused {
		t.Error("a padded, capitalized allowed action was refused")
	}
	if _, refused := semanticManagedInvocationRefusal(misSelection(), misRequest(`{"action":" DELETE_DATASET "}`)); !refused {
		t.Error("a padded, capitalized destructive action slipped through")
	}
}

// The bound belongs to this one legacy multiplexer. Applying it by accident to
// a trusted adapter would refuse calls that were never in scope.
func TestTheBoundAppliesOnlyToTheMISMultiplexer(t *testing.T) {
	other := tool.PlannedSelection{AdapterName: semanticTrustedFileReadAdapter}
	if _, refused := semanticManagedInvocationRefusal(other, misRequest(`{"path":"a.go"}`)); refused {
		t.Fatal("the MIS action bound leaked onto an unrelated adapter")
	}
}

// A bare refusal on a hundred-action multiplexer invites the model to guess
// around the bound one name at a time.
func TestTheRefusalTellsTheModelWhatItMayAskFor(t *testing.T) {
	text := semanticManagedMISRefusalText("mis_data", "mis_action_outside_managed_surface")
	for _, allowed := range []string{"resolve_intent", "query_records", "upsert_record"} {
		if !strings.Contains(text, allowed) {
			t.Errorf("refusal text does not name the permitted action %q", allowed)
		}
	}
	if strings.Contains(text, "delete_dataset") {
		t.Error("refusal text advertises an action the surface refuses")
	}
}

// The read-only projection is classified EffectReadOnly and survives policy
// states that deny business-data mutation. That is only sound if it provably
// cannot mutate, so its bound is the read list alone — not the mutation
// actions the wider multiplexer is allowed.
func TestTheReadOnlyBusinessDataSurfaceCannotWrite(t *testing.T) {
	for _, action := range managedMISWriteActions {
		reason, refused := semanticManagedInvocationRefusal(misQuerySelection(), misRequest(`{"action":"`+action+`"}`))
		if !refused {
			t.Errorf("the read-only business-data surface admitted the write action %q", action)
			continue
		}
		if reason != "mis_action_outside_read_surface" {
			t.Errorf("action %q refused as %q, want mis_action_outside_read_surface", action, reason)
		}
	}
	// It must still serve every read the wider surface serves, or splitting
	// the family would have cost the query half its reach.
	for _, action := range managedMISReadActions {
		if _, refused := semanticManagedInvocationRefusal(misQuerySelection(), misRequest(`{"action":"`+action+`"}`)); refused {
			t.Errorf("the read-only business-data surface refused the read action %q", action)
		}
	}
}

func TestTheReadOnlyRefusalPointsAtTheOtherSurface(t *testing.T) {
	text := semanticManagedMISRefusalText("mis_query", "mis_action_outside_read_surface")
	if !strings.Contains(text, "query_records") {
		t.Error("the read-only refusal does not name what it does allow")
	}
	if strings.Contains(text, "upsert_record") {
		t.Error("the read-only refusal advertises a write action it refuses")
	}
}

// The two halves must stay disjoint in the right direction: every read action
// is reachable from both, and no write action is reachable from the query
// surface.
func TestTheWriteSurfaceIsAStrictSupersetOfTheReadSurface(t *testing.T) {
	for _, action := range managedMISReadActions {
		if !managedMISActionAllowed("mis_data", action) {
			t.Errorf("the wider surface lost the read action %q", action)
		}
	}
	for _, action := range managedMISWriteActions {
		if managedMISActionAllowed("mis_query", action) {
			t.Errorf("write action %q is reachable from the read-only surface", action)
		}
	}
}
