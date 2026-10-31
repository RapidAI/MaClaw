package guiapp

import (
	"strings"
	"testing"
)

func TestImmediateCommandRunsInsideRequestActivityClock(t *testing.T) {
	started := make(chan struct{}, 1)
	requestActivityClockObserver.Store(func(func(string)) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() {
		requestActivityClockObserver.Store((func(func(string)))(nil))
	})

	h := &IMMessageHandler{}
	resp := h.HandleIMMessageWithProgress(IMUserMessage{
		UserID:   "clock-test-user",
		Platform: "desktop",
		Text:     "/coding-help",
		Lang:     "zh",
	}, func(string) {})
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("help command returned no text: %+v", resp)
	}
	select {
	case <-started:
	default:
		t.Fatal("immediate command returned before the request activity clock started")
	}
}
