package main

import (
	"fmt"
	"sort"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The managed call surface is the only thing a model can address on a governed
// turn. Individual adapters already assert their own forbidden fields, but a
// new adapter starts with no such assertion, so this gate enumerates the whole
// published surface and holds it against one reviewed baseline.

// Baseline reasons. Only the first one describes a closed design; the others
// record a legacy implementation that is still the published provider for its
// capability, and each says what has to exist before the entry can be deleted.
const (
	// The adapter resolves the value against the principal-bound workspace and
	// never hands the model's string to a provider as a host path.
	reasonWorkspaceConfinedLocation = "workspace-confined location resolved by the trusted adapter"
	// The remote target is the capability itself: acquiring or fetching cannot
	// be expressed without naming what to reach. The adapter validates the
	// scheme, rejects embedded credentials, and binds the destination itself.
	reasonCapabilityTargetURL = "remote target is the capability parameter, validated by the trusted adapter"
	// The legacy soup implementation is still the only provider for a managed
	// capability family. Delete once that family has a trusted adapter whose
	// schema carries only capability parameters.
	reasonLegacyManagedFamily = "legacy multiplexer still serves this managed family"
	// The same crossing on a projection that is classified read-only and held
	// to a read-only action bound before the adapter runs. It is still debt —
	// the identifier is still model-chosen and still unbound by the host — but
	// it cannot become a mutation, so it is tracked against its own ceiling
	// rather than pooled with the crossings that can.
	reasonReadOnlyLegacyFamily = "legacy multiplexer still serves this managed family, on a read-only projection"
)

// managedSchemaGateBaseline maps an adapter to the crossings that were reviewed
// and accepted, keyed by the schema pointer the gate reports. The value states
// why the crossing is tolerated and what has to change to delete the entry.
var managedSchemaGateBaseline = map[string]map[string]string{
	// The office writer takes a workspace-relative spreadsheet location that
	// trustedFileWriteResolvePath confines to the principal's workspace. Slide
	// image locations (2026-08-27: photos embedded into presentations) are
	// confined by the same rule — resolveOfficeSlideImages rewrites each one
	// through trustedFileWriteResolvePath and fails closed on escape or a
	// missing file before the deck renders.
	"semantic_write_trusted_office": {
		"path":                   reasonWorkspaceConfinedLocation,
		"slides[].images[].path": reasonWorkspaceConfinedLocation,
	},
	// The remote acquirer takes only the URL to acquire; the save path, request
	// headers and browser escalation the legacy downloader exposed are gone.
	"semantic_acquire_trusted_remote": {
		"url": reasonCapabilityTargetURL,
	},
	// business.data.mis is reachable from LabelBusinessData and is served by
	// the legacy MIS multiplexer: the model supplies every record identifier
	// and an open payload object. Unlike the other legacy families this one
	// cannot be closed by a single trusted adapter. app_id, blueprint_id and
	// dataset_id are query parameters the remote service authorizes itself, so
	// the host has nothing to bind them to.
	//
	// These stay frozen, but the reason has changed twice. The earliest note
	// treated the identifier fields as the exposure. They were not: `action`
	// was a free string over roughly a hundred and thirty actions, dispatched
	// under one app-level MIS token that is identical for every turn and every
	// IM user, so a managed turn could name delete_dataset or restore_backup
	// and be authorized for it. semanticManagedInvocationRefusal now bounds the
	// verb to a reviewed non-destructive list, which is what makes a
	// model-chosen record_id survivable.
	//
	// The identifiers cannot be unfrozen by decomposing the family, which the
	// previous note expected. They are frozen because MIS authorizes them
	// itself under a shared service account, so the host has no per-subject
	// authority to bind them against; splitting the capability does not give
	// it one. Deleting these entries needs per-subject credentials in the MIS
	// service, which is not a change on this side.
	"mis_data": {
		"app_id":             reasonLegacyManagedFamily,
		"blueprint_id":       reasonLegacyManagedFamily,
		"business_action_id": reasonLegacyManagedFamily,
		"dataset_id":         reasonLegacyManagedFamily,
		"record_id":          reasonLegacyManagedFamily,
		"data":               reasonLegacyManagedFamily,
	},
	// The read-only projection carries the same identifiers for the same
	// reason, but not the open `data` payload: there is nothing for a query to
	// write. That field is the one crossing the split actually removed.
	"mis_query": {
		"app_id":             reasonReadOnlyLegacyFamily,
		"blueprint_id":       reasonReadOnlyLegacyFamily,
		"business_action_id": reasonReadOnlyLegacyFamily,
		"dataset_id":         reasonReadOnlyLegacyFamily,
		"record_id":          reasonReadOnlyLegacyFamily,
	},
}

// managedCallSurfaceSchemas returns every model-facing invocation schema the
// planner can publish, across the channel and destination combinations that
// change publication.
func managedCallSurfaceSchemas(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	registry := NewToolRegistry()
	h := &IMMessageHandler{registry: registry}
	registerBuiltinTools(registry, h)
	schemas := make(map[string]map[string]interface{})
	for _, channel := range []string{"desktop", "lansenger"} {
		for _, destination := range []string{"", "private", "group"} {
			for _, registered := range registry.ListAvailable() {
				if semanticUnpublishedManagedProvider(registered, channel, destination) {
					continue
				}
				definition := registeredToolToDef(registered)
				if override, ok := semanticManagedDefinitionOverride(registered.Name); ok {
					definition = override
				}
				schema, err := semanticInvocationSchemaForRegisteredTool(registered, definition)
				if err != nil {
					t.Fatalf("invocation schema for %q: %v", registered.Name, err)
				}
				schemas[registered.Name] = schema
			}
		}
	}
	hostDefinitions := make(map[string]map[string]interface{})
	hostSchemas := make(map[string]map[string]interface{})
	var providers []tool.ProviderSpec
	if err := appendClosedHostSemanticProviders(&providers, hostDefinitions, hostSchemas, "lansenger", h); err != nil {
		t.Fatalf("closed host providers: %v", err)
	}
	for adapter, schema := range hostSchemas {
		schemas[adapter] = schema
	}
	if len(schemas) == 0 {
		t.Fatal("managed call surface is empty, the gate would pass vacuously")
	}
	return schemas
}

func TestManagedCallSurfaceHasNoUnreviewedParameterCrossing(t *testing.T) {
	var unreviewed []string
	for adapter, schema := range managedCallSurfaceSchemas(t) {
		reviewed := managedSchemaGateBaseline[adapter]
		for _, finding := range tool.InspectManagedInvocationSchema(schema) {
			if _, ok := reviewed[finding.Pointer]; ok {
				continue
			}
			unreviewed = append(unreviewed, fmt.Sprintf("%s: %s", adapter, finding))
		}
	}
	if len(unreviewed) > 0 {
		sort.Strings(unreviewed)
		t.Fatalf("managed call surface crosses the parameter closure at %d unreviewed sites:\n%s",
			len(unreviewed), joinLines(unreviewed))
	}
}

func TestManagedSchemaGateBaselineHasNoStaleEntries(t *testing.T) {
	schemas := managedCallSurfaceSchemas(t)
	var stale []string
	for adapter, reviewed := range managedSchemaGateBaseline {
		schema, published := schemas[adapter]
		if !published {
			stale = append(stale, adapter+": adapter is no longer published")
			continue
		}
		live := make(map[string]bool)
		for _, finding := range tool.InspectManagedInvocationSchema(schema) {
			live[finding.Pointer] = true
		}
		for pointer, reason := range reviewed {
			if reason == "" {
				stale = append(stale, adapter+" "+pointer+": baseline entry has no reason")
			}
			if !live[pointer] {
				stale = append(stale, adapter+" "+pointer+": crossing is gone, delete the entry")
			}
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("schema gate baseline is stale:\n%s", joinLines(stale))
	}
}

// A published adapter whose ParameterAuthorization cannot be derived, or whose
// allowed fields disagree with its schema, would let execution accept a shape
// the catalog never authorized.
func TestManagedCallSurfaceParameterAuthorizationIsComplete(t *testing.T) {
	for adapter, schema := range managedCallSurfaceSchemas(t) {
		authorization, err := tool.NewParameterAuthorization(schema)
		if err != nil {
			t.Fatalf("authorize %q: %v", adapter, err)
		}
		if authorization.Digest == "" || authorization.CanonicalizerVer == "" {
			t.Fatalf("%q authorization is not bound: %#v", adapter, authorization)
		}
		properties, _ := schema["properties"].(map[string]interface{})
		if len(authorization.AllowedFields) != len(properties) {
			t.Fatalf("%q allowed fields %v do not match its schema properties", adapter, authorization.AllowedFields)
		}
		for _, field := range authorization.AllowedFields {
			if _, declared := properties[field]; !declared {
				t.Fatalf("%q authorizes undeclared field %q", adapter, field)
			}
		}
	}
}

// The baseline is a debt register, so it may only shrink. A new managed family
// that needs an exception has to remove an old one or raise one of these
// numbers in a reviewed change.
//
// Mutating and read-only crossings are counted against separate ceilings
// rather than pooled. Pooling them would mean a read-only projection could be
// paid for by loosening the number that guards the crossings which can change
// business state, and the two are not interchangeable debt.
func TestManagedSchemaGateLegacySurfaceDoesNotGrow(t *testing.T) {
	const (
		reviewedLegacyCrossings         = 6
		reviewedReadOnlyLegacyCrossings = 5
	)
	closedByDesign := map[string]bool{
		reasonWorkspaceConfinedLocation: true,
		reasonCapabilityTargetURL:       true,
	}
	legacy, readOnly := 0, 0
	for _, reviewed := range managedSchemaGateBaseline {
		for _, reason := range reviewed {
			switch {
			case closedByDesign[reason]:
			case reason == reasonReadOnlyLegacyFamily:
				readOnly++
			default:
				legacy++
			}
		}
	}
	if legacy > reviewedLegacyCrossings {
		t.Fatalf("mutating legacy parameter crossings grew to %d, want at most %d", legacy, reviewedLegacyCrossings)
	}
	if readOnly > reviewedReadOnlyLegacyCrossings {
		t.Fatalf("read-only legacy parameter crossings grew to %d, want at most %d", readOnly, reviewedReadOnlyLegacyCrossings)
	}
}

// A read-only crossing is only a lesser debt while the surface carrying it is
// actually held to reads. If the bound stopped covering that adapter, the
// register would be understating what it tracks.
func TestAReadOnlyCrossingBelongsToASurfaceThatCannotMutate(t *testing.T) {
	for adapter, reviewed := range managedSchemaGateBaseline {
		readOnlyClaimed := false
		for _, reason := range reviewed {
			if reason == reasonReadOnlyLegacyFamily {
				readOnlyClaimed = true
				break
			}
		}
		if !readOnlyClaimed {
			continue
		}
		for _, action := range managedMISWriteActions {
			if managedMISActionAllowed(adapter, action) {
				t.Fatalf("%q is registered as a read-only crossing but admits the write action %q", adapter, action)
			}
		}
	}
}

func joinLines(items []string) string {
	out := ""
	for _, item := range items {
		out += "  " + item + "\n"
	}
	return out
}
