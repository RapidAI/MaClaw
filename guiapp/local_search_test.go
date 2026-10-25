package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestLocalSearchKnowledgeKeepsCitationPayload(t *testing.T) {
	app := newProjectSearchTestApp(t)
	t.Cleanup(func() {
		if app.enterpriseClient != nil {
			_ = app.enterpriseClient.Close()
			app.enterpriseClient = nil
		}
	})
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := app.knowledgeContext()
	if err := store.SaveSource(ctx, knowledge.Source{ID: "local-src", Kind: knowledge.SourceKindText, URI: "manual://local", Status: knowledge.StatusParsed, Title: "saved note"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, knowledge.Card{ID: "local-card", SourceID: "local-src", Title: "Gateway", Claim: "UniqueKnowledgeToken lives in the saved note."}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := app.LocalSearch(LocalSearchRequest{Corpus: "knowledge", Query: "UniqueKnowledgeToken", Retrieval: "fts"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Corpus != "knowledge" || got.Route != "fts" || got.Freshness != "fresh" || len(got.Hits) == 0 {
		t.Fatalf("result = %+v", got)
	}
	if got.Hits[0].Knowledge == nil || got.Hits[0].Knowledge.CardID != "local-card" {
		t.Fatalf("knowledge payload = %+v", got.Hits[0])
	}
	if got.Hits[0].Locator == "" || got.Hits[0].Title == "" {
		t.Fatalf("hit = %+v", got.Hits[0])
	}
}

func TestLocalSearchRejectsKnowledgePatternAndEmptyRequest(t *testing.T) {
	app := newProjectSearchTestApp(t)
	if _, err := app.LocalSearch(LocalSearchRequest{Corpus: "knowledge", Pattern: "token"}); err == nil {
		t.Fatal("expected a knowledge pattern to fail")
	}
	if _, err := app.LocalSearch(LocalSearchRequest{Corpus: "project", Query: "token", Pattern: "token"}); err == nil {
		t.Fatal("expected both query and pattern to fail")
	}
	if _, err := app.LocalSearch(LocalSearchRequest{Corpus: "project"}); err == nil {
		t.Fatal("expected an empty request to fail")
	}
	if _, err := app.LocalSearch(LocalSearchRequest{Corpus: "archive", Query: "token"}); err == nil {
		t.Fatal("expected an unknown corpus to fail")
	}
	if _, err := app.LocalSearch(LocalSearchRequest{Corpus: "project", Query: "token", Retrieval: "vector"}); err == nil {
		t.Fatal("expected vector retrieval on a project scan to fail")
	}
}

func TestLocalSearchProjectScansLiteralAndRegex(t *testing.T) {
	app := newProjectSearchTestApp(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.go"), []byte("package p\nfunc UniqueProjectToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"), []byte("func UniqueProjectToken hidden\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	literal, err := app.LocalSearch(LocalSearchRequest{
		Corpus:    "project",
		Root:      root,
		Query:     "UniqueProjectToken",
		FileTypes: []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if literal.Route != "scan" || literal.Freshness != "unindexed" || len(literal.Hits) != 1 {
		t.Fatalf("literal = %+v", literal)
	}
	if literal.Hits[0].Line != 2 || !strings.Contains(literal.Hits[0].Snippet, "UniqueProjectToken") || literal.Hits[0].Title != "note.go" {
		t.Fatalf("literal hit = %+v", literal.Hits[0])
	}

	regex, err := app.LocalSearch(LocalSearchRequest{
		Corpus:  "project",
		Root:    root,
		Pattern: `func UniqueProject\w+`,
		Globs:   []string{"*.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(regex.Hits) != 1 || regex.Hits[0].Line != 2 {
		t.Fatalf("regex = %+v", regex.Hits)
	}
}

func TestLocalSearchCloudWorkspaceStaysUnindexed(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	root := seedCloudWorkspaceSearchCache(t, app, "cws_local01", map[string]string{
		"papers/brief.md": "竞品分析结论写在这里\n",
	}, []string{"papers/brief.md"})
	created := app.createTaskRecordWithWorkingDir("经营材料", "# 经营材料\n", []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag("cws_local01")}, root, true)
	if created.ProjectPath == "" {
		t.Fatal("task was not created")
	}

	got, err := app.LocalSearch(LocalSearchRequest{Corpus: "cloud_workspace", Query: "竞品分析"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Route != "scan" || got.Freshness != "unindexed" || len(got.Hits) != 1 {
		t.Fatalf("cloud = %+v", got)
	}
	hit := got.Hits[0]
	if hit.RelativePath != "papers/brief.md" || hit.WorkspaceID != "cws_local01" || hit.Match != "content" || !strings.Contains(hit.Snippet, "竞品分析") {
		t.Fatalf("cloud hit = %+v", hit)
	}
	if !strings.Contains(hit.Locator, "cws_local01") || !strings.Contains(hit.Locator, "papers/brief.md") {
		t.Fatalf("locator = %q", hit.Locator)
	}
}

func TestLocalSearchKnowledgeFactUsesCardTitle(t *testing.T) {
	fact := knowledge.SearchResult{
		ResultType: "fact",
		CardTitle:  "Gateway",
		Claim:      "card claim",
		Summary:    "card summary",
		Snippet:    "service port 18099",
		Subject:    "service",
		Predicate:  "port",
		Object:     "18099",
	}
	if got := localSearchKnowledgeTitle(fact); got != "Gateway" {
		t.Fatalf("title = %q", got)
	}
	if got := localSearchKnowledgeSnippet(fact); got != "service port 18099" {
		t.Fatalf("snippet = %q", got)
	}

	fromTriple := fact
	fromTriple.Snippet = ""
	if got := localSearchKnowledgeSnippet(fromTriple); got != "service port 18099" {
		t.Fatalf("triple snippet = %q", got)
	}

	marked := fact
	marked.Source.Title = "[企业] Doc"
	if got := localSearchKnowledgeTitle(marked); got != "[企业] Gateway" {
		t.Fatalf("marked title = %q", got)
	}
	byLibrary := fact
	byLibrary.LibraryID = "lib-policies"
	if got := localSearchKnowledgeTitle(byLibrary); got != "[企业] Gateway" {
		t.Fatalf("library title = %q", got)
	}
	already := byLibrary
	already.CardTitle = "[企业] Gateway"
	if got := localSearchKnowledgeTitle(already); got != "[企业] Gateway" {
		t.Fatalf("already marked title = %q", got)
	}

	bare := knowledge.SearchResult{ResultType: "fact", Claim: "distinct fact"}
	if localSearchKnowledgeTitle(bare) != "distinct fact" || localSearchKnowledgeSnippet(bare) != "distinct fact" {
		t.Fatalf("bare title = %q snippet = %q", localSearchKnowledgeTitle(bare), localSearchKnowledgeSnippet(bare))
	}
}

func TestParseRipgrepHitsKeepsUnparsedLines(t *testing.T) {
	hits := parseRipgrepHits("C:\\work\\note.go:12:func Token()\nnot a located line\n... reached max_results=20\n")
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Path != `C:\work\note.go` || hits[0].Line != 12 || hits[0].Snippet != "func Token()" {
		t.Fatalf("parsed = %+v", hits[0])
	}
	if hits[1].Line != 0 || hits[1].Snippet != "not a located line" {
		t.Fatalf("unparsed = %+v", hits[1])
	}

	located := parseRipgrepHits("C:\\work\\note.go:12:see section:3:done\n")
	if len(located) != 1 || located[0].Path != `C:\work\note.go` || located[0].Line != 12 || located[0].Snippet != "see section:3:done" {
		t.Fatalf("content colon = %+v", located)
	}
}

func TestProjectScanResultReportsAnUnfinishedEmptyWalk(t *testing.T) {
	got, err := projectScanResult("... search stopped before the tree was finished", agent.SearchToolOutcomeNoMatch)
	if err == nil || len(got.Hits) != 0 {
		t.Fatalf("stopped = %+v err=%v", got, err)
	}

	partial, err := projectScanResult("C:\\work\\note.go:4:token\n... search stopped before the tree was finished", agent.SearchToolOutcomeMatched)
	if err != nil || len(partial.Hits) != 1 || partial.Hits[0].Line != 4 {
		t.Fatalf("partial = %+v err=%v", partial, err)
	}

	empty, err := projectScanResult("no matches found (searched 2 files)", agent.SearchToolOutcomeNoMatch)
	if err != nil || len(empty.Hits) != 0 || empty.Route != "scan" {
		t.Fatalf("empty = %+v err=%v", empty, err)
	}
	if empty.Hits == nil {
		t.Fatal("empty scan returned a nil hit list")
	}

	_, err = projectScanResult("   ", agent.SearchToolOutcomeError)
	if err == nil || err.Error() != "project search failed" {
		t.Fatalf("blank error = %v", err)
	}
}
