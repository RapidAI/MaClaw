package routingarch

import (
	"sort"
	"strings"
	"testing"
)

// zeroInvariantEvidence names the tests that hold each section 10.1 rate at
// zero. It is a register, not a second implementation of those tests: its
// value is that deleting or renaming the last enforcement of an invariant now
// fails here instead of passing silently.
var zeroInvariantEvidence = map[ZeroInvariant][]string{
	ZeroInvariantControlPlaneOverreach: {
		"gui:TestBuiltinDynamicGatewaysAreClassifiedAsControlPlane",
		"gui:TestManagedSemanticSurfaceRejectsLegacyBypassUnion",
		"gui:TestClosedManagedSemanticDefinitionsEmptyGrantsAdmitNothing",
		"gui:TestSemanticReplanSubsetRejectsWidenedAuthority",
		"gui:TestSemanticReplanBindingReplacementCannotAddParameterAuthority",
		"gui:TestManagedCallSurfaceParameterAuthorizationIsComplete",
		"corelib/tool:TestCatalogGateRejectsNonProvisionProviderWithProvisions",
		"corelib/tool:TestInvocationIssuerDoesNotAuthorizeBlockedPlanNodes",
		"corelib/tool:TestCanonicalizeInvocationArgumentsRejectsDuplicateUnknownAndReservedFields",
		"corelib/agentservice:TestReviewedHostAdaptersNeverPublishReservedOrOpenFields",
	},
	ZeroInvariantUnknownEffectReplay: {
		"corelib/tool:TestSemanticExecutionCoordinatorDeliveryOutboxDoesNotReplayUnknown",
		"corelib/tool:TestOutboxClaimIsCompareAndSetAndStaleConvergesUnknown",
		"corelib/tool:TestArtifactStoreClaimsOneTrustedDispatchAndDoesNotReplayUnknown",
		"corelib/tool:TestSQLiteArtifactStoreReconcilesStaleDispatchToUnknown",
		"corelib/tool:TestPlanExecutorRecordsUnknownProviderEffectWithoutReplay",
		"corelib/tool:TestHostCallJournalReconcilesInterruptedCallsToUnknown",
		"corelib/agentservice:TestLedgerDynamicExternalEffectCoordinatorDoesNotRedispatchAwaitingOperation",
		"corelib/agentservice:TestLedgerDynamicExternalEffectCoordinatorTransportFailureBecomesUnknown",
		"corelib/agentservice:TestLedgerDynamicExternalEffectCoordinatorLateReceiptResolvesUnknownWithoutRedispatch",
		"corelib/agentservice:TestSQLiteDynamicOperationLedgerPersistsAndReconcilesStaleRunning",
		"corelib/agentservice:TestCoreDynamicSemanticReceiptSourceReconcilesDurableOperationWithoutRedispatch",
		"corelib/agentservice:TestFireReviewedHostFileDeliverCASUnknownAndNoResend",
		"corelib/agentservice:TestFireReviewedHostMessageSendCASUnknownAndNoResend",
		"corelib/agentservice:TestFireReviewedHostScheduleDispatchCASUnknownAndNoResend",
	},
	ZeroInvariantDuplicateEffect: {
		"corelib/tool:TestOutboxClaimIsCompareAndSetAndStaleConvergesUnknown",
		"corelib/tool:TestArtifactStoreClaimsOneTrustedDispatchAndDoesNotReplayUnknown",
		"corelib/tool:TestArtifactStoreDeliveryOperationIsDestinationScoped",
		"corelib/tool:TestPlanExecutorRecordsPreparedExternalEffectAsAwaitingReceipt",
		"corelib/tool:TestHostCallJournalConcurrentAcquireHasOneAdmitter",
		"corelib/agentservice:TestDynamicOperationLedgerAcquireIsAtomic",
		"corelib/agentservice:TestLedgerDynamicExternalEffectCoordinatorDoesNotRedispatchAwaitingOperation",
	},
	ZeroInvariantStaleRevisionExecution: {
		"corelib/tool:TestMemoryRouteStateStorePublishesRevisionAndRetiresParent",
		"corelib/tool:TestSQLiteRouteStateStorePublishesRevisionAndRecoversLineage",
		"corelib/tool:TestRouteRevisionCompareAndPublishAllowsOneConcurrentChild",
		"corelib/tool:TestRouteRevisionSupersedesLegacyStateInSameAuthorityScope",
		"corelib/tool:TestDeliveryClaimFencedOffAfterNewRevision",
		"corelib/tool:TestDeliverySettleFencedOffAfterNewRevision",
		"corelib/tool:TestExternalEffectFencedOffAfterNewRevision",
		"corelib/tool:TestHostCallJournalReconcilesInterruptedCallsToUnknown",
		"corelib/agentservice:TestCoreDynamicSemanticBindingStalePublishesOneChildRevision",
		"gui:TestSemanticDynamicBindingStalePublishesRestrictedReplacementRevision",
		"gui:TestSemanticReplanCancellationNeverPublishesChildRevision",
	},
}

func repositoryTestFunctions(t *testing.T) map[string]TestFunction {
	t.Helper()
	found, err := ScanTestFunctions(repositoryRoot(t))
	if err != nil {
		t.Fatalf("scan test functions: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no test functions were found at all, the walk broke")
	}
	return found
}

// The register is worthless if it can name a test that does not exist, so a
// rename must fail here and be followed into the register.
func TestZeroInvariantEvidenceExists(t *testing.T) {
	found := repositoryTestFunctions(t)
	var missing []string
	for invariant, evidence := range zeroInvariantEvidence {
		for _, name := range evidence {
			if _, ok := found[name]; !ok {
				missing = append(missing, string(invariant)+" -> "+name)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("the zero-invariant register names %d tests that no longer exist:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// Every invariant needs evidence in more than one package: a single package's
// test can be satisfied by that package's own mock and still leave the
// end-to-end path unprotected.
func TestEveryZeroInvariantHasEvidenceInSeveralPackages(t *testing.T) {
	for _, invariant := range ZeroInvariants {
		evidence := zeroInvariantEvidence[invariant]
		if len(evidence) < 3 {
			t.Fatalf("invariant %q has only %d tests behind it", invariant, len(evidence))
		}
		packages := make(map[string]bool)
		seen := make(map[string]bool)
		for _, name := range evidence {
			if seen[name] {
				t.Fatalf("invariant %q lists %q twice", invariant, name)
			}
			seen[name] = true
			packages[name[:strings.Index(name, ":")]] = true
		}
		if len(packages) < 2 {
			t.Fatalf("invariant %q is only covered inside one package", invariant)
		}
	}
}

// The set is closed by the design, so a new constant must arrive with its
// evidence rather than as an empty entry.
func TestZeroInvariantSetIsComplete(t *testing.T) {
	if len(ZeroInvariants) != len(zeroInvariantEvidence) {
		t.Fatalf("the design lists %d zero invariants but the register holds %d",
			len(ZeroInvariants), len(zeroInvariantEvidence))
	}
	for _, invariant := range ZeroInvariants {
		if _, ok := zeroInvariantEvidence[invariant]; !ok {
			t.Fatalf("invariant %q has no evidence entry", invariant)
		}
	}
}

func TestScanTestFunctionsSkipsNonTestDeclarations(t *testing.T) {
	found := repositoryTestFunctions(t)
	for key, function := range found {
		if !strings.HasPrefix(function.Name, "Test") {
			t.Fatalf("scanner returned a non-test declaration %q", key)
		}
		if strings.HasPrefix(function.Package, ".") {
			t.Fatalf("scanner walked a tooling directory: %q", key)
		}
	}
	if _, ok := found["corelib/tool/routingarch:TestZeroInvariantEvidenceExists"]; !ok {
		t.Fatal("the scanner did not find this test file, so its own coverage is unproven")
	}
}
