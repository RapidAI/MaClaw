package agentruntime

import (
	"strings"
	"testing"
)

func TestNormalizeToolArgumentsJSONUsesOneEmptyShape(t *testing.T) {
	if got := NormalizeToolArgumentsJSON(" \n\t "); got != "{}" {
		t.Fatalf("empty args = %q, want {}", got)
	}
	if got := NormalizeToolArgumentsJSON("```json\n{\"path\":\"README.md\"}\n```"); got != `{"path":"README.md"}` {
		t.Fatalf("fenced args = %q", got)
	}
}

func TestSanitizeToolArgumentsPreservesEscapedQuotesInValidJSON(t *testing.T) {
	input := `{"message":"say \"hello\""}`
	got := SanitizeToolArguments(input)
	if got != input {
		t.Fatalf("valid JSON was rewritten: got %q, want %q", got, input)
	}
	args, err := ParseToolArgumentsObject(input)
	if err != nil {
		t.Fatalf("valid escaped-quote JSON rejected: %v", err)
	}
	if args["message"] != `say "hello"` {
		t.Fatalf("decoded message = %#v", args["message"])
	}
}

func TestParseToolArgumentsObjectRejectsNonObjects(t *testing.T) {
	for _, input := range []string{`[]`, `null`, `"text"`} {
		if _, err := ParseToolArgumentsObject(input); err == nil {
			t.Fatalf("ParseToolArgumentsObject(%s) unexpectedly succeeded", input)
		}
	}
	args, err := ParseToolArgumentsObject(`{"path":"a.txt"}`)
	if err != nil || args["path"] != "a.txt" {
		t.Fatalf("valid object = %#v, err=%v", args, err)
	}
}

func TestCanonicalizeBrowserToolCallParity(t *testing.T) {
	name, args, changed := CanonicalizeBrowserToolCall("browser_click", map[string]any{"ref": "@e1"})
	if !changed || name != "browser" || args["action"] != "click" {
		t.Fatalf("browser alias = %q %#v changed=%v", name, args, changed)
	}
	name, encoded, changed := CanonicalizeBrowserToolCallJSON("browser_navigate", `{"url":"https://example.com"}`)
	if !changed || name != "browser" || !strings.Contains(encoded, `"action":"navigate"`) {
		t.Fatalf("browser JSON alias = %q %q changed=%v", name, encoded, changed)
	}
	if got := UnsupportedBrowserAction("browser", map[string]any{"action": "eval"}); got != "eval" {
		t.Fatalf("unsupported browser action = %q", got)
	}
}

func TestCanonicalizeLocalFileSearchToolCallParity(t *testing.T) {
	name, args, changed := CanonicalizeLocalFileSearchToolCall("search_files", map[string]any{"glob_pattern": "*.md"})
	if !changed || name != "Glob" || args["pattern"] != "*.md" {
		t.Fatalf("local search alias = %q %#v changed=%v", name, args, changed)
	}
	name, encoded, changed := CanonicalizeLocalFileSearchToolCallJSON("glob", `{"glob":"*.go"}`)
	if !changed || name != "Glob" || !strings.Contains(encoded, `"pattern":"*.go"`) {
		t.Fatalf("local search JSON alias = %q %q changed=%v", name, encoded, changed)
	}
}

func TestCanonicalizeToolCallJSONIsIdempotent(t *testing.T) {
	name, args, changed := CanonicalizeToolCallJSON("browser_click", `{"ref":"@e1"}`)
	if !changed || name != "browser" || !strings.Contains(args, `"action":"click"`) {
		t.Fatalf("combined canonicalization = %q %q changed=%v", name, args, changed)
	}
	name2, args2, changed2 := CanonicalizeToolCallJSON(name, args)
	if changed2 || name2 != name || args2 != args {
		t.Fatalf("combined canonicalization is not idempotent: %q %q changed=%v", name2, args2, changed2)
	}
}
