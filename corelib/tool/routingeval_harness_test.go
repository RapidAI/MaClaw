package tool_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool/routingeval"
)

func TestRoutingEvalDatasets(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	files, err := routingeval.LoadDatasetFiles(filepath.Join(filepath.Dir(file), "routingeval", "data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("dataset files=%d, want the 10.1 slice categories", len(files))
	}
	for _, dataset := range files {
		for _, sample := range dataset.Samples {
			t.Run(dataset.Category+"/"+sample.ID, func(t *testing.T) {
				if err := routingeval.RunSample(sample); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
