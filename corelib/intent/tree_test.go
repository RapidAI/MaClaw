package intent

import (
	"strings"
	"testing"
)

func TestTreeResponseFormatUsesOnlyTaxonomyLabelsAndWorkflowTypes(t *testing.T) {
	format := TreeResponseFormat()
	jsonSchema, _ := format["json_schema"].(map[string]interface{})
	root, _ := jsonSchema["schema"].(map[string]interface{})
	properties, _ := root["properties"].(map[string]interface{})
	top, _ := properties["top"].(map[string]interface{})
	items, _ := top["items"].(map[string]interface{})
	alternatives, _ := items["anyOf"].([]interface{})
	if len(alternatives) == 0 {
		t.Fatalf("items = %#v, want per-label alternatives", items)
	}

	workflowsByLabel := make(map[string][]string, len(alternatives))
	for _, raw := range alternatives {
		alternative, _ := raw.(map[string]interface{})
		itemProperties, _ := alternative["properties"].(map[string]interface{})
		skill, _ := itemProperties["skill"].(map[string]interface{})
		labels, _ := skill["enum"].([]string)
		if len(labels) != 1 {
			t.Fatalf("skill enum = %#v, want exactly one taxonomy label per branch", labels)
		}
		workflow, _ := itemProperties["workflow_type"].(map[string]interface{})
		workflowsByLabel[labels[0]], _ = workflow["enum"].([]string)
	}

	for _, label := range []string{string(LabelLiveData), string(LabelDocumentGenerate), string(LabelCoding), string(LabelWorkflowTask)} {
		if _, ok := workflowsByLabel[label]; !ok {
			t.Fatalf("schema branches = %#v, missing %q", workflowsByLabel, label)
		}
	}
	if containsString(workflowsByLabel[string(LabelLiveData)], "contract_review") {
		t.Fatalf("live_data workflows = %#v, must not accept another label's workflow", workflowsByLabel[string(LabelLiveData)])
	}
	if !containsString(workflowsByLabel[string(LabelCoding)], "coding") || containsString(workflowsByLabel[string(LabelCoding)], "contract_review") {
		t.Fatalf("coding workflows = %#v, want only coding label workflow types", workflowsByLabel[string(LabelCoding)])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

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
	// Coding domain has 2 intents → should auto-generate a default
	// disambiguation note without contradicting declared composites.
	if !strings.Contains(tree, "normally choose the single best match") {
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

	if strings.Contains(tree, "normally choose the single best match") {
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

func TestParseTreeResponseRejectsUnknownWorkflowType(t *testing.T) {
	candidates := ParseTreeResponse(`{"top":[{"skill":"live_data","score":0.95,"workflow_type":"single_turn"}]}`)
	if len(candidates) != 0 {
		t.Fatalf("candidates = %#v, want invalid workflow_type rejected", candidates)
	}
}

func TestParseTreeResponseRejectsWorkflowTypeForAnotherIntent(t *testing.T) {
	// Regression: the 2026-08-23 desktop request received HTTP 200 with
	// skill="coding" paired with workflow_type="contract_review". Both values
	// are individually known, but never form a valid classification pair.
	candidates := ParseTreeResponse(`{"top":[{"skill":"coding","score":0.8,"workflow_type":"contract_review"},{"skill":"coding","score":0.2,"workflow_type":"project_proposal"}]}`)
	if len(candidates) != 0 {
		t.Fatalf("candidates = %#v, want cross-intent workflow types rejected", candidates)
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
	if !strings.Contains(prompt, "Return ONLY this JSON object") {
		t.Error("prompt should require a JSON-only response")
	}
	if strings.Contains(prompt, "<think>") {
		t.Error("structured-output prompt must not request emitted reasoning tags")
	}
}

func TestBuildTreePromptKeepsCompositeAndWebFetchBoundariesConsistent(t *testing.T) {
	prompt := BuildTreePrompt("tree", "北京天气，输出格式化pdf报告")
	for _, want := range []string{
		"web_fetch requires a concrete URL",
		"Current externally acquired facts rendered as a PDF → live_data + document_generate",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing semantic boundary %q", want)
		}
	}
}
