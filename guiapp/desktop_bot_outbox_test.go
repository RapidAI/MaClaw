package guiapp

import (
	"testing"
)

// resetDesktopBotOutboxForTest isolates the durable outbox store in a temp dir
// and clears it afterwards so one test never sees another's rows.
func resetDesktopBotOutboxForTest(t *testing.T) {
	t.Helper()
	prevDir := desktopBotOutboxDir
	desktopBotOutboxDir = t.TempDir()
	desktopBotOutboxForget()
	t.Cleanup(func() {
		desktopBotOutboxForget()
		desktopBotOutboxDir = prevDir
	})
}

// An admitted turn is terminal: Hub already named its run. A late or stray
// failure report must not downgrade it back to failed, or the page would show
// executed work as undelivered and offer a retry that replays a real run.
func TestDesktopBotOutboxAdmittedIsNotDowngradedByFailure(t *testing.T) {
	resetDesktopBotOutboxForTest(t)
	app := &App{}
	app.desktopBotOutboxSeed("bot_1", "alice", "req-1", "open the site")
	app.desktopBotOutboxMarkAdmitted("bot_1", "req-1")

	// Both page-facing failure entries must leave an admitted row admitted.
	if err := app.MarkDesktopBotOutboxFailed("req-1", "late transport error"); err != nil {
		t.Fatalf("MarkDesktopBotOutboxFailed: %v", err)
	}
	if err := app.DesktopBotSetOutboxStatus("req-1", "failed", "late transport error"); err != nil {
		t.Fatalf("DesktopBotSetOutboxStatus: %v", err)
	}

	status, _, ok := desktopBotOutboxProbe("req-1")
	if !ok || status != "admitted" {
		t.Fatalf("admitted row was downgraded: status=%q ok=%v", status, ok)
	}
	// And a retry must still refuse: the turn already ran.
	if err := app.RelaunchDesktopBotOutbox("req-1"); err == nil {
		t.Fatal("retrying an admitted turn should be refused")
	}
}

// A queued (never admitted) turn that fails stays failed and, when the failure
// is a transport error, is offered for retry — the guard must not block the
// legitimate failure path.
func TestDesktopBotOutboxQueuedFailureStillRetryable(t *testing.T) {
	resetDesktopBotOutboxForTest(t)
	app := &App{}
	app.desktopBotOutboxSeed("bot_1", "alice", "req-2", "open the site")
	app.desktopBotOutboxMarkFailed("req-2", "connection reset by peer", true)

	status, failed, ok := desktopBotOutboxProbe("req-2")
	if !ok || status != "failed" || failed == "" {
		t.Fatalf("queued failure not recorded: status=%q failed=%q ok=%v", status, failed, ok)
	}
	items := app.ListDesktopBotOutbox()
	if len(items) != 1 || !items[0].Retryable {
		t.Fatalf("transport failure should be retryable: %+v", items)
	}
}
