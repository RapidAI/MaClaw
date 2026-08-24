package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestPublishedExternalEffectHostAdaptersHaveAReceiptBoundary walks the host
// adapters this channel actually publishes and requires the executor to
// recognise every external-effect one.
//
// The failure it prevents is silent and end-to-end. Publication, planning and
// rendering all succeed for such an adapter, so the model is shown a tool and
// calls it; only then does the receipt-boundary guard refuse, because
// "external effect" with no recognised observer must never reach a legacy
// handler. The result is a family that looks wired up and cannot execute, which
// is how ssh, browser and computer use sat before repo mutation was added and
// this inventory was written.
//
// Deriving the list from the published providers rather than naming adapters is
// the point: publishing a new external-effect adapter without teaching the
// executor how its outcome is observed fails here, at the moment it is
// published.
func TestPublishedExternalEffectHostAdaptersHaveAReceiptBoundary(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	// Several host adapters publish only when their host is reachable. Wiring
	// the seams makes this inventory cover the conditional families too, which
	// are exactly the ones whose breakage is hardest to notice.
	h.semanticTrustedSSH = func(string, string) (string, error) { return "", nil }
	h.semanticTrustedBrowser = func(string, string, string) (string, error) { return "", nil }
	h.semanticTrustedComputerUse = func(string, string) (string, error) { return "", nil }
	h.semanticTrustedDelegate = func(string, string) (string, error) { return "", nil }
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "", nil }
	h.semanticTrustedRepoMutate = func(string, string, string) (string, error) { return "", nil }
	registerBuiltinTools(h.registry, h)

	var providers []tool.ProviderSpec
	definitions := map[string]map[string]interface{}{}
	schemas := map[string]map[string]interface{}{}
	if err := appendClosedHostSemanticProviders(&providers, definitions, schemas, "desktop", h); err != nil {
		t.Fatalf("publish host providers: %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("no host providers published; the inventory stopped covering anything")
	}

	checked := 0
	for _, provider := range providers {
		external := false
		for _, effect := range provider.Effects {
			if effect == tool.EffectExternalEffect {
				external = true
			}
		}
		if !external {
			continue
		}
		checked++
		selection := tool.PlannedSelection{
			Provider:    provider.Binding,
			AdapterName: provider.AdapterName,
			Effects:     provider.Effects,
		}
		if semanticHostObservedExternalSelection(selection) {
			continue
		}
		if semanticCurrentChannelArtifactDelivery(selection) || semanticScheduleChannelDispatch(selection) {
			continue
		}
		t.Errorf("host adapter %q publishes an external effect that no receipt boundary recognises; "+
			"it will plan, render, and then fail closed at execution", provider.AdapterName)
	}
	if checked == 0 {
		t.Fatal("no external-effect host adapter was examined; the inventory is vacuous")
	}
}

// TestIndeterminateHostOutcomeIsReportedAsUnknownNotSuccess pins the
// distinction the whole receipt design rests on.
//
// A trusted host adapter marks an effect it could not observe with a
// "[system unknown]" result: an SSH session that dropped mid-command, a host
// that vanished, a push whose remote could not be read back. The executor used
// to test only for the failure prefixes, so an unknown fell through to the
// success return and the plan recorded PlanExecutionSucceeded. That is the
// worst of the three answers to report, because the model is told the effect
// landed when nobody knows whether it did.
//
// Unknown must also stay distinct from failure. Failure means the effect did
// not happen and a retry is legitimate; unknown consumes the grant precisely so
// a retry cannot commit the same push or command a second time.
func TestIndeterminateHostOutcomeIsReportedAsUnknownNotSuccess(t *testing.T) {
	selection := tool.PlannedSelection{
		ID:          "selection:repo-mutate-unknown",
		Provider:    tool.ProviderBinding{Kind: "builtin", ProviderID: "host", ImplementationID: semanticTrustedRepoMutateImplementation},
		AdapterName: semanticTrustedRepoMutateAdapter,
		Effects:     []tool.EffectClass{tool.EffectExternalEffect},
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedRepoMutate = func(string, string, string) (string, error) {
		return "", errTrustedRepoPushReceiptUnknownFixture{}
	}
	callback := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "user-1",
		semanticSurface: &semanticCallSurface{
			parameterSchemas: map[string]map[string]interface{}{
				selection.AdapterName: semanticTrustedRepoMutateInvocationSchema(),
			},
		},
	}
	result := callback.executeBoundSemanticSelectionCanonical(selection, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"action":"push"}`),
		Values:        map[string]interface{}{"action": "push"},
	})
	if result.Succeeded {
		t.Fatalf("an unobservable push was reported as success: %+v", result)
	}
	if !result.Unknown {
		t.Fatalf("an unobservable push was not marked unknown: %+v", result)
	}
	if result.AwaitingReceipt {
		t.Fatalf("an unobservable push must not claim a receipt is still coming: %+v", result)
	}
}

type errTrustedRepoPushReceiptUnknownFixture struct{}

func (errTrustedRepoPushReceiptUnknownFixture) Error() string {
	return "trusted_repo_mutate_push_receipt_unknown"
}

// TestHostObservedExternalIsKeyedOnAdapterIdentity keeps the allow-list from
// degrading into "any builtin that declares an external effect". The declaration
// says what an implementation can do, not that anyone watched it happen, so an
// adapter the host does not observe must stay outside the boundary.
func TestHostObservedExternalIsKeyedOnAdapterIdentity(t *testing.T) {
	unknown := tool.PlannedSelection{
		Provider:    tool.ProviderBinding{Kind: "builtin", ProviderID: "host"},
		AdapterName: "some_future_external_adapter",
		Effects:     []tool.EffectClass{tool.EffectExternalEffect},
	}
	if semanticHostObservedExternalSelection(unknown) {
		t.Fatal("an unrecognised builtin external adapter must not be treated as observed")
	}
	notBuiltin := tool.PlannedSelection{
		Provider:    tool.ProviderBinding{Kind: "mcp", ProviderID: "server-a"},
		AdapterName: semanticTrustedRepoMutateAdapter,
		Effects:     []tool.EffectClass{tool.EffectExternalEffect},
	}
	if semanticHostObservedExternalSelection(notBuiltin) {
		t.Fatal("a non-builtin provider must not inherit the host observation boundary by adapter name")
	}
	localOnly := tool.PlannedSelection{
		Provider:    tool.ProviderBinding{Kind: "builtin", ProviderID: "host"},
		AdapterName: semanticTrustedRepoMutateAdapter,
		Effects:     []tool.EffectClass{tool.EffectSensitive},
	}
	if semanticHostObservedExternalSelection(localOnly) {
		t.Fatal("the boundary must not apply to a selection that declares no external effect")
	}
}
