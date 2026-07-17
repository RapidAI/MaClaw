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
