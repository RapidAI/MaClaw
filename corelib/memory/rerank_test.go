package memory

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/rerank"
)

type countingMemoryReranker struct {
	mu    sync.Mutex
	calls int
	lastN int
	fn    func([]rerank.Passage) ([]float64, error)
}

func (c *countingMemoryReranker) Rerank(_ context.Context, _ string, passages []rerank.Passage) ([]float64, error) {
	c.mu.Lock()
	c.calls++
	c.lastN = len(passages)
	fn := c.fn
	c.mu.Unlock()
	if fn != nil {
		return fn(passages)
	}
	return make([]float64, len(passages)), nil
}

func (c *countingMemoryReranker) Calls() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.lastN
}

func TestBlendPassageKeepsCompactForm(t *testing.T) {
	var seen string
	rr := &countingMemoryReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		for _, passage := range passages {
			if passage.ID == "long" {
				seen = passage.Text
			}
		}
		return make([]float64, len(passages)), nil
	}}
	candidates := []recallScored{{
		entry: Entry{ID: "long", Content: strings.Repeat("甲", rerank.MaxPassageRunes+40), CompactForm: "GOLDNEEDLE"},
		score: 1,
	}}
	for i := 0; i < 16; i++ {
		candidates = append(candidates, recallScored{entry: Entry{ID: "pad", Content: "pad"}, score: 0})
	}
	if _, meta := blendRelevanceRerank(context.Background(), rr, "why compare", candidates); !meta.applied {
		t.Fatal("expected the head to be scored")
	}
	if !strings.Contains(seen, "GOLDNEEDLE") {
		t.Fatalf("compact form dropped: %q", seen)
	}
	repeated := memoryPassageText(Entry{Content: "same body", CompactForm: "same"})
	if strings.Count(repeated, "same") != 1 {
		t.Fatalf("compact form repeated: %q", repeated)
	}
}

func TestBlendPassageKeepsLateContentHit(t *testing.T) {
	var seen string
	rr := &countingMemoryReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		for _, passage := range passages {
			if passage.ID == "late" {
				seen = passage.Text
			}
		}
		return make([]float64, len(passages)), nil
	}}
	candidates := []recallScored{{
		entry: Entry{
			ID:          "late",
			CompactForm: "摘要没有关键词",
			Content:     strings.Repeat("甲", rerank.MaxPassageRunes+80) + " GOLDNEEDLE",
		},
		score: 1,
	}}
	for i := 0; i < 16; i++ {
		candidates = append(candidates, recallScored{entry: Entry{ID: "pad", Content: "pad"}, score: 0})
	}
	if _, meta := blendRelevanceRerank(context.Background(), rr, "GOLDNEEDLE", candidates); !meta.applied {
		t.Fatal("expected the head to be scored")
	}
	if !strings.Contains(seen, "GOLDNEEDLE") {
		t.Fatalf("late content hit dropped: %q", seen)
	}
}

func TestBlendRelevanceRerankBoundsAndHead(t *testing.T) {
	wideGap := []recallScored{
		{entry: Entry{ID: "high", Content: "high"}, score: 10},
		{entry: Entry{ID: "low", Content: "low"}, score: 7},
	}
	for i := 0; i < 14; i++ {
		wideGap = append(wideGap, recallScored{entry: Entry{ID: "pad", Content: "pad"}, score: 1})
	}
	preferLow := &countingMemoryReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		scores := make([]float64, len(passages))
		for i, passage := range passages {
			if passage.ID == "low" {
				scores[i] = 1
			}
		}
		return scores, nil
	}}
	got, meta := blendRelevanceRerank(context.Background(), preferLow, "why compare", wideGap)
	if !meta.applied || meta.pool != len(wideGap) {
		t.Fatalf("meta = %+v, want pool %d", meta, len(wideGap))
	}
	if got[0].entry.ID != "high" {
		t.Fatalf("band overturned a gap above 2: %s", got[0].entry.ID)
	}

	near := []recallScored{
		{entry: Entry{ID: "first", Content: "first"}, score: 5.0},
		{entry: Entry{ID: "second", Content: "second"}, score: 4.9},
	}
	for i := 0; i < 14; i++ {
		near = append(near, recallScored{entry: Entry{ID: "pad", Content: "pad"}, score: 1})
	}
	preferSecond := &countingMemoryReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		scores := make([]float64, len(passages))
		for i, passage := range passages {
			if passage.ID == "second" {
				scores[i] = 1
			}
		}
		return scores, nil
	}}
	swapped, _ := blendRelevanceRerank(context.Background(), preferSecond, "why compare", near)
	if swapped[0].entry.ID != "second" {
		t.Fatalf("near tie should follow the reranker, got %s", swapped[0].entry.ID)
	}

	unchanged, meta := blendRelevanceRerank(context.Background(), preferSecond, "why compare", near[:memoryRerankMinCandidates])
	if meta.applied || unchanged[0].entry.ID != "first" {
		t.Fatal("a head that is not larger than the injection cap should stay put")
	}
	if _, meta := blendRelevanceRerank(context.Background(), nil, "why compare", near); meta.applied {
		t.Fatal("nil reranker should not apply")
	}
}

func TestBlendRelevanceRerankScoresHighestHead(t *testing.T) {
	var seen []string
	rr := &countingMemoryReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		for _, passage := range passages {
			seen = append(seen, passage.ID)
		}
		return make([]float64, len(passages)), nil
	}}
	candidates := make([]recallScored, 0, 40)
	for i := 0; i < 32; i++ {
		candidates = append(candidates, recallScored{entry: Entry{ID: "low", Content: "low"}, score: 1})
	}
	for i := 0; i < 8; i++ {
		candidates = append(candidates, recallScored{entry: Entry{ID: "top", Content: "top"}, score: 50})
	}
	if _, meta := blendRelevanceRerank(context.Background(), rr, "why compare", candidates); !meta.applied {
		t.Fatal("expected rerank")
	}
	tops := 0
	for _, id := range seen {
		if id == "top" {
			tops++
		}
	}
	if tops != 8 || len(seen) != memoryRerankHead {
		t.Fatalf("head = %v, tops = %d", seen, tops)
	}
}

func TestBlendThenDiversityStillPromotesOtherTheme(t *testing.T) {
	candidates := make([]recallScored, 0, 20)
	for i := 0; i < 16; i++ {
		candidates = append(candidates, recallScored{entry: Entry{ID: "go", Content: "go concurrency"}, score: 10 - float64(i)*0.01})
	}
	for i := 0; i < 4; i++ {
		candidates = append(candidates, recallScored{entry: Entry{ID: "pg", Content: "postgresql backup"}, score: 3 - float64(i)*0.01})
	}
	boostGo := &countingMemoryReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		scores := make([]float64, len(passages))
		for i, passage := range passages {
			if passage.ID == "go" {
				scores[i] = 1
			}
		}
		return scores, nil
	}}
	blended, meta := blendRelevanceRerank(context.Background(), boostGo, "why compare", candidates)
	if !meta.applied {
		t.Fatal("expected rerank")
	}
	themes := []ThemeNode{
		{ID: "go", EntryIDs: []string{"go"}},
		{ID: "pg", EntryIDs: []string{"pg"}},
	}
	diverse := themeAwareDiversityRerank(blended, themes, 2)
	if diverse[0].entry.ID != "go" || diverse[1].entry.ID != "pg" {
		t.Fatalf("diversity lost the other theme: %s then %s", diverse[0].entry.ID, diverse[1].entry.ID)
	}
}

func TestRecallRerankOnlyOnComplexBudgetedPaths(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	for i := 0; i < 20; i++ {
		content := "postgresql backup restore incident timeline item " + string(rune('a'+i))
		if i%2 == 0 {
			content = "go concurrency worker pool retry pattern item " + string(rune('a'+i))
		}
		if err := store.Save(Entry{Content: content, Category: CategoryProjectKnowledge, Tags: []string{"go"}, Status: StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	counter := &countingMemoryReranker{}
	store.SetReranker(counter)

	_ = store.RecallDynamic("api", "", "")
	if calls, _ := counter.Calls(); calls != 0 {
		t.Fatalf("simple query calls = %d", calls)
	}
	_ = store.recallScoredForPagination("why compare go concurrency and postgresql backup over time", "", "", "")
	if calls, _ := counter.Calls(); calls != 0 {
		t.Fatalf("pagination calls = %d", calls)
	}
	_ = store.RecallDynamic("why compare go concurrency and postgresql backup over time", "", "")
	calls, pool := counter.Calls()
	if calls != 1 || pool <= memoryRerankMinCandidates || pool > memoryRerankHead {
		t.Fatalf("dynamic calls=%d pool=%d", calls, pool)
	}
	trace := store.LastRecallTrace()
	if !trace.RerankApplied || trace.RerankPool != pool {
		t.Fatalf("trace = %+v", trace)
	}

	_ = store.RecallForProject("why compare go concurrency and postgresql backup over time", "")
	if calls, _ := counter.Calls(); calls < 2 {
		t.Fatalf("RecallForProject did not rerank, calls=%d", calls)
	}

	store.SetReranker(nil)
	before := store.RecallDynamic("why compare go concurrency and postgresql backup over time", "", "")
	again := store.RecallDynamic("why compare go concurrency and postgresql backup over time", "", "")
	if len(before) != len(again) {
		t.Fatalf("nil reranker changed the result count: %d vs %d", len(before), len(again))
	}
	for i := range before {
		if before[i].ID != again[i].ID {
			t.Fatalf("nil reranker changed order at %d", i)
		}
	}
	if store.LastRecallTrace().RerankApplied {
		t.Fatal("nil reranker recorded an application")
	}
}

// lockProbeReranker takes the store write lock from inside Rerank. That succeeds
// only when recall has already dropped its read lock.
type lockProbeReranker struct {
	store   *Store
	blocked atomic.Bool
	calls   atomic.Int32
}

func (p *lockProbeReranker) Rerank(_ context.Context, _ string, passages []rerank.Passage) ([]float64, error) {
	p.calls.Add(1)
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if p.store.mu.TryLock() {
			p.store.mu.Unlock()
			return make([]float64, len(passages)), nil
		}
		if time.Now().After(deadline) {
			p.blocked.Store(true)
			return make([]float64, len(passages)), nil
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRecallRerankDoesNotHoldStoreLock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	for i := 0; i < 20; i++ {
		content := "postgresql backup restore incident timeline item " + string(rune('a'+i))
		if i%2 == 0 {
			content = "go concurrency worker pool retry pattern item " + string(rune('a'+i))
		}
		if err := store.Save(Entry{Content: content, Category: CategoryProjectKnowledge, Tags: []string{"go"}, Status: StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	probe := &lockProbeReranker{store: store}
	store.SetReranker(probe)
	query := "why compare go concurrency and postgresql backup over time"
	_ = store.RecallDynamic(query, "", "")
	if probe.blocked.Load() || probe.calls.Load() != 1 {
		t.Fatalf("RecallDynamic blocked=%v calls=%d", probe.blocked.Load(), probe.calls.Load())
	}
	_ = store.RecallForProject(query, "")
	if probe.blocked.Load() || probe.calls.Load() != 2 {
		t.Fatalf("RecallForProject blocked=%v calls=%d", probe.blocked.Load(), probe.calls.Load())
	}
}
