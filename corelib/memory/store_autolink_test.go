package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// deterministicEmbedder returns different vectors based on content keywords,
// allowing us to control cosine similarity between entries.
type deterministicEmbedder struct {
	dim     int
	vectors map[string][]float32
}

func (d *deterministicEmbedder) Embed(text string) ([]float32, error) {
	if v, ok := d.vectors[text]; ok {
		return v, nil
	}
	// Default: zero vector.
	return make([]float32, d.dim), nil
}

func (d *deterministicEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := d.Embed(t)
		out[i] = v
	}
	return out, nil
}

func (d *deterministicEmbedder) Dim() int { return d.dim }
func (d *deterministicEmbedder) Close()   {}

// highSimVector returns a vector that has high cosine similarity with base.
func highSimVector(base []float32, offset float32) []float32 {
	v := make([]float32, len(base))
	copy(v, base)
	if len(v) > 0 {
		v[0] += offset
	}
	return v
}

func TestAutoLink_WithEmbedder_LinksHighCosine(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Create vectors: A and B are very similar, C is orthogonal.
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0} // high cosine with A
	vecC := []float32{0, 0, 0, 1}      // orthogonal to A

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"golang concurrency patterns": vecA,
			"go goroutine channels":       vecB,
			"french cooking recipes":      vecC,
		},
	}
	store.embedder = emb

	// Save entry A.
	if err := store.Save(Entry{
		Content:   "golang concurrency patterns",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	// Save entry C (orthogonal — should NOT link to A).
	if err := store.Save(Entry{
		Content:   "french cooking recipes",
		Category:  CategoryProjectKnowledge,
		Embedding: vecC,
	}); err != nil {
		t.Fatal(err)
	}

	// Save entry B (similar to A — should link to A).
	if err := store.Save(Entry{
		Content:   "go goroutine channels",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	// Find entry B.
	var entryB *Entry
	var entryA *Entry
	for i := range store.entries {
		if store.entries[i].Content == "go goroutine channels" {
			entryB = &store.entries[i]
		}
		if store.entries[i].Content == "golang concurrency patterns" {
			entryA = &store.entries[i]
		}
	}

	if entryB == nil || entryA == nil {
		t.Fatal("entries not found")
	}

	// B should be linked to A.
	if len(entryB.RelatedIDs) == 0 {
		t.Fatal("expected entry B to have related IDs")
	}
	found := false
	for _, id := range entryB.RelatedIDs {
		if id == entryA.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected entry B to be linked to entry A, got RelatedIDs=%v", entryB.RelatedIDs)
	}

	// A should also be linked to B (bidirectional).
	found = false
	for _, id := range entryA.RelatedIDs {
		if id == entryB.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected entry A to be linked to entry B (bidirectional), got RelatedIDs=%v", entryA.RelatedIDs)
	}
}

func TestAutoLink_WithoutEmbedder_FallsBackToBM25(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// No embedder set — should use BM25 only.

	// Save entries with overlapping keywords but enough unique terms
	// to avoid being considered duplicates by any dedup layer.
	if err := store.Save(Entry{
		Content:  "golang error handling best practices for production microservices",
		Category: CategoryProjectKnowledge,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:  "python data science tutorial with pandas and numpy libraries",
		Category: CategoryProjectKnowledge,
	}); err != nil {
		t.Fatal(err)
	}

	// This entry shares some keywords with the first but has enough
	// unique terms to not be considered a BM25 duplicate.
	if err := store.Save(Entry{
		Content:  "golang error handling patterns using custom error types and wrapping",
		Category: CategoryProjectKnowledge,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	// The third entry should have some BM25-based links.
	var thirdEntry *Entry
	var firstEntry *Entry
	for i := range store.entries {
		if store.entries[i].Content == "golang error handling patterns using custom error types and wrapping" {
			thirdEntry = &store.entries[i]
		}
		if store.entries[i].Content == "golang error handling best practices for production microservices" {
			firstEntry = &store.entries[i]
		}
	}

	if thirdEntry == nil || firstEntry == nil {
		t.Fatal("entries not found")
	}

	// With BM25 fallback, the third entry should link to the first (shared keywords).
	if len(thirdEntry.RelatedIDs) == 0 {
		t.Fatal("expected BM25 fallback to create links for overlapping content")
	}

	found := false
	for _, id := range thirdEntry.RelatedIDs {
		if id == firstEntry.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected third entry linked to first via BM25, got RelatedIDs=%v", thirdEntry.RelatedIDs)
	}
}

func TestAutoLink_RespectsMaxRelatedLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Use a base vector and create many similar entries.
	base := []float32{1, 0, 0, 0}
	vectors := map[string][]float32{}

	// Create 8 entries all similar to each other.
	contents := []string{
		"memory system design alpha",
		"memory system design beta",
		"memory system design gamma",
		"memory system design delta",
		"memory system design epsilon",
		"memory system design zeta",
		"memory system design eta",
		"memory system design theta",
	}
	for i, c := range contents {
		v := make([]float32, 4)
		copy(v, base)
		v[1] = float32(i) * 0.01 // slight variation, still high cosine
		vectors[c] = v
	}

	emb := &deterministicEmbedder{dim: 4, vectors: vectors}
	store.embedder = emb

	for _, c := range contents {
		if err := store.Save(Entry{
			Content:   c,
			Category:  CategoryProjectKnowledge,
			Embedding: vectors[c],
		}); err != nil {
			t.Fatal(err)
		}
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	// No entry should exceed maxRelatedPerEntry (5) links.
	for _, e := range store.entries {
		if len(e.RelatedIDs) > maxRelatedPerEntry {
			t.Errorf("entry %q has %d related IDs, max is %d",
				e.Content, len(e.RelatedIDs), maxRelatedPerEntry)
		}
	}
}

func TestAutoLink_SingleEntry_NoLinks(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	emb := &fakeEmbedder{dim: 4}
	store.embedder = emb

	// Save a single entry — nothing to link to.
	if err := store.Save(Entry{
		Content:  "only entry",
		Category: CategoryUserFact,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(store.entries[0].RelatedIDs) != 0 {
		t.Errorf("single entry should have no related IDs, got %v", store.entries[0].RelatedIDs)
	}
}

func TestAutoLink_LowCosine_NoLink(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Two orthogonal vectors — cosine ≈ 0.
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0, 1, 0, 0}

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"topic alpha": vecA,
			"topic beta":  vecB,
		},
	}
	store.embedder = emb

	if err := store.Save(Entry{
		Content:   "topic alpha",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:   "topic beta",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	// Neither entry should be linked (cosine ≈ 0, below 0.7 threshold).
	for _, e := range store.entries {
		if len(e.RelatedIDs) > 0 {
			t.Errorf("entry %q should have no links (low cosine), got RelatedIDs=%v",
				e.Content, e.RelatedIDs)
		}
	}
}

func TestAutoLink_GraphEdgeStrength(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.1, 0, 0} // cosine ≈ 0.995

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"memory graph alpha": vecA,
			"memory graph beta":  vecB,
		},
	}
	store.embedder = emb

	if err := store.Save(Entry{
		Content:   "memory graph alpha",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:   "memory graph beta",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	var idA, idB string
	for _, e := range store.entries {
		if e.Content == "memory graph alpha" {
			idA = e.ID
		}
		if e.Content == "memory graph beta" {
			idB = e.ID
		}
	}
	store.mu.RUnlock()

	// Check graph edge exists with positive strength.
	neighbors := store.graph.neighborsOf(idB)
	strength, ok := neighbors[idA]
	if !ok {
		t.Fatal("expected graph edge from B to A")
	}
	if strength <= 0 {
		t.Errorf("expected positive edge strength, got %f", strength)
	}
}

func TestGraphEdgesPersistStrengthAndType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.1, 0, 0}
	store.embedder = &deterministicEmbedder{dim: 4}

	if err := store.Save(Entry{Content: "persistent graph alpha", Category: CategoryProjectKnowledge, Embedding: vecA}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "persistent graph beta", Category: CategoryProjectKnowledge, Embedding: vecB}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	var idA, idB string
	for _, e := range store.entries {
		switch e.Content {
		case "persistent graph alpha":
			idA = e.ID
		case "persistent graph beta":
			idB = e.ID
		}
	}
	before := store.graph.neighborsTypedOf(idB)[idA]
	store.mu.RUnlock()
	if idA == "" || idB == "" || before.Strength <= 0 {
		t.Fatalf("expected graph edge before reload, ids=(%q,%q) edge=%+v", idA, idB, before)
	}
	if err := store.flush(); err != nil {
		t.Fatal(err)
	}
	store.Stop()

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Stop()

	after := reloaded.graph.neighborsTypedOf(idB)[idA]
	if after.Strength != before.Strength {
		t.Fatalf("edge strength was not persisted: before=%f after=%f", before.Strength, after.Strength)
	}
	if after.LinkType != before.LinkType {
		t.Fatalf("edge link type was not persisted: before=%q after=%q", before.LinkType, after.LinkType)
	}
}

func TestAutoLink_DuplicateContent_NoAutoLink(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	emb := &fakeEmbedder{dim: 4}
	store.embedder = emb

	// Save an entry.
	if err := store.Save(Entry{
		Content:  "duplicate content test",
		Category: CategoryUserFact,
	}); err != nil {
		t.Fatal(err)
	}

	// Save same content again — should update, not create new + autolink.
	if err := store.Save(Entry{
		Content:  "duplicate content test",
		Category: CategoryUserFact,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(store.entries) != 1 {
		t.Errorf("expected 1 entry (dedup), got %d", len(store.entries))
	}
}

func TestAutoLink_TopKLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Create 5 entries all very similar (via embeddings), then add a 6th.
	// The 6th should link to at most autoLinkTopK (3) entries.
	// Each entry has enough unique content to avoid dedup.
	base := []float32{1, 0, 0, 0}
	vectors := map[string][]float32{}
	contents := []string{
		"PostgreSQL database optimization techniques for large-scale deployments",
		"MySQL query performance tuning with index analysis and explain plans",
		"MongoDB sharding strategy for horizontal scaling across data centers",
		"Redis caching patterns for session management and rate limiting",
		"Elasticsearch cluster configuration for full-text search workloads",
	}
	for i, c := range contents {
		v := make([]float32, 4)
		copy(v, base)
		v[1] = float32(i) * 0.005
		vectors[c] = v
	}
	// The new entry is also very similar (via embeddings).
	newContent := "CockroachDB distributed transaction handling with Raft consensus protocol"
	vectors[newContent] = []float32{0.99, 0.01, 0, 0}

	emb := &deterministicEmbedder{dim: 4, vectors: vectors}
	store.embedder = emb

	for _, c := range contents {
		if err := store.Save(Entry{
			Content:   c,
			Category:  CategoryProjectKnowledge,
			Embedding: vectors[c],
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Save the new entry.
	if err := store.Save(Entry{
		Content:   newContent,
		Category:  CategoryProjectKnowledge,
		Embedding: vectors[newContent],
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	var newEntry *Entry
	for i := range store.entries {
		if store.entries[i].Content == newContent {
			newEntry = &store.entries[i]
			break
		}
	}
	if newEntry == nil {
		t.Fatal("new entry not found")
	}

	// The new entry should have at most autoLinkTopK related IDs from this save.
	// (It may have fewer if graph eviction kicked in from bidirectional links.)
	if len(newEntry.RelatedIDs) > autoLinkTopK {
		t.Errorf("expected at most %d related IDs from single save, got %d",
			autoLinkTopK, len(newEntry.RelatedIDs))
	}
}

// Verify RelatedIDs are sorted for deterministic persistence.
func TestAutoLink_RelatedIDs_Sorted(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	base := []float32{1, 0, 0, 0}
	vectors := map[string][]float32{
		"sort test alpha": {0.99, 0.01, 0, 0},
		"sort test beta":  {0.98, 0.02, 0, 0},
		"sort test gamma": base,
	}

	emb := &deterministicEmbedder{dim: 4, vectors: vectors}
	store.embedder = emb

	for _, c := range []string{"sort test alpha", "sort test beta"} {
		if err := store.Save(Entry{
			Content:   c,
			Category:  CategoryProjectKnowledge,
			Embedding: vectors[c],
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Save(Entry{
		Content:   "sort test gamma",
		Category:  CategoryProjectKnowledge,
		Embedding: vectors["sort test gamma"],
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	for _, e := range store.entries {
		if len(e.RelatedIDs) > 1 {
			sorted := make([]string, len(e.RelatedIDs))
			copy(sorted, e.RelatedIDs)
			sort.Strings(sorted)
			for i := range sorted {
				if sorted[i] != e.RelatedIDs[i] {
					t.Errorf("entry %q RelatedIDs not sorted: %v", e.Content, e.RelatedIDs)
					break
				}
			}
		}
	}
}

func TestDeleteSynchronizesPersistedGraphEdges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.1, 0, 0}
	store.embedder = &deterministicEmbedder{dim: 4}

	if err := store.Save(Entry{Content: "delete graph alpha", Category: CategoryProjectKnowledge, Embedding: vecA}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "delete graph beta", Category: CategoryProjectKnowledge, Embedding: vecB}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	var keepID, deleteID string
	for _, e := range store.entries {
		switch e.Content {
		case "delete graph alpha":
			deleteID = e.ID
		case "delete graph beta":
			keepID = e.ID
		}
	}
	store.mu.RUnlock()
	if keepID == "" || deleteID == "" {
		t.Fatalf("expected saved entry IDs, keep=%q delete=%q", keepID, deleteID)
	}

	if err := store.Delete(deleteID); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	for _, e := range store.entries {
		if e.ID == keepID {
			if len(e.RelatedIDs) != 0 || len(e.RelatedEdges) != 0 {
				store.mu.RUnlock()
				t.Fatalf("delete left stale persisted graph links: ids=%v edges=%+v", e.RelatedIDs, e.RelatedEdges)
			}
		}
	}
	store.mu.RUnlock()

	if err := store.flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), deleteID) {
		t.Fatalf("deleted entry ID %q remained in persisted memory JSON: %s", deleteID, string(data))
	}
}
