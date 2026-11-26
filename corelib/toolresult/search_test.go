package toolresult

import (
	"strings"
	"testing"
)

func TestReadQueryFindsKeywordAcrossChunksWithoutReturningPrefix(t *testing.T) {
	root := t.TempDir()
	raw := strings.Repeat("x", searchChunkBytes-4) + "needle" + strings.Repeat("y", 20_000)
	projection, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner", Content: raw, Limit: 1024, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Read(ReadOptions{
		ID:          projection.Handle.ID,
		SessionKey:  "owner",
		Root:        root,
		Query:       "NeEdLe",
		BeforeBytes: 8,
		AfterBytes:  9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 1 || !strings.Contains(res.Content, "needle") {
		t.Fatalf("cross-chunk match missing: %+v", res)
	}
	if strings.Contains(res.Content, strings.Repeat("x", 128)) {
		t.Fatal("query returned a large unrelated prefix")
	}
	if res.ReturnedBytes > 1024 || res.TotalBytes != len(raw) || res.Truncated {
		t.Fatalf("unexpected bounded search result: %+v", res)
	}
	if !strings.Contains(FormatReadResult(res), `query: "NeEdLe"`) {
		t.Fatalf("formatted result lost query metadata: %s", FormatReadResult(res))
	}
}

func TestFormatReadResultKeepsQueryOnTruncatedSearchHint(t *testing.T) {
	got := FormatReadResult(ReadResult{
		ID:         "h",
		Offset:     0,
		Query:      "needle",
		Truncated:  true,
		NextOffset: 400,
		Matches:    1,
		Content:    "hit",
	})
	if !strings.Contains(got, `query "needle"`) || !strings.Contains(got, "offset=400") || !strings.Contains(got, "do not drop the query") {
		t.Fatalf("truncated search hint dropped the query:\n%s", got)
	}
	bytePage := FormatReadResult(ReadResult{ID: "h", Offset: 0, Truncated: true, NextOffset: 16384})
	if strings.Contains(bytePage, "query") {
		t.Fatalf("byte page hint invented a query:\n%s", bytePage)
	}
}

func TestReadQueryPaginatesMatchesByOffset(t *testing.T) {
	root := t.TempDir()
	projection, err := Project(ProjectOptions{
		ToolName:   "bash",
		SessionKey: "owner",
		Content:    "needle\n----\nneedle\n----\nneedle\n",
		Root:       root,
		ForceSpill: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := Read(ReadOptions{
		ID:          projection.Handle.ID,
		SessionKey:  "owner",
		Root:        root,
		Query:       "needle",
		BeforeBytes: 1,
		AfterBytes:  1,
		MaxMatches:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Truncated || first.NextOffset != 18 || first.Matches != 2 {
		t.Fatalf("first search page = %+v", first)
	}
	second, err := Read(ReadOptions{
		ID:          projection.Handle.ID,
		SessionKey:  "owner",
		Root:        root,
		Offset:      first.NextOffset,
		Query:       "needle",
		BeforeBytes: 1,
		AfterBytes:  1,
		MaxMatches:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Truncated || second.NextOffset != projection.Handle.OriginalBytes || second.Matches != 1 {
		t.Fatalf("second search page = %+v", second)
	}
}

func TestReadQueryDecryptsCheckpointOnceAndReturnsMatch(t *testing.T) {
	root := t.TempDir()
	raw := `{"role":"tool","content":"prefix"},"` + `{"role":"assistant","content":"decision: keep D:/work/demo.go"}`
	projection, err := Project(ProjectOptions{
		ToolName:   "context_checkpoint",
		SessionKey: "owner",
		Content:    raw,
		Root:       root,
		ForceSpill: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if projection.Handle == nil || !projection.Handle.Encrypted {
		t.Fatal("checkpoint must stay encrypted at rest")
	}
	res, err := Read(ReadOptions{
		ID:          projection.Handle.ID,
		SessionKey:  "owner",
		Root:        root,
		Query:       "D:/work/demo.go",
		BeforeBytes: 12,
		AfterBytes:  4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 1 || !strings.Contains(res.Content, "D:/work/demo.go") {
		t.Fatalf("encrypted match = %+v", res)
	}
}

func TestReadQueryNoMatchCompletesWithoutSequentialPage(t *testing.T) {
	root := t.TempDir()
	projection, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner", Content: strings.Repeat("abcdef", 1000), Root: root})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Read(ReadOptions{ID: projection.Handle.ID, SessionKey: "owner", Root: root, Query: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 0 || res.Content != "" || res.Truncated || res.NextOffset != len("abcdef")*1000 {
		t.Fatalf("no-match result = %+v", res)
	}
}
