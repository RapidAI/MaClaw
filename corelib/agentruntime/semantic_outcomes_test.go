package agentruntime

import (
	"strings"
	"testing"
)

func TestSelectionOutcomeMarkers(t *testing.T) {
	if !SelectionFailed("[system rejected] stale_surface") {
		t.Fatal("rejection marker should be treated as a failure")
	}
	if !SelectionFailed("Error: tool unavailable") {
		t.Fatal("error marker should be treated as a failure")
	}
	if SelectionFailed("[system unknown] remote receipt unavailable") {
		t.Fatal("unknown outcome must not be retried as a hard failure")
	}
	if !SelectionOutcomeUnknown("  [system unknown] remote receipt unavailable") {
		t.Fatal("unknown marker should be recognized")
	}
	if SelectionOutcomeUnknown("[system rejected] stale_surface") {
		t.Fatal("rejection marker is not an unknown outcome")
	}
}

func TestGrantRejectMessageStableGuidance(t *testing.T) {
	if got := GrantRejectMessage(""); got != "[system rejected] selection_not_authorized. If a lookup already returned evidence, answer from that evidence. Do not ask the user to re-authorize tools." {
		t.Fatalf("default rejection guidance drifted: %q", got)
	}
	got := GrantRejectMessage("stale_surface")
	if got == "" || !strings.HasPrefix(got, "[system rejected]") ||
		!strings.Contains(got, "Re-issue this call as its own response") {
		t.Fatalf("stale-surface guidance drifted: %q", got)
	}
}

func TestPetitionGrantedMessageIsSharedAndTrimmed(t *testing.T) {
	want := "工具 web_search 已由主机授权并加入当前工具面，请立即重新发起对 web_search 的调用（参数不变）。"
	if got := PetitionGrantedMessage(" web_search "); got != want {
		t.Fatalf("petition guidance=%q; want %q", got, want)
	}
	if got := PetitionGrantedMessage(" "); got != "" {
		t.Fatalf("empty petition name=%q; want empty", got)
	}
}

func TestAdvanceAfterSuccessPreservesResultAndBlocksRetry(t *testing.T) {
	got := AdvanceAfterSuccess(" artifact published ")
	if !strings.HasPrefix(got, "artifact published\n") || !strings.Contains(got, "do not retry this grant") {
		t.Fatalf("advance guidance=%q", got)
	}
	if got := AdvanceAfterSuccess(" "); !strings.HasPrefix(got, "The previous step succeeded.") {
		t.Fatalf("empty result guidance=%q", got)
	}
}
