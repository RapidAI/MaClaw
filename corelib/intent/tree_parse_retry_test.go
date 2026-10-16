package intent

import (
	"strings"
	"testing"
)

func TestParseTreeResponseLastObjectAfterProse(t *testing.T) {
	text := "We need to parse. The tree has {braces} and \"quoted {x}\".\nAnswer:\n```json\n{\"top\":[{\"skill\":\"" + string(firstValidLabel(t)) + "\",\"score\":0.9,\"workflow_type\":\"\"}]}\n```"
	if got := ParseTreeResponse(text); len(got) != 1 {
		t.Fatalf("want 1 candidate, got %v", got)
	}
}

func TestClassifyByTreeRetriesOnceOnProse(t *testing.T) {
	calls := 0
	label := string(firstValidLabel(t))
	fn := func(system, user string) (string, error) {
		calls++
		if calls == 1 {
			return "We need to parse the user message and determine intent(s).", nil
		}
		if !strings.Contains(system, "ONLY one JSON object") {
			t.Fatalf("retry prompt missing JSON instruction")
		}
		return `{"top":[{"skill":"` + label + `","score":0.8,"workflow_type":""}]}`, nil
	}
	got, err := ClassifyByTree(fn, "tree", "hi")
	if err != nil || len(got) != 1 || calls != 2 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, calls)
	}
}

func TestClassifyByTreeGivesUpAfterOneRetry(t *testing.T) {
	calls := 0
	fn := func(system, user string) (string, error) { calls++; return "User Safety: safe", nil }
	if _, err := ClassifyByTree(fn, "tree", "hi"); err == nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRedactTreeSnippetHidesQuotedUserText(t *testing.T) {
	got := RedactTreeSnippet(`Message: "secret user text that is long 12345" ok`, 200)
	if strings.Contains(got, "secret") || !strings.Contains(got, "ok") {
		t.Fatalf("got %q", got)
	}
}

func firstValidLabel(t *testing.T) IntentLabel {
	for _, d := range DefaultDefinitions() {
		if d.Label.IsValid() {
			return d.Label
		}
	}
	t.Fatal("no labels")
	return ""
}
