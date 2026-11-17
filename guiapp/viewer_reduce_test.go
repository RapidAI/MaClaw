package guiapp

import (
	_ "embed"
	"encoding/json"
	"os"
	"testing"
)

//go:embed testdata/viewer_contract_vectors.json
var viewerContractVectors []byte

func TestReduceViewerEventMatchesGolden(t *testing.T) {
	var file struct {
		Cases []struct {
			Name   string           `json:"name"`
			Events []map[string]any `json:"events"`
			Expect map[string]any   `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(viewerContractVectors, &file); err != nil {
		t.Fatalf("vectors: %v", err)
	}
	if len(file.Cases) < 12 {
		t.Fatalf("vectors = %d", len(file.Cases))
	}
	for _, item := range file.Cases {
		t.Run(item.Name, func(t *testing.T) {
			state := emptyViewerReduceState()
			for _, event := range item.Events {
				state = reduceViewerEvent(state, event)
			}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var actual any
			if err := json.Unmarshal(raw, &actual); err != nil {
				t.Fatal(err)
			}
			if !viewerSubset(actual, item.Expect) {
				t.Fatalf("state %s\nexpect %#v", raw, item.Expect)
			}
		})
	}
}

func TestReduceViewerEventMatchesWireSnapshot(t *testing.T) {
	raw, err := os.ReadFile("testdata/viewer_wire_snapshots.json")
	if err != nil {
		t.Fatalf("wire snapshot fixture: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	assertWireSnapshotReduction(t, mustRemarshal(t, doc["watch_event"]), "helloworld", false)
	assertWireSnapshotReduction(t, mustRemarshal(t, doc["responded_event"]), "终稿", true)
}

func TestReduceViewerEventFlagOffSkipsViewerEvent(t *testing.T) {
	t.Setenv("MACLAW_VIEWER_REDUCER", "0")
	app := &App{}
	var names []string
	app.testEmit = func(name string, _ ...interface{}) { names = append(names, name) }
	var got int
	app.addViewerSubscriber(func(viewerEvent) { got++ })
	app.publishViewerEvent(viewerEvent{SessionKey: streamSessionKey(), Event: "token", Text: "x"})
	if got != 1 {
		t.Fatalf("subscriber = %d", got)
	}
	for _, name := range names {
		if name == "viewer-event" {
			t.Fatal("MACLAW_VIEWER_REDUCER=0 emitted viewer-event")
		}
	}
}

func viewerSubset(actual, expect any) bool {
	switch exp := expect.(type) {
	case map[string]any:
		act, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range exp {
			if !viewerSubset(act[key], value) {
				return false
			}
		}
		return true
	case []any:
		act, ok := actual.([]any)
		if !ok || len(act) < len(exp) {
			return false
		}
		for i := range exp {
			if !viewerSubset(act[i], exp[i]) {
				return false
			}
		}
		return true
	case float64:
		switch n := actual.(type) {
		case float64:
			return n == exp
		default:
			return false
		}
	default:
		return actual == expect
	}
}
