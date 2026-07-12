package textutil

import (
	"strings"
	"testing"
)

func TestPrepareChatBodyForDisplay(t *testing.T) {
	rocket := "\U0001F680"
	dart := "\U0001F3AF"
	pin := "\U0001F4CC"
	star := "\u2B50"
	smile := "\U0001F60A"
	thumbs := "\U0001F44D"
	check := "\u2705"
	warn := "\u26A0"
	cross := "\u274C"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"leading", rocket + " Done.", "Done."},
		{"heading", "### " + dart + " Goals", "### Goals"},
		{"list", "- " + pin + " note", "- note"},
		{"mid-star-kept", "Score " + star + star + " high", "Score " + star + star + " high"},
		{"mid-decorative-stripped", "Great plan " + smile, "Great plan"},
		{"mid-thumbs-stripped", "Fully ok " + thumbs + " go ahead", "Fully ok go ahead"},
		{"status-kept", "Good job " + check + " keep going", "Good job " + check + " keep going"},
		{"warn-kept", warn + " oil is high", warn + " oil is high"},
		{"cross-kept", cross + " sugar", cross + " sugar"},
		{"multi-line", "Line one\n" + rocket + " line two", "Line one\nline two"},
		{"fence", "```\n" + rocket + " keep\n```", "```\n" + rocket + " keep\n```"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PrepareChatBodyForDisplay(tc.in)
			if got != tc.want {
				t.Fatalf("PrepareChatBodyForDisplay(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripLeadingEmojiCluster(t *testing.T) {
	got := StripLeadingEmojiCluster("\U0001F50D **/btw**")
	if got != "**/btw**" {
		t.Fatalf("got %q", got)
	}
}

func TestPrepareChatBodyForDisplayIdempotent(t *testing.T) {
	in := "### \U0001F3AF Goals\n- \U0001F4CC note\nScore \u2B50 high"
	once := PrepareChatBodyForDisplay(in)
	twice := PrepareChatBodyForDisplay(once)
	if once != twice {
		t.Fatalf("not idempotent: once=%q twice=%q", once, twice)
	}
	if once != "### Goals\n- note\nScore \u2B50 high" {
		t.Fatalf("unexpected once=%q", once)
	}
}

func TestPrepareChatBodyForDisplayFastPathCleanText(t *testing.T) {
	in := "plain text\n### Heading\n- list item"
	// Must return the same string (no allocation / rewrite) when nothing to strip.
	got := PrepareChatBodyForDisplay(in)
	if got != in {
		t.Fatalf("clean text rewritten: %q", got)
	}
	// Status/star-only marks force a scan but keep content identity when nothing decorative removed.
	withMid := "Score \u2B50 high"
	gotMid := PrepareChatBodyForDisplay(withMid)
	if gotMid != withMid {
		t.Fatalf("mid-sentence rewritten: %q", gotMid)
	}
}

func TestStripLeadingEmojiClusterSkinTone(t *testing.T) {
	// thumbs up + light skin tone (both in pictograph ranges)
	in := "\U0001F44D\U0001F3FB done"
	got := StripLeadingEmojiCluster(in)
	if got != "done" {
		t.Fatalf("got %q want done", got)
	}
}

func TestPrepareChatBodyPreservesStatusVS16WhenStrippingDecorative(t *testing.T) {
	// U+26A0 + VS16 must survive when a decorative smile is also removed.
	warn := "\u26A0\uFE0F"
	smile := "\U0001F60A"
	in := warn + " oil " + smile
	want := warn + " oil"
	got := PrepareChatBodyForDisplay(in)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPrepareChatBodyStripsMidLineZWJ(t *testing.T) {
	family := "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	in := "Team " + family + " ready"
	got := PrepareChatBodyForDisplay(in)
	if got != "Team ready" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "\u200D") {
		t.Fatalf("orphaned ZWJ left in %q", got)
	}
}
