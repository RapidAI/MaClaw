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

func TestFormatSpeakerTurnsAutoLabelsMultipleSpeakers(t *testing.T) {
	got := FormatSpeakerTurnsAuto([]SpeakerTranscript{
		{Start: 1.5, End: 3, Speaker: 1, Text: " second "},
		{Start: 0, End: 1, Speaker: 0, Text: "first"},
		{Start: 4, End: 5, Speaker: 2, Text: "  "},
	})
	want := "[00:00-00:01] Speaker 1: first\n[00:01-00:03] Speaker 2: second"
	if got != want {
		t.Fatalf("FormatSpeakerTurnsAuto() = %q, want %q", got, want)
	}
}

func TestFormatSpeakerTurnsAutoPlainForSingleSpeaker(t *testing.T) {
	got := FormatSpeakerTurnsAuto([]SpeakerTranscript{
		{Start: 4, End: 5, Speaker: 0, Text: "第二段"},
		{Start: 0, End: 1, Speaker: 0, Text: " 第一段 "},
		{Start: 6, End: 7, Speaker: 0, Text: "  "},
	})
	want := "第一段\n第二段"
	if got != want {
		t.Fatalf("FormatSpeakerTurnsAuto() = %q, want %q", got, want)
	}
}

func TestFormatSpeakerTurnsAutoEmpty(t *testing.T) {
	if got := FormatSpeakerTurnsAuto(nil); got != "" {
		t.Fatalf("FormatSpeakerTurnsAuto(nil) = %q, want empty", got)
	}
	if got := FormatSpeakerTurnsAuto([]SpeakerTranscript{{Start: 0, End: 1, Speaker: 0, Text: " "}}); got != "" {
		t.Fatalf("FormatSpeakerTurnsAuto(blank) = %q, want empty", got)
	}
}
