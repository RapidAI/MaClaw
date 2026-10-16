package llmservice

import (
	"testing"
	"time"
)

func TestTokenBankAutoPauseFiresAtThresholdAndRetriesPastIt(t *testing.T) {
	t.Cleanup(resetTokenBankAutoPause)
	resetTokenBankAutoPause()
	var pauses []string
	ConfigureTokenBankAutoPause(func() int { return 3 }, func(shareID, model, reason string) {
		pauses = append(pauses, shareID+"/"+model+"/"+reason)
	})
	id := TokenBankMemberID("share-autopause", "llama")

	noteMemberAttempt(id, 400, time.Millisecond, "bad prompt", "")
	noteMemberAttempt(id, 404, time.Millisecond, "missing", "")
	noteMemberAttempt(id, 422, time.Millisecond, "unprocessable", "")
	noteMemberAttempt(id, 500, time.Millisecond, "upstream", "")
	noteMemberAttempt(id, 500, time.Millisecond, "upstream", "")
	if len(pauses) != 0 {
		t.Fatalf("pauses after two failures and ignored client errors = %v", pauses)
	}
	noteMemberAttempt(id, 429, time.Millisecond, "slow down", "")
	if len(pauses) != 1 {
		t.Fatalf("pauses at threshold = %v, want 1", pauses)
	}
	// A streak that continues past the threshold means the pause did not land
	// (the only writer that stops the traffic is the pause itself). Every
	// further failure must retry, or a transient store error at the exact
	// threshold moment leaves the share unpaused forever: the counter only
	// resets on success, so exact-match would never fire again.
	noteMemberAttempt(id, 500, time.Millisecond, "upstream", "")
	if len(pauses) != 2 {
		t.Fatalf("pauses past the threshold = %v, want retry (2)", pauses)
	}

	noteMemberAttempt(id, 200, time.Millisecond, "", "")
	noteMemberAttempt(id, 401, time.Millisecond, "unauthorized", "")
	noteMemberAttempt(id, 403, time.Millisecond, "forbidden", "")
	if len(pauses) != 2 {
		t.Fatalf("pauses before the new streak reaches 3 = %v", pauses)
	}
	noteMemberAttempt(id, 0, time.Millisecond, "dial failed", "")
	if len(pauses) != 3 {
		t.Fatalf("pauses after a reset streak = %d, want 3 (%v)", len(pauses), pauses)
	}
}

func TestTokenBankAutoPauseDisabledAtZero(t *testing.T) {
	t.Cleanup(resetTokenBankAutoPause)
	resetTokenBankAutoPause()
	pauses := 0
	ConfigureTokenBankAutoPause(func() int { return 0 }, func(string, string, string) { pauses++ })
	id := TokenBankMemberID("share-autopause-off", "llama")
	for i := 0; i < 6; i++ {
		noteMemberAttempt(id, 500, time.Millisecond, "upstream", "")
	}
	if pauses != 0 {
		t.Fatalf("pauses = %d, want 0 when the threshold is 0", pauses)
	}
}
