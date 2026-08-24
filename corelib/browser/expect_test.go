package browser

import (
	"strings"
	"testing"
)

func TestParseExpect(t *testing.T) {
	got := ParseExpect("url_contains:login")
	if got.Type != "url_contains" || got.Pattern != "login" {
		t.Fatalf("got=%#v", got)
	}
	if ParseExpect("").Type != "" {
		t.Fatal("empty expect should be zero")
	}
}

func TestVerifyExpectURLContains(t *testing.T) {
	s := &BrowserAgentSession{}
	obs := &BrowserObservation{Snapshot: BrowserSnapshot{URL: "https://example.com/login"}}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "url_contains", Pattern: "LOGIN"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "url_contains", Pattern: "missing"}); err == nil {
		t.Fatal("expected failure")
	}
}

func TestApplyExpectMarksFailure(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/home", Refs: []BrowserElementRef{{Ref: "@e1", Name: "Home"}}},
		},
	}
	result := &BrowserActionResult{SnapshotID: "snap-1", Status: "ok", Display: "clicked", Data: map[string]interface{}{
		"refs":   []CompactElementRef{{Ref: "@e1", Name: "Home"}},
		"target": "@e1",
	}}
	got := s.applyExpect(result, ExpectSpec{Type: "url_contains", Pattern: "cart"})
	if got.Status != "expect_failed" {
		t.Fatalf("status=%q", got.Status)
	}
	if _, ok := got.Data["delta"]; !ok {
		t.Fatal("missing delta")
	}
	if _, ok := got.Data["refs"]; ok {
		t.Fatal("expect failure should not dump refs")
	}
	if got.Data["target"] != "@e1" {
		t.Fatalf("target=%v", got.Data["target"])
	}
}

func TestVerifyExpectRefAppearsExactRefNotSubstring(t *testing.T) {
	s := &BrowserAgentSession{}
	obs := &BrowserObservation{Snapshot: BrowserSnapshot{
		Refs: []BrowserElementRef{
			{Ref: "@e12", Name: "Share e1 link"},
			{Ref: "@e2", Name: "Buy"},
		},
	}}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "ref_appears", Pattern: "@e1"}); err == nil {
		t.Fatal("expected @e1 not to match as a substring of names or @e12")
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "ref_appears", Pattern: "@e2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "ref_appears", Pattern: "Buy"}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyExpectMissingSnapshotFailsClosed(t *testing.T) {
	s := &BrowserAgentSession{snapshots: map[string]*BrowserSnapshot{}}
	result := &BrowserActionResult{SnapshotID: "missing", Status: "ok", Display: "clicked"}
	got := s.applyExpect(result, ExpectSpec{Type: "url_contains", Pattern: "cart"})
	if got.Status != "expect_failed" {
		t.Fatalf("status=%q", got.Status)
	}
	if _, ok := got.Data["delta"]; !ok {
		t.Fatal("missing delta")
	}
	if s.lastExpect.Type != "" {
		t.Fatalf("missing snapshot should not record last_expect: %#v", s.lastExpect)
	}
}

func TestApplyExpectSuccessClearsUnchanged(t *testing.T) {
	s := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/cart"},
		},
	}
	result := &BrowserActionResult{
		SnapshotID: "snap-1",
		Status:     "unchanged",
		Display:    "clicked @e1" + unchangedDisplaySuffix,
		Data:       map[string]interface{}{"delta": map[string]interface{}{"error": "page did not change"}},
	}
	got := s.applyExpect(result, ExpectSpec{Type: "url_contains", Pattern: "cart"})
	if got.Status != "ok" {
		t.Fatalf("status=%q", got.Status)
	}
	if strings.Contains(got.Display, "page did not change") {
		t.Fatalf("display still unchanged: %q", got.Display)
	}
	if _, ok := got.Data["delta"]; ok {
		t.Fatal("delta should be cleared when expect succeeds")
	}
}

func TestPlaybookMentionsObserveLoop(t *testing.T) {
	p := Playbook()
	for _, marker := range []string{"observe", "session_id", "hover", "expect=", "computer_*", "captcha_widget"} {
		if !strings.Contains(p, marker) {
			t.Fatalf("playbook missing %q", marker)
		}
	}
	if strings.Contains(p, "login wall, or MFA, stop") {
		t.Fatal("playbook must not auto-stop on login_wall/MFA")
	}
}

func TestVerifyExpectCheckedAndNoFlag(t *testing.T) {
	s := &BrowserAgentSession{}
	obs := &BrowserObservation{Snapshot: BrowserSnapshot{
		PageFlags: BrowserPageFlags{MFA: true},
		Refs:      []BrowserElementRef{{Ref: "@e1", Name: "Agree", Checked: true}},
	}}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "checked", Pattern: "@e1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "no_flag", Pattern: "captcha_widget"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "no_flag", Pattern: "mfa"}); err == nil {
		t.Fatal("mfa flag should fail no_flag")
	}
	if validExpect(ExpectSpec{Type: "checked"}) || validExpect(ExpectSpec{Type: "no_flag"}) {
		t.Fatal("checked/no_flag without a pattern are trivial")
	}
}

func TestVerifyExpectURLMatchesAndRefGone(t *testing.T) {
	s := &BrowserAgentSession{}
	obs := &BrowserObservation{Snapshot: BrowserSnapshot{
		URL:  "https://example.com/done/42",
		Refs: []BrowserElementRef{{Ref: "@e2", Name: "Next"}},
	}}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "url_matches", Pattern: `/done/\d+$`}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "ref_gone", Pattern: "@e1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "ref_gone", Pattern: "@e2"}); err == nil {
		t.Fatal("present ref should fail ref_gone")
	}
}

func TestValidExpectRejectsUnknownAndTrivialURLMatch(t *testing.T) {
	if validExpect(ExpectSpec{Type: "foo", Pattern: "abcdef"}) {
		t.Fatal("unknown expect type must not satisfy goal-class")
	}
	if validExpect(ExpectSpec{Type: "url_matches", Pattern: ".*"}) {
		t.Fatal("url_matches:.* is trivial")
	}
	if validExpect(ExpectSpec{Type: "url_matches", Pattern: "[."}) {
		t.Fatal("invalid url_matches regex must not satisfy goal-class")
	}
	if !validExpect(ExpectSpec{Type: "select_value", Pattern: "@e1=us"}) {
		t.Fatal("select_value should be a known expect type")
	}
}

func TestVerifyExpectSelectValue(t *testing.T) {
	s := &BrowserAgentSession{}
	obs := &BrowserObservation{Snapshot: BrowserSnapshot{
		Refs: []BrowserElementRef{{Ref: "@e1", Name: "Country", Value: "US"}},
	}}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "select_value", Pattern: "@e1=US"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "select_value", Pattern: "US"}); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyExpect(obs, ExpectSpec{Type: "select_value", Pattern: "@e1=CN"}); err == nil {
		t.Fatal("wrong value should fail")
	}
}

func TestLastExpectExcerptOmitsEmpty(t *testing.T) {
	if lastExpectExcerpt(ExpectSpec{}) != nil {
		t.Fatal("empty expect should not project a ledger excerpt")
	}
	got := lastExpectExcerpt(ExpectSpec{Type: " url_contains ", Pattern: " /ok "})
	if got["type"] != "url_contains" || got["pattern"] != "/ok" {
		t.Fatalf("got=%v", got)
	}
}

func TestAttachLastExpectLedgerDoesNotMutateInput(t *testing.T) {
	s := &BrowserAgentSession{lastExpect: ExpectSpec{Type: "text", Pattern: "Welcome"}}
	in := map[string]interface{}{"url": "https://example.com"}
	out := attachLastExpectLedger(s, in)
	if _, ok := in["last_expect"]; ok {
		t.Fatal("attachLastExpectLedger mutated caller data")
	}
	if out["url"] != "https://example.com" {
		t.Fatalf("out=%v", out)
	}
	excerpt, _ := out["last_expect"].(map[string]string)
	if excerpt["type"] != "text" {
		t.Fatalf("last_expect=%v", out["last_expect"])
	}
}

func TestTakeCheckpointNilSessionFn(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, nil, nil)
	supervisor.takeCheckpoint(&TaskState{}, 0)
	if supervisor.capturePageSnapshot() != nil {
		t.Fatal("nil sessionFn should not capture")
	}
}
