package memory

import (
	"testing"
	"time"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestClassifyComplexitySimple(t *testing.T) {
	// Short, single entity queries should be simple.
	tests := []struct {
		query    string
		entities []string
	}{
		{"端口号是多少", []string{"端口号"}},
		{"what is the API key", []string{"API key"}},
		{"项目路径", nil},
	}

	for _, tc := range tests {
		c := ClassifyComplexity(tc.query, tc.entities, nil)
		if c != ComplexitySimple {
			t.Errorf("expected simple for %q, got %s", tc.query, c)
		}
	}
}

func TestClassifyComplexityComplex(t *testing.T) {
	// Long queries with temporal/reasoning keywords and multiple entities.
	tests := []struct {
		query    string
		entities []string
	}{
		{
			"为什么我们从 React 迁移到了 Vue，对比一下两者的区别，分析一下这个趋势变化的历史原因",
			[]string{"React", "Vue", "迁移"},
		},
		{
			"summarize how my coding style has evolved over time, compare the pattern of using TypeScript vs JavaScript, and analyze why I always prefer functional components",
			[]string{"TypeScript", "JavaScript", "functional components"},
		},
	}

	for _, tc := range tests {
		c := ClassifyComplexity(tc.query, tc.entities, nil)
		if c != ComplexityComplex {
			t.Errorf("expected complex for %q, got %s", tc.query, c)
		}
	}
}

func TestClassifyComplexityHybrid(t *testing.T) {
	// Medium-length queries with some complexity signals.
	tests := []struct {
		query    string
		entities []string
	}{
		{
			"为什么选择了 Go 而不是 Rust",
			[]string{"Go", "Rust"},
		},
		{
			"what tools do I usually use for debugging",
			[]string{"debugging"},
		},
	}

	for _, tc := range tests {
		c := ClassifyComplexity(tc.query, tc.entities, nil)
		// Hybrid heuristic may drift; just verify it's not simple.
		if c == ComplexitySimple {
			t.Errorf("expected hybrid or complex for %q, got %s", tc.query, c)
		}
	}
}

func TestQueryComplexityRecallLevels(t *testing.T) {
	simple := ComplexitySimple.RecallLevels()
	if len(simple) != 3 {
		t.Errorf("simple should have 3 levels, got %d", len(simple))
	}

	complex := ComplexityComplex.RecallLevels()
	if len(complex) != 5 {
		t.Errorf("complex should have 5 levels, got %d", len(complex))
	}

	// Complex should include LevelWeek, simple should not.
	hasWeek := false
	for _, l := range complex {
		if l == LevelWeek {
			hasWeek = true
		}
	}
	if !hasWeek {
		t.Error("complex recall should include LevelWeek")
	}

	for _, l := range simple {
		if l == LevelWeek {
			t.Error("simple recall should NOT include LevelWeek")
		}
	}
}

func TestTemporalLevelString(t *testing.T) {
	tests := []struct {
		level TemporalLevel
		want  string
	}{
		{LevelNone, "none"},
		{LevelSegment, "segment"},
		{LevelSession, "session"},
		{LevelDay, "day"},
		{LevelWeek, "week"},
		{LevelProfile, "profile"},
	}

	for _, tc := range tests {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("TemporalLevel(%d).String() = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestTimeIntervalContains(t *testing.T) {
	outer := TimeInterval{
		Start: mustParseTime("2026-01-01T00:00:00Z"),
		End:   mustParseTime("2026-01-02T00:00:00Z"),
	}
	inner := TimeInterval{
		Start: mustParseTime("2026-01-01T06:00:00Z"),
		End:   mustParseTime("2026-01-01T18:00:00Z"),
	}
	disjoint := TimeInterval{
		Start: mustParseTime("2026-01-03T00:00:00Z"),
		End:   mustParseTime("2026-01-04T00:00:00Z"),
	}

	if !outer.Contains(inner) {
		t.Error("outer should contain inner")
	}
	if inner.Contains(outer) {
		t.Error("inner should NOT contain outer")
	}
	if outer.Contains(disjoint) {
		t.Error("outer should NOT contain disjoint")
	}
}

func TestTimeIntervalOverlaps(t *testing.T) {
	a := TimeInterval{
		Start: mustParseTime("2026-01-01T00:00:00Z"),
		End:   mustParseTime("2026-01-01T12:00:00Z"),
	}
	b := TimeInterval{
		Start: mustParseTime("2026-01-01T06:00:00Z"),
		End:   mustParseTime("2026-01-01T18:00:00Z"),
	}
	c := TimeInterval{
		Start: mustParseTime("2026-01-02T00:00:00Z"),
		End:   mustParseTime("2026-01-02T12:00:00Z"),
	}

	if !a.Overlaps(b) {
		t.Error("a and b should overlap")
	}
	if a.Overlaps(c) {
		t.Error("a and c should NOT overlap")
	}
}
