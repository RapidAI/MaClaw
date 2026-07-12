package views

import "testing"

func TestPrepareChatBodyForDisplay(t *testing.T) {
	// U+1F680 rocket, U+1F3AF dart, U+1F4CC pushpin, U+2B50 star
	rocket := "\U0001F680"
	dart := "\U0001F3AF"
	pin := "\U0001F4CC"
	star := "\u2B50"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"leading", rocket + " Done.", "Done."},
		{"heading", "### " + dart + " Goals", "### Goals"},
		{"list", "- " + pin + " note", "- note"},
		{"mid-sentence", "Score " + star + star + " high", "Score " + star + star + " high"},
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
