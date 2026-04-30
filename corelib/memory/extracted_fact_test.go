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
	// LLM wraps each triple in its own array — the bug that caused
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
