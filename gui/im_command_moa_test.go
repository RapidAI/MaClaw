package main

import (
	"strings"
	"testing"
)

func TestClassifyImmediateIMCommand_MoA(t *testing.T) {
	cases := []struct {
		in   string
		want imCommandKind
	}{
		{"/moa", imCommandMoA},
		{"/moa review plan", imCommandMoA},
		{"/MOA caps", imCommandMoA},
		{"  /moa  spaced  ", imCommandMoA},
		{"/moab", imCommandUnknown},
		{"/goal x", imCommandGoal},
	}
	for _, tc := range cases {
		if got := classifyImmediateIMCommand(tc.in); got != tc.want {
			t.Fatalf("classifyImmediateIMCommand(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestMoaPromptFromText(t *testing.T) {
	if got := moaPromptFromText("/moa"); got != "" {
		t.Fatalf("bare /moa: got %q", got)
	}
	if got := moaPromptFromText("/moa review plan"); got != "review plan" {
		t.Fatalf("prompt: got %q", got)
	}
	if got := moaPromptFromText("/MOA  评估方案"); got != "评估方案" {
		t.Fatalf("case fold: got %q", got)
	}
	if got := moaPromptFromText("/moa @review compare A vs B"); got != "compare A vs B" {
		t.Fatalf("@preset prompt: got %q", got)
	}
	if got := moaPromptFromText("/moa @review"); got != "" {
		t.Fatalf("@preset without prompt should be empty: %q", got)
	}
	if got := moaPromptFromText("/moa stats"); got != "" {
		t.Fatalf("stats is not a prompt: %q", got)
	}
}

func TestParseSlash_GUIImportParity(t *testing.T) {
	// Ensure GUI still classifies @preset lines as MoA commands.
	if classifyImmediateIMCommand("/moa @review plan") != imCommandMoA {
		t.Fatal("classify @preset")
	}
}

func TestHandleMoACommand_Usage(t *testing.T) {
	h := &IMMessageHandler{}
	usage, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: "desktop-user", Text: "/moa", Lang: "zh-Hans"},
		"/moa", nil, nil,
	)
	if !handled || usage == nil || !strings.Contains(usage.Text, "/moa") {
		t.Fatalf("usage: handled=%v text=%v", handled, usage)
	}
}

func TestLocalizedIMSlashHelpIncludesMoA(t *testing.T) {
	zh := localizedIMSlashHelpText("zh-Hans")
	en := localizedIMSlashHelpText("en")
	if !strings.Contains(zh, "/moa") {
		t.Fatalf("zh help missing /moa:\n%s", zh)
	}
	if !strings.Contains(en, "/moa") {
		t.Fatalf("en help missing /moa:\n%s", en)
	}
}
