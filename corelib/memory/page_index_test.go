package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPageIndex_IndexCompactedPage_Basic(t *testing.T) {
	pi := NewPageIndex()

	entries := []Entry{
		{ID: "e1", Content: "Created file D:\\workprj\\project\\main.go with package declaration", Status: StatusActive},
		{ID: "e2", Content: "User confirmed the architecture design using microservices", Status: StatusActive, Tags: []string{"architecture", "microservices"}},
		{ID: "e3", Content: "Tool output: npm install completed successfully", Status: StatusActive},
	}

	err := pi.IndexCompactedPage("user1", entries)
	if err != nil {
		t.Fatalf("IndexCompactedPage failed: %v", err)
	}

	if pi.PageCount("user1") != 1 {
		t.Errorf("expected 1 page, got %d", pi.PageCount("user1"))
	}
}

func TestPageIndex_Query_MatchesFilePath(t *testing.T) {
	pi := NewPageIndex()

	entries := []Entry{
		{ID: "e1", Content: "Edited D:\\workprj\\aicoder\\main.go to add new function", Status: StatusActive},
	}

	pi.IndexCompactedPage("user1", entries)

	candidates := pi.Query("user1", "main.go", nil)
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate for main.go query")
	}

	found := false
	for _, c := range candidates {
		if strings.Contains(c.Content, "main.go") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected candidate containing main.go")
	}
}

func TestPageIndex_Query_MatchesEntity(t *testing.T) {
	pi := NewPageIndex()

	entries := []Entry{
		{ID: "e1", Content: "Connected to api.rapidai.tech server via SSH", Status: StatusActive, Tags: []string{"ssh", "api-server"}},
	}

	pi.IndexCompactedPage("user1", entries)

	candidates := pi.Query("user1", "api server", nil)
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate for 'api server' query")
	}
}

func TestPageIndex_Query_RecencyBoost(t *testing.T) {
	pi := NewPageIndex()

	// Index 3 pages: oldest to newest.
	for i := 0; i < 3; i++ {
		entries := []Entry{
			{ID: fmt.Sprintf("e%d", i), Content: fmt.Sprintf("Project phase%d config file", i), Status: StatusActive, Tags: []string{fmt.Sprintf("phase%d", i)}},
		}
		pi.IndexCompactedPage("user1", entries)
		time.Sleep(time.Millisecond) // ensure different timestamps
	}

	// Query should boost most recent page.
	candidates := pi.Query("user1", "config file", nil)
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}

	// Most recent page should have highest score due to recency boost.
	// Page 2 (most recent, distance=0) gets +3.0, page 1 (distance=1) gets +2.0,
	// page 0 (distance=2) gets +1.0.
	for i := 1; i < len(candidates); i++ {
		if candidates[i].Score > candidates[i-1].Score {
			t.Errorf("candidates not sorted by score: [%d]=%f > [%d]=%f",
				i, candidates[i].Score, i-1, candidates[i-1].Score)
		}
	}
}

func TestPageIndex_FIFO_Eviction(t *testing.T) {
	pi := NewPageIndex()

	// Index 21 pages (exceeds maxPagesPerUser=20).
	for i := 0; i < 21; i++ {
		entries := []Entry{
			{ID: fmt.Sprintf("e%d", i), Content: fmt.Sprintf("Unique content for page %d with special-keyword-%d", i, i), Status: StatusActive},
		}
		pi.IndexCompactedPage("user1", entries)
	}

	// Should keep only the most recent 15 pages.
	if pi.PageCount("user1") != keepPagesAfterEviction {
		t.Errorf("expected %d pages after eviction, got %d", keepPagesAfterEviction, pi.PageCount("user1"))
	}

	// Oldest pages (0-5) should be gone. Query for page 0's unique content.
	candidates := pi.Query("user1", "special-keyword-0", nil)
	if len(candidates) > 0 {
		t.Error("expected page 0 content to be evicted")
	}

	// Most recent page (20) should still be queryable.
	candidates = pi.Query("user1", "special-keyword-20", nil)
	if len(candidates) == 0 {
		t.Error("expected page 20 content to still be present")
	}
}

func TestPageIndex_Dedup_SHA256(t *testing.T) {
	pi := NewPageIndex()

	// Index same file path in two pages — should be deduped.
	entries1 := []Entry{
		{ID: "e1", Content: "Read file /home/user/project/config.yaml", Status: StatusActive},
	}
	entries2 := []Entry{
		{ID: "e2", Content: "Updated file /home/user/project/config.yaml with new settings", Status: StatusActive},
	}

	pi.IndexCompactedPage("user1", entries1)
	pi.IndexCompactedPage("user1", entries2)

	// Query for config.yaml — should only appear once (from first page).
	candidates := pi.Query("user1", "config.yaml", nil)

	pathCount := 0
	for _, c := range candidates {
		if c.Kind == "file_path" && strings.Contains(c.Content, "config.yaml") {
			pathCount++
		}
	}
	if pathCount > 1 {
		t.Errorf("expected dedup to prevent duplicate file_path, got %d", pathCount)
	}
}

func TestPageIndex_Clear(t *testing.T) {
	pi := NewPageIndex()

	entries := []Entry{
		{ID: "e1", Content: "Some project knowledge about D:\\work\\app\\server.go", Status: StatusActive},
	}
	pi.IndexCompactedPage("user1", entries)

	if pi.PageCount("user1") != 1 {
		t.Fatal("expected 1 page before clear")
	}

	pi.Clear("user1")

	if pi.PageCount("user1") != 0 {
		t.Errorf("expected 0 pages after clear, got %d", pi.PageCount("user1"))
	}

	candidates := pi.Query("user1", "server.go", nil)
	if len(candidates) != 0 {
		t.Error("expected no candidates after clear")
	}
}

func TestPageIndex_EmptyEntries(t *testing.T) {
	pi := NewPageIndex()

	err := pi.IndexCompactedPage("user1", nil)
	if err != nil {
		t.Fatalf("unexpected error for empty entries: %v", err)
	}

	if pi.PageCount("user1") != 0 {
		t.Errorf("expected 0 pages for empty entries, got %d", pi.PageCount("user1"))
	}
}

func TestPageIndex_MultiUser_Isolation(t *testing.T) {
	pi := NewPageIndex()

	entries1 := []Entry{
		{ID: "e1", Content: "User1 project at C:\\users\\user1\\project\\app.js", Status: StatusActive},
	}
	entries2 := []Entry{
		{ID: "e2", Content: "User2 project at C:\\users\\user2\\project\\server.py", Status: StatusActive},
	}

	pi.IndexCompactedPage("user1", entries1)
	pi.IndexCompactedPage("user2", entries2)

	// user1 should not see user2's content.
	candidates := pi.Query("user1", "server.py", nil)
	if len(candidates) > 0 {
		t.Error("user1 should not see user2's indexed content")
	}

	// user2 should not see user1's content.
	candidates = pi.Query("user2", "app.js", nil)
	if len(candidates) > 0 {
		t.Error("user2 should not see user1's indexed content")
	}
}

func TestPageIndex_MaxItemsPerPage(t *testing.T) {
	pi := NewPageIndex()

	// Create an entry with lots of file paths and entities.
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("/home/user/project/file%d.go", i))
	}
	entries := []Entry{
		{ID: "e1", Content: strings.Join(lines, "\n"), Status: StatusActive},
	}

	pi.IndexCompactedPage("user1", entries)

	// Verify that items are capped.
	pi.mu.RLock()
	upi := pi.users["user1"]
	itemCount := len(upi.pages[0].Items)
	pi.mu.RUnlock()

	if itemCount > maxItemsPerPage {
		t.Errorf("expected at most %d items, got %d", maxItemsPerPage, itemCount)
	}
}

func TestPageIndex_IndexingPerformance(t *testing.T) {
	pi := NewPageIndex()

	// Create 100 entries to test indexing performance.
	entries := make([]Entry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = Entry{
			ID:      fmt.Sprintf("e%d", i),
			Content: fmt.Sprintf("Working on file /home/user/project/module%d/handler.go with database connection to postgres://localhost:5432/db%d. Decided to use REST API for module%d interface.", i, i, i),
			Status:  StatusActive,
			Tags:    []string{fmt.Sprintf("module%d", i), "golang", "rest-api"},
		}
	}

	start := time.Now()
	err := pi.IndexCompactedPage("user1", entries)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("IndexCompactedPage failed: %v", err)
	}

	// Requirement: complete within 500ms for up to 100 entries.
	if elapsed > 500*time.Millisecond {
		t.Errorf("indexing took %v, expected < 500ms", elapsed)
	}
}

func TestPageIndex_Query_WithQueryTokens(t *testing.T) {
	pi := NewPageIndex()

	entries := []Entry{
		{ID: "e1", Content: "Deployed service to api.rapidai.tech production server", Status: StatusActive, Tags: []string{"api-server", "deployment"}},
	}
	pi.IndexCompactedPage("user1", entries)

	// Query using pre-computed queryTokens (as done by ExpandQuery).
	queryTokens := []string{"api", "server", "deployment"}
	candidates := pi.Query("user1", "", queryTokens)
	if len(candidates) == 0 {
		t.Fatal("expected candidates when querying with queryTokens")
	}
}

func TestPageIndex_DecisionExtraction(t *testing.T) {
	pi := NewPageIndex()

	entries := []Entry{
		{ID: "e1", Content: "After discussion, we decided to use PostgreSQL instead of MySQL for the database.", Status: StatusActive},
		{ID: "e2", Content: "用户确认了使用React框架开发前端界面的方案", Status: StatusActive},
	}
	pi.IndexCompactedPage("user1", entries)

	// Query for the decision.
	candidates := pi.Query("user1", "postgresql database", nil)
	found := false
	for _, c := range candidates {
		if c.Kind == "decision" && strings.Contains(strings.ToLower(c.Content), "postgresql") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find decision about PostgreSQL")
	}
}
