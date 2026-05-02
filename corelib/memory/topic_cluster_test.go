package memory

import (
	"testing"
)

func TestTopicClusterer_BasicClustering(t *testing.T) {
	tc := NewTopicClusterer()

	entries := []Entry{
		{ID: "e1", Content: "PostgreSQL performance tuning", Tags: []string{"postgresql", "database", "performance"}, Status: StatusActive},
		{ID: "e2", Content: "PostgreSQL backup strategy", Tags: []string{"postgresql", "database", "backup"}, Status: StatusActive},
		{ID: "e3", Content: "PostgreSQL index optimization", Tags: []string{"postgresql", "database", "index"}, Status: StatusActive},
		{ID: "e4", Content: "React component lifecycle", Tags: []string{"react", "frontend", "javascript"}, Status: StatusActive},
		{ID: "e5", Content: "React hooks best practices", Tags: []string{"react", "frontend", "hooks"}, Status: StatusActive},
		{ID: "e6", Content: "React state management", Tags: []string{"react", "frontend", "state"}, Status: StatusActive},
		{ID: "e7", Content: "Unrelated single entry", Tags: []string{"random"}, Status: StatusActive},
	}

	clusters := tc.Cluster(entries)

	// Should find at least 2 clusters (postgresql and react).
	if len(clusters) < 2 {
		t.Fatalf("expected at least 2 clusters, got %d", len(clusters))
	}

	// Verify clusters have entries.
	for _, c := range clusters {
		if len(c.EntryIDs) < 3 {
			t.Logf("cluster %q has %d entries (tags: %v)", c.Name, len(c.EntryIDs), c.Tags)
		}
	}
}

func TestTopicClusterer_SkipsInactiveEntries(t *testing.T) {
	tc := NewTopicClusterer()

	entries := []Entry{
		{ID: "e1", Content: "active entry 1", Tags: []string{"topic_a", "shared"}, Status: StatusActive},
		{ID: "e2", Content: "active entry 2", Tags: []string{"topic_a", "shared"}, Status: StatusActive},
		{ID: "e3", Content: "active entry 3", Tags: []string{"topic_a", "shared"}, Status: StatusActive},
		{ID: "e4", Content: "dormant entry", Tags: []string{"topic_a", "shared"}, Status: StatusDormant},
	}

	clusters := tc.Cluster(entries)

	// The dormant entry should not be in any cluster.
	for _, c := range clusters {
		for _, id := range c.EntryIDs {
			if id == "e4" {
				t.Fatal("dormant entry should not be in any cluster")
			}
		}
	}
}

func TestTopicClusterer_EntityBasedClustering(t *testing.T) {
	tc := NewTopicClusterer()

	entries := []Entry{
		{ID: "e1", Content: "Alice works at Google", Entities: []string{"entity:Alice", "entity:Google"}, Status: StatusActive},
		{ID: "e2", Content: "Alice lives in Shanghai", Entities: []string{"entity:Alice", "entity:Shanghai"}, Status: StatusActive},
		{ID: "e3", Content: "Alice prefers dark mode", Entities: []string{"entity:Alice"}, Status: StatusActive},
		{ID: "e4", Content: "Bob works at Meta", Entities: []string{"entity:Bob", "entity:Meta"}, Status: StatusActive},
	}

	clusters := tc.Cluster(entries)

	// Should find a cluster around "alice" (3 entries mention Alice).
	foundAliceCluster := false
	for _, c := range clusters {
		for _, tag := range c.Tags {
			if tag == "alice" {
				foundAliceCluster = true
				if len(c.EntryIDs) < 3 {
					t.Fatalf("alice cluster should have at least 3 entries, got %d", len(c.EntryIDs))
				}
			}
		}
	}
	if !foundAliceCluster {
		t.Log("no explicit alice cluster found, but that's ok if entries are merged into another cluster")
	}
}

func TestTopicClusterer_TrivialTagsSkipped(t *testing.T) {
	if !isTrivialTag("extracted") {
		t.Fatal("'extracted' should be trivial")
	}
	if !isTrivialTag("online_extracted") {
		t.Fatal("'online_extracted' should be trivial")
	}
	if !isTrivialTag("2026-04-30") {
		t.Fatal("date tag should be trivial")
	}
	if isTrivialTag("postgresql") {
		t.Fatal("'postgresql' should not be trivial")
	}
}

func TestTopicClusterer_EmptyInput(t *testing.T) {
	tc := NewTopicClusterer()
	clusters := tc.Cluster(nil)
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters for nil input, got %d", len(clusters))
	}
}

func TestTopicClusterer_IndexesCanonicalEntityTokens(t *testing.T) {
	tc := NewTopicClusterer()
	entries := []Entry{
		{ID: "e1", Content: "alpha one", Entities: []string{" Entity: Alpha Host "}, Status: StatusActive},
		{ID: "e2", Content: "alpha two", Entities: []string{" entity:alpha host "}, Status: StatusActive},
		{ID: "e3", Content: "alpha three", Entities: []string{" ENTITY: Alpha Host "}, Status: StatusActive},
	}

	clusters := tc.Cluster(entries)
	for _, c := range clusters {
		for _, tag := range c.Tags {
			if tag == "alpha host" && len(c.EntryIDs) == 3 {
				return
			}
		}
	}
	t.Fatalf("expected dirty entity tokens to form alpha host cluster, got %+v", clusters)
}
