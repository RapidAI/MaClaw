package main

import (
	"math"
	"testing"
)

func TestFormatSpeakerTranscript(t *testing.T) {
	got := FormatSpeakerTranscript([]SpeakerTranscript{
		{Start: 62.2, End: 64.9, Speaker: 1, Text: " second speaker "},
		{Start: 0, End: 1, Speaker: 0, Text: "first speaker"},
		{Start: 2, End: 3, Speaker: 2, Text: "  "},
	})
	want := "[00:00-00:01] Speaker 1: first speaker\n[01:02-01:04] Speaker 2: second speaker"
	if got != want {
		t.Fatalf("FormatSpeakerTranscript() = %q, want %q", got, want)
	}
}

func TestFormatSpeakerTranscriptSanitizesMalformedTurns(t *testing.T) {
	got := FormatSpeakerTranscript([]SpeakerTranscript{
		{Start: math.NaN(), End: 2, Speaker: 0, Text: "not a timestamp"},
		{Start: -3, End: 1.9, Speaker: -1, Text: " clipped "},
		{Start: 5, End: 4, Speaker: 1, Text: "reversed"},
	})
	want := "[00:00-00:01] Speaker 1: clipped"
	if got != want {
		t.Fatalf("FormatSpeakerTranscript() = %q, want %q", got, want)
	}
}
