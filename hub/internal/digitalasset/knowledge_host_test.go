package digitalasset

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestKnowledgeHost_RefcountPreventsCloseWhileInUse(t *testing.T) {
	root := t.TempDir()
	h := NewKnowledgeHost(root, 1) // only one idle slot
	t.Cleanup(h.CloseAll)
	ctx := context.Background()

	// Ensure both library dirs can be created.
	if err := h.WithLibraryWrite(ctx, "t1", "lib_a", func(st *knowledge.SQLiteStore) error {
		if st == nil {
			t.Fatal("nil store A")
		}
		return nil
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}

	aEntered := make(chan struct{})
	aHold := make(chan struct{})
	var aErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		aErr = h.WithLibraryWrite(ctx, "t1", "lib_a", func(st *knowledge.SQLiteStore) error {
			if st == nil {
				return filepath.ErrBadPattern // unlikely; just non-nil
			}
			close(aEntered)
			<-aHold // hold A open
			// After B has been opened (and would have closed A under old LRU), store must still work.
			_, err := st.Search(ctx, knowledge.SearchOptions{Query: "x", Limit: 1})
			return err // empty search is fine; must not panic / closed-db
		})
	}()

	select {
	case <-aEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for A to enter")
	}

	// Open B while A is checked out. maxOpen=1: B becomes only "idle" candidate;
	// A must remain open because refs>0.
	if err := h.WithLibraryWrite(ctx, "t1", "lib_b", func(st *knowledge.SQLiteStore) error {
		if st == nil {
			t.Fatal("nil store B")
		}
		return nil
	}); err != nil {
		close(aHold)
		t.Fatalf("open B: %v", err)
	}

	close(aHold)
	wg.Wait()
	if aErr != nil {
		t.Fatalf("A after B: %v", aErr)
	}
}

func TestKnowledgeHost_OpenCountBoundedWhenIdle(t *testing.T) {
	root := t.TempDir()
	h := NewKnowledgeHost(root, 2)
	t.Cleanup(h.CloseAll)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := h.WithLibraryWrite(ctx, "t1", id, func(st *knowledge.SQLiteStore) error {
			return nil
		}); err != nil {
			t.Fatalf("open %s: %v", id, err)
		}
	}
	if n := h.OpenCount(); n > 2 {
		t.Fatalf("open count %d > max 2", n)
	}
}
