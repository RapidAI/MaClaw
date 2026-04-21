package intent

import (
	"strings"
	"testing"
)

func TestBuildIntentTreeText(t *testing.T) {
	defs := []IntentDefinition{
		{Label: LabelCoding, Domain: "Coding", TreeText: "create new software"},
		{Label: LabelBugFix, Domain: "Coding", TreeText: "fix bugs in existing code"},
		{Label: LabelSSH, Domain: "Remote", TreeText: "connect to remote servers"},
	}

	tree := BuildIntentTreeText(defs)

	if !strings.Contains(tree, "── Coding ──") {
		t.Error("expected Coding domain header")
	}
	if !strings.Contains(tree, "── Remote ──") {
		t.Error("expected Remote domain header")
	}
	if !strings.Contains(tree, "coding: create new software") {
		t.Error("expected coding entry")
	}
	if !strings.Contains(tree, "ssh: connect to remote servers") {
		t.Error("expected ssh entry")
	}
	// Coding domain has 2 intents → should auto-generate disambiguation note.
	if !strings.Contains(tree, "mutually exclusive") {
		t.Error("expected auto-generated disambiguation note for multi-intent domain")
	}
	if !strings.Contains(tree, "coding/bug_fix") {
		t.Error("expected disambiguation note to list sibling labels")
	}
	// Remote domain has 1 intent → no disambiguation note.
	if strings.Contains(tree, "ssh)") && strings.Contains(tree, "mutually exclusive") {
		// This is a weak check — just verify the note isn't on a single-intent domain.
		// The note should only appear after Coding, not after Remote.
	}
}

func TestBuildIntentTreeText_SingleIntentDomain(t *testing.T) {
	defs := []IntentDefinition{
		{Label: LabelSSH, Domain: "Remote", TreeText: "connect to servers"},
	}

	tree := BuildIntentTreeText(defs)

	if strings.Contains(tree, "mutually exclusive") {
		t.Error("single-intent domain should NOT have disambiguation note")
	}
}

func TestParseTreeResponse_WithThinkTags(t *testing.T) {
	response := `<think>
The user wants to fix a bug. Domain: Coding. Intent: bug_fix.
</think>
{"top": [{"skill": "bug_fix", "score": 0.90}, {"skill": "coding", "score": 0.30}, {"skill": "maintenance", "score": 0.20}]}`

	candidates := ParseTreeResponse(response)

	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	if candidates[0].Label != LabelBugFix {
		t.Errorf("expected top=bug_fix, got %s", candidates[0].Label)
	}
	if candidates[0].Score != 0.90 {
		t.Errorf("expected score 0.90, got %.2f", candidates[0].Score)
	}
}

func TestParseTreeResponse_PlainJSON(t *testing.T) {
	response := `{"top": [{"skill": "ssh", "score": 0.85}]}`

	candidates := ParseTreeResponse(response)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Label != LabelSSH {
		t.Errorf("expected ssh, got %s", candidates[0].Label)
	}
}

func TestParseTreeResponse_InvalidLabel(t *testing.T) {
	response := `{"top": [{"skill": "nonexistent_intent", "score": 0.90}]}`

	candidates := ParseTreeResponse(response)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for invalid label, got %d", len(candidates))
	}
}

func TestParseTreeResponse_ScoreClamping(t *testing.T) {
	response := `{"top": [{"skill": "coding", "score": 1.5}, {"skill": "ssh", "score": -0.3}]}`

	candidates := ParseTreeResponse(response)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Score != 1.0 {
		t.Errorf("expected clamped score 1.0, got %.2f", candidates[0].Score)
	}
	if candidates[1].Score != 0.0 {
		t.Errorf("expected clamped score 0.0, got %.2f", candidates[1].Score)
	}
}

func TestParseTreeResponse_Malformed(t *testing.T) {
	candidates := ParseTreeResponse("this is not json at all")
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for malformed input, got %d", len(candidates))
	}
}

func TestBuildTreePrompt(t *testing.T) {
	treeText := "── Coding ──\n  coding: create software"
	prompt := BuildTreePrompt(treeText, "开发一个游戏")

	if !strings.Contains(prompt, treeText) {
		t.Error("prompt should contain tree text")
	}
	if !strings.Contains(prompt, "开发一个游戏") {
		t.Error("prompt should contain user message")
	}
	if !strings.Contains(prompt, "<think>") {
		t.Error("prompt should instruct chain-of-thought reasoning")
	}
}
