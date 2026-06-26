package knowledge

import "testing"

func TestFactGraphIncludesStructuredTableFacts(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	graph, err := store.FactGraph(t.Context(), SearchOptions{Entity: "张三", Limit: 20})
	if err != nil {
		t.Fatalf("FactGraph: %v", err)
	}
	if !hasFactEdge(graph.Edges, "张三", "部门", "法务") {
		t.Fatalf("expected structured edge 张三 部门 法务, got %#v", graph.Edges)
	}
}

func TestEntityProfileIncludesStructuredTableFacts(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	profile, err := store.EntityProfile(t.Context(), SearchOptions{Entity: "张三", Limit: 20})
	if err != nil {
		t.Fatalf("EntityProfile: %v", err)
	}
	if !hasFactEdge(profile.Facts, "张三", "部门", "法务") {
		t.Fatalf("expected structured profile fact 张三 部门 法务, got %#v", profile.Facts)
	}
}

func hasFactEdge(edges []FactGraphEdge, subject, predicate, object string) bool {
	for _, edge := range edges {
		if edge.Subject == subject && edge.Predicate == predicate && edge.Object == object {
			return true
		}
	}
	return false
}
