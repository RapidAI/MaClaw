package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// A short Han span is matched through the deterministic bigram contract, not
// through a gse token that may differ between the index and the query.
func TestCJKSpanQueryUsesDeterministicBigrams(t *testing.T) {
	cases := map[string]string{
		"文件系统":   `"文件" AND "件系" AND "系统"`,
		"操作系统":   `"操作" AND "作系" AND "系统"`,
		"文件系统驱动": `"文件" AND "件系" AND "系统" AND "驱动"`,
		"学历":     `"学历"`,
		"马勇":     `"马勇"`,
	}
	for query, want := range cases {
		if got := buildFTSQuerySegmented(query); got != want {
			t.Fatalf("buildFTSQuerySegmented(%q) = %s, want %s", query, got, want)
		}
	}
}

// Bigrams are taken per contiguous script run. Joining the remaining
// characters of two sentences would index pairs that never occur.
func TestSegmentedTextDoesNotFormBigramsAcrossRuns(t *testing.T) {
	indexed := " " + segmentTextForFTS("文件。系统") + " "
	if !strings.Contains(indexed, " 文件 ") || !strings.Contains(indexed, " 系统 ") {
		t.Fatalf("per-run bigrams missing: %q", indexed)
	}
	if strings.Contains(indexed, " 件系 ") {
		t.Fatalf("bigram formed across a sentence boundary: %q", indexed)
	}
}

// Recall must not depend on the exact dictionary token. A legacy index can
// hold "文件 系统" with both bigrams present, while the query asks for
// 文件系统; the bigram intersection still finds that row.
func TestCJKQueryMatchesLegacySegmentedRow(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "深入解析Windows文件系统",
		Text:  "Windows 文件系统负责管理磁盘上的文件和目录。",
	}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	var nodeID string
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM document_nodes LIMIT 1`).Scan(&nodeID); err != nil {
		t.Fatalf("read node id: %v", err)
	}
	// Simulate an index written by an older dictionary pass: the span has no
	// "文件系统" token, only its pieces and the per-run bigrams.
	if _, err := store.db.ExecContext(ctx, `UPDATE document_nodes_fts SET text = ? WHERE node_id = ?`,
		"windows 文件 系统 负责 管理 磁盘 文件 目录 文件 件系 系统", nodeID); err != nil {
		t.Fatalf("rewrite fts row: %v", err)
	}

	rows, err := store.db.QueryContext(ctx,
		`SELECT node_id FROM document_nodes_fts WHERE document_nodes_fts MATCH ?`,
		buildFTSQuerySegmented("文件系统"))
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if id == nodeID {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !found {
		t.Fatalf("bigram query did not match legacy row %s", nodeID)
	}

	results, err := store.Search(ctx, SearchOptions{Query: "文件系统", ResultTypes: []string{"node"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, result := range results {
		if result.NodeID == nodeID {
			return
		}
	}
	t.Fatalf("Search lost the legacy row: %#v", results)
}

// The LIKE safety net is skipped only when the pool this query would keep is
// already full of nodes that contain every span. One hit is not coverage.
func TestStrictCoverageRequiresSaturatedPool(t *testing.T) {
	single := []SearchResult{{ResultType: "node", Snippet: "文件系统概述"}}
	if strictAnchorsAlreadyCovered("文件系统", single, 2) {
		t.Fatal("a single hit must not skip the LIKE scan")
	}
	full := []SearchResult{
		{ResultType: "node", Snippet: "文件系统概述"},
		{ResultType: "node", Snippet: "文件系统驱动"},
	}
	if !strictAnchorsAlreadyCovered("文件系统", full, 2) {
		t.Fatal("a saturated pool should skip a redundant LIKE scan")
	}
	if _, strict := bm25.QueryAnchors("文件系统"); !strict {
		t.Fatal("expected 文件系统 to be a strict short-span query")
	}
}
