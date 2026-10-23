package rerank

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type stubReranker struct {
	fn func(context.Context, string, []Passage) ([]float64, error)
}

func (s stubReranker) Rerank(ctx context.Context, query string, passages []Passage) ([]float64, error) {
	return s.fn(ctx, query, passages)
}

func TestApplyNilAndEmpty(t *testing.T) {
	passages := []Passage{{ID: "a", Text: "alpha"}}
	if got := Apply(context.Background(), nil, "q", passages, DefaultBudget); got.Applied {
		t.Fatal("nil reranker should not apply")
	}
	if got := Apply(context.Background(), stubReranker{fn: func(context.Context, string, []Passage) ([]float64, error) {
		t.Fatal("empty passages should not call the reranker")
		return nil, nil
	}}, "q", nil, DefaultBudget); got.Applied {
		t.Fatal("empty passages should not apply")
	}
}

func TestApplyRejectsMismatchTimeoutAndCanceled(t *testing.T) {
	passages := []Passage{{ID: "a", Text: "alpha"}, {ID: "b", Text: "beta"}}
	mismatch := stubReranker{fn: func(context.Context, string, []Passage) ([]float64, error) {
		return []float64{1}, nil
	}}
	if got := Apply(context.Background(), mismatch, "q", passages, time.Second); got.Applied {
		t.Fatal("length mismatch should not apply")
	}
	bad := stubReranker{fn: func(context.Context, string, []Passage) ([]float64, error) {
		return []float64{1, math.NaN()}, nil
	}}
	if got := Apply(context.Background(), bad, "q", passages, time.Second); got.Applied {
		t.Fatal("NaN score should not apply")
	}
	fail := stubReranker{fn: func(context.Context, string, []Passage) ([]float64, error) {
		return nil, errors.New("down")
	}}
	if got := Apply(context.Background(), fail, "q", passages, time.Second); got.Applied {
		t.Fatal("error should not apply")
	}
	slow := stubReranker{fn: func(ctx context.Context, _ string, _ []Passage) ([]float64, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return []float64{1, 0}, nil
		}
	}}
	if got := Apply(context.Background(), slow, "q", passages, 20*time.Millisecond); got.Applied {
		t.Fatal("timeout should not apply")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Apply(ctx, slow, "q", passages, time.Second); got.Applied {
		t.Fatal("canceled context should not apply")
	}
	parent, cancelParent := context.WithCancel(context.Background())
	late := stubReranker{fn: func(context.Context, string, []Passage) ([]float64, error) {
		cancelParent()
		return []float64{1, 0}, nil
	}}
	if got := Apply(parent, late, "q", passages, time.Second); got.Applied {
		t.Fatal("a score finished after cancel should not apply")
	}
}

func TestApplyReturnsScores(t *testing.T) {
	passages := []Passage{{ID: "a", Text: "alpha"}}
	ok := stubReranker{fn: func(context.Context, string, []Passage) ([]float64, error) {
		return []float64{0.4}, nil
	}}
	got := Apply(context.Background(), ok, "q", passages, time.Second)
	if !got.Applied || len(got.Scores) != 1 || got.Scores[0] != 0.4 {
		t.Fatalf("unexpected outcome: %+v", got)
	}
}

func TestWindowKeepsQueryHit(t *testing.T) {
	short := Window("alpha beta", "beta")
	if short != "alpha beta" {
		t.Fatalf("short text changed: %q", short)
	}
	late := strings.Repeat("甲", MaxPassageRunes) + " GOLDNEEDLE"
	got := Window(late, "goldneedle")
	if !strings.Contains(got, "GOLDNEEDLE") {
		t.Fatalf("late hit dropped: %q", got)
	}
	if utf8.RuneCountInString(got) > MaxPassageRunes {
		t.Fatalf("window runes = %d", utf8.RuneCountInString(got))
	}
	// The earlier short term must not steal the window from the longer phrase.
	early := strings.Repeat("甲", 40) + "政策" + strings.Repeat("乙", MaxPassageRunes) + "政策修订"
	focused := Window(early, "政策修订")
	if !strings.Contains(focused, "政策修订") || strings.Contains(focused, "甲") {
		t.Fatalf("longer term lost the window: %q", focused)
	}
	plain := Window(strings.Repeat("甲", MaxPassageRunes+8), "missing")
	if plain != strings.Repeat("甲", MaxPassageRunes) {
		t.Fatalf("missing query should keep the prefix")
	}

	// "go" is a word. The same letters inside "algorithm" are not a hit.
	lateGo := "algorithm " + strings.Repeat("甲", MaxPassageRunes) + " go language"
	if got := Window(lateGo, "go?"); !strings.Contains(got, "language") {
		t.Fatalf("word boundary missed: %q", got)
	}
	// A leading question particle covers one term. The later subject covers more.
	asked := strings.Repeat("什么", 40) + strings.Repeat("乙", MaxPassageRunes) + "修订政策"
	if got := Window(asked, "为什么要修订政策"); !strings.Contains(got, "修订政策") {
		t.Fatalf("question particle took the window: %q", got)
	}
	// 籍目 crosses 书籍 and 目录. It is not a query term, so it must not win.
	crossed := strings.Repeat("籍目", 40) + strings.Repeat("乙", MaxPassageRunes) + "书籍"
	if got := Window(crossed, "书籍目录"); !strings.Contains(got, "书籍") {
		t.Fatalf("crossing bigram took the window: %q", got)
	}

	// Each word also stands alone before and after the cluster, so the first
	// and last hits are not the cluster.
	var middle strings.Builder
	middle.WriteString("政策")
	middle.WriteString(strings.Repeat("甲", MaxPassageRunes))
	middle.WriteString("修订")
	middle.WriteString(strings.Repeat("乙", MaxPassageRunes))
	middle.WriteString("备份")
	middle.WriteString(strings.Repeat("丙", MaxPassageRunes))
	middle.WriteString("政策、修订、备份")
	middle.WriteString(strings.Repeat("丁", MaxPassageRunes))
	middle.WriteString("政策")
	middle.WriteString(strings.Repeat("戊", MaxPassageRunes))
	middle.WriteString("修订")
	middle.WriteString(strings.Repeat("己", MaxPassageRunes))
	middle.WriteString("备份")
	if got := Window(middle.String(), "政策修订备份"); !strings.Contains(got, "政策、修订、备份") {
		t.Fatalf("middle cluster missed: %q", got)
	}

	// Lead-in leaves 384 runes after the anchor. These two terms are farther
	// apart than that and still fit in one passage.
	gap := MaxPassageRunes - MaxPassageRunes/4 + 40
	var apart strings.Builder
	apart.WriteString(strings.Repeat("甲", 200))
	apart.WriteString("政策")
	apart.WriteString(strings.Repeat("乙", gap))
	apart.WriteString("修订")
	apart.WriteString(strings.Repeat("丙", MaxPassageRunes))
	if got := Window(apart.String(), "政策修订"); !strings.Contains(got, "政策") || !strings.Contains(got, "修订") || !strings.Contains(got, "甲") {
		t.Fatalf("pair inside the cap was split: %q", got)
	}

	// A name longer than 12 runes is split by the dictionary. The same pieces
	// earlier must not beat the intact name.
	name := "奇安信网神终端安全管理系统"
	split := strings.TrimPrefix(name, "奇") + strings.Repeat("甲", MaxPassageRunes) + name
	if got := Window(split, name); !strings.Contains(got, name) {
		t.Fatalf("long name lost to its fragments: %q", got)
	}

	// "R" is a word. A leading "语言" covers one term; "R语言" covers two.
	lateR := "语言" + strings.Repeat("甲", MaxPassageRunes) + "R语言"
	if got := Window(lateR, "R语言?"); !strings.Contains(got, "R语言") {
		t.Fatalf("single letter lost: %q", got)
	}
	lateC := strings.Repeat("甲", MaxPassageRunes) + " C "
	if got := Window(lateC, "C?"); !strings.Contains(got, "C") {
		t.Fatalf("lone letter dropped: %q", got)
	}

	// Equal coverage keeps the quarter-passage lead-in.
	lead := strings.Repeat("甲", 200) + "GOLDNEEDLE" + strings.Repeat("乙", MaxPassageRunes)
	if got := Window(lead, "goldneedle"); !strings.Contains(got, "GOLDNEEDLE") || !strings.Contains(got, "甲") {
		t.Fatalf("lead-in dropped: %q", got)
	}
}

func TestMinMaxAndTruncate(t *testing.T) {
	flat := MinMax([]float64{3, 3, 3})
	for _, score := range flat {
		if score != 0 {
			t.Fatalf("flat scores should stay zero, got %v", flat)
		}
	}
	scaled := MinMax([]float64{2, 4})
	if scaled[0] != 0 || scaled[1] != 1 {
		t.Fatalf("minmax = %v", scaled)
	}
	long := make([]rune, MaxPassageRunes+5)
	for i := range long {
		long[i] = '测'
	}
	if got := []rune(Truncate(string(long))); len(got) != MaxPassageRunes {
		t.Fatalf("truncate len = %d", len(got))
	}
}
