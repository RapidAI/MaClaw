package main

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func parseWashedArgs(t *testing.T, washed string) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal([]byte(washed), &out); err != nil {
		t.Fatalf("washed args are not JSON: %v", err)
	}
	return out
}

// A decimal-string timeout and the legacy "timeout" alias must be normalized
// before canonical validation can burn the one-shot shell grant.
func TestSemanticShellInvocationArgsWashesTimeoutShapes(t *testing.T) {
	got := parseWashedArgs(t, semanticShellInvocationArgs(`{"command":"python make_ppt.py","timeout_seconds":"60"}`))
	if got["timeout_seconds"] != float64(60) {
		t.Fatalf("string timeout not normalized: %#v", got)
	}
	got = parseWashedArgs(t, semanticShellInvocationArgs(`{"command":"ls","timeout":120}`))
	if got["timeout_seconds"] != float64(120) {
		t.Fatalf("timeout alias not folded: %#v", got)
	}
	if _, ok := got["timeout"]; ok {
		t.Fatalf("alias key must be gone: %#v", got)
	}
	// The canonical key wins over the alias.
	got = parseWashedArgs(t, semanticShellInvocationArgs(`{"command":"ls","timeout":5,"timeout_seconds":30}`))
	if got["timeout_seconds"] != float64(30) {
		t.Fatalf("canonical timeout_seconds must win: %#v", got)
	}
	// Unknown keys with real values (workdir) pass through untouched.
	mixed := `{"command":"ls","workdir":"/tmp"}`
	if got := semanticShellInvocationArgs(mixed); got != mixed {
		t.Fatalf("unknown key must pass through: %s", got)
	}
}

// Legacy file_path/text aliases fold into path/content; a real conflict
// (both path and file_path) passes through so admission fails closed.
func TestSemanticFileWriteInvocationArgsFoldsLegacyAliases(t *testing.T) {
	got := parseWashedArgs(t, semanticFileWriteInvocationArgs(`{"file_path":"a.txt","text":"hello"}`))
	if got["path"] != "a.txt" || got["content"] != "hello" {
		t.Fatalf("aliases not folded: %#v", got)
	}
	conflict := `{"path":"a.txt","file_path":"b.txt","content":"x"}`
	if got := semanticFileWriteInvocationArgs(conflict); got != conflict {
		t.Fatalf("conflict must pass through unchanged: %s", got)
	}
	got = parseWashedArgs(t, semanticFileWriteInvocationArgs(`{"path":"a.txt","content":"x","mode":null}`))
	if _, ok := got["mode"]; ok {
		t.Fatalf("null mode must be dropped: %#v", got)
	}
}

// A single URL-valued alias field is promoted to url; multiple candidates
// pass through so admission fails closed.
func TestSemanticAcquireRemoteInvocationArgsPromotesSingleURLAlias(t *testing.T) {
	got := parseWashedArgs(t, semanticAcquireRemoteInvocationArgs(`{"link":"https://example.com/cat.jpg"}`))
	if got["url"] != "https://example.com/cat.jpg" {
		t.Fatalf("url alias not promoted: %#v", got)
	}
	if _, ok := got["link"]; ok {
		t.Fatalf("alias key must be gone: %#v", got)
	}
	got = parseWashedArgs(t, semanticAcquireRemoteInvocationArgs(`{"url":"https://example.com/a.jpg","path":null}`))
	if _, ok := got["path"]; ok {
		t.Fatalf("null companion must be dropped: %#v", got)
	}
	mixed := `{"link":"https://example.com/a.jpg","mirror":"https://example.com/b.jpg"}`
	if got := semanticAcquireRemoteInvocationArgs(mixed); got != mixed {
		t.Fatalf("multiple candidates must pass through unchanged: %s", got)
	}
	notURL := `{"filename":"cat.jpg"}`
	if got := semanticAcquireRemoteInvocationArgs(notURL); got != notURL {
		t.Fatalf("non-URL value must pass through unchanged: %s", got)
	}
}

// The delivery adapter takes no input, so the empty envelope slip
// {"arguments": "{}"} washes to {} and the one-shot send_file grant survives.
// Contentful arguments (forged artifact_id/path, non-empty envelope) pass
// through unchanged so admission rejects them as unknown fields.
func TestSemanticDeliveryInvocationArgsWashesOnlyEmptyEnvelope(t *testing.T) {
	if got := semanticDeliveryInvocationArgs(`{"arguments": "{}"}`); got != "{}" {
		t.Fatalf("empty envelope slip must wash to {}: %s", got)
	}
	forged := `{"artifact_id":"forged","path":"C:/Windows/win.ini"}`
	if got := semanticDeliveryInvocationArgs(forged); got != forged {
		t.Fatalf("forged steering fields must pass through for rejection: %s", got)
	}
	contentful := `{"arguments": "{\"path\": \"/tmp/x.pdf\"}"}`
	if got := semanticDeliveryInvocationArgs(contentful); got != contentful {
		t.Fatalf("non-empty envelope must pass through for rejection: %s", got)
	}
	decoration := `{"path": null}`
	if got := semanticDeliveryInvocationArgs(decoration); got != decoration {
		t.Fatalf("non-envelope keys must pass through for rejection: %s", got)
	}
	garbage := `not json`
	if got := semanticDeliveryInvocationArgs(garbage); got != garbage {
		t.Fatalf("non-object garbage must pass through: %s", got)
	}
}

// Integration: the delivery selection's admission path itself must accept the
// envelope slip, so the one-shot send_file grant survives to actually deliver.
func TestSemanticDeliveryAdmissionSurvivesArgumentsEnvelope(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	var delivery *tool.PlannedSelection
	for i, selection := range cb.semanticSurface.plan.Selections {
		if selection.AdapterName == "semantic_deliver_current_file" {
			delivery = &cb.semanticSurface.plan.Selections[i]
			break
		}
	}
	if delivery == nil {
		t.Fatal("fixture plan must include a current-channel file delivery selection")
	}
	if _, err := cb.semanticCanonicalArguments(*delivery, `{"arguments": "{}"}`); err != nil {
		t.Fatalf("envelope slip must not burn the delivery grant: %v", err)
	}
}

// Destination-shaped keys are decoration (the host binds the destination) and
// must not burn the acquire grant; a second URL-shaped value stays ambiguous
// and fails closed; unknown keys fail closed.
func TestSemanticAcquireRemoteInvocationArgsDropsDestinationDecoration(t *testing.T) {
	got := parseWashedArgs(t, semanticAcquireRemoteInvocationArgs(`{"url":"https://example.com/cat.jpg","save_path":"F:/tmp/cat.jpg"}`))
	if got["url"] != "https://example.com/cat.jpg" {
		t.Fatalf("url must survive: %#v", got)
	}
	if _, ok := got["save_path"]; ok {
		t.Fatalf("save_path decoration must be dropped: %#v", got)
	}
	got = parseWashedArgs(t, semanticAcquireRemoteInvocationArgs(`{"url":"https://example.com/cat.jpg","filename":"cat.jpg","output":null}`))
	if _, ok := got["filename"]; ok {
		t.Fatalf("filename decoration must be dropped: %#v", got)
	}
	// A second URL-shaped value is ambiguous: pass through for rejection.
	mirror := `{"url":"https://example.com/a.jpg","mirror":"https://example.com/b.jpg"}`
	if got := semanticAcquireRemoteInvocationArgs(mirror); got != mirror {
		t.Fatalf("URL-valued companion must pass through unchanged: %s", got)
	}
	// Unknown keys fail closed.
	unknown := `{"url":"https://example.com/a.jpg","format":"jpg"}`
	if got := semanticAcquireRemoteInvocationArgs(unknown); got != unknown {
		t.Fatalf("unknown key must pass through unchanged: %s", got)
	}
}
