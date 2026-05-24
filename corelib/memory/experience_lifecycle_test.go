package memory

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestRecallDynamicEmitsExperienceRetrievedEvent(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatal(err)
	}
	trail := lifecycle.NewEventTrail(8)
	store.SetExperienceEventSink(trail)

	if err := store.Save(Entry{Category: CategoryProjectKnowledge, Content: "Go tests can fail when SQLite WAL lock contention keeps memory.db open."}); err != nil {
		t.Fatal(err)
	}
	results := store.RecallDynamic("sqlite wal go test", CategoryProjectKnowledge, "")
	if len(results) == 0 {
		t.Fatal("expected recall result")
	}

	events := trail.List()
	if len(events) != 1 {
		t.Fatalf("expected one lifecycle event, got %d", len(events))
	}
	event := events[0]
	if event.EventType != lifecycle.EventExperienceRetrieved || event.Query != "sqlite wal go test" || event.Reason != "dynamic" {
		t.Fatalf("unexpected recall event: %+v", event)
	}
	if len(event.EntryIDs) == 0 || event.EntryIDs[0] != results[0].ID {
		t.Fatalf("expected recalled entry id in event, got event=%+v result=%+v", event, results[0].ID)
	}
}
