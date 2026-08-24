package main

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Action bound for the managed projection of business.data.mis.
//
// The parameter-authorization audit recorded six out-of-bounds fields on this
// family (app_id, blueprint_id, business_action_id, dataset_id, record_id and
// an open data object) and froze them, on the grounds that the remote MIS
// service authorizes those identifiers itself and the host has nothing to bind
// them to. Both halves of that are true. What the record missed is that the
// identifiers were never the widest door.
//
// `action` is a free-form string. The legacy multiplexer dispatches roughly a
// hundred and thirty of them, including delete_record, bulk_delete_records,
// delete_dataset, apply_schema_proposal, run_maintenance and restore_backup.
// And the outbound call does not carry the requesting subject's credential: it
// carries one app-level token plus static tenant/user/role headers from
// Settings, identical for every turn and every IM user. So a managed turn
// could name any action against any dataset under whatever authority that one
// token holds. Freezing the identifier fields while leaving that open was
// bounding the arguments of an unbounded verb.
//
// This closes the verb. The identifiers stay model-chosen — they still cannot
// be bound to anything host-side — but their reach is now the reach of this
// list.

// managedMISReadActions are discovery and query actions. They cannot change
// business state, so a model-chosen dataset_id or record_id here costs at most
// a read the configured token was already entitled to make.
var managedMISReadActions = []string{
	"status", "get_capabilities",
	"list_domains", "get_domain",
	"list_business_objects", "list_app_installations", "get_app_installation",
	"resolve_object_role", "list_relationships", "resolve_intent",
	"list_business_actions", "get_business_action",
	"list_datasets", "get_dataset", "list_fields",
	"query_records", "get_record", "aggregate_records",
	"list_record_revisions", "get_record_timeline", "get_related_records",
	"list_business_views", "get_business_view", "query_business_view",
	"list_dashboards", "get_dashboard", "run_dashboard",
	"list_reports", "get_report", "run_report",
	"get_inbox", "get_inbox_summary", "get_stats",
	"list_agent_transactions",
	"list_record_approvals", "get_record_approval",
	"mis.approval.list", "mis.approval.get", "mis.approval.list_by_record",
	"mis.approval.my_inbox", "mis.approval.my_pending", "mis.approval.my_requests",
	"mis.approval.pending_my_approval", "mis.approval.handled", "mis.approval.attention",
}

// managedMISWriteActions are the business-level writes the family exists to
// perform — filing a record, running a defined business action, raising an
// approval. They are bounded and individually reversible by a human.
//
// execute_business_action is the widest entry that remains, because a business
// action is defined server-side and this host cannot see what a given one
// does. It stays because removing it would leave the family unable to do the
// thing users invoke it for. Narrowing it further needs the action contracts,
// which live in the MIS service.
var managedMISWriteActions = []string{
	"validate_record",
	"upsert_record",
	"execute_business_action",
	"ingest_event",
	"create_record_approval",
}

// Deliberately absent, and why:
//
//   - Destructive record and schema operations (delete_record, delete_dataset,
//     bulk_delete_records, bulk_update_records, restore_record, upsert_fields,
//     propose_schema, apply_schema_proposal): irreversible or schema-wide under
//     a shared service token.
//   - Backup and maintenance (create_backup, restore_backup, download_backup,
//     run_maintenance, bootstrap_templates): infrastructure, not business data.
//   - Bulk movement (batch_import_records, import_records_*, export_*, the
//     import/export job actions): a single call moves an unbounded set.
//   - Connector and event-plumbing actions: integration configuration, whose
//     blast radius is other systems.
//   - Approval decisions (review_record_approval,
//     update_record_approval_progress): approving is the control the approval
//     workflow exists to impose. A model may raise one; ruling on it is not a
//     business-data outcome.
//   - Audit reads (list_audit_logs, export_audit_logs_csv): governance
//     inspection is its own capability, not part of this family.
//
// An unmanaged legacy turn still reaches all of them. This bound is a property
// of the managed projection, exactly like the narrowed screenshot schema.

var managedMISActionSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(managedMISReadActions)+len(managedMISWriteActions))
	for _, action := range managedMISReadActions {
		set[action] = struct{}{}
	}
	for _, action := range managedMISWriteActions {
		set[action] = struct{}{}
	}
	return set
}()

var managedMISReadActionSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(managedMISReadActions))
	for _, action := range managedMISReadActions {
		set[action] = struct{}{}
	}
	return set
}()

// managedMISAdapterActions is the bound for one adapter. mis_query is the
// read-only projection, so its bound is the read list alone: the capability it
// serves is classified EffectReadOnly and survives policy states that deny
// mutation, which is only sound if it provably cannot mutate.
func managedMISAdapterActions(adapterName string) (map[string]struct{}, bool) {
	switch adapterName {
	case "mis_query":
		return managedMISReadActionSet, true
	case "mis_data":
		return managedMISActionSet, true
	default:
		return nil, false
	}
}

func managedMISActionAllowed(adapterName, action string) bool {
	allowed, ok := managedMISAdapterActions(adapterName)
	if !ok {
		return false
	}
	_, permitted := allowed[strings.ToLower(strings.TrimSpace(action))]
	return permitted
}

func managedMISAllowedActionList(adapterName string) []string {
	allowed, ok := managedMISAdapterActions(adapterName)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(allowed))
	for action := range allowed {
		out = append(out, action)
	}
	sort.Strings(out)
	return out
}

// semanticManagedInvocationRefusal is the last gate before a managed selection
// reaches a legacy multiplexer whose own schema is wider than the capability.
//
// It is enforcement, not advice. The parameter canonicalizer closes the set of
// field *names*; it has no notion of an enumerated value, so a schema listing
// the permitted actions would be a suggestion the executor never checks.
func semanticManagedInvocationRefusal(selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) (string, bool) {
	if _, bounded := managedMISAdapterActions(selection.AdapterName); !bounded {
		return "", false
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		// An unreadable request cannot be shown to be inside the bound.
		return "mis_action_unreadable", true
	}
	action, _ := args["action"].(string)
	if managedMISActionAllowed(selection.AdapterName, action) {
		return "", false
	}
	if selection.AdapterName == "mis_query" {
		return "mis_action_outside_read_surface", true
	}
	return "mis_action_outside_managed_surface", true
}

// semanticManagedMISRefusalText tells the model what it may ask for instead.
// A bare refusal on a hundred-action multiplexer invites the model to guess
// its way around the bound one name at a time.
func semanticManagedMISRefusalText(adapterName, reason string) string {
	allowed := managedMISAllowedActionList(adapterName)
	if len(allowed) == 0 {
		return "[system rejected] " + reason
	}
	switch reason {
	case "mis_action_outside_read_surface":
		return "[system rejected] " + reason +
			": this tool is the read-only business-data surface and covers only these actions: " +
			strings.Join(allowed, ", ") +
			". Writing a record, running a business action or raising an approval needs the separate business-data tool on this turn."
	case "mis_action_outside_managed_surface":
		return "[system rejected] " + reason +
			": this turn's business-data capability covers only these actions: " +
			strings.Join(allowed, ", ") +
			". Destructive, bulk, schema, backup, connector and approval-decision actions are not part of it. " +
			"If the request needs one of those, say so and stop."
	default:
		return "[system rejected] " + reason
	}
}
