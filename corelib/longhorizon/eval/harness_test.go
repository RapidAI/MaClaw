package eval

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestHorizonEvalDatasets(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	files, err := LoadDatasetFiles(filepath.Join(filepath.Dir(file), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("dataset files=%d, want at least the initial P5 categories", len(files))
	}
	seen := make(map[string]bool, len(files))
	for _, dataset := range files {
		if seen[dataset.Category] {
			t.Fatalf("duplicate category %q", dataset.Category)
		}
		seen[dataset.Category] = true
		if len(dataset.Samples) == 0 {
			t.Fatalf("category %q has no samples", dataset.Category)
		}
	}
	for _, dataset := range files {
		for _, sample := range dataset.Samples {
			sample := sample
			category := dataset.Category
			t.Run(category+"/"+sample.ID, func(t *testing.T) {
				if _, err := EvaluateSample(sample); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	report := RunDatasets(files)
	t.Logf("\n%s", report.String())
	if report.Failures() != 0 {
		t.Fatalf("report shows %d failed samples after subtests passed", report.Failures())
	}
}
