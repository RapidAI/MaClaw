package guiapp

import "testing"

func TestClaudeSummaryReducerKeepsRecentImportantFiles(t *testing.T) {
	reducer := NewClaudeSummaryReducer()
	current := SessionSummary{
		ImportantFiles: []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
	}

	next := reducer.Apply(current, []ImportantEvent{
		{Type: "file.read", RelatedFile: "c.go", Summary: "Read c.go"},
		{Type: "file.change", RelatedFile: "f.go", Summary: "Changed f.go"},
	}, nil)

	want := []string{"b.go", "d.go", "e.go", "c.go", "f.go"}
	if len(next.ImportantFiles) != len(want) {
		t.Fatalf("important file count = %d, want %d", len(next.ImportantFiles), len(want))
	}
	for i := range want {
		if next.ImportantFiles[i] != want[i] {
			t.Fatalf("important_files[%d] = %q, want %q", i, next.ImportantFiles[i], want[i])
		}
	}
}

func TestClaudeSummaryReducerIgnoresEmptyImportantFiles(t *testing.T) {
	reducer := NewClaudeSummaryReducer()

	next := reducer.Apply(SessionSummary{}, []ImportantEvent{
		{Type: "file.read", RelatedFile: "", Summary: "Read file"},
	}, nil)

	if len(next.ImportantFiles) != 0 {
		t.Fatalf("important file count = %d, want 0", len(next.ImportantFiles))
	}
}

func TestClaudeSummaryReducerHandlesSessionFailed(t *testing.T) {
	reducer := NewClaudeSummaryReducer()

	next := reducer.Apply(SessionSummary{}, []ImportantEvent{
		{Type: "session.failed", Summary: "launch failed"},
	}, nil)

	if next.Status != string(SessionError) {
		t.Fatalf("status = %q, want %q", next.Status, SessionError)
	}
	if next.LastResult != "launch failed" {
		t.Fatalf("last result = %q, want %q", next.LastResult, "launch failed")
	}
}

func TestClaudeSummaryReducerHandlesSessionClosed(t *testing.T) {
	reducer := NewClaudeSummaryReducer()

	next := reducer.Apply(SessionSummary{}, []ImportantEvent{
		{Type: "session.closed", Severity: "warn", Summary: "Session exited with code 1"},
	}, nil)

	if next.Status != string(SessionExited) {
		t.Fatalf("status = %q, want %q", next.Status, SessionExited)
	}
	if next.Severity != "warn" {
		t.Fatalf("severity = %q, want %q", next.Severity, "warn")
	}
	if next.LastResult != "Session exited with code 1" {
		t.Fatalf("last result = %q, want %q", next.LastResult, "Session exited with code 1")
	}
}
