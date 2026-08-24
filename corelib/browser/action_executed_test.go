package browser

import "testing"

// Callers outside this package decide whether to report an action as performed
// or refused, and getting it backwards is costly in one direction: calling a
// completed action refused tells the model nothing happened and invites it to
// repeat an effect that already holds.
//
// This pins the whole vocabulary this package assigns, so adding a status
// without deciding which side it falls on fails here rather than in a caller.
func TestBrowserActionExecutedCoversEveryStatusThisPackageAssigns(t *testing.T) {
	for _, tc := range []struct {
		status   string
		executed bool
		why      string
	}{
		{"ok", true, "the action completed"},
		{"", true, "an unset status comes from paths that completed"},
		{"unchanged", true, "the page was navigated, it just looks the same"},
		{"ask", true, "the captcha sits on a page that was already loaded"},
		{"expect_failed", true, "the action happened; only its verification failed"},
		{"blocked", false, "policy refused before the request left"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			if got := BrowserActionExecuted(&BrowserActionResult{Status: tc.status}); got != tc.executed {
				t.Fatalf("BrowserActionExecuted(%q) = %v, want %v because %s", tc.status, got, tc.executed, tc.why)
			}
		})
	}
	if BrowserActionExecuted(nil) {
		t.Fatal("a missing result describes no action at all")
	}
}

// The refusal a caller most needs to recognise is the one this package builds
// itself, so drive the real constructor rather than a status literal.
func TestBrowserActionExecutedRejectsTheBlockResultThisPackageBuilds(t *testing.T) {
	blocked := blockedActionResult("browser_navigate", "browser policy blocked URL scheme: javascript")
	if BrowserActionExecuted(blocked) {
		t.Fatalf("the package's own block result read as a performed action: %#v", blocked)
	}
}

// A navigation that policy denies must reach the caller through that same
// block result and with a nil error, which is the shape callers have to defend
// against. If this ever starts returning an error instead, the callers that
// only check Status can be simplified -- and the ones that only check err stop
// being wrong.
func TestPolicyDeniedNavigationIsReportedAsAResultNotAnError(t *testing.T) {
	denied := validateNavigationPolicy(BrowserPolicy{}, "javascript:alert(1)", "")
	if !isPolicyDenied(denied) {
		t.Fatalf("javascript: should be denied by policy, got %v", denied)
	}
	result, err := policyBlockResult(&BrowserAgentSession{}, "browser_navigate", denied)
	if err != nil {
		t.Fatalf("a policy denial still arrives as a nil error today, got %v", err)
	}
	if BrowserActionExecuted(result) {
		t.Fatalf("a denied navigation read as a visited page: %#v", result)
	}
}
