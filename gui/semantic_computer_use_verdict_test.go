package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func computerUseAdapterResult(t *testing.T, handlerErr error) string {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedComputerUse = func(string, string) (string, error) {
		return "", handlerErr
	}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedComputerUse(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"action":"observe"}`),
	})
	if !strings.Contains(got, handlerErr.Error()) {
		t.Fatalf("adapter did not reach the handler failure %q, got %q", handlerErr, got)
	}
	return got
}

// The runtime decides whether it saw the screen and whether a completion was
// accepted, and it says so with a flag. Both used to be flattened into a
// string, which left the managed surface reading prose to find out -- and
// finding nothing, so every refusal arrived as a success.
func TestComputerUseRefusalsAreDefiniteFailures(t *testing.T) {
	for _, name := range []string{
		"trusted_computer_use_observe_failed",
		"trusted_computer_use_done_refused",
	} {
		t.Run(name, func(t *testing.T) {
			got := computerUseAdapterResult(t, fmt.Errorf("%s", name))
			if !strings.HasPrefix(got, "[system rejected]") {
				t.Fatalf("%s = %q, want a definite rejection", name, got)
			}
			// Nothing was seen and nothing was completed, so claiming the
			// outcome might hold would be as wrong as claiming success.
			if strings.Contains(got, "[system unknown]") {
				t.Fatalf("%s claimed uncertainty about an action that did nothing: %q", name, got)
			}
		})
	}
}

// The refusal names above can only reach the model if the runtime's own
// verdict survives the helper that produces the text. These pin it at that
// source: the two refusals the legacy surface can describe only in prose must
// come back marked as refusals, and an accepted completion must not.
func TestComputerUseDoneVerdictSurvivesAsAFlag(t *testing.T) {
	t.Run("audit rejection", func(t *testing.T) {
		resetComputerUseRuntimeForTest(t)
		resetComputerUseSessionForTest(t)
		markComputerUseSessionActive()
		setComputerUseOwner("sk-verdict-reject")
		beginComputerUseTask("sk-verdict-reject", "req-1", "保存文档", []string{"已保存"})
		seedComputerUseObserve(t, "草稿未保存", "")
		text, ok := cuDoneResult("done")
		if ok {
			t.Fatalf("a completion the audit rejected reported itself accepted: %q", text)
		}
	})
	t.Run("long horizon claim", func(t *testing.T) {
		resetComputerUseRuntimeForTest(t)
		resetComputerUseSessionForTest(t)
		setComputerUseOwner("hz-verdict")
		beginComputerUseTask("hz-verdict", "req-hz", "open notepad", []string{"window visible"})
		seedComputerUseObserve(t, "notepad", "")
		setHorizonComputerUseClaimOnly("hz-verdict", true)
		text, ok := cuDoneResult("opened")
		if ok {
			t.Fatalf("a claim that completes nothing reported itself accepted: %q", text)
		}
	})
	t.Run("accepted", func(t *testing.T) {
		resetComputerUseRuntimeForTest(t)
		resetComputerUseSessionForTest(t)
		markComputerUseSessionActive()
		setComputerUseOwner("sk-verdict-pass")
		beginComputerUseTask("sk-verdict-pass", "req-1", "open notepad", nil)
		text, ok := cuDoneResult("done")
		if !ok {
			t.Fatalf("an accepted completion reported itself refused: %q", text)
		}
	})
}

// A runtime that is switched off is a different fact again: it is reported as
// unknown today, which is over-cautious rather than unsafe. Pinning it keeps
// the refusal names above from quietly inheriting that treatment.
func TestComputerUseUnavailableRuntimeStillReportsUnknown(t *testing.T) {
	got := computerUseAdapterResult(t, fmt.Errorf("trusted_computer_use_runtime_unavailable"))
	if !strings.HasPrefix(got, "[system unknown]") {
		t.Fatalf("unavailable runtime = %q, want the existing unknown treatment", got)
	}
}
