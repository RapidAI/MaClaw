package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func codingTurnPlan(t *testing.T, h *IMMessageHandler, turn string) (*semanticCallSurface, bool, error) {
	t.Helper()
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "改一下这个函数", "desktop", "root-"+turn, "turn-"+turn,
		&intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .98},
	)
	return surface, handled, err
}

// Shipping the dial must not move anything on its own.
func TestTheCodingFamilyIsOpenWithoutADial(t *testing.T) {
	t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", "")
	h := semanticCodingHandler(t, intent.LabelCoding)
	surface, handled, err := codingTurnPlan(t, h, "default-open")
	if err != nil || !handled || surface == nil {
		t.Fatalf("an undialed coding turn must still plan: handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
}

// Withdrawal returns the family to the behavior it had before it was migrated,
// which is refusal. The assertion is on the exit taken, not merely on failure.
func TestWithdrawingTheCodingFamilyRefusesTheTurnAsUnmapped(t *testing.T) {
	t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", "0")
	h := semanticCodingHandler(t, intent.LabelCoding)
	surface, handled, err := codingTurnPlan(t, h, "withdrawn")
	if err == nil {
		t.Fatal("a withdrawn coding turn must not plan a managed surface")
	}
	if surface != nil {
		t.Fatal("a withdrawn coding turn published a surface")
	}
	// handled=false would hand the turn to the legacy keyword/name router.
	// Withdrawing a managed surface is not permission to reopen the surface it
	// replaced, so the host must own the refusal.
	if !handled {
		t.Fatal("withdrawal fell through to the legacy router instead of failing closed")
	}
	want := fmt.Sprintf("unmapped capability label %q", intent.LabelCoding)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("withdrawn turn err = %q, want it to leave by the unmapped door (%s)", err, want)
	}
}

// A rolled-back family and a never-migrated family must be indistinguishable
// downstream. If these two exits ever diverge, some caller will learn to tell
// them apart and start treating one of them as recoverable.
func TestWithdrawalAndNonMigrationLeaveByTheSameDoor(t *testing.T) {
	unmigrated := semanticUnmigratedFixtureLabel(t)

	t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", "")
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, _, _, unmigratedErr := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "跑一下这个流程", "desktop", "root-unmigrated", "turn-unmigrated",
		&intent.ClassificationResult{Primary: unmigrated, Confidence: .98},
	)
	if unmigratedErr == nil {
		t.Fatalf("label %q is supposed to have no rule", unmigrated)
	}

	t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", "0")
	_, _, withdrawnErr := codingTurnPlan(t, h, "same-door")
	if withdrawnErr == nil {
		t.Fatal("withdrawn coding turn planned")
	}

	normalize := func(err error, label intent.IntentLabel) string {
		return strings.ReplaceAll(err.Error(), string(label), "<label>")
	}
	if got, want := normalize(withdrawnErr, intent.LabelCoding), normalize(unmigratedErr, unmigrated); got != want {
		t.Fatalf("withdrawal exit %q differs from non-migration exit %q", got, want)
	}
}

// The dial names one family. A blast radius wider than that would make it
// unusable in exactly the situation it exists for.
func TestWithdrawalTakesOnlyTheCodingFamily(t *testing.T) {
	t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", "0")
	h := semanticCodingHandler(t, intent.LabelCoding)

	for _, label := range semanticCodingFamilyLabels {
		if _, withdrawn := semanticWithdrawnCapabilityLabel(h, "user-1",
			intent.ClassificationResult{Primary: label, Confidence: .98}); !withdrawn {
			t.Fatalf("label %q belongs to the coding family but survived a full withdrawal", label)
		}
	}

	for label := range imSemanticIntentRuleSet {
		if isCodingFamilyLabel(label) {
			continue
		}
		if got, withdrawn := semanticWithdrawnCapabilityLabel(h, "user-1",
			intent.ClassificationResult{Primary: label, Confidence: .98}); withdrawn {
			t.Fatalf("withdrawing coding also took %q (reported %q)", label, got)
		}
	}
}

func isCodingFamilyLabel(label intent.IntentLabel) bool {
	for _, coding := range semanticCodingFamilyLabels {
		if label == coding {
			return true
		}
	}
	return false
}

// A typo in an operations env var must fail toward serving traffic. The
// opposite default would let a stray character withdraw the largest migrated
// family across a fleet.
func TestAnUnreadableDialDoesNotWithdrawAnything(t *testing.T) {
	for _, raw := range []string{"abc", "", "  ", "%"} {
		t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", raw)
		percent, _ := doctor.SemanticCodingPercentFromEnv()
		if percent != 100 {
			t.Fatalf("env %q resolved to %d%%, want the family left open", raw, percent)
		}
	}
}

// Sticky, or a single coding turn could be managed at the dispatcher and
// withdrawn at the planning gate.
func TestTheCodingDialIsStickyPerUser(t *testing.T) {
	t.Setenv("MACLAW_SEMANTIC_CODING_PERCENT", "37")
	h := semanticCodingHandler(t, intent.LabelCoding)
	first := semanticCodingFamilyOpen(h, "sticky-user-xyz")
	for i := 0; i < 20; i++ {
		if semanticCodingFamilyOpen(h, "sticky-user-xyz") != first {
			t.Fatal("the coding dial flapped for one user inside a turn")
		}
	}
}

// The two dials must not select the same population. Sharing a bucket would
// mean the first users withdrawn from coding are the users already held back
// from the shared loop — the group least likely to have surfaced the problem
// the withdrawal is responding to.
func TestTheCodingDialDoesNotReuseTheSharedLoopBuckets(t *testing.T) {
	disagreements := 0
	for i := 0; i < 500; i++ {
		user := fmt.Sprintf("user-%d", i)
		if doctor.SemanticCodingCanaryBucket(user) != doctor.SharedLoopCanaryBucket(user) {
			disagreements++
		}
	}
	// Independent hashes collide on about 1% of ids; identical ones never
	// disagree at all.
	if disagreements < 400 {
		t.Fatalf("only %d/500 ids land in different buckets; the two dials look correlated", disagreements)
	}
}

// 0 must reach every caller, including the identity-less desktop path. A
// safety valve with an exempt population is not a safety valve.
func TestAFullWithdrawalReachesTheAnonymousCaller(t *testing.T) {
	if doctor.SemanticCodingCanaryAllows("", 0) {
		t.Fatal("an empty userID escaped a 0% withdrawal")
	}
	if !doctor.SemanticCodingCanaryAllows("", 100) {
		t.Fatal("an empty userID was withdrawn at 100%")
	}
}
