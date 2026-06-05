package browser

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestNotifyReplayCompleteDoesNotAttachScreenshot(t *testing.T) {
	statusC := make(chan agent.StatusEvent, 1)
	state := &TaskState{Checkpoints: []Checkpoint{{ScreenshotB64: "legacy-screenshot"}}}

	notifyReplayComplete(statusC, "loop-1", "flow", time.Second, true, 1, 1, "", state)

	select {
	case ev := <-statusC:
		if ev.Extra != nil {
			t.Fatalf("replay completion attached extra metadata = %#v, want nil", ev.Extra)
		}
	default:
		t.Fatal("missing replay completion event")
	}
}
