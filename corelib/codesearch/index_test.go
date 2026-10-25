package codesearch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExpandIdentifiersSplitsCamelAndSnake(t *testing.T) {
	got := expandIdentifiers("parseConfig and config_parser")
	for _, want := range []string{"parseConfig", "parse", "Config", "config_parser", "config", "parser"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expand %q missing %q", got, want)
		}
	}
}

func TestSearchRanksDefinitionAboveReference(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "parse.go"), "package p\n\nfunc parseConfig(path string) {\n\t_ = path\n}\n")
	writeFile(t, filepath.Join(dir, "run.go"), "package p\n\nfunc run() {\n\tparseConfig(\"x\")\n}\n")
	for _, query := range []string{"parse config", "config parser"} {
		res := Search(Request{Root: dir, Query: query, Wait: 5 * time.Second})
		if res.Freshness != indexedFreshness || len(res.Hits) == 0 {
			t.Fatalf("query %q result %+v", query, res)
		}
		if res.Hits[0].Path != "parse.go" || res.Hits[0].Symbol != "parseConfig" {
			t.Fatalf("query %q top = %+v", query, res.Hits[0])
		}
	}
}

func TestSearchDownranksTests(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "parse.go"), "package p\n\nfunc parseConfig() {\n\t// parse config\n}\n")
	writeFile(t, filepath.Join(dir, "parse_test.go"), "package p\n\nfunc TestParseConfig() {\n\tparseConfig()\n}\n")
	res := Search(Request{Root: dir, Query: "parse config", Wait: 5 * time.Second})
	if len(res.Hits) == 0 || res.Hits[0].Path != "parse.go" {
		t.Fatalf("implementation should rank first, got %+v", res.Hits)
	}
}

func TestSearchFindsChineseComment(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "disk.go"), "package p\n\nfunc writeOut() {\n\t// 把配置写到磁盘\n}\n")
	res := Search(Request{Root: dir, Query: "把配置写到磁盘", Wait: 5 * time.Second})
	if res.Freshness != indexedFreshness || len(res.Hits) == 0 || res.Hits[0].Path != "disk.go" {
		t.Fatalf("chinese query = %+v", res)
	}
}

func TestNoteDirtyReloadsFileWithoutFullMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "parse.go")
	writeFile(t, path, "package p\n\nfunc parseConfig() {}\n")
	first := Search(Request{Root: dir, Query: "parse config", Wait: 5 * time.Second})
	if len(first.Hits) == 0 || first.Hits[0].Symbol != "parseConfig" {
		t.Fatalf("before rewrite %+v", first)
	}
	time.Sleep(20 * time.Millisecond)
	writeFile(t, path, "package p\n\nfunc saveWidget() {\n\t// save widget\n}\n")
	NoteDirty(path)
	saved := Search(Request{Root: dir, Query: "save widget", Wait: time.Second})
	if len(saved.Hits) == 0 || saved.Hits[0].Symbol != "saveWidget" {
		t.Fatalf("dirty reload = %+v", saved)
	}
	old := Search(Request{Root: dir, Query: "parse config", Wait: time.Second})
	for _, hit := range old.Hits {
		if hit.Symbol == "parseConfig" {
			t.Fatalf("old symbol still indexed: %+v", old.Hits)
		}
	}
}

func TestWarmingIsNotAnIndexedMiss(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "parse.go"), "package p\n\nfunc parseConfig() {}\n")
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	SetBuildPauseForTest(func() { <-release })
	defer SetBuildPauseForTest(nil)
	defer unlock()
	cold := Search(Request{Root: dir, Query: "parse config", Wait: 40 * time.Millisecond})
	if cold.Freshness != warmingFreshness || len(cold.Hits) != 0 || cold.Hint == "" {
		t.Fatalf("cold = %+v", cold)
	}
	unlock()
	warm := Search(Request{Root: dir, Query: "parse config", Wait: 5 * time.Second})
	if warm.Freshness != indexedFreshness || len(warm.Hits) == 0 {
		t.Fatalf("warm = %+v", warm)
	}
	miss := Search(Request{Root: dir, Query: "zzzz-not-a-symbol", Wait: time.Second})
	if miss.Freshness != indexedFreshness || len(miss.Hits) != 0 || miss.Hint == warmingHint {
		t.Fatalf("miss = %+v", miss)
	}
}

func TestWarmHoldsCorpusPastIdleEviction(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "parse.go"), "package p\n\nfunc writeConfig() {}\n")
	Warm(filepath.Join(dir, "missing"))
	stop := KeepWarm(dir)
	stop()
	stop()
	res := Search(Request{Root: dir, Query: "write config", Wait: 15 * time.Second})
	if res.Freshness != indexedFreshness || len(res.Hits) == 0 || res.Hits[0].Symbol != "writeConfig" {
		t.Fatalf("warm build %+v", res)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := rootKey(filepath.Clean(abs))
	storeMu.Lock()
	c := corpora[key]
	storeMu.Unlock()
	if c == nil {
		t.Fatal("corpus was not published")
	}
	c.used.Store(time.Now().Add(-corpusIdle - time.Second).UnixNano())
	Warm(dir)
	evictIdle(time.Now())
	storeMu.Lock()
	_, held := corpora[key]
	storeMu.Unlock()
	if !held {
		t.Fatal("warm corpus was evicted")
	}
	c.used.Store(time.Now().Add(-corpusIdle - time.Second).UnixNano())
	evictIdle(time.Now())
	storeMu.Lock()
	_, held = corpora[key]
	storeMu.Unlock()
	if held {
		t.Fatal("idle corpus stayed")
	}
}

func TestGitignoreAndScopedCap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "skipme/\n")
	writeFile(t, filepath.Join(dir, "skipme", "secret.go"), "package p\n\nfunc hiddenSecret() {}\n")
	writeFile(t, filepath.Join(dir, "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	writeFile(t, filepath.Join(dir, "pkg", "other.go"), "package p\n\nfunc otherVisible() {}\n")
	res := Search(Request{Root: dir, Query: "hidden secret", Wait: 5 * time.Second})
	for _, hit := range res.Hits {
		if strings.Contains(hit.Path, "skipme") {
			t.Fatalf("gitignored file indexed: %+v", hit)
		}
	}
	kept := Search(Request{Root: dir, Query: "kept visible", Wait: time.Second})
	if len(kept.Hits) == 0 || kept.Hits[0].Path != "keep.go" {
		t.Fatalf("kept = %+v", kept)
	}

	prev := SetMaxFilesForTest(1)
	defer SetMaxFilesForTest(prev)
	wide := t.TempDir()
	writeFile(t, filepath.Join(wide, "a.go"), "package p\n\nfunc alphaOne() {}\n")
	writeFile(t, filepath.Join(wide, "b", "b.go"), "package p\n\nfunc betaTwo() {}\n")
	scoped := Search(Request{Root: wide, Path: "b", Query: "beta two", Wait: 5 * time.Second})
	if !scoped.Scoped || len(scoped.Hits) == 0 || scoped.Hits[0].Path != "b/b.go" {
		t.Fatalf("scoped = %+v", scoped)
	}
}

func TestAnchoredGitignoreDoesNotCoverNestedDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "/generated\n/secret.go\n")
	writeFile(t, filepath.Join(dir, "generated", "secret.go"), "package p\n\nfunc hiddenSecret() {}\n")
	writeFile(t, filepath.Join(dir, "secret.go"), "package p\n\nfunc rootSecret() {}\n")
	writeFile(t, filepath.Join(dir, "pkg", "generated", "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	writeFile(t, filepath.Join(dir, "pkg", "secret.go"), "package p\n\nfunc pkgSecret() {}\n")
	hidden := Search(Request{Root: dir, Query: "hidden secret", Wait: 5 * time.Second})
	for _, hit := range hidden.Hits {
		if hit.Symbol == "hiddenSecret" || hit.Symbol == "rootSecret" {
			t.Fatalf("anchored root ignore indexed: %+v", hit)
		}
	}
	kept := Search(Request{Root: dir, Query: "kept visible", Wait: time.Second})
	if len(kept.Hits) == 0 || kept.Hits[0].Path != "pkg/generated/keep.go" {
		t.Fatalf("nested dir = %+v", kept)
	}
	pkg := Search(Request{Root: dir, Query: "pkg secret", Wait: time.Second})
	if len(pkg.Hits) == 0 || pkg.Hits[0].Symbol != "pkgSecret" {
		t.Fatalf("nested file = %+v", pkg)
	}

	extra := filepath.Join(dir, "pkg", "generated", "extra.go")
	writeFile(t, extra, "package p\n\nfunc extraVisible() {}\n")
	NoteDirty(extra)
	added := Search(Request{Root: dir, Query: "extra visible", Wait: time.Second})
	if len(added.Hits) == 0 || added.Hits[0].Symbol != "extraVisible" {
		t.Fatalf("nested note = %+v", added)
	}
	buried := filepath.Join(dir, "generated", "more.go")
	writeFile(t, buried, "package p\n\nfunc moreSecret() {}\n")
	before := testIndexBuilds.Load()
	NoteDirty(buried)
	got := Search(Request{Root: dir, Query: "more secret", Wait: time.Second})
	for _, hit := range got.Hits {
		if hit.Symbol == "moreSecret" {
			t.Fatalf("anchored note indexed: %+v", hit)
		}
	}
	if testIndexBuilds.Load() != before {
		t.Fatalf("anchored ignore rebuilt the index: %d -> %d", before, testIndexBuilds.Load())
	}
}

func TestScopedCorpusIgnoresNotesOutsideItsPath(t *testing.T) {
	prev := SetMaxFilesForTest(1)
	defer SetMaxFilesForTest(prev)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package p\n\nfunc alphaOne() {}\n")
	writeFile(t, filepath.Join(dir, "pkg", "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	scoped := Search(Request{Root: dir, Path: "pkg", Query: "kept visible", Wait: 5 * time.Second})
	if !scoped.Scoped || scoped.IndexedFiles != 1 || len(scoped.Hits) == 0 || scoped.Hits[0].Path != "pkg/keep.go" {
		t.Fatalf("scoped = %+v", scoped)
	}
	writeFile(t, filepath.Join(dir, "other", "noise.go"), "package p\n\nfunc noiseVisible() {}\n")
	NoteDirty(filepath.Join(dir, "other", "noise.go"))
	after := Search(Request{Root: dir, Path: "pkg", Query: "kept visible", Wait: time.Second})
	if after.IndexedFiles != 1 {
		t.Fatalf("outside note entered the scoped index: %+v", after)
	}
	for _, hit := range after.Hits {
		if hit.Symbol == "noiseVisible" {
			t.Fatalf("outside hit: %+v", hit)
		}
	}
	extra := filepath.Join(dir, "pkg", "extra.go")
	writeFile(t, extra, "package p\n\nfunc extraVisible() {}\n")
	NoteDirty(extra)
	added := Search(Request{Root: dir, Path: "pkg", Query: "extra visible", Wait: time.Second})
	if added.IndexedFiles != 2 || len(added.Hits) == 0 || added.Hits[0].Symbol != "extraVisible" {
		t.Fatalf("in-scope note = %+v", added)
	}
}

func TestDirtyFileInsideGitignoreIsNotIndexed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "generated\n")
	writeFile(t, filepath.Join(dir, "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	first := Search(Request{Root: dir, Query: "kept visible", Wait: 5 * time.Second})
	if len(first.Hits) == 0 || first.Hits[0].Symbol != "keptVisible" {
		t.Fatalf("keep %+v", first)
	}
	secret := filepath.Join(dir, "generated", "secret.go")
	writeFile(t, secret, "package p\n\nfunc hiddenSecret() {}\n")
	before := testIndexBuilds.Load()
	NoteDirty(secret)
	got := Search(Request{Root: dir, Query: "hidden secret", Wait: time.Second})
	for _, hit := range got.Hits {
		if strings.Contains(hit.Path, "generated") || hit.Symbol == "hiddenSecret" {
			t.Fatalf("indexed gitignored file: %+v", hit)
		}
	}
	if testIndexBuilds.Load() != before {
		t.Fatalf("gitignore note rebuilt the index: %d -> %d", before, testIndexBuilds.Load())
	}
}

func TestGitignoreNegationKeepsExplicitFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "generated/*\n!generated/keep.go\n")
	writeFile(t, filepath.Join(dir, "generated", "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	writeFile(t, filepath.Join(dir, "generated", "secret.go"), "package p\n\nfunc hiddenSecret() {}\n")
	kept := Search(Request{Root: dir, Query: "kept visible", Wait: 5 * time.Second})
	if len(kept.Hits) == 0 || kept.Hits[0].Path != "generated/keep.go" {
		t.Fatalf("negated file = %+v", kept)
	}
	hidden := Search(Request{Root: dir, Query: "hidden secret", Wait: time.Second})
	for _, hit := range hidden.Hits {
		if strings.Contains(hit.Path, "secret.go") {
			t.Fatalf("ignored file indexed: %+v", hit)
		}
	}
}

func TestGitignoreDoesNotIgnoreSiblingPrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "pkg\n")
	writeFile(t, filepath.Join(dir, "pkgExtra", "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	got := Search(Request{Root: dir, Query: "kept visible", Wait: 5 * time.Second})
	if len(got.Hits) == 0 || got.Hits[0].Path != "pkgExtra/keep.go" {
		t.Fatalf("sibling = %+v", got)
	}
}

func TestGitignoreNoteFoldsWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "skipme/\n")
	writeFile(t, filepath.Join(dir, "SkipMe", "secret.go"), "package p\n\nfunc hiddenSecret() {}\n")
	writeFile(t, filepath.Join(dir, "Pkg", ".gitignore"), "secret.go\n")
	writeFile(t, filepath.Join(dir, "Pkg", "keep.go"), "package p\n\nfunc pkgVisible() {}\n")
	writeFile(t, filepath.Join(dir, "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	first := Search(Request{Root: dir, Query: "hidden secret", Wait: 5 * time.Second})
	for _, hit := range first.Hits {
		if hit.Symbol == "hiddenSecret" {
			t.Fatalf("walk indexed a different-case ignore: %+v", hit)
		}
	}
	pkg := Search(Request{Root: dir, Query: "pkg visible", Wait: time.Second})
	if len(pkg.Hits) == 0 || pkg.Hits[0].Symbol != "pkgVisible" {
		t.Fatalf("pkg keep %+v", pkg)
	}
	secret := filepath.Join(dir, "Pkg", "secret.go")
	writeFile(t, secret, "package p\n\nfunc pkgSecret() {}\n")
	NoteDirty(secret)
	got := Search(Request{Root: dir, Query: "pkg secret", Wait: time.Second})
	for _, hit := range got.Hits {
		if hit.Symbol == "pkgSecret" {
			t.Fatalf("nested ignore missed a different-case note: %+v", hit)
		}
	}
}

func TestSnippetStopsBeforeTheTail(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package p\n\nfunc longFunc() {\n")
	for i := 0; i < 80; i++ {
		b.WriteString("\t_ = i\n")
	}
	b.WriteString("\t_ = \"UNIQUE_TAIL_MARKER\"\n}\n")
	writeFile(t, filepath.Join(dir, "long.go"), b.String())
	res := Search(Request{Root: dir, Query: "long func", Wait: 5 * time.Second})
	if len(res.Hits) == 0 {
		t.Fatal("expected a hit")
	}
	if strings.Contains(res.Hits[0].Snippet, "UNIQUE_TAIL_MARKER") {
		t.Fatalf("snippet included the tail:\n%s", res.Hits[0].Snippet)
	}
	if strings.Count(res.Hits[0].Snippet, "\n")+1 > maxSnippetLn {
		t.Fatalf("snippet lines = %d", strings.Count(res.Hits[0].Snippet, "\n")+1)
	}
}

func TestSymbolQueryKeepsHitsAndHintsNavigation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "parse.go"), "package p\n\nfunc parseConfig() {}\n")
	res := Search(Request{Root: dir, Query: "parseConfig", Wait: 5 * time.Second})
	if len(res.Hits) == 0 || res.Hint != symbolHint {
		t.Fatalf("symbol query = %+v", res)
	}
}

func TestSearchDoesNotBoostAPartialSymbol(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "load.go"), "package p\n\nfunc loadConfig() {\n\t_ = 1\n}\n")
	writeFile(t, filepath.Join(dir, "disk.go"), "package p\n\nfunc writeOut() {\n\t// parse the config before start\n}\n")
	res := Search(Request{Root: dir, Query: "parse config", Wait: 5 * time.Second})
	if len(res.Hits) == 0 || res.Hits[0].Path != "disk.go" {
		t.Fatalf("body match should outrank a partial symbol, got %+v", res.Hits)
	}
}

func TestSnippetIncludesTheMatchingLine(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package p\n\nfunc longFunc() {\n")
	for i := 0; i < 80; i++ {
		b.WriteString("\t_ = i\n")
	}
	b.WriteString("\t_ = \"UNIQUE_TAIL_MARKER\"\n}\n")
	writeFile(t, filepath.Join(dir, "long.go"), b.String())
	res := Search(Request{Root: dir, Query: "UNIQUE_TAIL_MARKER", Wait: 5 * time.Second})
	if len(res.Hits) == 0 || !strings.Contains(res.Hits[0].Snippet, "UNIQUE_TAIL_MARKER") {
		t.Fatalf("snippet missed the matching line: %+v", res.Hits)
	}
}

func TestUnderPathRejectsSiblingPrefix(t *testing.T) {
	if underPath("pkgExtra/a.go", "pkg") || !underPath("pkg/a.go", "pkg") || !underPath("pkg/a.go", "") {
		t.Fatal("underPath")
	}
	if dirMayHold("pkgExtra", "pkg") || !dirMayHold("pkg", "pkg/a.go") {
		t.Fatal("dirMayHold")
	}
}

func TestDocCommentStaysWithTheNextFunc(t *testing.T) {
	src := "package p\n\nfunc Foo() {\n\treturn\n}\n\n// Bar writes the config to disk.\nfunc Bar() {\n\treturn\n}\n"
	chunks := chunkSource("a.go", src)
	foo, bar := chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil {
		t.Fatalf("chunks %+v", chunks)
	}
	if strings.Contains(foo.text, "writes the config") {
		t.Fatalf("doc stayed on Foo: %s", foo.text)
	}
	if !strings.Contains(bar.text, "writes the config") || bar.start != 7 {
		t.Fatalf("doc missing on Bar: %+v", *bar)
	}

	separated := "package p\n\nfunc Foo() {\n\treturn\n}\n\n// orphan note\n\nfunc Bar() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", separated)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(bar.text, "orphan note") || !strings.Contains(foo.text, "orphan note") {
		t.Fatalf("separated comment moved: foo=%v bar=%v", foo, bar)
	}

	block := "package p\n\nfunc Foo() {\n\treturn\n}\n/* Bar writes the config */\nfunc Bar() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", block)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "writes the config") || !strings.Contains(bar.text, "writes the config") || bar.start != 6 {
		t.Fatalf("one-line block doc: foo=%v bar=%v", foo, bar)
	}

	multi := "package p\n\nfunc Foo() {\n\treturn\n}\n/*\nBar writes the config\n*/\nfunc Bar() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", multi)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "writes the config") || !strings.Contains(bar.text, "writes the config") || bar.start != 6 {
		t.Fatalf("block doc: foo=%v bar=%v", foo, bar)
	}

	indented := "package p\n\nfunc Foo() {\n\t// writes the config\n\treturn\n}\nfunc Bar() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", indented)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || !strings.Contains(foo.text, "writes the config") || strings.Contains(bar.text, "writes the config") {
		t.Fatalf("indented comment moved: foo=%v bar=%v", foo, bar)
	}

	pkg := "// Package p writes the config.\npackage p\n\nfunc Foo() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", pkg)
	foo = chunkBySymbol(chunks, "Foo")
	if foo == nil || strings.Contains(foo.text, "Package p writes") || !chunkTextHas(chunks, "Package p writes") {
		t.Fatalf("package comment moved onto Foo: %+v", chunks)
	}

	py := "def Foo():\n    return\n\n# Bar writes the config to disk.\n@dec\ndef Bar():\n    return\n"
	chunks = chunkSource("a.py", py)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "writes the config") || strings.Contains(foo.text, "@dec") || !strings.Contains(bar.text, "writes the config") || !strings.Contains(bar.text, "@dec") {
		t.Fatalf("python doc/decorator: foo=%v bar=%v", foo, bar)
	}
	if !strings.Contains(bar.text, "# Bar writes the config to disk.\n@dec\ndef Bar():") {
		t.Fatalf("python prefix order: %s", bar.text)
	}

	pyBetween := "def Foo():\n    return\n\n@dec\n# Bar writes the config to disk.\ndef Bar():\n    return\n"
	chunks = chunkSource("a.py", pyBetween)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "@dec") || strings.Contains(foo.text, "writes the config") || !strings.Contains(bar.text, "@dec\n# Bar writes the config to disk.\ndef Bar():") {
		t.Fatalf("comment between decorator and def: foo=%v bar=%v", foo, bar)
	}

	star := "package p\n\nfunc Foo() {\n\treturn\n}\n\n/*\n * Bar writes the config to disk.\n\n * Still the doc.\n */\nfunc Bar() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", star)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "writes the config") || strings.Contains(foo.text, "Still the doc") || !strings.Contains(bar.text, "writes the config") || !strings.Contains(bar.text, "Still the doc") || bar.start != lineOf(star, "/*") {
		t.Fatalf("star doc: foo=%v bar=%v", foo, bar)
	}

	header := "/*\n * Copyright header.\n */\n\npackage p\n\nfunc Foo() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", header)
	foo = chunkBySymbol(chunks, "Foo")
	if foo == nil || strings.Contains(foo.text, "Copyright header") || !chunkTextHas(chunks, "Copyright header") {
		t.Fatalf("file header moved onto Foo: %+v", chunks)
	}
}

func TestDecoratorArgsStayWithTheNextFunc(t *testing.T) {
	src := "def Foo():\n    return\n\n@router.post(\n    \"/disk\",\n    summary=\"write configuration\",\n)\ndef Bar():\n    marker_body = 1\n"
	chunks := chunkSource("a.py", src)
	foo, bar := chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "write configuration") || strings.Contains(foo.text, "marker_body") || !strings.Contains(bar.text, "summary=\"write configuration\",\n)\ndef Bar():") || !strings.Contains(bar.text, "marker_body") {
		t.Fatalf("decorator call moved: foo=%v bar=%v", foo, bar)
	}

	quoted := "def Foo():\n    return\n\n@dec(\"(\")\ndef Bar():\n    marker_body = 1\n"
	chunks = chunkSource("a.py", quoted)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "@dec") || strings.Contains(foo.text, "marker_body") || !strings.Contains(bar.text, "@dec(\"(\")\ndef Bar():") || !strings.Contains(bar.text, "marker_body") {
		t.Fatalf("paren inside decorator string: foo=%v bar=%v", foo, bar)
	}

	commented := "def Foo():\n    return\n\n@dec(  # )\n    \"write configuration\",\n)\ndef Bar():\n    return\n"
	chunks = chunkSource("a.py", commented)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "write configuration") || !strings.Contains(bar.text, "write configuration") {
		t.Fatalf("paren inside decorator comment: foo=%v bar=%v", foo, bar)
	}
}

func TestLiteralTextIsNotADeclaration(t *testing.T) {
	src := "package p\n\nfunc Foo() {\n\tsample := `" + "\nfunc NotReal() {\n\treturn\n}\n" + "`\n\treturn writeConfig()\n}\n\nfunc Bar() {\n\treturn\n}\n"
	chunks := chunkSource("a.go", src)
	foo, bar := chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || chunkBySymbol(chunks, "NotReal") != nil || !strings.Contains(foo.text, "writeConfig") || strings.Contains(bar.text, "writeConfig") {
		t.Fatalf("raw string decl: foo=%v bar=%v chunks=%+v", foo, bar, chunks)
	}

	block := "package p\n\nfunc Foo() {\n\treturn\n}\n\n/*\nfunc Old() {\n\t// writes the config\n}\n*/\nfunc Bar() {\n\treturn\n}\n"
	chunks = chunkSource("a.go", block)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || chunkBySymbol(chunks, "Old") != nil || strings.Contains(foo.text, "writes the config") || !strings.Contains(bar.text, "writes the config") {
		t.Fatalf("commented-out decl: foo=%v bar=%v chunks=%+v", foo, bar, chunks)
	}

	opened := "package p\n\nfunc Foo() { /*\nfunc StillFoo() {\n}\n*/\n\treturn writeConfig()\n}\n"
	chunks = chunkSource("a.go", opened)
	foo = chunkBySymbol(chunks, "Foo")
	if foo == nil || chunkBySymbol(chunks, "StillFoo") != nil || !strings.Contains(foo.text, "writeConfig") {
		t.Fatalf("comment opened on the decl: foo=%v chunks=%+v", foo, chunks)
	}

	py := "def Foo():\n    sample = \"\"\"\ndef NotReal():\n    return\n\"\"\"\n    return write_config()\n\ndef Bar():\n    return\n"
	chunks = chunkSource("a.py", py)
	foo, bar = chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || chunkBySymbol(chunks, "NotReal") != nil || !strings.Contains(foo.text, "write_config") || strings.Contains(bar.text, "write_config") {
		t.Fatalf("python string decl: foo=%v bar=%v chunks=%+v", foo, bar, chunks)
	}
}

func TestContinuedStringDoesNotSwallowTheNextFunc(t *testing.T) {
	src := "package p\n\nfunc Foo() {\n\ts := \"\\\n\"\n\treturn writeConfig()\n}\n\nfunc Bar() {\n\treturn\n}\n"
	chunks := chunkSource("a.go", src)
	foo, bar := chunkBySymbol(chunks, "Foo"), chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || !strings.Contains(foo.text, "writeConfig") || strings.Contains(bar.text, "writeConfig") {
		t.Fatalf("continued string: foo=%v bar=%v", foo, bar)
	}
}

func TestCColumnZeroFunctionKeepsItsBody(t *testing.T) {
	// The signature sits inside the first 60-line file window. The body token
	// sits past that window, so a whole-file stride would index them apart.
	var b strings.Builder
	b.WriteString("static void earlier(int x) {\n")
	for i := 0; i < 30; i++ {
		b.WriteString("\tint pad = 0;\n")
	}
	b.WriteString("}\n")
	b.WriteString("const int SHADOW_TAPS = 5; // rays per lamp (area light)\n")
	b.WriteString("extern bool g_smoke; // mode (selftest)\n")
	b.WriteString("/*\nstatic void notAFunction(int x) {\n\treturn decoy_body;\n}\n*/\n")
	b.WriteString("static void fillAxis(int x) {\n")
	b.WriteString("\tconst char *s = \"static void notAString(int x) {\";\n")
	for strings.Count(b.String(), "\n") < 60 {
		b.WriteString("\tint step = 0;\n")
	}
	b.WriteString("\treturn marker_body;\n}\n")
	src := b.String()
	sig := lineOf(src, "static void fillAxis")
	body := lineOf(src, "marker_body")
	if sig > windowLines || body <= windowLines || body-sig >= windowLines {
		t.Fatalf("fixture does not cross a file window: sig=%d body=%d", sig, body)
	}
	chunks := chunkSource("render.cpp", src)
	if chunkBySymbol(chunks, "notAFunction") != nil || chunkBySymbol(chunks, "notAString") != nil {
		t.Fatalf("lookalike became a symbol: %+v", chunks)
	}
	earlier := chunkBySymbol(chunks, "earlier")
	fill := chunkBySymbol(chunks, "fillAxis")
	if earlier == nil || fill == nil {
		t.Fatalf("missing decls: %+v", chunks)
	}
	if strings.Contains(earlier.text, "marker_body") || !strings.Contains(fill.text, "marker_body") || strings.Contains(fill.text, "SHADOW_TAPS") {
		t.Fatalf("body left the signature: earlier=%v fill=%v", earlier, fill)
	}
	if !strings.Contains(earlier.text, "SHADOW_TAPS") || chunkBySymbol(chunks, "SHADOW_TAPS") != nil {
		t.Fatalf("initializer became a declaration: %+v", chunks)
	}

	opened := "static void fillAxis(int x) { /*\nstatic void notAFunction(int x) {\n}\n*/\n\treturn marker_body;\n}\n"
	chunks = chunkSource("a.cpp", opened)
	fill = chunkBySymbol(chunks, "fillAxis")
	if fill == nil || chunkBySymbol(chunks, "notAFunction") != nil || !strings.Contains(fill.text, "marker_body") {
		t.Fatalf("comment opened on the decl: fill=%v chunks=%+v", fill, chunks)
	}

	continued := "static void fillAxis(int x) {\n\tconst char *s = \"\\\nstatic void notAFunction(int x) {\\\n\";\n\treturn marker_body;\n}\n"
	chunks = chunkSource("a.c", continued)
	fill = chunkBySymbol(chunks, "fillAxis")
	if fill == nil || chunkBySymbol(chunks, "notAFunction") != nil || !strings.Contains(fill.text, "marker_body") {
		t.Fatalf("continued string: fill=%v chunks=%+v", fill, chunks)
	}

	guard := "static void guard(int x) {\nif (x) {\n\treturn guard_body;\n}\n}\n"
	chunks = chunkSource("a.h", guard)
	g := chunkBySymbol(chunks, "guard")
	if g == nil || !strings.Contains(g.text, "guard_body") {
		t.Fatalf("column-0 if split the body: %v", g)
	}

	worker := "static DWORD WINAPI audioWorker(LPVOID) {\n\treturn worker_body;\n}\n"
	chunks = chunkSource("a.cc", worker)
	w := chunkBySymbol(chunks, "audioWorker")
	if w == nil || !strings.Contains(w.text, "worker_body") {
		t.Fatalf("qualified declarator: %+v", chunks)
	}

	proto := "void earlier(int x);\nvoid fillAxis(int x);\n"
	chunks = chunkSource("a.hpp", proto)
	if chunkBySymbol(chunks, "earlier") == nil || chunkBySymbol(chunks, "fillAxis") == nil {
		t.Fatalf("prototypes: %+v", chunks)
	}
	if strings.Contains(chunkBySymbol(chunks, "fillAxis").text, "earlier") {
		t.Fatalf("prototypes merged: %s", chunkBySymbol(chunks, "fillAxis").text)
	}

	include := "#include \"render.h\"\nstatic void fillAxis(int x) {\n\treturn marker_body;\n}\n"
	chunks = chunkSource("a.cpp", include)
	fill = chunkBySymbol(chunks, "fillAxis")
	if fill == nil || strings.Contains(fill.text, "#include") || !strings.Contains(fill.text, "marker_body") || !chunkTextHas(chunks, "#include \"render.h\"") {
		t.Fatalf("preprocessor peeled onto the function: fill=%v chunks=%+v", fill, chunks)
	}
}

func TestClassMethodIsItsOwnChunk(t *testing.T) {
	src := "class Game {\n" +
		"    Game() { reset(); }\n" +
		"    void reset() {\n" +
		"        if (n) {\n" +
		"            call({1, 2});\n" +
		"            return marker_body;\n" +
		"        }\n" +
		"    }\n" +
		"    // food avoids the body\n" +
		"    void spawnFood() {\n" +
		"        return food_body;\n" +
		"    }\n" +
		"    bool collides() const {\n" +
		"        return hit_body;\n" +
		"    }\n" +
		"    void fillAxis(int x,\n" +
		"                  int y) {\n" +
		"        return axis_body;\n" +
		"    }\n" +
		"};\n"
	chunks := chunkSource("game.h", src)
	reset := chunkBySymbol(chunks, "reset")
	food := chunkBySymbol(chunks, "spawnFood")
	hit := chunkBySymbol(chunks, "collides")
	axis := chunkBySymbol(chunks, "fillAxis")
	if reset == nil || food == nil || hit == nil || axis == nil {
		t.Fatalf("missing methods: %+v", chunks)
	}
	if chunkBySymbol(chunks, "Game") != nil || chunkBySymbol(chunks, "if") != nil || chunkBySymbol(chunks, "y") != nil {
		t.Fatalf("member noise became a symbol: %+v", chunks)
	}
	if !strings.Contains(reset.text, "marker_body") || strings.Contains(reset.text, "food_body") || strings.Contains(reset.text, "Game()") {
		t.Fatalf("reset: %s", reset.text)
	}
	if !strings.Contains(food.text, "food_body") || !strings.Contains(food.text, "food avoids") || strings.Contains(food.text, "marker_body") {
		t.Fatalf("spawnFood: %s", food.text)
	}
	if !strings.Contains(hit.text, "hit_body") || strings.Contains(hit.text, "food_body") {
		t.Fatalf("collides: %s", hit.text)
	}
	if !strings.Contains(axis.text, "axis_body") || strings.Contains(axis.text, "hit_body") {
		t.Fatalf("multiline method: %s", axis.text)
	}

	// A wrapped expression, a parenthetical string, a local, and an inner
	// typedef sit inside a function. Indent does not make them declarations.
	body := "double contrastRatio(const Element& a, const Element& b) {\n" +
		"    const double la = 0.2126 * srgbToLinear(a.r) +\n" +
		"                      0.7152 * srgbToLinear(a.g) +\n" +
		"                      0.0722 * srgbToLinear(a.b);\n" +
		"    failed += reportCheck(!enabled,\n" +
		"                          \"self test runs with sound muted (no audible beeps)\");\n" +
		"    std::vector<double> profile((size_t)n, 0.0);\n" +
		"    typedef HRESULT (WINAPI *SetAttrFn)(HWND, DWORD, LPCVOID, DWORD);\n" +
		"    return marker_body;\n" +
		"}\n"
	chunks = chunkSource("quality.cpp", body)
	ratio := chunkBySymbol(chunks, "contrastRatio")
	if ratio == nil || chunkBySymbol(chunks, "srgbToLinear") != nil || chunkBySymbol(chunks, "muted") != nil || chunkBySymbol(chunks, "profile") != nil || chunkBySymbol(chunks, "SetAttrFn") != nil {
		t.Fatalf("function body split: %+v", chunks)
	}
	if !strings.Contains(ratio.text, "marker_body") || !strings.Contains(ratio.text, "srgbToLinear") || !strings.Contains(ratio.text, "SetAttrFn") {
		t.Fatalf("body left the function: %s", ratio.text)
	}
}

func TestCFunctionPointerDeclaratorKeepsItsName(t *testing.T) {
	src := "// composition mode hands the letter keys back\n" +
		"typedef HANDLE (WINAPI *ImmAssociateContextFn)(HWND, HANDLE);\n" +
		"static void detachIme(HWND hwnd) {\n\treturn marker_body;\n}\n"
	chunks := chunkSource("snake.cpp", src)
	if chunkBySymbol(chunks, "HANDLE") != nil {
		t.Fatalf("return type became the symbol: %+v", chunks)
	}
	fn := chunkBySymbol(chunks, "ImmAssociateContextFn")
	ime := chunkBySymbol(chunks, "detachIme")
	if fn == nil || ime == nil {
		t.Fatalf("missing decls: %+v", chunks)
	}
	if !strings.Contains(fn.text, "ImmAssociateContextFn") || !strings.Contains(fn.text, "composition mode") || strings.Contains(fn.text, "marker_body") {
		t.Fatalf("typedef chunk: %s", fn.text)
	}
	if !strings.Contains(ime.text, "marker_body") || strings.Contains(ime.text, "typedef HANDLE") {
		t.Fatalf("function chunk: %s", ime.text)
	}

	ptr := "static void (*cb)(int);\nstatic void fillAxis(int x) {\n\treturn marker_body;\n}\n"
	chunks = chunkSource("a.cpp", ptr)
	cb := chunkBySymbol(chunks, "cb")
	fill := chunkBySymbol(chunks, "fillAxis")
	if chunkBySymbol(chunks, "void") != nil || cb == nil || fill == nil || strings.Contains(cb.text, "marker_body") || !strings.Contains(fill.text, "marker_body") {
		t.Fatalf("bare pointer declarator: %+v", chunks)
	}

	param := "static void fillAxis(int *x, HANDLE *h) {\n\treturn marker_body;\n}\n" +
		"static void takeCmp(int (*cmp)(const void *a, const void *b)) {\n\treturn cmp_body;\n}\n"
	chunks = chunkSource("a.cpp", param)
	fill = chunkBySymbol(chunks, "fillAxis")
	take := chunkBySymbol(chunks, "takeCmp")
	if fill == nil || take == nil || chunkBySymbol(chunks, "x") != nil || chunkBySymbol(chunks, "h") != nil || chunkBySymbol(chunks, "cmp") != nil {
		t.Fatalf("parameter became the symbol: %+v", chunks)
	}
	if !strings.Contains(fill.text, "marker_body") || !strings.Contains(take.text, "cmp_body") || strings.Contains(fill.text, "cmp_body") {
		t.Fatalf("parameter split the body: fill=%v take=%v", fill, take)
	}
}

func TestLeadingBOMIsNotSource(t *testing.T) {
	src := "\uFEFF// presentation layer (see render.h)\nstatic void fillAxis(int x) {\n\treturn marker_body;\n}\n"
	chunks := chunkSource("render.cpp", src)
	if chunkBySymbol(chunks, "layer") != nil {
		t.Fatalf("BOM banner became a declaration: %+v", chunks)
	}
	fill := chunkBySymbol(chunks, "fillAxis")
	if fill == nil || !strings.Contains(fill.text, "marker_body") || strings.Contains(fill.text, "\uFEFF") {
		t.Fatalf("BOM hid the function: %v", fill)
	}

	goSrc := "\uFEFFfunc ParseConfig() {\n\treturn marker_body\n}\n"
	chunks = chunkSource("a.go", goSrc)
	fn := chunkBySymbol(chunks, "ParseConfig")
	if fn == nil || !strings.Contains(fn.text, "marker_body") {
		t.Fatalf("BOM hid the Go func: %+v", chunks)
	}

	pySrc := "\uFEFFdef save_widget():\n    return marker_body\n"
	chunks = chunkSource("a.py", pySrc)
	save := chunkBySymbol(chunks, "save_widget")
	if save == nil || !strings.Contains(save.text, "marker_body") {
		t.Fatalf("BOM hid the Python def: %+v", chunks)
	}
}

func TestPythonMethodIsItsOwnChunk(t *testing.T) {
	src := "class Store:\n    # writes the config to disk\n    @router.post(\n        summary=\"write configuration\",\n    )\n    def save_widget(self):\n        return marker_body\n\n    def load(self):\n        return\n"
	chunks := chunkSource("a.py", src)
	store := chunkBySymbol(chunks, "Store")
	save := chunkBySymbol(chunks, "save_widget")
	load := chunkBySymbol(chunks, "load")
	if store == nil || save == nil || load == nil {
		t.Fatalf("chunks %+v", chunks)
	}
	if strings.Contains(store.text, "write configuration") || strings.Contains(store.text, "writes the config") || strings.Contains(store.text, "marker_body") {
		t.Fatalf("method text stayed on the class: %s", store.text)
	}
	if !strings.Contains(save.text, "writes the config") || !strings.Contains(save.text, "summary=\"write configuration\"") || !strings.Contains(save.text, "marker_body") {
		t.Fatalf("method chunk: %s", save.text)
	}
	if strings.Contains(save.text, "def load") || strings.Contains(load.text, "marker_body") {
		t.Fatalf("methods merged: save=%s load=%s", save.text, load.text)
	}
}

func TestPythonDedentClosesTheBlock(t *testing.T) {
	assign := "def Foo():\n    return 1\n\nCONFIG = \"write configuration\"\n"
	chunks := chunkSource("a.py", assign)
	foo := chunkBySymbol(chunks, "Foo")
	if foo == nil || strings.Contains(foo.text, "write configuration") || !chunkTextHas(chunks, "write configuration") {
		t.Fatalf("module assignment stayed on Foo: foo=%v chunks=%+v", foo, chunks)
	}
	for _, ch := range chunks {
		if strings.Contains(ch.text, "write configuration") && ch.symbol == "Foo" {
			t.Fatalf("assignment ranked under Foo: %+v", ch)
		}
	}

	inner := "def Foo():\n    def helper():\n        return 1\n    return write_config()\n"
	chunks = chunkSource("a.py", inner)
	foo = chunkBySymbol(chunks, "Foo")
	helper := chunkBySymbol(chunks, "helper")
	if foo == nil || helper == nil || strings.Contains(helper.text, "write_config") || !strings.Contains(foo.text, "write_config") || strings.Contains(foo.text, "return 1") {
		t.Fatalf("inner suite: foo=%v helper=%v", foo, helper)
	}

	attr := "class Store:\n    def save(self):\n        return 1\n    timeout = \"write configuration\"\n"
	chunks = chunkSource("a.py", attr)
	store := chunkBySymbol(chunks, "Store")
	save := chunkBySymbol(chunks, "save")
	if store == nil || save == nil || strings.Contains(save.text, "write configuration") || strings.Contains(store.text, "return 1") || !strings.Contains(store.text, "timeout = \"write configuration\"") {
		t.Fatalf("class attribute: store=%v save=%v", store, save)
	}

	body := "def Foo():\n    # writes the config\n    return 1\n"
	chunks = chunkSource("a.py", body)
	foo = chunkBySymbol(chunks, "Foo")
	if foo == nil || !strings.Contains(foo.text, "def Foo():\n    # writes the config\n    return 1") {
		t.Fatalf("body comment moved above the def: %v", foo)
	}

	docs := "def Foo():\n    return\n\n# one writes the config\n# two writes the config\ndef Bar():\n    return\n"
	chunks = chunkSource("a.py", docs)
	foo = chunkBySymbol(chunks, "Foo")
	bar := chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "writes the config") || !strings.Contains(bar.text, "# one writes the config\n# two writes the config\ndef Bar():") {
		t.Fatalf("stacked docs: foo=%v bar=%v", foo, bar)
	}

	blank := "def Foo():\n    return\n\n# orphan note\n\ndef Bar():\n    return\n"
	chunks = chunkSource("a.py", blank)
	foo = chunkBySymbol(chunks, "Foo")
	bar = chunkBySymbol(chunks, "Bar")
	if foo == nil || bar == nil || strings.Contains(foo.text, "orphan note") || strings.Contains(bar.text, "orphan note") || !chunkTextHas(chunks, "orphan note") {
		t.Fatalf("blank did not stop the doc: foo=%v bar=%v chunks=%+v", foo, bar, chunks)
	}

	decor := "class Store:\n    @dec\n    timeout = \"write configuration\"\n"
	chunks = chunkSource("a.py", decor)
	store = chunkBySymbol(chunks, "Store")
	if store == nil || !strings.HasPrefix(store.text, "class Store:\n    @dec\n    timeout = \"write configuration\"") {
		t.Fatalf("decorator landed above the class: %v", store)
	}
}

func lineOf(src, needle string) int {
	i := strings.Index(src, needle)
	if i < 0 {
		return 0
	}
	return 1 + strings.Count(src[:i], "\n")
}

func chunkBySymbol(chunks []rawChunk, symbol string) *rawChunk {
	for i := range chunks {
		if chunks[i].symbol == symbol {
			return &chunks[i]
		}
	}
	return nil
}

func TestExplodeKeepsLinesPastTheRuneCap(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		if i == 25 {
			lines = append(lines, "UNIQUE_MID_MARKER")
			continue
		}
		lines = append(lines, strings.Repeat("x", 400))
	}
	chunks := explode("f.go", lineGroup{start: 1, lines: lines})
	if !chunkTextHas(chunks, "UNIQUE_MID_MARKER") {
		t.Fatal("marker past the rune cap was not indexed")
	}
	long := strings.Repeat("a", maxChunkBytes) + "UNIQUE_LINE_TAIL"
	tail := explode("f.go", lineGroup{start: 4, lines: []string{long, "UNIQUE_AFTER_LONG_LINE"}})
	if !chunkTextHas(tail, "UNIQUE_LINE_TAIL") || !chunkTextHas(tail, "UNIQUE_AFTER_LONG_LINE") {
		t.Fatal("long line tail or the following line was not indexed")
	}
	ident := strings.Repeat("a", maxChunkBytes-5) + " saveWidget " + strings.Repeat("b", 80)
	parts := splitLongLine("f.go", lineGroup{start: 1, lines: []string{ident}}, 0)
	if !chunkTextHas(parts, "saveWidget") {
		t.Fatal("long-line split cut through saveWidget")
	}
}

func chunkTextHas(chunks []rawChunk, needle string) bool {
	for _, ch := range chunks {
		if strings.Contains(ch.text, needle) {
			return true
		}
	}
	return false
}

func TestFocusSnippetKeepsTheMatchWhenTheLeadIsLong(t *testing.T) {
	lead := strings.Repeat("x", maxSnippetRun)
	text := lead + "\nfindThisLine\n" + strings.Repeat("y", 40)
	snippet, start, end := focusSnippet(text, "findThisLine", []string{"findthisline"}, 10)
	if !strings.Contains(snippet, "findThisLine") {
		t.Fatalf("dropped match snippet=%q range=%d-%d", snippet, start, end)
	}
	if len([]rune(snippet)) > maxSnippetRun {
		t.Fatalf("runes=%d", len([]rune(snippet)))
	}
	if start > 11 || end < 11 {
		t.Fatalf("range %d-%d does not cover the matching line", start, end)
	}
}

func TestSearchPathFoldsWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	if underPath("pkgExtra/a.go", "PKG") || !underPath("pkg/a.go", "PKG") {
		t.Fatal("underPath case fold")
	}
	if !dirMayHold("pkg", "PKG/write.go") || dirMayHold("pkgExtra", "PKG") {
		t.Fatal("dirMayHold case fold")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pkg", "write.go"), "package pkg\n\nfunc writeOut() {\n\t// persist widget config\n}\n")
	writeFile(t, filepath.Join(dir, "pkgExtra", "write.go"), "package pkg\n\nfunc writeOut() {\n\t// persist widget config\n}\n")
	res := Search(Request{Root: dir, Path: "PKG", Query: "persist widget config", Wait: 5 * time.Second})
	if res.Freshness != indexedFreshness || len(res.Hits) == 0 {
		t.Fatalf("folded scope %+v", res)
	}
	for _, hit := range res.Hits {
		if !strings.EqualFold(hit.Path, "pkg/write.go") {
			t.Fatalf("hit outside pkg: %+v", hit)
		}
	}

	prev := SetMaxFilesForTest(1)
	defer SetMaxFilesForTest(prev)
	wide := t.TempDir()
	writeFile(t, filepath.Join(wide, "a.go"), "package p\n\nfunc alphaOne() {}\n")
	writeFile(t, filepath.Join(wide, "pkg", "write.go"), "package pkg\n\nfunc writeOut() {\n\t// persist widget config\n}\n")
	scoped := Search(Request{Root: wide, Path: "PKG", Query: "persist widget config", Wait: 5 * time.Second})
	if !scoped.Scoped || len(scoped.Hits) == 0 || !strings.EqualFold(scoped.Hits[0].Path, "pkg/write.go") {
		t.Fatalf("truncated folded scope %+v", scoped)
	}
}

func TestNonSourceNoteDoesNotRebuildIndex(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.go")
	writeFile(t, keep, "package p\n\nfunc keptVisible() {}\n")
	first := Search(Request{Root: dir, Query: "kept visible", Wait: 5 * time.Second})
	if len(first.Hits) == 0 || first.Hits[0].Path != "keep.go" {
		t.Fatalf("before note %+v", first)
	}
	note := filepath.Join(dir, "README.md")
	writeFile(t, note, "not source\n")
	NoteDirty(note)
	before := testIndexBuilds.Load()
	again := Search(Request{Root: dir, Query: "kept visible", Wait: time.Second})
	if len(again.Hits) == 0 || again.Hits[0].Path != "keep.go" {
		t.Fatalf("after markdown note %+v", again)
	}
	if got := testIndexBuilds.Load(); got != before {
		t.Fatalf("non-source note rebuilt the index: %d -> %d", before, got)
	}
	kept := Search(Request{Root: dir, Query: "kept visible", Wait: time.Second})
	if len(kept.Hits) == 0 || kept.Hits[0].Symbol != "keptVisible" {
		t.Fatalf("kept = %+v", kept)
	}
	if got := testIndexBuilds.Load(); got != before {
		t.Fatalf("consumed non-source note rebuilt the index: %d -> %d", before, got)
	}
}

func TestDeletedFileIsNotRebuiltOnTheNextSearch(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.go")
	gone := filepath.Join(dir, "gone.go")
	writeFile(t, keep, "package p\n\nfunc keptVisible() {}\n")
	writeFile(t, gone, "package p\n\nfunc goneSymbol() {}\n")
	first := Search(Request{Root: dir, Query: "gone symbol", Wait: 5 * time.Second})
	if len(first.Hits) == 0 || first.Hits[0].Symbol != "goneSymbol" {
		t.Fatalf("before delete %+v", first)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	NoteDirty(gone)
	before := testIndexBuilds.Load()
	missed := Search(Request{Root: dir, Query: "gone symbol", Wait: time.Second})
	for _, hit := range missed.Hits {
		if hit.Symbol == "goneSymbol" {
			t.Fatalf("deleted symbol still indexed: %+v", missed.Hits)
		}
	}
	mid := testIndexBuilds.Load()
	if mid == before {
		t.Fatal("deletion did not reload the index")
	}
	kept := Search(Request{Root: dir, Query: "kept visible", Wait: time.Second})
	if len(kept.Hits) == 0 || kept.Hits[0].Path != "keep.go" {
		t.Fatalf("kept = %+v", kept)
	}
	if got := testIndexBuilds.Load(); got != mid {
		t.Fatalf("search after a consumed deletion rebuilt the index: %d -> %d", mid, got)
	}
}

func TestReconcileIndexesFileAddedOutsideTheProcess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.go"), "package p\n\nfunc keptVisible() {}\n")
	first := Search(Request{Root: dir, Query: "kept visible", Wait: 5 * time.Second})
	if len(first.Hits) == 0 || first.Hits[0].Path != "keep.go" {
		t.Fatalf("before add %+v", first)
	}
	time.Sleep(50 * time.Millisecond)
	writeFile(t, filepath.Join(dir, "added.go"), "package p\n\nfunc addedWidget() {\n\t// added widget\n}\n")
	reconcileOnce()
	found := Search(Request{Root: dir, Query: "added widget", Wait: time.Second})
	if len(found.Hits) == 0 || found.Hits[0].Path != "added.go" {
		t.Fatalf("added file = %+v", found)
	}
}

func TestBuildPanicIsWarmingThenRetries(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "parse.go"), "package p\n\nfunc parseConfig() {}\n")
	var once sync.Once
	SetBuildPauseForTest(func() {
		once.Do(func() { panic("boom") })
	})
	defer SetBuildPauseForTest(nil)
	cold := Search(Request{Root: dir, Query: "parse config", Wait: time.Second})
	if cold.Freshness != warmingFreshness || len(cold.Hits) != 0 {
		t.Fatalf("panic build = %+v", cold)
	}
	warm := Search(Request{Root: dir, Query: "parse config", Wait: 5 * time.Second})
	if warm.Freshness != indexedFreshness || len(warm.Hits) == 0 {
		t.Fatalf("retry = %+v", warm)
	}
}

func TestPruneNotesDropsOldestWhenNoneAreStale(t *testing.T) {
	storeMu.Lock()
	prev := dirtyNotes
	dirtyNotes = make(map[string]time.Time, 9000)
	now := time.Now()
	for i := 0; i < 9000; i++ {
		dirtyNotes[fmt.Sprintf("f-%05d", i)] = now.Add(time.Duration(i))
	}
	pruneNotesLocked()
	n := len(dirtyNotes)
	_, oldest := dirtyNotes["f-00000"]
	dirtyNotes = prev
	storeMu.Unlock()
	if n > 8192 {
		t.Fatalf("notes = %d", n)
	}
	if oldest {
		t.Fatal("oldest note kept")
	}
}

func TestEncodeDoesNotShrinkTheCallerSnippet(t *testing.T) {
	snippet := strings.Repeat("x", maxResultBytes)
	res := Result{Freshness: indexedFreshness, Hits: []Hit{{Path: "a.go", StartLine: 1, EndLine: 1, Snippet: snippet}}}
	if encoded := Encode(res); strings.Contains(encoded, snippet) {
		t.Fatal("encode kept an oversized snippet")
	}
	if res.Hits[0].Snippet != snippet {
		t.Fatal("encode mutated the caller snippet")
	}
}

func TestRelUnderFoldsWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	rel, ok := relUnder(`D:\Work\Proj`, `d:\work\proj\pkg\a.go`)
	if !ok || rel != "pkg/a.go" {
		t.Fatalf("rel=%q ok=%v", rel, ok)
	}
	if _, ok := relUnder(`D:\Work\Proj`, `D:\Work\ProjExtra\a.go`); ok {
		t.Fatal("sibling prefix counted as inside")
	}
}

func TestNoteDirtyMatchesDifferentPathCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "parse.go")
	writeFile(t, path, "package p\n\nfunc parseConfig() {}\n")
	first := Search(Request{Root: dir, Query: "parse config", Wait: 5 * time.Second})
	if len(first.Hits) == 0 || first.Hits[0].Symbol != "parseConfig" {
		t.Fatalf("before rewrite %+v", first)
	}
	flipped := foldOnePathLetter(path)
	if strings.EqualFold(flipped, path) && flipped == path {
		t.Fatal("path had no letter to fold")
	}
	time.Sleep(20 * time.Millisecond)
	writeFile(t, flipped, "package p\n\nfunc saveWidget() {\n\t// save widget\n}\n")
	NoteDirty(flipped)
	saved := Search(Request{Root: dir, Query: "save widget", Wait: time.Second})
	if len(saved.Hits) == 0 || saved.Hits[0].Symbol != "saveWidget" {
		t.Fatalf("case-folded dirty reload = %+v", saved)
	}
}

func foldOnePathLetter(path string) string {
	r := []rune(path)
	for i, ch := range r {
		if i < 3 {
			continue
		}
		switch {
		case ch >= 'a' && ch <= 'z':
			r[i] = ch - ('a' - 'A')
			return string(r)
		case ch >= 'A' && ch <= 'Z':
			r[i] = ch + ('a' - 'A')
			return string(r)
		}
	}
	return path
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
