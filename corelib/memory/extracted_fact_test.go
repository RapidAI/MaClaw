package memory

import (
	"encoding/json"
	"testing"
)

func TestParsedEntities_FlatArray(t *testing.T) {
	raw := json.RawMessage(`["entity:Alice", "relation:lives_in", "entity:Shanghai"]`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	if len(got) != 3 {
		t.Fatalf("expected 3 entities, got %d: %v", len(got), got)
	}
	if got[0] != "entity:Alice" || got[2] != "entity:Shanghai" {
		t.Errorf("unexpected entities: %v", got)
	}
}

func TestParsedEntities_NestedArray(t *testing.T) {
	// LLM wraps each triple in its own array - the bug that caused
	// "json: cannot unmarshal array into Go struct field ExtractedFact.entities of type string"
	raw := json.RawMessage(`[["entity:Alice", "relation:lives_in", "entity:Shanghai"], ["entity:Bob", "relation:works_at", "entity:Google"]]`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	if len(got) != 6 {
		t.Fatalf("expected 6 entities (2 triples flattened), got %d: %v", len(got), got)
	}
	if got[0] != "entity:Alice" || got[3] != "entity:Bob" {
		t.Errorf("unexpected entities: %v", got)
	}
}

func TestParsedEntities_SingleString(t *testing.T) {
	raw := json.RawMessage(`"entity:Alice"`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	if len(got) != 1 || got[0] != "entity:Alice" {
		t.Fatalf("expected [entity:Alice], got %v", got)
	}
}

func TestParsedEntities_Empty(t *testing.T) {
	f := ExtractedFact{Content: "test"}
	got := f.ParsedEntities()
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestParsedEntities_EmptyArray(t *testing.T) {
	raw := json.RawMessage(`[]`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestExtractedFact_JSONRoundTrip(t *testing.T) {
	// Verify that ExtractedFact can be unmarshaled from LLM output
	// that uses nested arrays for entities.
	input := `{
		"content": "Alice lives in Shanghai",
		"category": "user_fact",
		"entities": [["entity:Alice", "relation:lives_in", "entity:Shanghai"]]
	}`
	var f ExtractedFact
	if err := json.Unmarshal([]byte(input), &f); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if f.Content != "Alice lives in Shanghai" {
		t.Errorf("unexpected content: %s", f.Content)
	}
	got := f.ParsedEntities()
	if len(got) != 3 {
		t.Fatalf("expected 3 entities, got %d: %v", len(got), got)
	}
}

func TestExtractedFact_JSONRoundTrip_FlatEntities(t *testing.T) {
	input := `{
		"content": "Bob works at Google",
		"category": "user_fact",
		"entities": ["entity:Bob", "relation:works_at", "entity:Google"]
	}`
	var f ExtractedFact
	if err := json.Unmarshal([]byte(input), &f); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	got := f.ParsedEntities()
	if len(got) != 3 || got[0] != "entity:Bob" {
		t.Fatalf("expected [entity:Bob, relation:works_at, entity:Google], got %v", got)
	}
}

func TestParsedEntities_CanonicalizesRelations(t *testing.T) {
	raw := json.RawMessage(`[["entity:User", " Relation:prefers ", "entity:dark_mode"], ["entity:User", "relation:lives_in", "entity:Shanghai"], ["entity:User", "relation:works_at", "entity:OpenAI"]]`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	want := []string{"entity:User", "relation:preference_for", "entity:dark_mode", "entity:User", "relation:located_in", "entity:Shanghai", "entity:User", "relation:works_at", "entity:OpenAI"}
	if len(got) != len(want) {
		t.Fatalf("expected %d items, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d: expected %q, got %q (all: %v)", i, want[i], got[i], got)
		}
	}
}

func TestParsedEntities_CanonicalizesEntityTokenShape(t *testing.T) {
	raw := json.RawMessage(`[" Entity: User ", " Relation: HAS-PORT ", " Entity: Port 2222 "]`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	want := []string{"entity:User", "relation:config_of", "entity:Port 2222"}
	if len(got) != len(want) {
		t.Fatalf("expected %d items, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d: expected %q, got %q (all: %v)", i, want[i], got[i], got)
		}
	}
}

func TestParsedEntities_SwapsReverseRelationSynonyms(t *testing.T) {
	raw := json.RawMessage(`["entity:alpha", "relation:blocked_by", "entity:beta"]`)
	f := ExtractedFact{Content: "test", RawEntities: raw}
	got := f.ParsedEntities()
	want := []string{"entity:beta", "relation:blocks", "entity:alpha"}
	if len(got) != len(want) {
		t.Fatalf("expected %d items, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d: expected %q, got %q (all: %v)", i, want[i], got[i], got)
		}
	}
}
