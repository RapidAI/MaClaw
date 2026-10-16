package guiapp

import "testing"

func TestGiftCodeFromInput(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SECRETCODE", "SECRETCODE"},
		{"  SECRETCODE  ", "SECRETCODE"},
		{"https://hub.example/c/SECRETCODE", "SECRETCODE"},
		{"https://hub.example/c/SECRETCODE/", "SECRETCODE"},
		{"https://hub.example/c/SECRETCODE?x=1", "SECRETCODE"},
		{"maclaw://credit/SECRETCODE", "SECRETCODE"},
		{"", ""},
		{"   ", ""},
		{"maclaw://credit/", ""},
	}
	for _, tc := range cases {
		if got := giftCodeFromInput(tc.in); got != tc.want {
			t.Errorf("giftCodeFromInput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
