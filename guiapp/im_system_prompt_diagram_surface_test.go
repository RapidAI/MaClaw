package guiapp

import (
	"strings"
	"testing"
)

// TestReplyRendersDiagramsUnknownSurfaceIsFailClosed guards the conservative
// half of the decision: when no owner could be resolved the answer must be
// "no diagrams", never "assume desktop".
//
// This matters because several IM builders default an unresolved owner to
// desktopUserID. If that guess were allowed here, any future caller that skips
// the loop context would silently start requesting diagrams on every surface,
// including ones that render nothing.
func TestReplyRendersDiagramsUnknownSurfaceIsFailClosed(t *testing.T) {
	cases := []struct {
		owner string
		want  bool
	}{
		{owner: desktopUserID, want: true},
		{owner: "file-companion:s1", want: true},
		{owner: "", want: false},
		{owner: "   ", want: false},
		{owner: "scheduled_task", want: false},
		{owner: "lansenger-scheduled:3:bot:4:t1", want: false},
		{owner: "lansenger-group:5:g:6:u", want: false},
	}
	for _, tc := range cases {
		if got := replyRendersDiagrams(tc.owner); got != tc.want {
			t.Fatalf("replyRendersDiagrams(%q) = %v, want %v", tc.owner, got, tc.want)
		}
	}
}

// TestDiagramSurfaceOwnerIDPrefersLoopOwner guards against silently relapsing
// into promptUserID, whose provider (promptRuntimeUserID) FALLS BACK to
// desktopUserID whenever no loop context is present. That fallback would make
// every externally delivered turn look like a desktop turn.
func TestDiagramSurfaceOwnerIDPrefersLoopOwner(t *testing.T) {
	scheduled := "lansenger-scheduled:3:bot:4:t1"
	cases := []struct {
		name    string
		loopCtx *LoopContext
		want    string
	}{
		{name: "loop policy owner wins", loopCtx: &LoopContext{Runtime: RuntimeContext{PolicyOwnerID: scheduled}}, want: scheduled},
		{name: "loop workflow owner used next", loopCtx: &LoopContext{Runtime: RuntimeContext{WorkflowOwnerID: scheduled}}, want: scheduled},
		{name: "loop UserID used last", loopCtx: &LoopContext{UserID: scheduled}, want: scheduled},
		// No context at all must yield empty, so the caller can treat the turn
		// conservatively instead of assuming a desktop surface.
		{name: "unknown owner stays empty", loopCtx: nil, want: ""},
	}
	for _, tc := range cases {
		if got := diagramSurfaceOwnerID(&IMMessageHandler{}, tc.loopCtx); got != tc.want {
			t.Fatalf("%s: owner = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestBuildIMEntrySystemPrompt_DiagramCueFollowsReplySurface covers the FULL
// profile entry, which im_system_prompt_visualization_test.go does not reach:
// that file exercises buildLightIMSystemPrompt only. Both ends of the flag
// must hold here too — the desktop surface gets the cue, and the two turns
// that ride this builder but deliver outside the GUI do not.
func TestBuildIMEntrySystemPrompt_DiagramCueFollowsReplySurface(t *testing.T) {
	cases := []struct {
		name     string
		userID   string
		platform string
		wantCue  bool
	}{
		{name: "desktop assistant tab", userID: desktopUserID, platform: desktopPlatform, wantCue: true},
		{name: "project tab", userID: desktopUserID + ":D:\\proj", platform: desktopPlatform, wantCue: true},
		{name: "file companion", userID: "file-companion:s1", platform: desktopLaunchFileCompanion, wantCue: true},
		{name: "scheduled task", userID: "scheduled_task", platform: "scheduler", wantCue: false},
		{name: "lansenger scheduled task", userID: "lansenger-scheduled:3:bot:4:t1", platform: "scheduler", wantCue: false},
		{name: "lansenger group", userID: "lansenger-group:5:g:6:u", platform: "im", wantCue: false},
	}
	for _, tc := range cases {
		h := &IMMessageHandler{}
		msg := IMUserMessage{
			UserID:   tc.userID,
			Text:     "解释一下这个系统的架构",
			Platform: tc.platform,
		}
		// Mirror production: the loop context carries the inbound message's
		// owner (see runtime_context.go PolicyOwnerID / WorkflowOwnerID).
		// Without it, the prompt builder falls back to desktopUserID and every
		// turn looks like a desktop turn.
		prompt := h.buildIMEntrySystemPrompt(msg, nil, &LoopContext{
			Runtime: RuntimeContext{
				PolicyOwnerID:   tc.userID,
				WorkflowOwnerID: tc.userID,
			},
		}, false, "", "", "", "")
		got := strings.Contains(prompt, "讲解配图")
		if got != tc.wantCue {
			t.Fatalf("%s: diagram cue present=%v, want %v (server=%q)", tc.name, got, tc.wantCue, desktopUserID)
		}
		if tc.wantCue && !strings.Contains(prompt, "```mermaid") {
			t.Fatalf("%s: wants diagrams but teaches no fence", tc.name)
		}
	}
}
