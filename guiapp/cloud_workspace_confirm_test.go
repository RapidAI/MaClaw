package guiapp

import "testing"

func TestResolveFrontendConfirmDeliversAnswer(t *testing.T) {
	ch := make(chan bool, 1)
	frontendConfirmMu.Lock()
	frontendConfirmWaiters["frontend_confirm_test"] = ch
	frontendConfirmMu.Unlock()
	t.Cleanup(func() {
		frontendConfirmMu.Lock()
		delete(frontendConfirmWaiters, "frontend_confirm_test")
		frontendConfirmMu.Unlock()
	})
	if err := resolveFrontendConfirm("frontend_confirm_test", true); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	select {
	case got := <-ch:
		if !got {
			t.Fatal("expected confirmed=true")
		}
	default:
		t.Fatal("waiter was not signalled")
	}
	if err := resolveFrontendConfirm("frontend_confirm_test", false); err == nil {
		t.Fatal("second resolve must fail")
	}
}

func TestAskFrontendConfirmKeepsLocalWithoutWailsContext(t *testing.T) {
	app := &App{}
	if !app.askFrontendConfirm("t", "m", "ok", "cancel", false, true) {
		t.Fatal("unavailable UI should keep local")
	}
	if app.askFrontendConfirm("t", "m", "ok", "cancel", true, false) {
		t.Fatal("unavailable UI should not steal")
	}
}
