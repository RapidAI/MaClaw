package guiapp

import (
	"encoding/json"
	"strings"
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

// Legacy exec-shaped ssh calls (action/session_id/host/password from the
// unpublished full ssh tool, still visible in conversation history) must wash
// to the bare {command} the closed schema binds; connect-shaped calls get a
// dedicated already-bound rejection instead of burning the grant on a schema
// error. Keys that would change execution semantics pass through for
// canonicalization to reject.
func TestSemanticSSHInvocationArgsWashesLegacyShapes(t *testing.T) {
	washed, err := semanticSSHInvocationArgs(`{"action":"exec","command":"df -h","session_id":"ssh_root@host:22_1"}`, false)
	if err != nil {
		t.Fatalf("exec-shaped wash must not error: %v", err)
	}
	got := parseWashedArgs(t, washed)
	if got["command"] != "df -h" {
		t.Fatalf("command must survive: %#v", got)
	}
	if _, ok := got["session_id"]; ok {
		t.Fatalf("host-bound session_id must be dropped: %#v", got)
	}
	if _, ok := got["action"]; ok {
		t.Fatalf("host-bound action must be dropped: %#v", got)
	}

	washed, err = semanticSSHInvocationArgs(`{"action":"connect","host":"h","user":"root","password":"s3cret","label":"驱网"}`, false)
	if err == nil || !strings.Contains(err.Error(), "trusted_ssh_session_already_bound") {
		t.Fatalf("exec-mode connect shape must be rejected as already bound, got washed=%q err=%v", washed, err)
	}

	// Bare command passes through untouched.
	bare := `{"command":"df -h"}`
	if washed, err = semanticSSHInvocationArgs(bare, false); err != nil || washed != bare {
		t.Fatalf("bare command must pass through: washed=%q err=%v", washed, err)
	}
	// Execution-affecting keys are not decoration: they survive the wash so
	// canonicalization rejects them instead of silently ignoring them.
	mixed := `{"command":"df -h","wait_seconds":120}`
	if washed, err = semanticSSHInvocationArgs(mixed, false); err != nil || washed != mixed {
		t.Fatalf("execution-affecting key must pass through: washed=%q err=%v", washed, err)
	}
	// Garbage passes through.
	garbage := `not json`
	if washed, err = semanticSSHInvocationArgs(garbage, false); err != nil || washed != garbage {
		t.Fatalf("garbage must pass through: washed=%q err=%v", washed, err)
	}
}

// Connect mode admits the connection fields itself; only pure legacy
// decoration (action/session_id) washes away, because the model's natural
// call shape — copied from the legacy tool in conversation history — is
// {"action":"connect","host":…,"password":…}. Rejecting it on the action
// field would burn an iteration for nothing.
func TestSemanticSSHInvocationArgsConnectModeAbsorbsAction(t *testing.T) {
	washed, err := semanticSSHInvocationArgs(`{"action":"connect","host":"h","user":"root","password":"s3cret","label":"驱网","session_id":"ssh_x_1"}`, true)
	if err != nil {
		t.Fatalf("connect-shaped wash must not error: %v", err)
	}
	got := parseWashedArgs(t, washed)
	if got["host"] != "h" || got["user"] != "root" || got["password"] != "s3cret" || got["label"] != "驱网" {
		t.Fatalf("connect fields must survive: %#v", got)
	}
	if _, ok := got["action"]; ok {
		t.Fatalf("legacy action must be dropped in connect mode: %#v", got)
	}
	if _, ok := got["session_id"]; ok {
		t.Fatalf("legacy session_id must be dropped in connect mode: %#v", got)
	}
	// Clean connect args pass through untouched.
	bare := `{"host":"h","user":"root"}`
	if washed, err = semanticSSHInvocationArgs(bare, true); err != nil || washed != bare {
		t.Fatalf("bare connect args must pass through: washed=%q err=%v", washed, err)
	}
	// Unknown / execution-affecting keys stay for canonicalization to reject.
	mixed := `{"host":"h","user":"root","wait_seconds":30}`
	if washed, err = semanticSSHInvocationArgs(mixed, true); err != nil || washed != mixed {
		t.Fatalf("unknown key must pass through in connect mode: washed=%q err=%v", washed, err)
	}
	// The optional post-connect command is a declared connect-schema field and
	// must survive the wash so the adapter can run it after connecting.
	withCommand := `{"host":"h","user":"root","password":"x","command":"uptime"}`
	washed, err = semanticSSHInvocationArgs(withCommand, true)
	if err != nil {
		t.Fatalf("connect+command wash must not error: %v", err)
	}
	got = parseWashedArgs(t, washed)
	if got["command"] != "uptime" || got["host"] != "h" {
		t.Fatalf("connect+command must survive the wash: %#v", got)
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
