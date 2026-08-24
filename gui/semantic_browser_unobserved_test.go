package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/gorilla/websocket"
)

// browserAdapterResult drives the trusted browser adapter with a handler that
// fails in a named way, so the assertions below are about how a named fact is
// classified rather than about how any particular session misbehaves.
func browserAdapterResult(t *testing.T, action string, handlerErr error) string {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedBrowser = func(string, string, string) (string, error) {
		return "", handlerErr
	}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	args := fmt.Sprintf(`{"action":%q,"url":"https://example.com"}`, action)
	if action != "navigate" {
		args = fmt.Sprintf(`{"action":%q}`, action)
	}
	got := callbacks.executeTrustedBrowser(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(args),
	})
	// Without this the assertions could pass for the wrong reason: an argument
	// or admission rejection also yields a "[system rejected]" string, and
	// would look identical to the classification under test.
	if !strings.Contains(got, handlerErr.Error()) {
		t.Fatalf("adapter did not reach the handler failure %q, got %q", handlerErr, got)
	}
	return got
}

// A navigate that was dispatched before its target vanished may already have
// happened, and navigating can consume a one-time URL or move server state.
// Reporting a definite failure is what invites the model to send it a second
// time, which is the whole reason the unknown outcome exists.
func TestBrowserDispatchedThenLostIsUnknownNotAFailure(t *testing.T) {
	got := browserAdapterResult(t, "navigate", fmt.Errorf("trusted_browser_outcome_unobserved"))
	if !strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("dispatched-then-lost navigate = %q, want an unknown outcome", got)
	}
}

// The opposite fact must stay a definite failure. A session that was never
// available never carried the request, so calling it unknown would tell the
// user an effect might hold when none possibly can.
func TestBrowserSessionThatNeverCarriedTheRequestStaysADefiniteFailure(t *testing.T) {
	got := browserAdapterResult(t, "navigate", fmt.Errorf("trusted_browser_session_disconnected"))
	if !strings.HasPrefix(got, "[system rejected]") {
		t.Fatalf("never-dispatched navigate = %q, want a definite rejection", got)
	}
}

// semanticOutcomeCategory keeps the comparison below about the verdict rather
// than the wording. The two results always differ as strings, because each
// echoes its own identifier, so comparing them whole would prove nothing.
func semanticOutcomeCategory(result string) string {
	if !strings.HasPrefix(result, "[") {
		return ""
	}
	if end := strings.Index(result, "]"); end > 0 {
		return result[:end+1]
	}
	return ""
}

// The two facts must not collapse into one verdict. If the handler gives them
// the same name again, no classifier downstream can tell them apart -- and the
// two tests above could both still pass while the distinction itself was gone.
func TestBrowserKeepsTheTwoLostSessionFactsDistinct(t *testing.T) {
	unobserved := semanticOutcomeCategory(browserAdapterResult(t, "navigate", fmt.Errorf("trusted_browser_outcome_unobserved")))
	neverRan := semanticOutcomeCategory(browserAdapterResult(t, "navigate", fmt.Errorf("trusted_browser_session_disconnected")))
	if unobserved == "" || neverRan == "" {
		t.Fatalf("adapter returned an unclassified result: %q and %q", unobserved, neverRan)
	}
	if unobserved == neverRan {
		t.Fatalf("dispatched-then-lost and never-dispatched both classified as %s", unobserved)
	}
}

// Uncertainty is not a virtue on its own; it is worth reporting only when an
// effect might already hold. Observing leaves nothing behind, so a lost
// snapshot is a plain failure and the model should feel free to ask again.
func TestBrowserLostSnapshotIsAPlainFailureNotAnUnknown(t *testing.T) {
	got := browserAdapterResult(t, "snapshot", fmt.Errorf("trusted_browser_session_disconnected"))
	if strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("read-only snapshot claimed an effect might hold: %q", got)
	}
	if !strings.HasPrefix(got, "[system rejected]") {
		t.Fatalf("lost snapshot = %q, want a definite rejection", got)
	}
}

// A refused navigation reaches the caller as a nil error carrying the refusal
// text, which is the shape that turns "we blocked this" into "here is the page
// you asked for".
//
// Which statuses count as performed is decided by the browser package now, so
// this only checks that the managed surface asks it the question the right way
// round. The vocabulary itself is pinned where it is produced.
func TestTrustedBrowserRefusalIsNotMistakenForAVisitedPage(t *testing.T) {
	for _, performed := range []string{"ok", "unchanged", ""} {
		if trustedBrowserActionRefused(&browser.BrowserActionResult{Status: performed}) {
			t.Fatalf("status %q is a performed action, not a refusal", performed)
		}
	}
	// "blocked" is the status the browser package attaches to a policy denial,
	// which is the only refusal that can reach this path today.
	if !trustedBrowserActionRefused(&browser.BrowserActionResult{Status: "blocked"}) {
		t.Fatal("a policy denial must not read as a performed action")
	}
	if !trustedBrowserActionRefused(nil) {
		t.Fatal("a missing result must not read as a performed action")
	}
}

// The refusal must also survive the adapter as a definite failure: nothing was
// navigated, so claiming uncertainty would be as wrong as claiming success.
func TestBrowserRefusedActionIsADefiniteFailure(t *testing.T) {
	got := browserAdapterResult(t, "navigate", fmt.Errorf("trusted_browser_action_refused"))
	if !strings.HasPrefix(got, "[system rejected]") {
		t.Fatalf("refused navigation = %q, want a definite rejection", got)
	}
}

// trustedBrowserUnobservedError produces a genuine dispatched-then-lost error
// by driving the browser package against a server that takes the command and
// never answers.
//
// It has to be the real thing. Whether a command left the process is not
// something its message states, so a hand-written error that merely reads like
// one would pass this test while proving nothing about the path in production.
func trustedBrowserUnobservedError(t *testing.T) error {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		time.Sleep(time.Second)
	}))
	t.Cleanup(srv.Close)

	client, err := browser.ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("ConnectCDP: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_, sendErr := client.Send("Page.navigate", map[string]interface{}{"url": "https://example.com/"}, 100*time.Millisecond)
	if sendErr == nil || !browser.IsOutcomeUnobserved(sendErr) {
		t.Fatalf("fixture did not produce an unobserved outcome: %v", sendErr)
	}
	return sendErr
}

// The tests above check how the adapter classifies a name it is handed. This
// one checks the step before it -- which name the handler picks for the same
// vanished session -- because that is where the effect-bearing action and the
// read part ways, and the two must not converge.
func TestTrustedBrowserNamesTheLostSessionByWhetherAnEffectCouldRemain(t *testing.T) {
	vanished := trustedBrowserUnobservedError(t)

	navigate := trustedBrowserLostSessionError("navigate", vanished)
	if navigate == nil || navigate.Error() != "trusted_browser_outcome_unobserved" {
		t.Fatalf("lost navigate = %v, want the unobserved name", navigate)
	}
	snapshot := trustedBrowserLostSessionError("snapshot", vanished)
	if snapshot == nil || snapshot.Error() != "trusted_browser_session_disconnected" {
		t.Fatalf("lost snapshot = %v, want the never-carried name", snapshot)
	}
	if navigate.Error() == snapshot.Error() {
		t.Fatalf("both actions named the same fact: %v", navigate)
	}
	// An unrelated failure must pass through untouched, or every browser error
	// would be laundered into a session verdict it did not earn.
	if other := trustedBrowserLostSessionError("navigate", fmt.Errorf("trusted_browser_empty")); other != nil {
		t.Fatalf("unrelated error was reclassified as a lost session: %v", other)
	}
	if none := trustedBrowserLostSessionError("navigate", nil); none != nil {
		t.Fatalf("nil error produced %v", none)
	}
}

// A target that was already gone when the action was picked up never carried
// the request, whichever action it was. Calling that uncertain tells the user
// an effect might hold when none possibly can, and it is the reading the old
// substring check gave every navigation: "gone" is the word the pre-dispatch
// check uses, while a command lost on the wire says "cdp timeout" and matched
// nothing at all.
func TestTrustedBrowserKeepsANeverSentNavigationDefinite(t *testing.T) {
	preDispatch := fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	for _, action := range []string{"navigate", "snapshot"} {
		got := trustedBrowserLostSessionError(action, preDispatch)
		if got == nil || got.Error() != "trusted_browser_session_disconnected" {
			t.Fatalf("%s on an already-gone target = %v, want the never-carried name", action, got)
		}
	}
}
