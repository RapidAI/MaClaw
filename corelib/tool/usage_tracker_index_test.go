package tool

import (
	"math/rand"
	"strconv"
	"testing"
	"time"
)

// TestUsageTrackerIndexEquivalence checks the per-tool index path against the
// full-scan fallback on randomized history, including recovery tools.
func TestUsageTrackerIndexEquivalence(t *testing.T) {
	tr, err := NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(11))
	tools := []string{"bash", "ssh", "read", "grep", "web", "edit"}
	followUps := []string{"", "retry", "abandon", "continue"}
	now := time.Now()
	for i := 0; i < 800; i++ {
		rec := ToolExperience{
			ToolName:    tools[r.Intn(len(tools))],
			QueryTokens: []string{"tok" + strconv.Itoa(r.Intn(8)), "shared"},
			Success:     r.Intn(3) != 0,
			FollowUp:    followUps[r.Intn(len(followUps))],
			Timestamp:   now.Add(-time.Duration(r.Intn(1200)) * time.Hour),
		}
		if r.Intn(5) == 0 {
			rec.RecoveryTool = tools[r.Intn(len(tools))]
		}
		tr.RecordExperience(rec)
	}
	q := []string{"tok1", "shared", "tok3"}
	for _, tool := range append(append([]string{}, tools...), "never-used") {
		expIdx := tr.ExperienceScore(tool, q)
		outIdx := tr.ContextOutcomeScore(tool, q)
		globIdx := tr.OutcomeScore(tool)
		hintIdx := tr.ExplainRoutingHintAdjustment(tool, q)

		tr.mu.Lock()
		tr.indexLen = -1 // force the fallback scan
		tr.mu.Unlock()

		expFull := tr.ExperienceScore(tool, q)
		outFull := tr.ContextOutcomeScore(tool, q)
		globFull := tr.OutcomeScore(tool)
		hintFull := tr.ExplainRoutingHintAdjustment(tool, q)

		tr.mu.Lock()
		tr.indexLen = len(tr.records)
		tr.mu.Unlock()

		// ExperienceScore's recency factor reads time.Now(), so two successive
		// calls differ in the last bits even over an identical record set.
		if d := expIdx - expFull; d > 1e-9 || d < -1e-9 {
			t.Fatalf("%s experience: index %v full %v", tool, expIdx, expFull)
		}
		if outIdx != outFull {
			t.Fatalf("%s outcome: index %v full %v", tool, outIdx, outFull)
		}
		if globIdx != globFull {
			t.Fatalf("%s global: index %v full %v", tool, globIdx, globFull)
		}
		if hintIdx.Adjustment != hintFull.Adjustment ||
			hintIdx.MatchingRecords != hintFull.MatchingRecords ||
			hintIdx.RecoveryEvidence != hintFull.RecoveryEvidence {
			t.Fatalf("%s hint: index %+v full %+v", tool, hintIdx, hintFull)
		}
	}
}
