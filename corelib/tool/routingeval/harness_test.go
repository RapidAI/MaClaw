package routingeval

import (
	"path/filepath"
	"runtime"
	"testing"

	tool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestRoutingEvalDatasets(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	files, err := LoadDatasetFiles(filepath.Join(filepath.Dir(file), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("dataset files=%d, want at least the 10.1 slice categories", len(files))
	}
	seen := make(map[int]string, 40)
	for _, dataset := range files {
		if dataset.CategoryID < 1 || dataset.CategoryID > 40 {
			t.Fatalf("category %q has category_id=%d, want 1-40", dataset.Category, dataset.CategoryID)
		}
		if prev, ok := seen[dataset.CategoryID]; ok {
			t.Fatalf("duplicate category_id %d in %q and %q", dataset.CategoryID, prev, dataset.Category)
		}
		seen[dataset.CategoryID] = dataset.Category
	}
	for id := 1; id <= 40; id++ {
		if _, ok := seen[id]; !ok {
			t.Fatalf("missing design 10.1 category_id %d", id)
		}
	}
	plans := make(map[string]tool.ToolPlan)
	for _, dataset := range files {
		for _, sample := range dataset.Samples {
			sample := sample
			category := dataset.Category
			t.Run(category+"/"+sample.ID, func(t *testing.T) {
				plan, err := EvaluateSample(sample)
				if err != nil {
					t.Fatal(err)
				}
				if plan != nil {
					plans[sampleKey(category, sample.ID)] = *plan
				}
			})
		}
	}
	for _, dataset := range files {
		for _, sample := range dataset.Samples {
			if sample.Expected.PlanIDEquals == "" && sample.Expected.PlanIDDiffers == "" && sample.Expected.EquivalentTo == "" {
				continue
			}
			sample := sample
			category := dataset.Category
			t.Run(category+"/"+sample.ID+"/identity", func(t *testing.T) {
				if err := assertCrossSampleInvariants(plans, category, sample.ID, sample.Expected); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
