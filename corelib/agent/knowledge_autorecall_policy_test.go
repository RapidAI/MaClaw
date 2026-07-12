package agent

import "testing"

func TestKnowledgeAutoRecallMaxInjectWithMin(t *testing.T) {
	t.Parallel()
	if got := KnowledgeAutoRecallMaxInjectWithMin(3.5, 0.3); got != 5 {
		t.Fatalf("strong = %d, want 5", got)
	}
	if got := KnowledgeAutoRecallMaxInjectWithMin(1.5, 0.3); got != 3 {
		t.Fatalf("medium = %d, want 3", got)
	}
	if got := KnowledgeAutoRecallMaxInjectWithMin(0.5, 0.3); got != 2 {
		t.Fatalf("weak default = %d, want 2", got)
	}
	// Raising min score to 1.0 excludes the 0.5 band.
	if got := KnowledgeAutoRecallMaxInjectWithMin(0.5, 1.0); got != 0 {
		t.Fatalf("below custom min = %d, want 0", got)
	}
	if got := KnowledgeAutoRecallMaxInjectWithMin(1.2, 1.0); got != 3 {
		t.Fatalf("above custom min mid-band = %d, want 3", got)
	}
	// Non-positive min falls back to default threshold.
	if got := KnowledgeAutoRecallMaxInjectWithMin(0.5, 0); got != KnowledgeAutoRecallMaxInject(0.5) {
		t.Fatalf("zero min should match default MaxInject")
	}
}
