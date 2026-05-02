package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSemanticGraphSearch_RelationAwareCurrentFacts(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	oldInvalid := now.Add(-time.Hour)
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	current := Entry{
		ID:        "current",
		Content:   "4090 server ssh port is 2222",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: now,
		Entities:  []string{"entity:4090 server", "relation:config_of", "entity:ssh port 2222"},
	}
	old := Entry{
		ID:        "old",
		Content:   "4090 server ssh port used to be 22",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: now.Add(-24 * time.Hour),
		InvalidAt: &oldInvalid,
		Entities:  []string{"entity:4090 server", "relation:config_of", "entity:ssh port 22"},
	}

	store.semanticGraph.Rebuild([]Entry{current, old})
	hits := store.semanticGraph.Search([]string{"4090 server"}, now, "")
	if len(hits) == 0 {
		t.Fatal("expected semantic graph hit")
	}
	if hits[0].EntryID != "current" {
		t.Fatalf("expected current fact first, got %+v", hits)
	}
	for _, hit := range hits {
		if hit.EntryID == "old" {
			t.Fatalf("invalidated fact should not be recalled as current: %+v", hits)
		}
	}
}

func TestRecallDynamic_UsesSemanticGraphSignal(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:  "Host alpha listens on a nonstandard remote shell endpoint.",
		Category: CategoryProjectKnowledge,
		Entities: []string{"entity:alpha-host", "relation:config_of", "entity:ssh-port-2222"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:  "Unrelated note about UI color choices.",
		Category: CategoryProjectKnowledge,
		Entities: []string{"entity:design", "relation:about", "entity:colors"},
	}); err != nil {
		t.Fatal(err)
	}

	results := store.RecallDynamic("alpha-host connection config", "", "")
	if len(results) == 0 {
		t.Fatal("expected recall results")
	}
	if results[0].Content != "Host alpha listens on a nonstandard remote shell endpoint." {
		t.Fatalf("expected semantic graph-backed memory first, got %q", results[0].Content)
	}
}

func TestSemanticGraphSearch_ResolvesAliasEntities(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alias",
			Content:   "4090server is the same machine as gpu-server.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:4090server", "relation:alias_of", "entity:gpu-server"},
		},
		{
			ID:        "config",
			Content:   "gpu-server ssh port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:gpu-server", "relation:config_of", "entity:ssh-port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"4090server"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
	})
	if len(hits) == 0 {
		t.Fatal("expected alias-expanded semantic hits")
	}
	foundConfig := false
	for _, hit := range hits {
		if hit.EntryID == "config" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Fatalf("expected config fact reachable through alias, got %+v", hits)
	}
}

func TestSemanticGraphSearch_RelationIntentReranksPaths(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "owner",
			Content:   "alpha appears in the test notes.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:about", "entity:test-notes"},
		},
		{
			ID:        "config",
			Content:   "alpha has ssh port 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:ssh-port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
	})
	if len(hits) < 2 {
		t.Fatalf("expected two hits, got %+v", hits)
	}
	if hits[0].EntryID != "config" {
		t.Fatalf("expected config path to rank first for config intent, got %+v", hits)
	}
}

func TestSemanticGraphSearch_BoundedMultiHopFindsRelatedFacts(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "dependency",
			Content:   "alpha depends on api-service.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:api-service"},
		},
		{
			ID:        "service-config",
			Content:   "api-service endpoint is https://api.example.test.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:api-service", "relation:config_of", "entity:https://api.example.test"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		MaxHops:       2,
	})
	foundConfig := false
	for _, hit := range hits {
		if hit.EntryID == "service-config" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Fatalf("expected second-hop config fact, got %+v", hits)
	}
}

func TestSemanticGraphSearch_MaxHopsLimitsTraversal(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "dependency",
			Content:   "alpha depends on api-service.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:api-service"},
		},
		{
			ID:        "service-config",
			Content:   "api-service endpoint is https://api.example.test.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:api-service", "relation:config_of", "entity:https://api.example.test"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		MaxHops:       1,
	})
	for _, hit := range hits {
		if hit.EntryID == "service-config" {
			t.Fatalf("did not expect second-hop config with MaxHops=1, got %+v", hits)
		}
	}
}

func TestSemanticGraphAdjacencyUpdatesAfterRemove(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "keep",
			Content:   "alpha depends on api-service.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:api-service"},
		},
		{
			ID:        "remove",
			Content:   "api-service config points to endpoint.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:api-service", "relation:config_of", "entity:endpoint"},
		},
	})

	_, factsBefore, adjBefore := g.Stats()
	if factsBefore != 2 || adjBefore == 0 {
		t.Fatalf("expected populated graph before remove, facts=%d adjacency=%d", factsBefore, adjBefore)
	}
	g.RemoveEntry("remove")
	_, factsAfter, _ := g.Stats()
	if factsAfter != 1 {
		t.Fatalf("expected one fact after remove, got %d", factsAfter)
	}
	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, MaxHops: 2})
	for _, hit := range hits {
		if hit.EntryID == "remove" {
			t.Fatalf("removed entry should not be reachable through adjacency: %+v", hits)
		}
	}
}

func TestSemanticGraphSearch_DirectionalRelationsLimitReverseExpansion(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "config",
			Content:   "alpha config is endpoint-a.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:endpoint-a"},
		},
		{
			ID:        "downstream",
			Content:   "alpha depends on api-service.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:api-service"},
		},
	})

	hits := g.SearchWithOptions([]string{"endpoint-a"}, SemanticSearchOptions{Now: now, MaxHops: 2})
	for _, hit := range hits {
		if hit.EntryID == "downstream" {
			t.Fatalf("reverse config_of traversal should not expand through alpha, got %+v", hits)
		}
	}
}

func TestSemanticGraphSearch_AliasAllowsReverseExpansion(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alias",
			Content:   "alpha is also called endpoint-a.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:alias_of", "entity:endpoint-a"},
		},
		{
			ID:        "downstream",
			Content:   "alpha depends on api-service.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:api-service"},
		},
	})

	hits := g.SearchWithOptions([]string{"endpoint-a"}, SemanticSearchOptions{Now: now, MaxHops: 2})
	found := false
	for _, hit := range hits {
		if hit.EntryID == "downstream" {
			found = true
		}
	}
	if !found {
		t.Fatalf("alias relation should allow reverse expansion to alpha facts, got %+v", hits)
	}
}

func TestSemanticGraphCanonicalizesRelationSynonyms(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "port",
			Content:   "alpha has ssh port 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:has_port", "entity:ssh-port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
	})
	if len(hits) == 0 || hits[0].EntryID != "port" {
		t.Fatalf("expected canonicalized config relation hit, got %+v", hits)
	}
	foundCanonicalPath := false
	for _, path := range hits[0].Paths {
		if strings.Contains(path, "config_of") {
			foundCanonicalPath = true
		}
	}
	if !foundCanonicalPath {
		t.Fatalf("expected path to use canonical relation config_of, got %+v", hits[0].Paths)
	}
}

func TestSemanticGraphSearch_DegreePenaltyPrefersSpecificEntity(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{
			ID:        "specific",
			Content:   "alpha-node has port 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha-node", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "generic",
			Content:   "server has generic port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:server", "relation:config_of", "entity:port-22"},
		},
	}
	for i := 0; i < 8; i++ {
		entries = append(entries, Entry{
			ID:        fmt.Sprintf("hub-%d", i),
			Content:   fmt.Sprintf("server generic relation %d", i),
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:server", "relation:about", fmt.Sprintf("entity:generic-%d", i)},
		})
	}
	g := NewSemanticGraph()
	g.Rebuild(entries)

	hits := g.SearchWithOptions([]string{"alpha-node", "server"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		MaxHops:       1,
	})
	if len(hits) < 2 {
		t.Fatalf("expected multiple hits, got %+v", hits)
	}
	if hits[0].EntryID != "specific" {
		t.Fatalf("expected specific low-degree entity first, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_AliasTransitiveClosure(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alias-ab",
			Content:   "alpha is beta.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:alias_of", "entity:beta"},
		},
		{
			ID:        "alias-bc",
			Content:   "beta is gamma.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:beta", "relation:alias_of", "entity:gamma"},
		},
		{
			ID:        "config-c",
			Content:   "gamma has port 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:gamma", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	found := false
	for _, hit := range hits {
		if hit.EntryID == "config-c" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected transitive alias to reach gamma config, got %+v", hits)
	}
}

func TestSemanticGraphSearch_ProvenanceBoostsPinnedManualFacts(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:         "conversation",
			Content:    "alpha port may be 22.",
			Category:   CategoryProjectKnowledge,
			SourceType: "conversation",
			UpdatedAt:  now,
			Entities:   []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:         "manual-pinned",
			Content:    "alpha port is 2222.",
			Category:   CategoryProjectKnowledge,
			SourceType: "manual",
			Pinned:     true,
			UpdatedAt:  now,
			Entities:   []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected two hits, got %+v", hits)
	}
	if hits[0].EntryID != "manual-pinned" {
		t.Fatalf("expected pinned manual fact first, got %+v", hits)
	}
}

func TestSemanticGraphSearch_DominancePrefersRecentCurrentConfig(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-config",
			Content:   "alpha port is 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-30 * 24 * time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:        "new-config",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected competing config hits, got %+v", hits)
	}
	if hits[0].EntryID != "new-config" {
		t.Fatalf("expected newer config to dominate stale competing config, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_DominanceRespectsPinnedManualConfig(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:         "manual-config",
			Content:    "alpha port is 2222.",
			Category:   CategoryProjectKnowledge,
			SourceType: "manual",
			Pinned:     true,
			UpdatedAt:  now.Add(-30 * 24 * time.Hour),
			Entities:   []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "recent-config",
			Content:   "alpha port might be 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected competing config hits, got %+v", hits)
	}
	if hits[0].EntryID != "manual-config" {
		t.Fatalf("expected pinned manual config to dominate, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_DominanceAggregatesCorroboratingEvidence(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-a",
			Content:   "alpha port is 2222 from deployment notes.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-10 * 24 * time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "old-b",
			Content:   "alpha ssh endpoint also uses port 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-9 * 24 * time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "new-weak",
			Content:   "alpha might use port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 3 {
		t.Fatalf("expected competing corroborated hits, got %+v", hits)
	}
	if hits[0].EntryID == "new-weak" {
		t.Fatalf("single newer weak fact should not dominate corroborated config cluster, got %+v", hits[:3])
	}
	foundA, foundB := false, false
	for _, hit := range hits[:2] {
		if hit.EntryID == "old-a" {
			foundA = true
		}
		if hit.EntryID == "old-b" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Fatalf("expected both corroborating facts to stay near top, got %+v", hits[:3])
	}
}

func TestSemanticGraphSearch_CertaintyBeatsNewerSpeculation(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "certain",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-7 * 24 * time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "speculative",
			Content:   "alpha might use port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected competing hits, got %+v", hits)
	}
	if hits[0].EntryID != "certain" {
		t.Fatalf("expected certain fact to beat newer speculation, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_ConfirmedStatementBoost(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "plain",
			Content:   "alpha port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:        "confirmed",
			Content:   "confirmed: alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected competing hits, got %+v", hits)
	}
	if hits[0].EntryID != "confirmed" {
		t.Fatalf("expected confirmed statement to rank first, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_NegatedFactDoesNotActAsStrongPositive(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "positive",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "negated",
			Content:   "alpha does not use port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected positive and negated hits, got %+v", hits)
	}
	if hits[0].EntryID == "negated" {
		t.Fatalf("negated config should not rank as strongest positive config, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_NegatedCurrentFactSuppressesOldPositiveSameObject(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-positive",
			Content:   "alpha port is 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-30 * 24 * time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:        "current-negative",
			Content:   "alpha no longer uses port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected competing positive/negative hits, got %+v", hits)
	}
	if hits[0].EntryID != "current-negative" {
		t.Fatalf("current negation should dominate old positive for same object, got %+v", hits[:2])
	}
}

func TestSemanticGraphSearch_HistoricalModeIncludesInvalidatedFacts(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	invalidAt := now.Add(-24 * time.Hour)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-port",
			Content:   "alpha previously used port 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-30 * 24 * time.Hour),
			InvalidAt: &invalidAt,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:        "current-port",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	currentHits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	for _, hit := range currentHits {
		if hit.EntryID == "old-port" {
			t.Fatalf("current mode should not include invalidated fact, got %+v", currentHits)
		}
	}

	historicalHits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		TemporalMode:  SemanticTemporalHistorical,
	})
	foundOld := false
	for _, hit := range historicalHits {
		if hit.EntryID == "old-port" {
			foundOld = true
		}
	}
	if !foundOld {
		t.Fatalf("historical mode should include invalidated fact, got %+v", historicalHits)
	}
}

func TestSemanticTemporalModeFromQuery(t *testing.T) {
	if got := semanticTemporalModeFromQuery("what was alpha's previous port before it changed?"); got != SemanticTemporalHistorical {
		t.Fatalf("expected historical mode, got %v", got)
	}
	if got := semanticTemporalModeFromQuery("what was alpha port on 2025-12-01?"); got != SemanticTemporalAsOf {
		t.Fatalf("expected as-of mode, got %v", got)
	}
	if got := semanticTemporalModeFromQuery("what is alpha port now?"); got != SemanticTemporalCurrent {
		t.Fatalf("expected current mode, got %v", got)
	}
}

func TestSemanticGraphSearch_AsOfModeUsesValidityWindow(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	validAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	invalidAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2025, 12, 1, 23, 59, 59, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-window",
			Content:   "alpha port was 22 in 2025.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: validAt,
			ValidAt:   &validAt,
			InvalidAt: &invalidAt,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	currentHits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(currentHits) != 0 {
		t.Fatalf("current mode should not include expired validity window, got %+v", currentHits)
	}
	asOfHits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		AsOf:          &asOf,
		RelationHints: []string{"config"},
		TemporalMode:  SemanticTemporalAsOf,
	})
	if len(asOfHits) == 0 || asOfHits[0].EntryID != "old-window" {
		t.Fatalf("as-of mode should include fact valid at requested time, got %+v", asOfHits)
	}
}

func TestSemanticGraphSearch_AsOfModeIncludesSupersededFactValidThen(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	validAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	invalidAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2025, 12, 1, 23, 59, 59, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-superseded-window",
			Content:   "alpha port was 22 before 2026.",
			Category:  CategoryProjectKnowledge,
			Status:    StatusSuperseded,
			UpdatedAt: invalidAt,
			ValidAt:   &validAt,
			InvalidAt: &invalidAt,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
	})

	currentHits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(currentHits) != 0 {
		t.Fatalf("current mode should not include superseded fact, got %+v", currentHits)
	}
	asOfHits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		AsOf:          &asOf,
		RelationHints: []string{"config"},
		TemporalMode:  SemanticTemporalAsOf,
	})
	if len(asOfHits) == 0 || asOfHits[0].EntryID != "old-superseded-window" {
		t.Fatalf("as-of mode should include superseded fact valid at requested time, got %+v", asOfHits)
	}
}
func TestSemanticAsOfTimeFromQuery(t *testing.T) {
	iso := semanticAsOfTimeFromQuery("show alpha config at 2025-12-03")
	if iso == nil || iso.Year() != 2025 || iso.Month() != 12 || iso.Day() != 3 {
		t.Fatalf("expected parsed ISO date, got %v", iso)
	}
	cn := semanticAsOfTimeFromQuery("show alpha at 2025\u5e7412\u6708")
	if cn == nil || cn.Year() != 2025 || cn.Month() != 12 || cn.Day() != 1 {
		t.Fatalf("expected parsed Chinese month date, got %v", cn)
	}
}

func TestStoreSemanticRecallDebugExposesPaths(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "alpha port is 2222.",
		Category: CategoryProjectKnowledge,
		Entities: []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	}); err != nil {
		t.Fatal(err)
	}

	hits := store.SemanticRecallDebug("alpha config", "")
	if len(hits) == 0 {
		t.Fatal("expected semantic debug hits")
	}
	if len(hits[0].Paths) == 0 {
		t.Fatalf("expected path explanations, got %+v", hits[0])
	}
}

func TestStoreLastSemanticHitsIsDefensiveCopy(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "alpha port is 2222.",
		Category: CategoryProjectKnowledge,
		Entities: []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.RecallDynamic("alpha config", "", "")

	first := store.LastSemanticHits()
	if len(first) == 0 {
		t.Fatal("expected last semantic hits after RecallDynamic")
	}
	for id, hit := range first {
		hit.Paths[0] = "mutated"
		first[id] = hit
		break
	}
	second := store.LastSemanticHits()
	for _, hit := range second {
		if len(hit.Paths) > 0 && hit.Paths[0] == "mutated" {
			t.Fatalf("LastSemanticHits should return a defensive copy, got %+v", second)
		}
	}
}

func TestSemanticGraphSearch_MaxHitsTruncatesAfterSorting(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	entries := make([]Entry, 0, 6)
	for i := 0; i < 5; i++ {
		entries = append(entries, Entry{
			ID:        fmt.Sprintf("plain-%d", i),
			Content:   fmt.Sprintf("alpha generic config %d.", i),
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:about", fmt.Sprintf("entity:item-%d", i)},
		})
	}
	entries = append(entries, Entry{
		ID:        "best",
		Content:   "confirmed: alpha port is 2222.",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: now,
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	})
	g := NewSemanticGraph()
	g.Rebuild(entries)

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		MaxHits:       2,
	})
	if len(hits) != 2 {
		t.Fatalf("expected exactly 2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].EntryID != "best" {
		t.Fatalf("expected best hit preserved after sorting/truncation, got %+v", hits)
	}
}

func TestSemanticGraphSearch_MaxVisitedFactsLimitsTraversal(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "dependency",
			Content:   "alpha depends on api-service.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:api-service"},
		},
		{
			ID:        "config",
			Content:   "api-service port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:api-service", "relation:config_of", "entity:port-2222"},
		},
	})

	limited := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:             now,
		RelationHints:   []string{"config"},
		MaxHops:         2,
		MaxVisitedFacts: 1,
	})
	for _, hit := range limited {
		if hit.EntryID == "config" {
			t.Fatalf("did not expect second fact with MaxVisitedFacts=1, got %+v", limited)
		}
	}

	unlimited := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:             now,
		RelationHints:   []string{"config"},
		MaxHops:         2,
		MaxVisitedFacts: 10,
	})
	foundConfig := false
	for _, hit := range unlimited {
		if hit.EntryID == "config" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Fatalf("expected second fact with sufficient budget, got %+v", unlimited)
	}
}

func TestSemanticGraphSearch_BudgetPrioritizesRelationIntent(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "about",
			Content:   "alpha appears in a note.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:about", "entity:note"},
		},
		{
			ID:        "config",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:             now,
		RelationHints:   []string{"config"},
		MaxHops:         1,
		MaxVisitedFacts: 1,
	})
	if len(hits) == 0 {
		t.Fatal("expected one hit")
	}
	if hits[0].EntryID != "config" {
		t.Fatalf("expected config edge to be visited first under budget, got %+v", hits)
	}
}

func TestSemanticGraphSearch_SeedWeightsAffectBudgetPriority(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "short-seed",
			Content:   "api port is 1111.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:api", "relation:config_of", "entity:port-1111"},
		},
		{
			ID:        "long-seed",
			Content:   "alpha-production-api port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha-production-api", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"api", "alpha-production-api"}, SemanticSearchOptions{
		Now:             now,
		RelationHints:   []string{"config"},
		SeedWeights:     semanticSeedWeightsFromEntities([]string{"api", "alpha-production-api"}),
		MaxVisitedFacts: 1,
	})
	if len(hits) == 0 {
		t.Fatal("expected weighted seed hit")
	}
	if hits[0].EntryID != "long-seed" {
		t.Fatalf("expected longer, more specific seed to win tight budget, got %+v", hits)
	}
}

func TestSemanticGraphSearch_RelationOnlyFallback(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "config",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "note",
			Content:   "general note.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:note", "relation:about", "entity:misc"},
		},
	})

	hits := g.SearchWithOptions(nil, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		MaxHits:       1,
	})
	if len(hits) != 1 || hits[0].EntryID != "config" {
		t.Fatalf("expected relation-only config hit, got %+v", hits)
	}
	noHint := g.SearchWithOptions(nil, SemanticSearchOptions{Now: now})
	if len(noHint) != 0 {
		t.Fatalf("relation-only search without hints should not scan graph, got %+v", noHint)
	}
}

func TestSemanticGraphSearch_ProjectScopeFiltersFactsBeforeTraversal(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alpha-port",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "beta-port",
			Content:   "alpha port is 3333 in beta.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\beta`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-3333"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		ProjectPath:   `D:\workprj\alpha`,
		RelationHints: []string{"port"},
	})
	if len(hits) == 0 || hits[0].EntryID != "alpha-port" {
		t.Fatalf("expected alpha scoped fact first, got %+v", hits)
	}
	for _, hit := range hits {
		if hit.EntryID == "beta-port" {
			t.Fatalf("beta scoped fact leaked into alpha project hits: %+v", hits)
		}
	}
}

func TestSemanticGraphSearch_ProjectScopeFiltersAliasTraversal(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alpha-alias-in-alpha",
			Content:   "alpha is gpu-server in the alpha project.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:alias_of", "entity:gpu-server"},
		},
		{
			ID:        "gpu-beta-port",
			Content:   "gpu-server port is 3333 in beta.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\beta`},
			UpdatedAt: now,
			Entities:  []string{"entity:gpu-server", "relation:config_of", "entity:port-3333"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		ProjectPath:   `D:\workprj\beta`,
		RelationHints: []string{"port"},
	})
	for _, hit := range hits {
		if hit.EntryID == "gpu-beta-port" {
			t.Fatalf("project-local alias leaked into beta traversal: %+v", hits)
		}
	}
}

func TestSemanticGraphSearch_ProjectScopeRequiresPathBoundary(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alpha-port",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "alphabet-port",
			Content:   "alphabet project alpha port is 3333.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alphabet`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-3333"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		ProjectPath:   `D:\workprj\alpha`,
		RelationHints: []string{"port"},
	})
	if len(hits) == 0 || hits[0].EntryID != "alpha-port" {
		t.Fatalf("expected alpha scoped fact first, got %+v", hits)
	}
	for _, hit := range hits {
		if hit.EntryID == "alphabet-port" {
			t.Fatalf("sibling project path leaked through prefix match: %+v", hits)
		}
	}
}

func TestSemanticGraphSearch_ProjectScopeAllowsFilePathUnderProject(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "file-tagged-port",
			Content:   "alpha port is 2222 from a project file.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha\config\server.md`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		ProjectPath:   `D:\workprj\alpha`,
		RelationHints: []string{"port"},
	})
	if len(hits) == 0 || hits[0].EntryID != "file-tagged-port" {
		t.Fatalf("expected file path under project to remain visible, got %+v", hits)
	}
}
func TestStoreSemanticRecallDebugForProjectUsesProjectScope(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(filepath.Join(tmp, "memory.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	if err := store.Save(Entry{
		ID:        "alpha-debug-port",
		Content:   "alpha port is 2222.",
		Category:  CategoryProjectKnowledge,
		Scope:     ScopeProject,
		Tags:      []string{`D:\workprj\alpha`},
		UpdatedAt: now,
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	}); err != nil {
		t.Fatalf("Save alpha: %v", err)
	}
	if err := store.Save(Entry{
		ID:        "beta-debug-port",
		Content:   "alpha port is 3333 in beta.",
		Category:  CategoryProjectKnowledge,
		Scope:     ScopeProject,
		Tags:      []string{`D:\workprj\beta`},
		UpdatedAt: now,
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-3333"},
	}); err != nil {
		t.Fatalf("Save beta: %v", err)
	}

	hits := store.SemanticRecallDebugForProject("alpha port", `D:\workprj\alpha`, "")
	if len(hits) == 0 || hits[0].EntryID != "alpha-debug-port" {
		t.Fatalf("expected alpha debug hit first, got %+v", hits)
	}
	for _, hit := range hits {
		if hit.EntryID == "beta-debug-port" {
			t.Fatalf("debug API leaked beta fact into alpha project: %+v", hits)
		}
	}
}

func TestSemanticGraphFactsRequireAlignedTriples(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{{
		ID:        "malformed-offset",
		Content:   "malformed tokens should not produce a cross-window fact.",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: now,
		Entities: []string{
			"entity:alpha",
			"junk",
			"entity:beta",
			"relation:config_of",
			"entity:port-2222",
		},
	}})

	_, factCount, _ := g.Stats()
	if factCount != 0 {
		t.Fatalf("expected malformed unaligned triples to produce no facts, got %d", factCount)
	}
	hits := g.SearchWithOptions(nil, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) != 0 {
		t.Fatalf("unaligned tokens should not create searchable relation facts, got %+v", hits)
	}
	diag := g.Diagnostics(SemanticSearchOptions{Now: now})
	if diag.MalformedTripleCount == 0 {
		t.Fatalf("expected diagnostics to report malformed triples, got %+v", diag)
	}
}

func TestSemanticGraphDiagnosticsReportsQualitySignals(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	entries := []Entry{
		{
			ID:        "alpha-port-2222",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "alpha-port-3333",
			Content:   "alpha port is 3333.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-3333"},
		},
		{
			ID:        "alpha-alias",
			Content:   "alpha is also called gpu-alpha.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:alias_of", "entity:gpu-alpha"},
		},
		{
			ID:        "unknown-relation",
			Content:   "alpha deploys in prod-east.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:deployed_in", "entity:prod-east"},
		},
	}
	for i := 0; i < 3; i++ {
		entries = append(entries, Entry{
			ID:        fmt.Sprintf("server-note-%d", i),
			Content:   fmt.Sprintf("server generic note %d.", i),
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:server", "relation:about", fmt.Sprintf("entity:item-%d", i)},
		})
	}
	entries = append(entries, Entry{
		ID:        "server-port",
		Content:   "server port is 22.",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: now,
		Entities:  []string{"entity:server", "relation:config_of", "entity:port-22"},
	})
	g.Rebuild(entries)

	diag := g.Diagnostics(SemanticSearchOptions{Now: now})
	if diag.FactCount != len(entries) {
		t.Fatalf("expected all facts counted, got %+v", diag)
	}
	if diag.RelationCounts["config_of"] != 3 {
		t.Fatalf("expected config relation count, got %+v", diag.RelationCounts)
	}
	if len(diag.UnknownRelations) != 1 || diag.UnknownRelations[0] != "deployed_in" {
		t.Fatalf("expected deployed_in unknown relation, got %+v", diag.UnknownRelations)
	}
	if len(diag.UnknownRelationDetails) != 1 || diag.UnknownRelationDetails[0].Relation != "deployed_in" || diag.UnknownRelationDetails[0].Count != 1 || !sameStringSlice(diag.UnknownRelationDetails[0].EntryIDs, []string{"unknown-relation"}) {
		t.Fatalf("expected detailed unknown relation provenance, got %+v", diag.UnknownRelationDetails)
	}
	foundServerHub := false
	for _, hub := range diag.HighDegreeEntities {
		if hub.Entity == "server" && hub.Degree == 4 {
			foundServerHub = true
			break
		}
	}
	if !foundServerHub {
		t.Fatalf("expected server high-degree hub, got %+v", diag.HighDegreeEntities)
	}
	if len(diag.AliasComponents) != 1 || !sameStringSlice(diag.AliasComponents[0], []string{"alpha", "gpu-alpha"}) {
		t.Fatalf("expected alpha alias component, got %+v", diag.AliasComponents)
	}
	if len(diag.DominanceConflicts) != 1 || diag.DominanceConflicts[0].Subject != "alpha" || len(diag.DominanceConflicts[0].Objects) != 2 {
		t.Fatalf("expected alpha config conflict, got %+v", diag.DominanceConflicts)
	}
}

func TestSemanticGraphDiagnosticsRespectsProjectScope(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alpha-visible",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "beta-hidden-unknown",
			Content:   "alpha beta-only relation.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\beta`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:beta_only", "entity:hidden"},
		},
	})

	diag := g.Diagnostics(SemanticSearchOptions{Now: now, ProjectPath: `D:\workprj\alpha`})
	if diag.FactCount != 1 {
		t.Fatalf("expected one visible fact, got %+v", diag)
	}
	if len(diag.UnknownRelations) != 0 {
		t.Fatalf("hidden beta relation should not be diagnosed in alpha scope: %+v", diag.UnknownRelations)
	}
}

func TestSemanticGraphSearch_UnknownRelationDoesNotDriveTraversal(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "unknown-bridge",
			Content:   "alpha deploys in prod-east.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:deployed_in", "entity:prod-east"},
		},
		{
			ID:        "hidden-through-unknown",
			Content:   "prod-east endpoint is https://prod.example.test.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:prod-east", "relation:config_of", "entity:https://prod.example.test"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config"},
		MaxHops:       2,
	})
	for _, hit := range hits {
		if hit.EntryID == "hidden-through-unknown" {
			t.Fatalf("unknown relation should not expand traversal, got %+v", hits)
		}
	}
}

func TestSemanticGraphSearch_UnknownRelationIsDownweighted(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "unknown-direct",
			Content:   "alpha deploys in prod-east.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:deployed_in", "entity:prod-east"},
		},
		{
			ID:        "known-direct",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, MaxHops: 1})
	if len(hits) < 2 {
		t.Fatalf("expected direct known and unknown hits, got %+v", hits)
	}
	if hits[0].EntryID != "known-direct" {
		t.Fatalf("known relation should outrank unknown relation, got %+v", hits)
	}
}

func TestSemanticGraphSearch_RelationOnlyFallbackPrefersSchemaRelations(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "unknown-only",
			Content:   "alpha deploys in prod-east.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:deployed_in", "entity:prod-east"},
		},
		{
			ID:        "known-config",
			Content:   "beta port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:beta", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions(nil, SemanticSearchOptions{
		Now:           now,
		RelationHints: []string{"config", "deployed_in"},
		MaxHits:       1,
	})
	if len(hits) != 1 || hits[0].EntryID != "known-config" {
		t.Fatalf("schema relation should win relation-only fallback, got %+v", hits)
	}
}

func TestSemanticRelationSchemaDrivesRelationBehavior(t *testing.T) {
	if !semanticKnownRelation("config_of") || !semanticIsDominanceRelation("config_of") {
		t.Fatalf("config_of should be known functional relation")
	}
	if semanticRelationWeight("config_of") != semanticRelationSchema["config_of"].Weight {
		t.Fatalf("relation weight should come from schema")
	}
	if !semanticAllowsExpansion("depends_on") {
		t.Fatalf("depends_on should allow forward expansion")
	}
	if semanticAllowsExpansion("deployed_in") {
		t.Fatalf("unknown relation must not allow expansion")
	}
	if !semanticAllowsReverseExpansion("alias_of") || semanticAllowsReverseExpansion("config_of") {
		t.Fatalf("reverse expansion should follow relation schema")
	}
	if got := semanticDirectionFactor("belongs_to", false, true); got != semanticRelationSchema["belongs_to"].ReverseFactor {
		t.Fatalf("reverse direction factor should come from schema, got %v", got)
	}
}

func TestSemanticRelationHintFamiliesStayInsideSchema(t *testing.T) {
	inputs := []string{"config", "credential", "dependency", "alias", "preference", "location", "work"}
	for _, input := range inputs {
		for _, relation := range relationHintFamily(input) {
			if !semanticKnownRelation(relation) {
				t.Fatalf("relation hint %q expanded to relation outside schema: %q", input, relation)
			}
		}
	}
}

func TestSemanticRelationSchemaSnapshotIsStableAndCanonical(t *testing.T) {
	items := SemanticRelationSchema()
	if len(items) == 0 {
		t.Fatal("expected relation schema items")
	}
	seen := make(map[string]struct{}, len(items))
	last := ""
	for i, item := range items {
		if item.Name == "" || item.Weight <= 0 {
			t.Fatalf("invalid schema item: %+v", item)
		}
		if i > 0 && item.Name < last {
			t.Fatalf("schema items must be sorted: %q before %q", last, item.Name)
		}
		last = item.Name
		seen[item.Name] = struct{}{}
	}
	for _, relation := range []string{"preference_for", "located_in", "works_at", "config_of"} {
		if _, ok := seen[relation]; !ok {
			t.Fatalf("schema missing canonical relation %q", relation)
		}
	}
}

func TestSemanticGraphDiagnosticsReportsMalformedTriples(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "bad-shape",
			Content:   "bad semantic entities",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "entity:missing-relation", "entity:object", "entity:tail"},
		},
		{
			ID:        "good-shape",
			Content:   "alpha port is 2222",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	diag := g.Diagnostics(SemanticSearchOptions{Now: now})
	if diag.MalformedTripleCount != 2 || len(diag.MalformedTriples) != 2 {
		t.Fatalf("expected malformed triple and incomplete tail, got %+v", diag.MalformedTriples)
	}
	if diag.MalformedTriples[0].EntryID != "bad-shape" || diag.MalformedTriples[0].Offset != 0 || diag.MalformedTriples[0].Reason != "missing_relation" {
		t.Fatalf("unexpected first malformed issue: %+v", diag.MalformedTriples[0])
	}
	if diag.MalformedTriples[1].EntryID != "bad-shape" || diag.MalformedTriples[1].Offset != 3 || diag.MalformedTriples[1].Reason != "incomplete_triple" {
		t.Fatalf("unexpected second malformed issue: %+v", diag.MalformedTriples[1])
	}
}

func TestSemanticGraphDiagnosticsMalformedTriplesRespectScope(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alpha-bad",
			Content:   "alpha malformed entities",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "entity:missing-relation", "entity:object"},
		},
		{
			ID:        "beta-bad",
			Content:   "beta malformed entities",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\beta`},
			UpdatedAt: now,
			Entities:  []string{"entity:beta", "entity:missing-relation", "entity:object"},
		},
	})

	diag := g.Diagnostics(SemanticSearchOptions{Now: now, ProjectPath: `D:\workprj\alpha`})
	if len(diag.MalformedTriples) != 1 || diag.MalformedTriples[0].EntryID != "alpha-bad" {
		t.Fatalf("expected only alpha malformed triple, got %+v", diag.MalformedTriples)
	}
}

func TestSemanticGraphParsesTrimmedCaseInsensitiveTokens(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "dirty-tokens",
			Content:   "Alpha has port 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{" Entity: Alpha ", " Relation: HAS-PORT ", " Entity: Port 2222 "},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) == 0 || hits[0].EntryID != "dirty-tokens" {
		t.Fatalf("expected dirty tokens to produce a searchable config fact, got %+v", hits)
	}
	foundCanonicalPath := false
	for _, path := range hits[0].Paths {
		if strings.Contains(path, "config_of") {
			foundCanonicalPath = true
		}
	}
	if !foundCanonicalPath {
		t.Fatalf("expected canonical relation in path, got %+v", hits[0].Paths)
	}
}

func TestSemanticGraphParsesReverseRelationSynonyms(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "reverse-blocked",
			Content:   "alpha is blocked by beta.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:blocked_by", "entity:beta"},
		},
	})

	hits := g.SearchWithOptions([]string{"beta"}, SemanticSearchOptions{Now: now, RelationHints: []string{"blocks"}})
	if len(hits) == 0 || hits[0].EntryID != "reverse-blocked" {
		t.Fatalf("expected beta --blocks--> alpha fact, got %+v", hits)
	}
	found := false
	for _, path := range hits[0].Paths {
		if strings.Contains(path, "beta --blocks--> alpha") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected subject/object swap for blocked_by, got paths %+v", hits[0].Paths)
	}
}

func TestSemanticEvidenceClusterBoostCountsDistinctEntries(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	facts := []SemanticFact{
		{EntryID: "same", Subject: "alpha", Predicate: "config_of", Object: "port-2222", UpdatedAt: now},
		{EntryID: "same", Subject: "alpha", Predicate: "config_of", Object: "port-2222", UpdatedAt: now},
		{EntryID: "other", Subject: "alpha", Predicate: "config_of", Object: "port-2222", UpdatedAt: now},
	}
	if got := semanticEvidenceSourceCount(facts, []int{0, 1}); got != 1 {
		t.Fatalf("duplicate facts from one entry should count as one evidence source, got %d", got)
	}
	if got := semanticEvidenceSourceCount(facts, []int{0, 1, 2}); got != 2 {
		t.Fatalf("independent entries should count as separate evidence sources, got %d", got)
	}
}

func TestSemanticFactsFromEntryDeduplicatesIdenticalTriples(t *testing.T) {
	facts := semanticFactsFromEntry(&Entry{
		ID:        "dup-triples",
		Content:   "alpha port is 2222.",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		Entities: []string{
			"entity:alpha", "relation:config_of", "entity:port-2222",
			"entity:alpha", "relation:config_of", "entity:port-2222",
			"entity:alpha", "relation:depends_on", "entity:api-service",
		},
	})
	if len(facts) != 2 {
		t.Fatalf("expected duplicate triples from one entry to be collapsed, got %+v", facts)
	}
}

func TestSemanticGraphRelationOnlySearchPrefersRecentFacts(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-config",
			Content:   "alpha port is 22.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-90 * 24 * time.Hour),
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:        "new-config",
			Content:   "beta port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now.Add(-time.Hour),
			Entities:  []string{"entity:beta", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions(nil, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) < 2 {
		t.Fatalf("expected relation-only hits, got %+v", hits)
	}
	if hits[0].EntryID != "new-config" {
		t.Fatalf("relation-only search should prefer recent comparable facts, got %+v", hits)
	}
}

func TestSemanticRelationHintsFromChineseQuery(t *testing.T) {
	expanded := ExpandQuery("alpha \u7aef\u53e3 \u914d\u7f6e")
	hints := normalizeRelationHints(semanticRelationHintsFromQuery("alpha \u7aef\u53e3 \u914d\u7f6e", expanded))
	if _, ok := hints["config_of"]; !ok {
		t.Fatalf("expected Chinese port/config query to map to config_of, got %+v", hints)
	}
	if _, ok := hints["credential_for"]; !ok {
		t.Fatalf("canonical config hints should expand to neighboring config-family relations, got %+v", hints)
	}

	expanded = ExpandQuery("alpha \u4f9d\u8d56 beta")
	hints = normalizeRelationHints(semanticRelationHintsFromQuery("alpha \u4f9d\u8d56 beta", expanded))
	if _, ok := hints["depends_on"]; !ok {
		t.Fatalf("expected Chinese dependency query to map to depends_on, got %+v", hints)
	}
	if _, ok := hints["blocks"]; !ok {
		t.Fatalf("canonical dependency hints should expand to causal/blocking relations, got %+v", hints)
	}
}

func TestSemanticGraphDiagnosticsAdjacencyKeysRespectScope(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "alpha-visible",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\alpha`},
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "beta-hidden",
			Content:   "beta depends on gamma.",
			Category:  CategoryProjectKnowledge,
			Scope:     ScopeProject,
			Tags:      []string{`D:\workprj\beta`},
			UpdatedAt: now,
			Entities:  []string{"entity:beta", "relation:depends_on", "entity:gamma"},
		},
	})

	diag := g.Diagnostics(SemanticSearchOptions{Now: now, ProjectPath: `D:\workprj\alpha`})
	if diag.FactCount != 1 {
		t.Fatalf("expected one visible fact, got %+v", diag)
	}
	if diag.AdjacencyKeys != 2 {
		t.Fatalf("adjacency keys should describe only visible subgraph, got %+v", diag)
	}
}

func TestSemanticGraphRelationOnlySearchFiltersToHintedRelations(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "config",
			Content:   "alpha port is 2222.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
		{
			ID:        "dependency",
			Content:   "alpha depends on beta.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:depends_on", "entity:beta"},
		},
	})

	hits := g.SearchWithOptions(nil, SemanticSearchOptions{Now: now, RelationHints: []string{"config"}})
	if len(hits) != 1 || hits[0].EntryID != "config" {
		t.Fatalf("relation-only search should filter to hinted relation family, got %+v", hits)
	}
}

func TestSemanticGraphSearch_AsOfModeExcludesFactsNotYetKnown(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	asOf := time.Date(2025, 12, 1, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "future-write",
			Content:   "alpha port is 2222 learned later.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		AsOf:          &asOf,
		RelationHints: []string{"config"},
		TemporalMode:  SemanticTemporalAsOf,
	})
	if len(hits) != 0 {
		t.Fatalf("as-of search should not include facts written after the requested time without ValidAt, got %+v", hits)
	}
}

func TestSemanticGraphSearch_AsOfModeAllowsBackfilledValidFact(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	validAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2025, 12, 1, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "backfilled",
			Content:   "alpha port was 2222 throughout 2025.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			ValidAt:   &validAt,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		AsOf:          &asOf,
		RelationHints: []string{"config"},
		TemporalMode:  SemanticTemporalAsOf,
	})
	if len(hits) == 0 || hits[0].EntryID != "backfilled" {
		t.Fatalf("as-of search should include backfilled facts with explicit ValidAt, got %+v", hits)
	}
}

func TestSemanticGraphSearch_AsOfModeRanksBackfilledFactsByEffectiveTime(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	oldValid := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newerValid := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2025, 12, 1, 12, 0, 0, 0, time.UTC)
	g := NewSemanticGraph()
	g.Rebuild([]Entry{
		{
			ID:        "old-backfilled-late",
			Content:   "alpha port was 22 early in 2025.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: now,
			ValidAt:   &oldValid,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
		},
		{
			ID:        "newer-effective",
			Content:   "alpha port was 2222 by late 2025.",
			Category:  CategoryProjectKnowledge,
			UpdatedAt: newerValid,
			ValidAt:   &newerValid,
			Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
		},
	})

	hits := g.SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:           now,
		AsOf:          &asOf,
		RelationHints: []string{"config"},
		TemporalMode:  SemanticTemporalAsOf,
	})
	if len(hits) < 2 {
		t.Fatalf("expected both backfilled facts, got %+v", hits)
	}
	if hits[0].EntryID != "newer-effective" {
		t.Fatalf("as-of ranking should use effective ValidAt instead of future write time, got %+v", hits[:2])
	}
}
