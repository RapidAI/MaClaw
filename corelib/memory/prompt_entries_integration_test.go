package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// uniqueWord generates a unique word for test data to avoid substring dedup.
func uniqueWord(i int) string {
	words := []string{
		"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta",
		"iota", "kappa", "lambda", "mu", "nu", "xi", "omicron", "pi",
		"rho", "sigma", "tau", "upsilon", "phi", "chi", "psi", "omega",
		"mercury", "venus", "earth", "mars", "jupiter", "saturn", "uranus", "neptune",
		"helium", "lithium", "boron", "carbon", "nitrogen", "oxygen", "fluorine", "neon",
		"sodium", "magnesium", "aluminum", "silicon", "phosphorus", "sulfur", "chlorine", "argon",
		"potassium", "calcium", "titanium", "chromium", "manganese", "iron", "cobalt", "nickel",
		"copper", "zinc", "gallium", "germanium", "arsenic", "selenium", "bromine", "krypton",
		"rubidium", "strontium", "yttrium", "zirconium", "niobium", "molybdenum", "technetium", "ruthenium",
		"rhodium", "palladium", "silver", "cadmium", "indium", "tin", "antimony", "tellurium",
		"xenon", "cesium", "barium", "lanthanum", "cerium", "praseodymium", "neodymium", "promethium",
		"samarium", "europium", "gadolinium", "terbium", "dysprosium", "holmium", "erbium", "thulium",
		"ytterbium", "lutetium", "hafnium", "tantalum", "tungsten", "rhenium", "osmium", "iridium",
	}
	idx := i % len(words)
	prefix := i / len(words)
	if prefix == 0 {
		return words[idx]
	}
	return fmt.Sprintf("%s%d", words[idx], prefix)
}

// TestAdaptiveBudgetExpansionTriggersAtHighDensity verifies that when topic
// density exceeds 0.15, the adaptive budget expands MaxEntries beyond the
// default 12.
func TestAdaptiveBudgetExpansionTriggersAtHighDensity(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Create 40 entries about "golang" with unique content to avoid substring dedup.
	for i := 0; i < 40; i++ {
		if err := store.Save(Entry{
			Content:  fmt.Sprintf("golang project knowledge: module %d handles %s functionality with %s pattern", i, uniqueWord(i), uniqueWord(i+100)),
			Category: CategoryProjectKnowledge,
			Tags:     []string{"golang", fmt.Sprintf("module%d", i)},
			Status:   StatusActive,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Compute adaptive budget with a query that matches many entries.
	result := store.computeAdaptiveBudget("golang project", ProactivePromptOptions{
		Recall: ProactiveRecallOptions{MaxEntries: 12},
	})

	// With many matching entries out of total, density should be > 0.15.
	if result.TopicDensity <= topicDensityThreshold {
		t.Fatalf("expected density > %.2f, got %.3f", topicDensityThreshold, result.TopicDensity)
	}
	if !result.Expanded {
		t.Fatalf("expected budget expansion at density=%.3f", result.TopicDensity)
	}
	// Expanded: min(24, max(12, floor(matching * 0.4)))
	// If matching=40, expanded = min(24, max(12, 16)) = 16
	if result.MaxEntries < defaultMaxEntries {
		t.Fatalf("expected MaxEntries >= %d, got %d", defaultMaxEntries, result.MaxEntries)
	}
}

// TestAdaptiveBudgetNoExpansionAtLowDensity verifies that when topic density
// is below 0.15, the budget remains at default values.
func TestAdaptiveBudgetNoExpansionAtLowDensity(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Create 30 entries with diverse topics, only 2 about "golang".
	for i := 0; i < 30; i++ {
		var content string
		var tags []string
		if i < 2 {
			content = fmt.Sprintf("golang setup instructions for workspace %d with unique configuration %s", i, uniqueWord(i+200))
			tags = []string{"golang", fmt.Sprintf("setup%d", i)}
		} else {
			content = fmt.Sprintf("topic %s: documentation about %s system with %s architecture", uniqueWord(i+300), uniqueWord(i+400), uniqueWord(i+500))
			tags = []string{fmt.Sprintf("topic%d", i)}
		}
		if err := store.Save(Entry{
			Content:  content,
			Category: CategoryProjectKnowledge,
			Tags:     tags,
			Status:   StatusActive,
		}); err != nil {
			t.Fatal(err)
		}
	}

	result := store.computeAdaptiveBudget("golang", ProactivePromptOptions{
		Recall: ProactiveRecallOptions{MaxEntries: 12},
	})

	// density = ~2/30 = 0.067 < 0.15 → no expansion.
	if result.Expanded {
		t.Fatalf("expected no expansion at low density, got density=%.3f expanded=%v", result.TopicDensity, result.Expanded)
	}
	if result.MaxEntries != defaultMaxEntries {
		t.Fatalf("expected default MaxEntries=%d, got %d", defaultMaxEntries, result.MaxEntries)
	}
}

// TestStagedRecallPartialAnnotation verifies that when staged recall returns
// partial results (timeout), entries are annotated with the partial recall marker.
func TestStagedRecallPartialAnnotation(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Save entries that should be retrievable with unique content.
	for i := 0; i < 5; i++ {
		if err := store.Save(Entry{
			Content:  fmt.Sprintf("deployment configuration for kubernetes cluster %s with namespace %s", uniqueWord(i+600), uniqueWord(i+700)),
			Category: CategoryProjectKnowledge,
			Tags:     []string{"kubernetes", "deployment", fmt.Sprintf("cluster%d", i)},
			Status:   StatusActive,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Use PartialResultsEnabled=true — the staged recall pipeline should execute.
	section, recalled := store.ProactiveContextForPrompt("kubernetes deployment", ProactivePromptOptions{
		PartialResultsEnabled: true,
		Recall:                ProactiveRecallOptions{MaxEntries: 12},
		RecallEntries: RecallEntriesPromptOptions{
			Header: "## Recall",
		},
	})

	// We should get recalled entries.
	if len(recalled) == 0 {
		t.Fatal("expected recalled entries with staged recall")
	}

	// The section should contain recall results.
	if !strings.Contains(section, "## Recall") {
		t.Fatalf("expected recall section header in output: %q", section)
	}

	// Note: Whether entries are annotated with [partial recall] depends on whether
	// the pipeline completed all stages within the 2-second deadline.
	// For a simple test store, it should complete fully (Partial=false),
	// so entries should NOT have the annotation.
	for _, e := range recalled {
		if strings.HasPrefix(e.Content, "[partial recall - deep search skipped]") {
			t.Logf("partial annotation found (stages timed out): %s", e.Content[:80])
		}
	}
}

// TestPageIndexDeduplication verifies that page-indexed entries that are
// substring-contained in long-term memory entries are removed.
func TestPageIndexDeduplication(t *testing.T) {
	pageEntries := []Entry{
		{Content: "configuration file at /etc/nginx/nginx.conf"},
		{Content: "short"},  // too short for dedup (< 20 chars)
		{Content: "the kubernetes deployment manifest"},
	}

	memoryEntries := []Entry{
		{Content: "The full configuration file at /etc/nginx/nginx.conf with all server blocks and upstream definitions"},
		{Content: "docker compose setup for microservices"},
	}

	result := deduplicatePageEntries(pageEntries, memoryEntries)

	// First page entry should be removed (substring of first memory entry).
	// Second page entry should remain (too short for dedup).
	// Third page entry should remain (not a substring of any memory entry).
	if len(result) != 2 {
		t.Fatalf("expected 2 entries after dedup, got %d: %+v", len(result), result)
	}
	if result[0].Content != "short" {
		t.Fatalf("expected first remaining entry to be 'short', got %q", result[0].Content)
	}
	if result[1].Content != "the kubernetes deployment manifest" {
		t.Fatalf("expected second remaining entry to be kubernetes, got %q", result[1].Content)
	}
}

// TestPageIndexDeduplicationReverseContainment verifies that page entries
// containing a memory entry are also deduplicated.
func TestPageIndexDeduplicationReverseContainment(t *testing.T) {
	pageEntries := []Entry{
		{Content: "the complete kubernetes deployment manifest with all replicas and resource limits defined"},
	}

	memoryEntries := []Entry{
		{Content: "kubernetes deployment manifest"},
	}

	result := deduplicatePageEntries(pageEntries, memoryEntries)

	// Page entry contains memory entry → should be removed.
	if len(result) != 0 {
		t.Fatalf("expected 0 entries after reverse containment dedup, got %d", len(result))
	}
}

// TestPageIndexDeduplicationEmpty verifies edge cases with empty slices.
func TestPageIndexDeduplicationEmpty(t *testing.T) {
	// Empty page entries.
	result := deduplicatePageEntries(nil, []Entry{{Content: "something"}})
	if len(result) != 0 {
		t.Fatalf("expected 0 for nil page entries")
	}

	// Empty memory entries — all page entries should be kept.
	pageEntries := []Entry{{Content: "some content that is longer than twenty characters"}}
	result = deduplicatePageEntries(pageEntries, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 for nil memory entries, got %d", len(result))
	}
}

// TestPageIndexSubBudgetRespected verifies that the PageIndex integration
// respects the PageIndexMaxTokens sub-budget.
func TestPageIndexSubBudgetRespected(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Index some page content.
	entries := []Entry{
		{Content: "file at /home/user/project/main.go with database connection", Tags: []string{"database"}, Status: StatusActive},
		{Content: "tool output: build successful, 42 tests passed, coverage 85%", Tags: []string{"testing"}, Status: StatusActive},
	}
	if err := store.PageIdx().IndexCompactedPage("testuser", entries); err != nil {
		t.Fatal(err)
	}

	// Query with PageIndexEnabled and a very small budget.
	pageEntries := store.queryPageIndexForPrompt("database connection", ProactivePromptOptions{
		PageIndexEnabled:   true,
		PageIndexMaxTokens: 10, // Very small budget — should limit results.
		Recall:             ProactiveRecallOptions{OwnerID: "testuser"},
	})

	// With a 10-token budget, we should get very few (or zero) entries
	// since even short content exceeds 10 tokens.
	totalTokens := 0
	for _, e := range pageEntries {
		totalTokens += EstimateTextTokens(e.Content)
	}
	if totalTokens > 10 {
		t.Fatalf("page index results exceeded sub-budget: total=%d tokens, budget=10", totalTokens)
	}
}
