package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type compactProjectionLLM struct {
	calls int
}

func (l *compactProjectionLLM) IsConfigured() bool { return true }
func (l *compactProjectionLLM) ChatCall([]map[string]string) (string, error) {
	l.calls++
	return "short durable fact", nil
}

func TestCompressorBuildsProjectionWithoutRewritingCanonicalContent(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	original := strings.TrimSpace("deployment fact: " + strings.Repeat("preserve every concrete detail ", 20))
	if err := store.Save(Entry{ID: "fact", Content: original, Category: CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}
	llm := &compactProjectionLLM{}
	compressor := NewCompressor(store, llm, nil)
	first, err := compressor.Compress(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.CompressedCount != 1 {
		t.Fatalf("compressed projections = %d, want 1", first.CompressedCount)
	}
	entry := store.SearchDirectByID("fact")[0]
	if entry.Content != original {
		t.Fatalf("compression rewrote canonical memory content:\ngot:  %q\nwant: %q", entry.Content, original)
	}
	if entry.CompactForm != "short durable fact" {
		t.Fatalf("compact form = %q", entry.CompactForm)
	}

	second, err := compressor.Compress(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.CompressedCount != 0 || llm.calls != 1 {
		t.Fatalf("unchanged compact form was regenerated: result=%+v calls=%d", second, llm.calls)
	}
	if got := store.SearchDirectByID("fact")[0].Content; got != original {
		t.Fatal("second maintenance cycle changed canonical memory content")
	}
}
