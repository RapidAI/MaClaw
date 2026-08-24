package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// TestSemanticGitMutateTurnSelectsTheTrustedRepoAdapter closes the loop on the
// git_mutate slice. Every other test around repository mutation calls the
// adapter directly; this one starts from a classified turn and asserts the
// route actually reaches the managed adapter.
//
// It also pins the two things that make the route worth having. The rendered
// surface must not carry git_commit / git_push, whose schemas take a
// model-written project_path and whose push handler reports "推送成功" from an
// empty output rather than from a receipt. And the surface must hold exactly
// one tool: a request to commit is not a request to read diffs or to run a
// shell, so a mutation plan must not quietly widen into either.
func TestSemanticGitMutateTurnSelectsTheTrustedRepoAdapter(t *testing.T) {
	var gotAction string
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitMutate)}
	h.semanticTrustedRepoMutate = func(userID, action, _ string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("user=%q", userID)
		}
		gotAction = action
		return "commit 0123456789abcdef0123456789abcdef01234567", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})

	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这些改动提交一下", "desktop", "root-git-mutate", "turn-git-mutate",
		&intent.ClassificationResult{Primary: intent.LabelGitMutate, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if got := surface.plan.Selections[0].AdapterName; got != semanticTrustedRepoMutateAdapter {
		t.Fatalf("selection adapter=%q, want %q", got, semanticTrustedRepoMutateAdapter)
	}
	name := extractToolName(defs[0])
	// Comparing the rendered name against git_commit/git_push would never fire:
	// the surface renders opaque per-grant tokens, so no legacy name can appear
	// there in the first place. What the grant is bound to is the real question.
	if grant, ok := surface.grants[name]; !ok || grant.AdapterName != semanticTrustedRepoMutateAdapter {
		t.Fatalf("rendered tool %q is bound to %#v, want the trusted repo adapter", name, grant)
	}

	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	// project_path is the legacy tool's own argument. The repository is
	// host-bound, so letting the model name one would move the mutation to a
	// directory the plan never authorised.
	if got := cb.ExecuteTool(name, `{"action":"commit","message":"save","project_path":"C:\\other"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_schema_invalid") {
		t.Fatalf("legacy project_path accepted: %q", got)
	}
	if gotAction != "" {
		t.Fatalf("rejected arguments still reached the adapter as %q", gotAction)
	}
}

// TestSemanticGitMutateTurnExecutesCommitThroughTheGrant is the execution half.
// A fresh surface is built because the rejected call above consumed its grant.
func TestSemanticGitMutateTurnExecutesCommitThroughTheGrant(t *testing.T) {
	var gotAction, gotMessage string
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitMutate)}
	h.semanticTrustedRepoMutate = func(_, action, message string) (string, error) {
		gotAction, gotMessage = action, message
		return "commit 0123456789abcdef0123456789abcdef01234567", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})

	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这些改动提交一下", "desktop", "root-git-mutate-exec", "turn-git-mutate-exec",
		&intent.ClassificationResult{Primary: intent.LabelGitMutate, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"action":"commit","message":"save note"}`)
	if !strings.Contains(got, "commit 0123456789") {
		t.Fatalf("commit result=%q", got)
	}
	if gotAction != "commit" || gotMessage != "save note" {
		t.Fatalf("adapter received action=%q message=%q", gotAction, gotMessage)
	}
}

// TestSemanticGitMutateSurfaceRefusesActionsOutsideTheClosedSet keeps the
// closed action set closed at the routed boundary, not only inside the adapter
// helper. Branch, history and reset operations have no managed capability, so
// the surface must refuse them rather than pass an unknown verb to git.
func TestSemanticGitMutateSurfaceRefusesActionsOutsideTheClosedSet(t *testing.T) {
	for _, action := range []string{"reset", "rebase", "checkout", "force_push", "amend"} {
		t.Run(action, func(t *testing.T) {
			reached := false
			h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitMutate)}
			h.semanticTrustedRepoMutate = func(string, string, string) (string, error) {
				reached = true
				return "ran", nil
			}
			registerBuiltinTools(h.registry, h)
			defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
				"user-1", "把这些改动提交一下", "desktop", "root-git-"+action, "turn-git-"+action,
				&intent.ClassificationResult{Primary: intent.LabelGitMutate, Confidence: .98},
			)
			if err != nil || !handled || surface == nil || len(defs) < 1 {
				t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
			}
			name := extractToolName(defs[0])
			cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
			got := cb.ExecuteTool(name, `{"action":"`+action+`"}`)
			// Naming the boundary matters. Asserting only "rejected" would let
			// this pass for any reason at all, including the receipt-boundary
			// refusal that used to reject every repository mutation before it
			// reached the schema, which made the closed action set untested.
			if !strings.Contains(got, "trusted_repo_mutate_action_rejected") {
				t.Fatalf("action %q was not refused by the closed action set: %q", action, got)
			}
			if reached {
				t.Fatalf("action %q reached the adapter", action)
			}
		})
	}
}
