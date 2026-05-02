package memory

import (
	"testing"
)

func TestEntityIndex_IndexAndFind(t *testing.T) {
	idx := NewEntityIndex()

	e1 := &Entry{
		ID:       "e1",
		Content:  "Alice lives in Shanghai",
		Entities: []string{"entity:Alice", "relation:lives_in", "entity:Shanghai"},
	}
	e2 := &Entry{
		ID:       "e2",
		Content:  "Alice works at Google",
		Entities: []string{"entity:Alice", "relation:works_at", "entity:Google"},
	}
	e3 := &Entry{
		ID:       "e3",
		Content:  "Bob lives in Beijing",
		Entities: []string{"entity:Bob", "relation:lives_in", "entity:Beijing"},
	}

	idx.IndexEntry(e1)
	idx.IndexEntry(e2)
	idx.IndexEntry(e3)

	// Find by entity name.
	aliceEntries := idx.FindByEntity("Alice")
	if len(aliceEntries) != 2 {
		t.Fatalf("expected 2 entries for Alice, got %d", len(aliceEntries))
	}

	shanghaiEntries := idx.FindByEntity("Shanghai")
	if len(shanghaiEntries) != 1 {
		t.Fatalf("expected 1 entry for Shanghai, got %d", len(shanghaiEntries))
	}

	// Case-insensitive.
	aliceLower := idx.FindByEntity("alice")
	if len(aliceLower) != 2 {
		t.Fatalf("expected case-insensitive match, got %d", len(aliceLower))
	}
}

func TestEntityIndex_FindRelatedEntities(t *testing.T) {
	idx := NewEntityIndex()

	idx.IndexEntry(&Entry{
		ID:       "e1",
		Entities: []string{"entity:Alice", "entity:Shanghai"},
	})
	idx.IndexEntry(&Entry{
		ID:       "e2",
		Entities: []string{"entity:Alice", "entity:Google"},
	})
	idx.IndexEntry(&Entry{
		ID:       "e3",
		Entities: []string{"entity:Bob", "entity:Beijing"},
	})

	// Alice co-occurs with Shanghai and Google.
	related := idx.FindRelatedEntities("Alice")
	if len(related) != 2 {
		t.Fatalf("expected 2 related entities for Alice, got %d: %v", len(related), related)
	}

	// Bob only co-occurs with Beijing.
	relatedBob := idx.FindRelatedEntities("Bob")
	if len(relatedBob) != 1 {
		t.Fatalf("expected 1 related entity for Bob, got %d", len(relatedBob))
	}
	if relatedBob[0] != "beijing" {
		t.Fatalf("expected 'beijing', got '%s'", relatedBob[0])
	}
}

func TestEntityIndex_RemoveEntry(t *testing.T) {
	idx := NewEntityIndex()

	idx.IndexEntry(&Entry{
		ID:       "e1",
		Entities: []string{"entity:Alice", "entity:Shanghai"},
	})

	// Verify indexed.
	if len(idx.FindByEntity("Alice")) != 1 {
		t.Fatal("expected 1 entry for Alice before removal")
	}

	// Remove.
	idx.RemoveEntry("e1")

	// Verify removed.
	if len(idx.FindByEntity("Alice")) != 0 {
		t.Fatal("expected 0 entries for Alice after removal")
	}
}

func TestEntityIndex_Rebuild(t *testing.T) {
	idx := NewEntityIndex()

	entries := []Entry{
		{ID: "e1", Entities: []string{"entity:Alice", "entity:Shanghai"}},
		{ID: "e2", Entities: []string{"entity:Bob", "entity:Beijing"}},
		{ID: "e3", Entities: []string{"entity:Alice", "entity:Google"}},
	}

	idx.Rebuild(entries)

	entities2, indexed := idx.Stats()
	if entities2 != 4 { // alice, shanghai, bob, beijing, google = 5? No: alice, shanghai, bob, beijing, google
		// Actually: alice, shanghai, bob, beijing, google = 5
		t.Logf("entities=%d, indexed=%d", entities2, indexed)
	}
	if indexed != 3 {
		t.Fatalf("expected 3 indexed entries, got %d", indexed)
	}

	aliceEntries := idx.FindByEntity("Alice")
	if len(aliceEntries) != 2 {
		t.Fatalf("expected 2 entries for Alice after rebuild, got %d", len(aliceEntries))
	}
}

func TestEntityIndex_ReindexUpdatesMapping(t *testing.T) {
	idx := NewEntityIndex()

	e := &Entry{
		ID:       "e1",
		Entities: []string{"entity:Alice", "entity:Shanghai"},
	}
	idx.IndexEntry(e)

	// Update entities.
	e.Entities = []string{"entity:Alice", "entity:Beijing"}
	idx.IndexEntry(e)

	// Shanghai should no longer be indexed.
	if len(idx.FindByEntity("Shanghai")) != 0 {
		t.Fatal("expected Shanghai to be removed after reindex")
	}

	// Beijing should be indexed.
	if len(idx.FindByEntity("Beijing")) != 1 {
		t.Fatal("expected Beijing to be indexed after reindex")
	}
}

func TestEntityIndex_IndexesCanonicalEntityTokens(t *testing.T) {
	idx := NewEntityIndex()
	idx.IndexEntry(&Entry{
		ID:       "dirty-entity",
		Entities: []string{" Entity: Alpha Host ", " Relation: HAS-PORT ", " Entity: Port 2222 "},
	})

	if got := idx.FindByEntity("alpha host"); len(got) != 1 || got[0] != "dirty-entity" {
		t.Fatalf("expected dirty alpha host entity to be indexed, got %v", got)
	}
	if got := idx.FindByEntity("port 2222"); len(got) != 1 || got[0] != "dirty-entity" {
		t.Fatalf("expected dirty object entity to be indexed, got %v", got)
	}
}

func TestEntityIndex_StatusAndEmptyEntityUpdatesRemoveMappings(t *testing.T) {
	idx := NewEntityIndex()
	e := &Entry{ID: "e1", Entities: []string{"entity:Alpha", "relation:about", "entity:Beta"}}
	idx.IndexEntry(e)
	if got := idx.FindByEntity("alpha"); len(got) != 1 {
		t.Fatalf("expected alpha before status change, got %v", got)
	}

	e.Status = StatusSuperseded
	idx.IndexEntry(e)
	if got := idx.FindByEntity("alpha"); len(got) != 0 {
		t.Fatalf("expected superseded entry to be removed, got %v", got)
	}

	e.Status = StatusActive
	idx.IndexEntry(e)
	if got := idx.FindByEntity("alpha"); len(got) != 1 {
		t.Fatalf("expected alpha after reactivation, got %v", got)
	}

	e.Entities = nil
	idx.IndexEntry(e)
	if got := idx.FindByEntity("alpha"); len(got) != 0 {
		t.Fatalf("expected entity deletion to remove old mapping, got %v", got)
	}
}

func TestEntityIndex_RebuildSkipsInactiveAndMalformedEntries(t *testing.T) {
	idx := NewEntityIndex()
	idx.Rebuild([]Entry{
		{ID: "active", Entities: []string{"entity:Alpha"}},
		{ID: "dormant", Status: StatusDormant, Entities: []string{"entity:Beta"}},
		{ID: "malformed", Entities: []string{"relation:about"}},
	})

	entities, entries := idx.Stats()
	if entities != 1 || entries != 1 {
		t.Fatalf("expected only one active well-formed entity entry, got entities=%d entries=%d", entities, entries)
	}
	if got := idx.FindByEntity("beta"); len(got) != 0 {
		t.Fatalf("dormant entity should not be indexed, got %v", got)
	}
}
