package taskeval

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tool/surfaceeval"
)

// seedPath resolves a file in the shipped data/ directory relative to this
// test file.
func seedPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "data", name)
}

// TestRunFileSeedAdequate replays the fully adequate seed transcript: every
// catalog-known called tool is in the rendered surface for every eligible
// turn, and the chitchat turn is cost-only (excluded from the adequacy
// denominator but still surface-cost measured).
func TestRunFileSeedAdequate(t *testing.T) {
	res, err := RunFile(seedPath(t, "adequate-1.json"))
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if res.ID != "adequate-1" || res.Turns != 4 {
		t.Fatalf("id/turns=%q/%d, want adequate-1/4", res.ID, res.Turns)
	}
	if res.EligibleTurns != 3 || res.AdequateTurns != 3 {
		t.Fatalf("eligible/adequate=%d/%d, want 3/3", res.EligibleTurns, res.AdequateTurns)
	}
	if res.SurfaceAdequacy != 1.0 {
		t.Fatalf("surface_adequacy=%v, want 1.0", res.SurfaceAdequacy)
	}
	if res.ModelErrorCalls != 0 {
		t.Fatalf("model_error_calls=%d, want 0", res.ModelErrorCalls)
	}
	if len(res.InadequateTurns()) != 0 {
		t.Fatalf("inadequate turns=%v, want none", res.InadequateTurns())
	}
	// Turn 3 ("好的，谢谢") declares no calls: cost-only, not eligible, not
	// adequate, and it still got a surface measured.
	costOnly := res.TurnResults[2]
	if costOnly.Eligible || costOnly.Adequate {
		t.Fatalf("chitchat turn eligible/adequate=%v/%v, want false/false", costOnly.Eligible, costOnly.Adequate)
	}
	if costOnly.SurfaceSize == 0 || costOnly.BaselineSurfaceSize != len(surfaceeval.DefaultCatalog()) {
		t.Fatalf("chitchat turn cost not measured: %+v", costOnly)
	}
}

// TestRunFileSeedInadequate replays the seed transcript whose second turn
// called a tool the routed surface did not render: adequacy is 0.5 and the
// missing tool is reported.
func TestRunFileSeedInadequate(t *testing.T) {
	res, err := RunFile(seedPath(t, "inadequate-1.json"))
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if res.Turns != 2 || res.EligibleTurns != 2 || res.AdequateTurns != 1 {
		t.Fatalf("turns/eligible/adequate=%d/%d/%d, want 2/2/1", res.Turns, res.EligibleTurns, res.AdequateTurns)
	}
	if res.SurfaceAdequacy != 0.5 {
		t.Fatalf("surface_adequacy=%v, want 0.5", res.SurfaceAdequacy)
	}
	inad := res.InadequateTurns()
	if len(inad) != 1 || inad[0].UserText != "现在几点了" {
		t.Fatalf("inadequate turns=%+v, want the 现在几点了 turn", inad)
	}
	if len(inad[0].MissingTools) != 1 || inad[0].MissingTools[0] != "web_search" {
		t.Fatalf("missing_tools=%v, want [web_search]", inad[0].MissingTools)
	}
}

// TestRunFileSeedConditional verifies the simulated_intent path: with the
// UIC cache stub declaring the ssh intent, the fail-closed ssh tool is in
// the rendered surface and both turns are adequate.
func TestRunFileSeedConditional(t *testing.T) {
	res, err := RunFile(seedPath(t, "conditional-1.json"))
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if res.SurfaceAdequacy != 1.0 || res.AdequateTurns != 2 {
		t.Fatalf("adequacy=%v adequate=%d, want 1.0/2", res.SurfaceAdequacy, res.AdequateTurns)
	}
	for _, tr := range res.TurnResults {
		found := false
		for _, name := range tr.SelectedTools {
			if name == "ssh" {
				found = true
			}
		}
		if !found {
			t.Fatalf("turn %q selected=%v, want ssh activated by simulated_intent", tr.UserText, tr.SelectedTools)
		}
	}
}

// TestSimulatedIntentFailClosedWithoutStub is the negative control for the
// conditional seed: the same ssh call with no simulated_intent stays
// fail-closed, so the turn is eligible but inadequate.
func TestSimulatedIntentFailClosedWithoutStub(t *testing.T) {
	ds := &Dataset{Version: 1, Transcript: []Turn{{
		UserText:    "帮我连上测试服务器 10.0.0.1",
		CalledTools: []string{"ssh"},
	}}}
	res := Run(ds)
	if res.EligibleTurns != 1 || res.AdequateTurns != 0 {
		t.Fatalf("eligible/adequate=%d/%d, want 1/0 (ssh fail-closed without UIC stub)", res.EligibleTurns, res.AdequateTurns)
	}
	if res.SurfaceAdequacy != 0 {
		t.Fatalf("surface_adequacy=%v, want 0", res.SurfaceAdequacy)
	}
}

// fakeSelector is a deterministic TurnSelector for grading-math tests: it
// returns a fixed surface per turn index and records the turns it saw.
type fakeSelector struct {
	surfaces [][]string
	calls    int
}

func (f *fakeSelector) SurfaceFor(turn Turn) []string {
	idx := f.calls
	f.calls++
	if idx >= len(f.surfaces) {
		return nil
	}
	return f.surfaces[idx]
}

// TestGradeTurnModelError covers the model-error semantics: called tools
// that do not exist in the catalog are counted in ModelErrorCalls and
// excluded from the adequacy denominator — they can neither rescue nor
// dilute adequacy. This path is unreachable through RunFile (validation
// rejects unknown called_tools) but Run on a hand-built dataset must still
// grade honestly.
func TestGradeTurnModelError(t *testing.T) {
	ds := &Dataset{Version: 1, Transcript: []Turn{
		// One real call rendered + one hallucinated name: eligible and
		// adequate; the hallucination is counted but excluded.
		{UserText: "a", CalledTools: []string{"read_file", "not_a_real_tool"}},
		// Only hallucinated names: not eligible, not adequate, cost-only.
		{UserText: "b", CalledTools: []string{"not_a_real_tool"}},
		// One real call NOT rendered: eligible, inadequate.
		{UserText: "c", CalledTools: []string{"bash"}},
	}}
	sel := &fakeSelector{surfaces: [][]string{
		{"read_file"},
		{"read_file"},
		{"read_file"},
	}}
	res := RunWithSelector(ds, sel, "model-error")
	if res.ModelErrorCalls != 2 {
		t.Fatalf("model_error_calls=%d, want 2", res.ModelErrorCalls)
	}
	if res.EligibleTurns != 2 || res.AdequateTurns != 1 {
		t.Fatalf("eligible/adequate=%d/%d, want 2/1", res.EligibleTurns, res.AdequateTurns)
	}
	if res.SurfaceAdequacy != 0.5 {
		t.Fatalf("surface_adequacy=%v, want 0.5 (hallucinated-only turn excluded from denominator)", res.SurfaceAdequacy)
	}
	first := res.TurnResults[0]
	if !first.Adequate || len(first.UnknownCalledTools) != 1 || first.UnknownCalledTools[0] != "not_a_real_tool" {
		t.Fatalf("turn 1=%+v, want adequate with one model-error call", first)
	}
	second := res.TurnResults[1]
	if second.Eligible || second.Adequate || len(second.UnknownCalledTools) != 1 {
		t.Fatalf("turn 2=%+v, want cost-only with one model-error call", second)
	}
	third := res.TurnResults[2]
	if !third.Eligible || third.Adequate || len(third.MissingTools) != 1 || third.MissingTools[0] != "bash" {
		t.Fatalf("turn 3=%+v, want eligible+inadequate missing bash", third)
	}
}

// TestAggregateResults checks the cross-transcript rollup over the three
// shipped seeds: mean adequacy averages per-transcript adequacy (each
// transcript one vote), pure cost transcripts are excluded, and token
// savings fold across transcripts.
func TestAggregateResults(t *testing.T) {
	var results []*TranscriptResult
	wantAdequacy := map[string]float64{
		"adequate-1":    1.0,
		"inadequate-1":  0.5,
		"conditional-1": 1.0,
	}
	for name, want := range wantAdequacy {
		res, err := RunFile(seedPath(t, name+".json"))
		if err != nil {
			t.Fatalf("RunFile(%s): %v", name, err)
		}
		if res.SurfaceAdequacy != want {
			t.Fatalf("%s adequacy=%v, want %v", name, res.SurfaceAdequacy, want)
		}
		results = append(results, res)
	}
	agg := AggregateResults(results)
	if agg.Transcripts != 3 || agg.AdequacyEligibleTranscripts != 3 {
		t.Fatalf("transcripts/eligible=%d/%d, want 3/3", agg.Transcripts, agg.AdequacyEligibleTranscripts)
	}
	wantMean := (1.0 + 0.5 + 1.0) / 3.0
	if agg.MeanSurfaceAdequacy != wantMean {
		t.Fatalf("mean adequacy=%v, want %v", agg.MeanSurfaceAdequacy, wantMean)
	}
	if agg.MinSurfaceAdequacy != 0.5 {
		t.Fatalf("min adequacy=%v, want 0.5", agg.MinSurfaceAdequacy)
	}
	if agg.Turns != 4+2+2 {
		t.Fatalf("turns=%d, want 8", agg.Turns)
	}
	wantTokens := 0
	for _, r := range results {
		wantTokens += r.TotalEstTokens
	}
	if agg.TotalEstTokens != wantTokens {
		t.Fatalf("total est tokens=%d, want %d", agg.TotalEstTokens, wantTokens)
	}
	if agg.TotalBaselineEstTokens == 0 || agg.TokenSavings != 1.0-float64(agg.TotalEstTokens)/float64(agg.TotalBaselineEstTokens) {
		t.Fatalf("token rollup broken: baseline=%d savings=%v", agg.TotalBaselineEstTokens, agg.TokenSavings)
	}
}

// TestRunFileValidationErrors covers strict transcript validation: wrong
// version, empty transcript, empty user text, called tools outside the
// catalog, and malformed simulated_intent must all be rejected.
func TestRunFileValidationErrors(t *testing.T) {
	if _, err := RunFile(filepath.Join("data", "does-not-exist.json")); err == nil {
		t.Fatal("RunFile on missing file: want error")
	}
	sim := &surfaceeval.SimulatedIntent{Label: "ssh", Confidence: 0.9, ToolNames: []string{"ssh"}}
	bad := []Dataset{
		{Version: 2, Transcript: []Turn{{UserText: "x"}}},
		{Version: 1, Transcript: nil},
		{Version: 1, Transcript: []Turn{{UserText: ""}}},
		{Version: 1, Transcript: []Turn{{UserText: "x", CalledTools: []string{"not_in_catalog"}}}},
		{Version: 1, Transcript: []Turn{{UserText: "x", SimulatedIntent: &surfaceeval.SimulatedIntent{Label: "not_a_label", Confidence: 0.9, ToolNames: []string{"ssh"}}}}},
		{Version: 1, Transcript: []Turn{{UserText: "x", SimulatedIntent: &surfaceeval.SimulatedIntent{Label: "ssh", Confidence: 1.5, ToolNames: []string{"ssh"}}}}},
		{Version: 1, Transcript: []Turn{{UserText: "x", SimulatedIntent: &surfaceeval.SimulatedIntent{Label: "ssh", Confidence: 0.9, ToolNames: []string{"not_in_catalog"}}}}},
	}
	for i, ds := range bad {
		if err := validate(&ds); err == nil {
			t.Fatalf("dataset %d: validate want error, got nil", i)
		}
	}
	good := Dataset{Version: 1, Transcript: []Turn{
		{UserText: "x", CalledTools: []string{"ssh"}, SimulatedIntent: sim},
		{UserText: "y"},
	}}
	if err := validate(&good); err != nil {
		t.Fatalf("valid transcript rejected: %v", err)
	}
}

// TestDeterminism replays every seed twice and requires byte-identical
// results: the harness and the router replay must be fully deterministic.
func TestDeterminism(t *testing.T) {
	for _, name := range []string{"adequate-1.json", "inadequate-1.json", "conditional-1.json"} {
		first, err := RunFile(seedPath(t, name))
		if err != nil {
			t.Fatalf("RunFile(%s) run 1: %v", name, err)
		}
		second, err := RunFile(seedPath(t, name))
		if err != nil {
			t.Fatalf("RunFile(%s) run 2: %v", name, err)
		}
		b1, _ := json.Marshal(first)
		b2, _ := json.Marshal(second)
		if string(b1) != string(b2) {
			t.Fatalf("%s: two runs differ:\n%s\n%s", name, b1, b2)
		}
	}
}

// TestAggregateMinSet covers the 0-ambiguity guard on the aggregate minimum:
// MinSet is false (and Min 0) when no transcript had eligible turns, true
// otherwise — a bare 0 can then no longer be read as "measured 0".
func TestAggregateMinSet(t *testing.T) {
	// All-ineligible: two pure cost transcripts.
	costOnly := func() *TranscriptResult {
		return RunWithSelector(&Dataset{Version: 1, Transcript: []Turn{
			{UserText: "你好"},
		}}, nil, "cost")
	}
	agg := AggregateResults([]*TranscriptResult{costOnly(), costOnly()})
	if agg.MinSet {
		t.Fatal("MinSet must be false when no transcript is adequacy-eligible")
	}
	if agg.MinSurfaceAdequacy != 0 || agg.MeanSurfaceAdequacy != 0 || agg.AdequacyEligibleTranscripts != 0 {
		t.Fatalf("all-ineligible aggregate: %+v", agg)
	}

	// Mixed: an eligible transcript joins; Min becomes a real measurement.
	eligible := RunWithSelector(&Dataset{Version: 1, Transcript: []Turn{
		{UserText: "读一下 README.md", CalledTools: []string{"read_file"}},
	}}, nil, "eligible")
	agg = AggregateResults([]*TranscriptResult{costOnly(), eligible})
	if !agg.MinSet {
		t.Fatal("MinSet must be true once any transcript is adequacy-eligible")
	}
	if agg.MinSurfaceAdequacy != eligible.SurfaceAdequacy {
		t.Fatalf("min=%v, want %v", agg.MinSurfaceAdequacy, eligible.SurfaceAdequacy)
	}

	// Zero eligible turns but measured adequacy 0 elsewhere still counts as
	// set: a genuinely inadequate transcript sets Min to a real 0.
	inad := RunWithSelector(&Dataset{Version: 1, Transcript: []Turn{
		{UserText: "现在几点了", CalledTools: []string{"web_search"}},
	}}, nil, "inad")
	if inad.SurfaceAdequacy != 0 || inad.EligibleTurns == 0 {
		t.Fatalf("fixture must be an eligible 0-adequacy transcript: %+v", inad)
	}
	agg = AggregateResults([]*TranscriptResult{inad, eligible})
	if !agg.MinSet || agg.MinSurfaceAdequacy != 0 {
		t.Fatalf("measured 0 must set MinSet with Min 0: %+v", agg)
	}
}

// TestRunLeavesRoutingStatsUntouched verifies the offline harness does not
// pollute live telemetry: replaying the legacy router must leave the global
// corelib/tool routing counters exactly as before and restore the
// RecordRoutingStats switch.
func TestRunLeavesRoutingStatsUntouched(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() {
		_ = tool.ResetRoutingStats()
		maclawpath.SetBaseDir(oldBase)
	})
	if err := tool.ResetRoutingStats(); err != nil {
		t.Fatalf("reset routing stats: %v", err)
	}
	before := tool.GetRoutingStats()

	for _, name := range []string{"adequate-1.json", "inadequate-1.json", "conditional-1.json"} {
		if _, err := RunFile(seedPath(t, name)); err != nil {
			t.Fatalf("RunFile(%s): %v", name, err)
		}
	}

	after := tool.GetRoutingStats()
	if before != after {
		t.Fatalf("routing stats changed by offline eval: before=%+v after=%+v", before, after)
	}
	if !tool.RecordRoutingStats {
		t.Fatal("RecordRoutingStats must be restored after Run")
	}
}
