package tree

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- In-memory TreeStore for testing ---

type memStore struct {
	nodes map[string]*TreeNode
}

func newMemStore() *memStore {
	return &memStore{nodes: make(map[string]*TreeNode)}
}

func (s *memStore) Save(node *TreeNode) error {
	s.nodes[node.ID] = node
	return nil
}

func (s *memStore) Get(id string) (*TreeNode, error) {
	n, ok := s.nodes[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return n, nil
}

func (s *memStore) ListByLevel(level TreeLevel) ([]*TreeNode, error) {
	var result []*TreeNode
	for _, n := range s.nodes {
		if n.Level == level {
			result = append(result, n)
		}
	}
	return result, nil
}

func (s *memStore) ListByLevelAndDate(level TreeLevel, from, to time.Time) ([]*TreeNode, error) {
	var result []*TreeNode
	for _, n := range s.nodes {
		if n.Level == level && !n.CreatedAt.Before(from) && n.CreatedAt.Before(to) {
			result = append(result, n)
		}
	}
	return result, nil
}

func (s *memStore) ListChildren(parentID string) ([]*TreeNode, error) {
	var result []*TreeNode
	for _, n := range s.nodes {
		if n.ParentID == parentID {
			result = append(result, n)
		}
	}
	return result, nil
}

func (s *memStore) Delete(id string) error {
	delete(s.nodes, id)
	return nil
}

func (s *memStore) Search(query string, maxResults int) ([]*TreeNode, error) {
	var result []*TreeNode
	for _, n := range s.nodes {
		if strings.Contains(strings.ToLower(n.Content), strings.ToLower(query)) {
			result = append(result, n)
			if len(result) >= maxResults {
				break
			}
		}
	}
	return result, nil
}

// --- Tests ---

func TestSealDaily_NotEnoughChunks(t *testing.T) {
	store := newMemStore()
	sealer := NewSealer(store, nil, DefaultSealConfig())

	// Add only 2 chunks (min is 3)
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local)
	store.Save(&TreeNode{ID: "c1", Level: LevelChunk, Source: "conversation", Content: "chunk 1", CreatedAt: day.Add(time.Hour)})
	store.Save(&TreeNode{ID: "c2", Level: LevelChunk, Source: "conversation", Content: "chunk 2", CreatedAt: day.Add(2 * time.Hour)})

	created, err := sealer.SealDaily(day)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Errorf("should not seal with only 2 chunks, created %d", created)
	}
}

func TestSealDaily_CreatesL1(t *testing.T) {
	store := newMemStore()
	sealer := NewSealer(store, nil, DefaultSealConfig())

	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local)
	for i := 0; i < 5; i++ {
		store.Save(&TreeNode{
			ID:        fmt.Sprintf("c%d", i),
			Level:     LevelChunk,
			Source:    "conversation",
			Content:   fmt.Sprintf("User discussed topic %d with details about project X", i),
			CreatedAt: day.Add(time.Duration(i) * time.Hour),
			Tags:      []string{"project-x"},
		})
	}

	created, err := sealer.SealDaily(day)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Errorf("expected 1 L1 node, got %d", created)
	}

	// Verify L1 node exists
	l1Nodes, _ := store.ListByLevel(LevelDaily)
	if len(l1Nodes) != 1 {
		t.Fatalf("expected 1 L1 node in store, got %d", len(l1Nodes))
	}
	if len(l1Nodes[0].Children) != 5 {
		t.Errorf("L1 node should have 5 children, got %d", len(l1Nodes[0].Children))
	}
	if l1Nodes[0].Source != "conversation" {
		t.Errorf("L1 source should be conversation, got %s", l1Nodes[0].Source)
	}
}

func TestSealDaily_GroupsBySource(t *testing.T) {
	store := newMemStore()
	sealer := NewSealer(store, nil, DefaultSealConfig())

	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local)
	// 3 conversation chunks + 3 tool chunks
	for i := 0; i < 3; i++ {
		store.Save(&TreeNode{ID: fmt.Sprintf("conv%d", i), Level: LevelChunk, Source: "conversation", Content: fmt.Sprintf("conv %d", i), CreatedAt: day.Add(time.Duration(i) * time.Hour)})
		store.Save(&TreeNode{ID: fmt.Sprintf("tool%d", i), Level: LevelChunk, Source: "tool", Content: fmt.Sprintf("tool %d", i), CreatedAt: day.Add(time.Duration(i) * time.Hour)})
	}

	created, err := sealer.SealDaily(day)
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Errorf("expected 2 L1 nodes (one per source), got %d", created)
	}
}

func TestSealWeekly(t *testing.T) {
	store := newMemStore()
	sealer := NewSealer(store, nil, DefaultSealConfig())

	weekStart := time.Date(2026, 5, 12, 0, 0, 0, 0, time.Local) // Monday
	// Create 5 daily nodes
	for i := 0; i < 5; i++ {
		store.Save(&TreeNode{
			ID:        fmt.Sprintf("d%d", i),
			Level:     LevelDaily,
			Content:   fmt.Sprintf("Daily summary for day %d", i),
			CreatedAt: weekStart.Add(time.Duration(i) * 24 * time.Hour),
		})
	}

	created, err := sealer.SealWeekly(weekStart)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Errorf("expected 1 L2 node, got %d", created)
	}

	l2Nodes, _ := store.ListByLevel(LevelWeekly)
	if len(l2Nodes) != 1 {
		t.Fatalf("expected 1 L2 in store, got %d", len(l2Nodes))
	}
	if len(l2Nodes[0].Children) != 5 {
		t.Errorf("L2 should have 5 children, got %d", len(l2Nodes[0].Children))
	}
}

func TestSealMonthly(t *testing.T) {
	store := newMemStore()
	sealer := NewSealer(store, nil, DefaultSealConfig())

	monthStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	// Create 4 weekly nodes
	for i := 0; i < 4; i++ {
		store.Save(&TreeNode{
			ID:        fmt.Sprintf("w%d", i),
			Level:     LevelWeekly,
			Content:   fmt.Sprintf("Weekly summary for week %d", i),
			CreatedAt: monthStart.Add(time.Duration(i) * 7 * 24 * time.Hour),
		})
	}

	created, err := sealer.SealMonthly(monthStart)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Errorf("expected 1 L3 node, got %d", created)
	}
}

func TestSealDaily_WithSummarizer(t *testing.T) {
	store := newMemStore()
	summarizer := func(content string) (string, error) {
		cutoff := 50
		if len(content) < cutoff {
			cutoff = len(content)
		}
		return "SUMMARIZED: " + content[:cutoff], nil
	}
	sealer := NewSealer(store, summarizer, DefaultSealConfig())

	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local)
	for i := 0; i < 4; i++ {
		store.Save(&TreeNode{ID: fmt.Sprintf("c%d", i), Level: LevelChunk, Source: "conversation", Content: fmt.Sprintf("content %d", i), CreatedAt: day.Add(time.Duration(i) * time.Hour)})
	}

	created, _ := sealer.SealDaily(day)
	if created != 1 {
		t.Fatalf("expected 1, got %d", created)
	}

	l1Nodes, _ := store.ListByLevel(LevelDaily)
	if !strings.HasPrefix(l1Nodes[0].Content, "SUMMARIZED:") {
		t.Errorf("summarizer not used, content: %s", l1Nodes[0].Content[:50])
	}
}

func TestSealDaily_CollectsTags(t *testing.T) {
	store := newMemStore()
	sealer := NewSealer(store, nil, DefaultSealConfig())

	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local)
	store.Save(&TreeNode{ID: "c1", Level: LevelChunk, Source: "s", Content: "a", CreatedAt: day.Add(time.Hour), Tags: []string{"go", "api"}})
	store.Save(&TreeNode{ID: "c2", Level: LevelChunk, Source: "s", Content: "b", CreatedAt: day.Add(2 * time.Hour), Tags: []string{"api", "rest"}})
	store.Save(&TreeNode{ID: "c3", Level: LevelChunk, Source: "s", Content: "c", CreatedAt: day.Add(3 * time.Hour), Tags: []string{"go"}})

	sealer.SealDaily(day)
	l1Nodes, _ := store.ListByLevel(LevelDaily)
	if len(l1Nodes) != 1 {
		t.Fatal("expected 1 L1")
	}
	// Should have deduplicated tags: api, go, rest
	if len(l1Nodes[0].Tags) != 3 {
		t.Errorf("expected 3 unique tags, got %d: %v", len(l1Nodes[0].Tags), l1Nodes[0].Tags)
	}
}


