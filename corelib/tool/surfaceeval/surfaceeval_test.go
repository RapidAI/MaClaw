package surfaceeval

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestRunIrrelevantRouterYieldsZeroRecall builds a dataset whose catalog has
// nothing relevant to the user text (English-only descriptions vs a pure
// Chinese time query) and no bootstrap/fallback names, so the router selects
// none of the expected tools: recall must be 0 and the sample must appear in
// Failures.
func TestRunIrrelevantRouterYieldsZeroRecall(t *testing.T) {
	ds := &Dataset{
		Version: 1,
		Tools: []CatalogTool{
			{Name: "ssh", Description: "Remote shell access to a server over the SSH protocol."},
			{Name: "database", Description: "Run SQL queries against the business database."},
		},
		Samples: []Sample{
			{ID: "miss-1", UserText: "现在几点了", ExpectTools: []string{"ssh"}, Categories: []string{"miss"}},
		},
	}
	report := Run(ds)
	if len(report.Results) != 1 {
		t.Fatalf("results=%d, want 1", len(report.Results))
	}
	res := report.Results[0]
	if res.Recall != 0 {
		t.Fatalf("recall=%v, want 0 (selected=%v)", res.Recall, res.SelectedTools)
	}
	if len(res.SelectedTools) != 0 {
		t.Fatalf("selected=%v, want empty surface", res.SelectedTools)
	}
	failures := report.Failures()
	if len(failures) != 1 || failures[0].ID != "miss-1" {
		t.Fatalf("failures=%v, want [miss-1]", failures)
	}
	if report.MeanRecall != 0 || report.MinRecall != 0 {
		t.Fatalf("mean/min recall=%v/%v, want 0/0", report.MeanRecall, report.MinRecall)
	}
}

// TestBaselineMatchesCatalogSize checks that the full-catalog baseline metrics
// are internally consistent for every sample: baseline surface size equals
// the catalog size and the baseline token estimate equals the sum of the
// per-tool estimates over the whole catalog.
func TestBaselineMatchesCatalogSize(t *testing.T) {
	ds := &Dataset{
		Version: 1,
		Samples: []Sample{
			{ID: "s1", UserText: "帮我连上服务器 10.0.0.1", ExpectTools: []string{"ssh"}},
			{ID: "s2", UserText: "你好", ExpectTools: []string{}},
		},
	}
	report := Run(ds)
	catalog := DefaultCatalog()
	wantTokens := 0
	for _, tool := range catalog {
		wantTokens += int(float64(len(tool.Name)+len(tool.Description))/4.0 + 0.5)
	}
	if report.CatalogSize != len(catalog) {
		t.Fatalf("catalog size=%d, want %d", report.CatalogSize, len(catalog))
	}
	for _, res := range report.Results {
		if res.BaselineSurfaceSize != len(catalog) {
			t.Fatalf("sample %q baseline_surface_size=%d, want catalog size %d", res.ID, res.BaselineSurfaceSize, len(catalog))
		}
		if res.BaselineEstTokens != wantTokens {
			t.Fatalf("sample %q baseline_est_tokens=%d, want %d", res.ID, res.BaselineEstTokens, wantTokens)
		}
		if res.SurfaceSize > res.BaselineSurfaceSize {
			t.Fatalf("sample %q surface_size=%d exceeds baseline %d", res.ID, res.SurfaceSize, res.BaselineSurfaceSize)
		}
	}
	if report.TotalBaselineEstTokens != len(ds.Samples)*wantTokens {
		t.Fatalf("total baseline tokens=%d, want %d", report.TotalBaselineEstTokens, len(ds.Samples)*wantTokens)
	}
}

// TestRunFileSeedDataset runs the shipped baseline dataset: it must parse and
// validate cleanly and cover every sample.
func TestRunFileSeedDataset(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "data", "baseline-v1.json")
	report, err := RunFile(path)
	if err != nil {
		t.Fatalf("RunFile(%q): %v", path, err)
	}
	if report.SamplesRun != 12 {
		t.Fatalf("samples_run=%d, want 12", report.SamplesRun)
	}
	if report.CatalogSize != len(DefaultCatalog()) {
		t.Fatalf("catalog_size=%d, want default catalog %d", report.CatalogSize, len(DefaultCatalog()))
	}
	if len(report.Results) != report.SamplesRun {
		t.Fatalf("results=%d, want %d", len(report.Results), report.SamplesRun)
	}
	if report.RecallEligible != 10 {
		t.Fatalf("recall_eligible=%d, want 10 (2 cost-only samples)", report.RecallEligible)
	}
	for _, res := range report.Results {
		if res.ID == "" || res.BaselineSurfaceSize != report.CatalogSize {
			t.Fatalf("malformed result: %+v", res)
		}
	}
	// Known baseline findings, asserted so regressions in the harness (not the
	// router) stand out. ssh-connect-1 declares a simulated_intent (UIC cache
	// stub), so the fail-closed ssh tool is activated; lowercase glob resolves
	// through the reviewed provision alias; and web_search is retrieved via
	// its bilingual BuiltinEnrichments aliases. The seed failure set is EMPTY:
	// assert that explicitly so any new failure stands out.
	if failures := report.Failures(); len(failures) != 0 {
		ids := make([]string, 0, len(failures))
		for _, f := range failures {
			ids = append(ids, f.ID)
		}
		t.Fatalf("failures=%v, want none (seed baseline is fully covered)", ids)
	}
	for _, res := range report.Results {
		if res.ID == "ssh-connect-1" {
			if res.Recall != 1.0 {
				t.Fatalf("ssh-connect-1 recall=%v, want 1.0 with simulated_intent (selected=%v)", res.Recall, res.SelectedTools)
			}
			found := false
			for _, name := range res.SelectedTools {
				if name == "ssh" {
					found = true
				}
			}
			if !found {
				t.Fatalf("ssh-connect-1 selected=%v, want ssh activated by the UIC stub", res.SelectedTools)
			}
		}
	}
}

// TestEmptyExpectExcludedFromAverages checks that samples with empty
// expect_tools are pure cost-measurement samples: they run, but they do not
// enter recall averages, min recall or Failures.
func TestEmptyExpectExcludedFromAverages(t *testing.T) {
	ds := &Dataset{
		Version: 1,
		Tools: []CatalogTool{
			{Name: "read_file", Description: "Read a local file from disk."},
		},
		Samples: []Sample{
			{ID: "cost-1", UserText: "现在几点了", ExpectTools: []string{}},
			{ID: "cost-2", UserText: "好的谢谢", ExpectTools: nil},
		},
	}
	report := Run(ds)
	if report.SamplesRun != 2 {
		t.Fatalf("samples_run=%d, want 2", report.SamplesRun)
	}
	if report.RecallEligible != 0 {
		t.Fatalf("recall_eligible=%d, want 0", report.RecallEligible)
	}
	if report.MeanRecall != 0 || report.MinRecall != 0 {
		t.Fatalf("mean/min recall=%v/%v, want 0/0 when no sample is eligible", report.MeanRecall, report.MinRecall)
	}
	if len(report.Failures()) != 0 {
		t.Fatalf("failures=%v, want none for cost-only samples", report.Failures())
	}
	for _, res := range report.Results {
		if !res.RecallEligible && res.Recall != 1.0 {
			t.Fatalf("sample %q recall=%v, want 1.0 (empty expect convention)", res.ID, res.Recall)
		}
	}
}

// TestRunFileValidationErrors covers the strict dataset validation: unknown
// expected tools, duplicate sample ids, wrong versions and missing files must
// all be rejected with an error rather than silently mis-measured.
func TestRunFileValidationErrors(t *testing.T) {
	if _, err := RunFile(filepath.Join("data", "does-not-exist.json")); err == nil {
		t.Fatal("RunFile on missing file: want error")
	}
	bad := []Dataset{
		{Version: 2, Samples: []Sample{{ID: "a", UserText: "x"}}},
		{Version: 1, Samples: []Sample{
			{ID: "a", UserText: "x"},
			{ID: "a", UserText: "y"},
		}},
		{Version: 1, Samples: []Sample{{ID: "a", UserText: "x", ExpectTools: []string{"not_in_catalog"}}}},
		{Version: 1, Samples: []Sample{{ID: "a", UserText: ""}}},
	}
	for i, ds := range bad {
		if err := validate(&ds); err == nil {
			t.Fatalf("dataset %d: validate want error, got nil", i)
		}
	}
}

// TestSimulatedIntentActivatesConditionalTool verifies the UIC cache stub:
// a sample declaring simulated_intent for the fail-closed ssh tool gets ssh
// selected (recall 1.0), while the same wording without simulated_intent
// stays fail-closed (recall 0). Both runs share the default catalog.
func TestSimulatedIntentActivatesConditionalTool(t *testing.T) {
	withUIC := &Dataset{
		Version: 1,
		Samples: []Sample{{
			ID:              "ssh-1",
			UserText:        "帮我连上服务器 10.0.0.1",
			ExpectTools:     []string{"ssh"},
			SimulatedIntent: &SimulatedIntent{Label: "ssh", Confidence: 0.9, ToolNames: []string{"ssh"}},
		}},
	}
	report := Run(withUIC)
	res := report.Results[0]
	if res.Recall != 1.0 {
		t.Fatalf("recall=%v, want 1.0 (selected=%v)", res.Recall, res.SelectedTools)
	}
	found := false
	for _, name := range res.SelectedTools {
		if name == "ssh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("selected=%v, want ssh activated by the simulated_intent stub", res.SelectedTools)
	}

	withoutUIC := &Dataset{
		Version: 1,
		Samples: []Sample{{
			ID:          "ssh-1",
			UserText:    "帮我连上服务器 10.0.0.1",
			ExpectTools: []string{"ssh"},
		}},
	}
	report = Run(withoutUIC)
	res = report.Results[0]
	if res.Recall != 0 {
		t.Fatalf("recall=%v, want 0 without simulated_intent (fail-closed)", res.Recall)
	}
	for _, name := range res.SelectedTools {
		if name == "ssh" {
			t.Fatalf("selected=%v, want no ssh without simulated_intent", res.SelectedTools)
		}
	}
}

// TestSimulatedIntentBelowActivationThreshold verifies the stub honors the
// router's activation threshold: a simulated_intent whose confidence is
// below 0.50 does not activate the named conditional tool.
func TestSimulatedIntentBelowActivationThreshold(t *testing.T) {
	ds := &Dataset{
		Version: 1,
		Samples: []Sample{{
			ID:              "ssh-lowconf",
			UserText:        "帮我连上服务器 10.0.0.1",
			ExpectTools:     []string{"ssh"},
			SimulatedIntent: &SimulatedIntent{Label: "ssh", Confidence: 0.30, ToolNames: []string{"ssh"}},
		}},
	}
	report := Run(ds)
	res := report.Results[0]
	if res.Recall != 0 {
		t.Fatalf("recall=%v, want 0 below the activation threshold (selected=%v)", res.Recall, res.SelectedTools)
	}
}

// TestValidateSimulatedIntent covers the strict simulated_intent validation:
// unknown intent labels, confidence outside [0,1], and tool_names that do
// not exist in the catalog must all be rejected.
func TestValidateSimulatedIntent(t *testing.T) {
	base := func(sim *SimulatedIntent) Dataset {
		return Dataset{
			Version: 1,
			Samples: []Sample{{
				ID:              "s",
				UserText:        "x",
				SimulatedIntent: sim,
			}},
		}
	}
	bad := []SimulatedIntent{
		{Label: "not_a_label", Confidence: 0.9, ToolNames: []string{"ssh"}},
		{Label: "ssh", Confidence: 1.5, ToolNames: []string{"ssh"}},
		{Label: "ssh", Confidence: -0.1, ToolNames: []string{"ssh"}},
		{Label: "ssh", Confidence: 0.9, ToolNames: []string{"not_in_catalog"}},
	}
	for i, sim := range bad {
		ds := base(&sim)
		if err := validate(&ds); err == nil {
			t.Fatalf("case %d (%+v): validate want error, got nil", i, sim)
		}
	}
	good := base(&SimulatedIntent{Label: "ssh", Confidence: 1.0, ToolNames: []string{"ssh"}})
	if err := validate(&good); err != nil {
		t.Fatalf("valid simulated_intent rejected: %v", err)
	}
	// nil simulated_intent (absent field) stays valid.
	none := base(nil)
	if err := validate(&none); err != nil {
		t.Fatalf("nil simulated_intent rejected: %v", err)
	}
}

// TestRunLeavesRoutingStatsUntouched verifies the offline harness does not
// pollute live telemetry: RunFile replays the legacy router but the global
// corelib/tool routing counters must be exactly as before.
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

	if _, err := RunFile(filepath.Join("data", "baseline-v1.json")); err != nil {
		t.Fatalf("RunFile: %v", err)
	}

	after := tool.GetRoutingStats()
	if before != after {
		t.Fatalf("routing stats changed by offline eval: before=%+v after=%+v", before, after)
	}
	if tool.RecordRoutingStats != true {
		t.Fatal("RecordRoutingStats must be restored after Run")
	}
}
