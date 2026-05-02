package main

import "testing"

func TestWantsTTSPlaybackDetectsReadRequests(t *testing.T) {
	cases := []string{
		"读笑话给我听",
		"把这段内容读给我听",
		"这个新闻朗读一下",
		"念给我听",
		"read this to me",
	}
	for _, tc := range cases {
		if !wantsTTSPlayback(tc) {
			t.Fatalf("wantsTTSPlayback(%q) = false, want true", tc)
		}
	}
}

func TestWantsTTSPlaybackHonorsNegation(t *testing.T) {
	cases := []string{
		"不要读出来，文字发我就行",
		"别念了",
		"不用朗读",
	}
	for _, tc := range cases {
		if wantsTTSPlayback(tc) {
			t.Fatalf("wantsTTSPlayback(%q) = true, want false", tc)
		}
	}
}
