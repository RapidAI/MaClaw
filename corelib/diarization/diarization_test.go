package diarization

import (
	"math"
	"testing"
)

func TestClusterRespectsKnownSpeakerCount(t *testing.T) {
	embs := [][]float32{{1, 0}, {.99, .01}, {0, 1}, {.02, .99}}
	got := cluster(embs, Config{KnownSpeakers: 2}.normalized())
	if got[0] != got[1] || got[2] != got[3] || got[0] == got[2] {
		t.Fatalf("labels = %v, want two speaker groups", got)
	}
}

func TestClusterRespectsAutomaticMaxSpeakerCount(t *testing.T) {
	// These deliberately dissimilar embeddings would not meet the normal
	// merge threshold. Automatic mode must still cap the number of labels so a
	// noisy recording cannot fan out into one ASR job per analysis window.
	embs := [][]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	got := cluster(embs, Config{MinSpeakers: 1, MaxSpeakers: 2}.normalized())
	seen := map[int]struct{}{}
	for _, label := range got {
		seen[label] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("labels = %v, want at most two automatic speaker groups", got)
	}
}

func TestAutomaticSpeakerCapShortMeeting(t *testing.T) {
	cfg := Config{MinSpeakers: 1, MaxSpeakers: 15}.normalized()
	// ~27s of speech (the user-reported two-person product demo) must not allow
	// seven invented speakers.
	if got := automaticSpeakerCap(27, cfg); got != 2 {
		t.Fatalf("automaticSpeakerCap(27) = %d, want 2", got)
	}
	if got := automaticSpeakerCap(90, cfg); got != 5 {
		t.Fatalf("automaticSpeakerCap(90) = %d, want 5", got)
	}
	if got := automaticSpeakerCap(0, cfg); got != 15 {
		t.Fatalf("automaticSpeakerCap(0) = %d, want MaxSpeakers fallback", got)
	}
}

func TestClusterDurationSoftCapStopsOverSegmentation(t *testing.T) {
	// Seven mutually dissimilar short-window embeddings — the classic failure
	// mode where each interjection becomes Speaker N on a short two-person clip.
	embs := [][]float32{
		{1, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0, 0},
		{0, 0, 1, 0, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 0},
		{0, 0, 0, 0, 1, 0, 0},
		{0, 0, 0, 0, 0, 1, 0},
		{0, 0, 0, 0, 0, 0, 1},
	}
	got := cluster(embs, Config{
		MinSpeakers:   1,
		MaxSpeakers:   15,
		SpeechSeconds: 27,
	}.normalized())
	seen := map[int]struct{}{}
	for _, label := range got {
		seen[label] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("labels = %v (%d speakers), want 2 for a ~27s automatic meeting", got, len(seen))
	}
}

func TestClusterSoftMergeCollapsesNearDuplicatesUnderCapPressure(t *testing.T) {
	// Two real speakers (axis-aligned) plus near-duplicates that fall below the
	// hard 0.78 bar but above the soft 0.55 bar. With a short speech budget the
	// near-duplicates must rejoin their parent speaker rather than invent IDs.
	// Cosine( [1,0], normalize([1,0.5]) ) ≈ 0.894 → hard-merge.
	// Use a weaker near-match: [1, 0.7] → cos ≈ 0.819 hard; need softer.
	// [1, 0] vs normalize([0.7, 0.7]) = cos( [1,0], [0.707,0.707] ) = 0.707 soft.
	a := []float32{1, 0}
	aNear := []float32{0.7071, 0.7071} // cos≈0.707 with a (below hard 0.78, above soft 0.55)
	b := []float32{0, 1}
	// Keep four embeddings that should collapse to at most 2 under softCap=2.
	embs := [][]float32{a, aNear, b, {0.1, 0.995}}
	for i := range embs {
		normalize(embs[i])
	}
	got := cluster(embs, Config{
		MinSpeakers:   1,
		MaxSpeakers:   15,
		SpeechSeconds: 25,
	}.normalized())
	seen := map[int]struct{}{}
	for _, label := range got {
		seen[label] = struct{}{}
	}
	if len(seen) > 2 {
		t.Fatalf("labels = %v, want at most 2 speakers after soft/duration cap", got)
	}
}

func TestSmoothCollapsesSingletonBlipSpeaker(t *testing.T) {
	got := smooth([]Segment{
		{0, 2, 0},
		{2, 2.6, 1}, // 0.6s singleton blip — noise, not a real turn
		{2.6, 4, 0},
	})
	for _, s := range got {
		if s.Speaker != 0 {
			t.Fatalf("smooth left singleton blip: %#v", got)
		}
	}
}

func TestSmoothKeepsShortButRealSecondSpeaker(t *testing.T) {
	got := smooth([]Segment{
		{0, 2, 0},
		{2, 3, 1}, // 1s reply must not be swallowed
		{3, 5, 0},
	})
	seen := map[int]struct{}{}
	for _, s := range got {
		seen[s.Speaker] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("smooth swallowed real short reply: %#v", got)
	}
}

func TestConfigNormalizesUnsafeKnownSpeakerCount(t *testing.T) {
	if got := (Config{KnownSpeakers: -3}).normalized().KnownSpeakers; got != 0 {
		t.Fatalf("negative KnownSpeakers = %d, want automatic mode (0)", got)
	}
	if got := (Config{KnownSpeakers: 99, MaxSpeakers: 2}).normalized().KnownSpeakers; got != 2 {
		t.Fatalf("oversized KnownSpeakers = %d, want MaxSpeakers (2)", got)
	}
}

func TestSmoothMidpointAndMerge(t *testing.T) {
	got := smooth([]Segment{{0, 1.5, 0}, {.75, 2, 0}, {2, 3, 1}})
	if len(got) != 2 || got[0].Speaker != 0 || math.Abs(got[0].End-2) > .001 || got[1].Speaker != 1 {
		t.Fatalf("smooth = %#v", got)
	}
}

func TestChunkPadsAndKeepsTimes(t *testing.T) {
	pcm := make([]float32, SampleRate*2)
	got := chunk([]speechSpan{{start: 0, end: 2}}, pcm, 1.5, .75)
	if len(got) != 2 || len(got[0].pcm) != SampleRate*3/2 || got[1].end != 2 {
		t.Fatalf("chunk = %#v", got)
	}
}

func TestChunkBoundedCoversLongSpeechWithoutExceedingWindowBudget(t *testing.T) {
	pcm := make([]float32, SampleRate*600) // ten minutes; no real model/VAD needed.
	spans := []speechSpan{{start: 0, end: 600}}
	got := chunkBounded(spans, pcm, 1.5, .75, 40)
	if len(got) > 40 {
		t.Fatalf("windows = %d, want at most 40", len(got))
	}
	if len(got) < 2 || got[0].start != 0 || got[len(got)-1].end != 600 {
		t.Fatalf("bounded windows do not cover meeting: first=%#v last=%#v", got[0], got[len(got)-1])
	}
}

func TestChunkBoundedStrictlyCapsManySpeechSpans(t *testing.T) {
	pcm := make([]float32, SampleRate*30)
	spans := make([]speechSpan, 30)
	for i := range spans {
		spans[i] = speechSpan{start: float64(i), end: float64(i) + .6}
	}
	got := chunkBounded(spans, pcm, 1.5, .75, 8)
	if len(got) != 8 {
		t.Fatalf("windows = %d, want strict budget of 8", len(got))
	}
	if got[0].start != 0 || got[len(got)-1].start < 29 {
		t.Fatalf("span sampling lost recording endpoints: first=%#v last=%#v", got[0], got[len(got)-1])
	}
}
