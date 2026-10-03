package guiapp

import "testing"

func TestCreditGiftFromArgsAcceptsClaimLinksOnly(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"maclaw://credit/abcdef2345", "ABCDEF2345"},
		{"maclaw://credit/ABCDEF2345/", "ABCDEF2345"},
		{"https://hub.example/c/ABCDEF2345", "ABCDEF2345"},
		{"https://hub.example/c/ABCDEF2345?x=1", "ABCDEF2345"},
		{"maclaw://credit/short", ""},
		{"maclaw://credit/%3Cscript%3E", ""},
		{"maclaw://onboarding?referral_handoff=0123456789abcdef", ""},
		{"maclaw://cloud-workspace-share?token=tok", ""},
	}
	for _, tc := range cases {
		got := creditGiftFromArgs([]string{"--flag", tc.raw})
		if got.Code != tc.want {
			t.Fatalf("creditGiftFromArgs(%q) = %q, want %q", tc.raw, got.Code, tc.want)
		}
	}
}

func TestConsumeCreditGiftHandoffReturnsTheCodeOnce(t *testing.T) {
	app := &App{}
	app.setPendingCreditGift(CreditGiftLaunch{Code: "abcdef2345"})
	got := app.ConsumeCreditGiftHandoff()
	if got.Code != "ABCDEF2345" {
		t.Fatalf("first consume = %q", got.Code)
	}
	if again := app.ConsumeCreditGiftHandoff(); again.Code != "" {
		t.Fatalf("second consume = %q, want empty", again.Code)
	}
}
