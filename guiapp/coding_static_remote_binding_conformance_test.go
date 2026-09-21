package guiapp

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingStaticRemoteBindingConformanceVersion is the hermetic conformance
// suite version for the remote-session binding rules. It must change exactly
// when codingStaticRemoteBindingVersion does; the equality test below is the
// machine evidence pinning suite to implementation, in the E1 discipline.
const codingStaticRemoteBindingConformanceVersion = "coding-remote-binding-v1"

func TestCodingStaticRemoteBindingVersionMatchesConformanceSuite(t *testing.T) {
	if codingStaticRemoteBindingVersion != codingStaticRemoteBindingConformanceVersion {
		t.Fatalf("binding implementation version %q does not match conformance suite version %q", codingStaticRemoteBindingVersion, codingStaticRemoteBindingConformanceVersion)
	}
}

// codingStaticRemoteBindingConformance records which binding properties the
// hermetic suite proves. It is evidence metadata, not a production gate.
type codingStaticRemoteBindingConformance struct {
	HasOpaqueHostOwnedHandle   bool
	HasCrossHandleInvalidation bool
	HasSwapRejection           bool
}

// codingStaticRemoteBindingConformanceCoverage maps every Has* field to the
// conformance tests that prove it. Function references make a missing or
// renamed test a compile error; the meta-test below makes an unmapped field
// a test failure.
var codingStaticRemoteBindingConformanceCoverage = map[string][]func(*testing.T){
	"HasOpaqueHostOwnedHandle":   {TestCodingStaticRemoteBindingOpaqueHandleAccepted},
	"HasCrossHandleInvalidation": {TestCodingStaticRemoteBindingCrossHandleInvalidation},
	"HasSwapRejection":           {TestCodingStaticRemoteBindingSwapRejection},
}

func TestCodingStaticRemoteBindingConformanceFieldsHaveCoverage(t *testing.T) {
	conformanceType := reflect.TypeOf(codingStaticRemoteBindingConformance{})
	hasFields := 0
	for i := 0; i < conformanceType.NumField(); i++ {
		field := conformanceType.Field(i)
		if !strings.HasPrefix(field.Name, "Has") || field.Type.Kind() != reflect.Bool {
			continue
		}
		hasFields++
		if len(codingStaticRemoteBindingConformanceCoverage[field.Name]) == 0 {
			t.Errorf("binding conformance field %s has no hermetic coverage", field.Name)
		}
	}
	if hasFields != 3 {
		t.Fatalf("binding conformance field count changed without updating the coverage map: %d", hasFields)
	}
	// Red line (pinned elsewhere in more detail): transport evidence never
	// enables alias materialization.
	cb := &remoteCodingCallbacks{}
	if cb.codingDynamicAliasesMayMaterialize() {
		t.Fatal("codingDynamicAliasesMayMaterialize must stay false")
	}
}

func remoteBindingPlanForHandle(t *testing.T, handle string) codingStaticPlanPreparation {
	t.Helper()
	envelope := codingStaticRemoteExecutionEnvelope{
		Identity: remoteShadowTestIdentity(),
		Session:  codingStaticRemoteSessionBinding{SessionHandle: handle, HostKind: "remote"},
		Posture:  codingRequestImplementation,
		Role:     codingRoleWorker,
	}
	prepared, err := prepareCodingStaticRemoteShadowPlan(envelope, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("prepare remote shadow plan: %v", err)
	}
	return prepared
}

// TestCodingStaticRemoteBindingOpaqueHandleAccepted: the binding treats the
// session handle as opaque — an arbitrary host-owned string with no URL,
// host address, model, or credential content completes the binding and
// binds the plan's selections. Authority comes only from the host having
// issued the handle, never from anything parseable inside it.
func TestCodingStaticRemoteBindingOpaqueHandleAccepted(t *testing.T) {
	const opaque = "opaque-host-handle-7f3d"
	prepared := remoteBindingPlanForHandle(t, opaque)
	if len(prepared.Plan.Selections) != 2 {
		t.Fatalf("selections = %d, want 2", len(prepared.Plan.Selections))
	}
	binding := codingStaticRemoteSessionBinding{SessionHandle: opaque, HostKind: "remote"}
	for _, selection := range prepared.Plan.Selections {
		if !codingStaticRemoteSelectionBoundToSession(selection, binding) {
			t.Fatalf("selection %q not bound to the opaque handle it was planned under", selection.AdapterName)
		}
	}
	// No URL/host/credential content was needed at any point.
	if strings.Contains(opaque, "/") || strings.Contains(opaque, "@") || strings.Contains(opaque, ":") {
		t.Fatalf("test handle unexpectedly carries parseable transport content: %q", opaque)
	}
}

// TestCodingStaticRemoteBindingCrossHandleInvalidation: rebind invalidates
// previously bound shadow-plan FitProofs at the plan/binding layer — every
// selection planned under H1 fails the binding check under H2, and vice
// versa. No network is involved: invalidation is exact-string identity, not
// a transport property.
func TestCodingStaticRemoteBindingCrossHandleInvalidation(t *testing.T) {
	preparedH1 := remoteBindingPlanForHandle(t, "handle-h1")
	preparedH2 := remoteBindingPlanForHandle(t, "handle-h2")
	bindingH1 := codingStaticRemoteSessionBinding{SessionHandle: "handle-h1", HostKind: "remote"}
	bindingH2 := codingStaticRemoteSessionBinding{SessionHandle: "handle-h2", HostKind: "remote"}
	for _, selection := range preparedH1.Plan.Selections {
		if !codingStaticRemoteSelectionBoundToSession(selection, bindingH1) {
			t.Fatalf("H1 selection %q not bound under its own handle", selection.AdapterName)
		}
		if codingStaticRemoteSelectionBoundToSession(selection, bindingH2) {
			t.Fatalf("H1 selection %q remained executable under H2 — rebind must invalidate", selection.AdapterName)
		}
	}
	for _, selection := range preparedH2.Plan.Selections {
		if codingStaticRemoteSelectionBoundToSession(selection, bindingH1) {
			t.Fatalf("H2 selection %q executable under H1 — rebind must invalidate", selection.AdapterName)
		}
	}
}

// TestCodingStaticRemoteBindingSwapRejection: a swapped/incomplete binding
// (wrong host kind or empty handle) fails the check even when the selection
// was planned under a complete binding — the check consults the LIVE binding,
// not the plan text.
func TestCodingStaticRemoteBindingSwapRejection(t *testing.T) {
	prepared := remoteBindingPlanForHandle(t, "handle-real")
	selection := prepared.Plan.Selections[0]
	swapped := codingStaticRemoteSessionBinding{SessionHandle: "handle-real", HostKind: "local"}
	if codingStaticRemoteSelectionBoundToSession(selection, swapped) {
		t.Fatal("local-host binding must never satisfy a remote selection check")
	}
	empty := codingStaticRemoteSessionBinding{}
	if codingStaticRemoteSelectionBoundToSession(selection, empty) {
		t.Fatal("empty binding must never satisfy a remote selection check")
	}
	wrongHandle := codingStaticRemoteSessionBinding{SessionHandle: "handle-other", HostKind: "remote"}
	if codingStaticRemoteSelectionBoundToSession(selection, wrongHandle) {
		t.Fatal("a different complete remote handle must not satisfy the selection check")
	}
}
