package codingruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLedgerBoundsApplyToEveryDurableTextBoundary(t *testing.T) {
	stores := []struct {
		name  string
		store Store
		close func()
	}{
		{name: "memory", store: NewMemoryStore(), close: func() {}},
		{name: "sqlite", store: newBoundedSQLiteStore(t), close: func() {}},
	}
	for _, tt := range stores {
		t.Run(tt.name, func(t *testing.T) {
			defer tt.close()
			work := strings.Repeat("w", maxRequestedWorkRunes+100)
			task, err := tt.store.CreateTask(Task{RequestedWork: work})
			if err != nil {
				t.Fatal(err)
			}
			if got := len([]rune(task.RequestedWork)); got != maxRequestedWorkRunes {
				t.Fatalf("requested work runes=%d, want %d", got, maxRequestedWorkRunes)
			}
			now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
			attempt, err := tt.store.StartAttempt(task.TaskID, "owner", time.Minute, PolicySnapshot{}, now)
			if err != nil {
				t.Fatal(err)
			}
			event, err := tt.store.AppendEvent(attempt.AttemptID, "owner", strings.Repeat("t", maxEventTypeRunes+50), strings.Repeat("d", maxPayloadDigestRunes+50), now)
			if err != nil {
				t.Fatal(err)
			}
			if got := len([]rune(event.Type)); got != maxEventTypeRunes {
				t.Fatalf("event type runes=%d, want %d", got, maxEventTypeRunes)
			}
			if got := len([]rune(event.PayloadDigest)); got != maxPayloadDigestRunes {
				t.Fatalf("event digest runes=%d, want %d", got, maxPayloadDigestRunes)
			}
			// Runner takes evidence from adapters, so its persistence path must
			// enforce the same limit rather than trusting every host adapter.
			boundedTask, boundedAttempt, err := (Runner{Store: tt.store, LeaseOwner: "runner", Now: func() time.Time { return now }}).Run(context.Background(), Task{TaskID: "runner-" + tt.name, ProjectRef: "runner-project", Mode: "local"}, PolicySnapshot{ProjectRoot: "runner-project", Mode: "local", ReadOnly: true}, executorFunc(func(_ context.Context, _ ExecutionRequest) ExecutionResult {
				return ExecutionResult{Status: TaskCompleted, Evidence: []Evidence{{Type: strings.Repeat("e", maxEvidenceTypeRunes+20), Digest: strings.Repeat("p", maxPayloadDigestRunes+20)}}}
			}))
			if err != nil || boundedTask.Status != TaskCompleted || boundedAttempt.Status != TaskCompleted {
				t.Fatalf("bounded runner task=%#v attempt=%#v err=%v", boundedTask, boundedAttempt, err)
			}
			finished, err := tt.store.FinishAttempt(attempt.AttemptID, "owner", FinishInput{Status: TaskFailed, ErrorCode: strings.Repeat("c", maxErrorCodeRunes+50), ErrorSummary: strings.Repeat("s", maxErrorSummaryRunes+50)}, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := len([]rune(finished.ErrorCode)); got != maxErrorCodeRunes {
				t.Fatalf("error code runes=%d, want %d", got, maxErrorCodeRunes)
			}
			if got := len([]rune(finished.ErrorSummary)); got != maxErrorSummaryRunes {
				t.Fatalf("error summary runes=%d, want %d", got, maxErrorSummaryRunes)
			}
		})
	}
}

func TestLedgerBoundsAlsoApplyToChildRequestedWork(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(parent.TaskID, "owner", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handles, err := service.AdmitReadOnlyChildren(attempt.AttemptID, "owner", []ChildTaskSpec{{Name: "inspect", RequestedWork: strings.Repeat("x", maxRequestedWorkRunes+1), ProjectRef: "repo", Mode: "local"}}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.GetTask(handles[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(child.RequestedWork)); got != maxRequestedWorkRunes {
		t.Fatalf("child requested work runes=%d, want %d", got, maxRequestedWorkRunes)
	}
}

func newBoundedSQLiteStore(t *testing.T) Store {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
