package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkContentTrigramsBytes(b *testing.B) {
	content := []byte(strings.Repeat("package main\nfunc TargetSymbol() {}\nvar OtherSymbol = TargetSymbol\n", 2048))
	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = contentTrigramsBytes(content)
	}
}

func BenchmarkLiteralSearchTermsFromText(b *testing.B) {
	text := strings.Repeat("TargetSymbol target_symbol \u672c\u5730\u68c0\u7d22\u80fd\u529b \u5927\u4ed3\u5e93\u68c0\u7d22\u80fd\u529b target[0]+literal ", 128)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = literalSearchTermsFromText(text)
	}
}

func BenchmarkIndexedSearchCandidates(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 2000; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file_%04d.go", i))
		body := fmt.Sprintf("package bench\nfunc Symbol%04d() {}\n", i)
		if i%100 == 0 {
			body += "func TargetNeedleForBenchmark() {}\n"
		}
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	resetSearchIndexCacheForBenchmark()
	if candidates, ok, stats := indexedSearchCandidates(dir, "TargetNeedleForBenchmark", "**/*.go", "", "", false, false); !ok || len(candidates) == 0 || stats.candidateFiles == 0 {
		b.Fatalf("index warmup failed: ok=%v candidates=%d stats=%+v", ok, len(candidates), stats)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates, ok, _ := indexedSearchCandidates(dir, "TargetNeedleForBenchmark", "**/*.go", "", "", false, false)
		if !ok || len(candidates) == 0 {
			b.Fatalf("indexed search failed: ok=%v candidates=%d", ok, len(candidates))
		}
	}
}

func BenchmarkMatchGlobCached(b *testing.B) {
	pattern := "**/*.{go,tsx,md} **/internal/**"
	rels := []string{
		"cmd/app/main.go",
		"web/src/App.tsx",
		"docs/README.md",
		"pkg/internal/cache.txt",
	}
	resetSearchGlobPatternCacheForBenchmark()
	if matched, err := matchGlob(pattern, rels[0], false); err != nil || !matched {
		b.Fatalf("glob warmup failed: matched=%v err=%v", matched, err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matched, err := matchGlob(pattern, rels[i%len(rels)], false)
		if err != nil || !matched {
			b.Fatalf("matchGlob failed: matched=%v err=%v", matched, err)
		}
	}
}
func resetSearchIndexCacheForBenchmark() {
	searchIndexCache.Lock()
	searchIndexCache.byRoot = make(map[string]*localSearchIndex)
	searchIndexCache.Unlock()
}

func resetSearchGlobPatternCacheForBenchmark() {
	searchGlobPatternCache.Lock()
	searchGlobPatternCache.byPattern = make(map[string]compiledSearchGlobEntry)
	searchGlobPatternCache.order = nil
	searchGlobPatternCache.Unlock()
}
