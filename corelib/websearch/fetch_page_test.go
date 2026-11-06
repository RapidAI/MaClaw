package websearch

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestContinueFetchURL(t *testing.T) {
	if got := ContinueFetchURL(" https://start.example/a \n", "https://final.example/b"); got != "https://start.example/a" {
		t.Fatalf("hint=%q", got)
	}
	if got := ContinueFetchURL("https://same.example/a", "https://same.example/a"); got != "" {
		t.Fatalf("same address hinted %q", got)
	}
	if got := ContinueFetchURL("https://exa\nmple.com/a", "https://example.com/a"); got != "" {
		t.Fatalf("newline-only difference hinted %q", got)
	}
	if got := ContinueFetchURL(strings.Repeat("a", continueFetchURLMax+1), "https://final.example/b"); got != "" {
		t.Fatalf("long hint len=%d", len(got))
	}
}

func TestReadBoundedBodyMarksOnlyALongerReader(t *testing.T) {
	exact, clipped, err := readBoundedBody(strings.NewReader("abcd"), 4)
	if err != nil || clipped || string(exact) != "abcd" {
		t.Fatalf("exact body=%q clipped=%t err=%v", exact, clipped, err)
	}
	longer, clipped, err := readBoundedBody(strings.NewReader("abcde"), 4)
	if err != nil || !clipped || string(longer) != "abcd" {
		t.Fatalf("longer body=%q clipped=%t err=%v", longer, clipped, err)
	}
	split, clipped, err := readBoundedBody(bytes.NewReader([]byte("ab中")), 4)
	if err != nil || !clipped || string(split) != "ab" {
		t.Fatalf("split rune body=%q clipped=%t err=%v", split, clipped, err)
	}
}

func TestClampFetchPageCutsOnRuneBoundary(t *testing.T) {
	result := &FetchResult{
		Content:    strings.Repeat("中", 100),
		TotalChars: 500,
		NextOffset: 200,
	}
	ClampFetchPage(result, 10)
	if result.Content != strings.Repeat("中", 3) {
		t.Fatalf("content=%q", result.Content)
	}
	if result.NextOffset != 103 {
		t.Fatalf("next=%d", result.NextOffset)
	}
	if !result.HasMore || !result.Truncated {
		t.Fatal("expected a remaining page")
	}
	if result.TotalChars != 500 {
		t.Fatalf("total=%d", result.TotalChars)
	}
}

func TestClampFetchPageLeavesAFittingPage(t *testing.T) {
	result := &FetchResult{Content: "abcdef", TotalChars: 6, NextOffset: 6}
	ClampFetchPage(result, 100)
	if result.Content != "abcdef" || result.NextOffset != 6 || result.HasMore || result.Truncated {
		t.Fatalf("%#v", result)
	}
}

func TestClampFetchPageDoesNotInventTotal(t *testing.T) {
	result := &FetchResult{Content: strings.Repeat("中", 10), NextOffset: 10}
	ClampFetchPage(result, 6)
	if result.TotalChars != 0 {
		t.Fatalf("total invented: %d", result.TotalChars)
	}
	if result.Content != strings.Repeat("中", 2) || result.NextOffset != 2 {
		t.Fatalf("content=%q next=%d", result.Content, result.NextOffset)
	}
}

func TestFitFetchPageStopsAtBudget(t *testing.T) {
	const total = 20000
	result := &FetchResult{
		Content:    strings.Repeat("中", total),
		TotalChars: total,
		NextOffset: total,
	}
	original := result.Content
	text := FitFetchPage(result, 1000, func(page *FetchResult) string {
		return page.Content + fmt.Sprintf("\nNext: offset=%d", page.NextOffset)
	})
	if len(text) > 1000 {
		t.Fatalf("len=%d", len(text))
	}
	if !strings.HasSuffix(text, fmt.Sprintf("\nNext: offset=%d", result.NextOffset)) {
		t.Fatalf("cursor=%d tail=%q", result.NextOffset, text[max(0, len(text)-48):])
	}
	if result.NextOffset <= 0 || result.NextOffset >= total {
		t.Fatalf("next=%d", result.NextOffset)
	}
	if !strings.HasPrefix(original, result.Content) {
		t.Fatal("fitted page is not a prefix of the source")
	}
	if utfLen(result.Content) != result.NextOffset {
		t.Fatalf("visible=%d next=%d", utfLen(result.Content), result.NextOffset)
	}
}

func utfLen(s string) int {
	return len([]rune(s))
}
