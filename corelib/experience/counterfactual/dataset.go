package counterfactual

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// FilterByTraceID returns the events of one trace so an evaluation can be
// re-run by trace id (Phase 7 acceptance: "evaluator 可按 trace id 重跑").
func FilterByTraceID(events []lifecycle.Event, traceID string) []lifecycle.Event {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil
	}
	out := make([]lifecycle.Event, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.TraceID) == traceID {
			out = append(out, event)
		}
	}
	return out
}

// DatasetCase is one counterfactual evaluation sample: a recorded event
// stream plus the verdicts the evaluator is expected to produce per pair key.
type DatasetCase struct {
	Name           string            `json:"name"`
	Events         []lifecycle.Event `json:"events"`
	SandboxDryRun  bool              `json:"sandbox_dry_run,omitempty"`
	ExpectVerdicts map[string]string `json:"expect_verdicts"`
}

// LoadDatasetFiles reads every JSON dataset case under dir, mirroring the
// routingeval harness layout so evaluation corpora can live as data files.
func LoadDatasetFiles(dir string) ([]DatasetCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var cases []DatasetCase
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		var dc DatasetCase
		if err := json.Unmarshal(raw, &dc); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(dc.Name) == "" {
			return nil, fmt.Errorf("%s: name is required", entry.Name())
		}
		cases = append(cases, dc)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

// EvaluateCase runs the evaluator over one dataset case and checks the
// expected verdicts. Expected verdicts use the CounterfactualVerdict string
// values ("retrieval_helpful", "retrieval_harmful", "no_difference",
// "not_evaluable"); pair keys absent from ExpectVerdicts are not asserted.
func EvaluateCase(ctx context.Context, dc DatasetCase) ([]lifecycle.CounterfactualRetrievalEvidence, error) {
	evaluator := &Evaluator{Opts: Options{SandboxDryRun: dc.SandboxDryRun}}
	evidence := evaluator.Evaluate(ctx, dc.Events)
	got := map[string]string{}
	for _, ev := range evidence {
		got[ev.PairKey] = string(ev.Verdict)
	}
	for pairKey, want := range dc.ExpectVerdicts {
		if got[pairKey] != want {
			return evidence, fmt.Errorf("case %s pair %s: verdict %q, want %q", dc.Name, pairKey, got[pairKey], want)
		}
	}
	return evidence, nil
}
