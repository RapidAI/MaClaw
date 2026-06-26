package knowledge

import "testing"

func TestFactIndexIncludesStructuredTableFacts(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	entities, err := store.FactIndex(t.Context(), FactIndexOptions{SearchOptions: SearchOptions{Query: "张三", Limit: 20}, Kind: "entity"})
	if err != nil {
		t.Fatalf("FactIndex entity: %v", err)
	}
	if !hasFactIndexItem(entities.Items, "张三", "entity") {
		t.Fatalf("expected entity 张三, got %#v", entities.Items)
	}

	predicates, err := store.FactIndex(t.Context(), FactIndexOptions{SearchOptions: SearchOptions{Query: "部门", Limit: 20}, Kind: "predicate"})
	if err != nil {
		t.Fatalf("FactIndex predicate: %v", err)
	}
	if !hasFactIndexItem(predicates.Items, "部门", "predicate") {
		t.Fatalf("expected predicate 部门, got %#v", predicates.Items)
	}
}

func TestSuggestIncludesStructuredFactIndexItems(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	suggestions, err := store.Suggest(t.Context(), KnowledgeSuggestOptions{
		SearchOptions: SearchOptions{Query: "部门", Limit: 20},
		Kinds:         []string{"predicate"},
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	for _, item := range suggestions.Items {
		if item.Kind == "predicate" && item.Label == "部门" {
			return
		}
	}
	t.Fatalf("expected predicate suggestion 部门, got %#v", suggestions.Items)
}

func hasFactIndexItem(items []FactIndexItem, label, kind string) bool {
	for _, item := range items {
		if item.Label == label && item.Kind == kind {
			return true
		}
	}
	return false
}
