package skill

import (
	"reflect"
	"sort"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Regression guards for the craft-generate-pdf failure class: a learned skill
// recorded verbatim commands whose PowerShell format items, shell loop
// variables, and content tokens were misread as required template parameters
// ("0", "1", "i", "t", "r", "n", "p_theta"), burning seven LLM retries.

func TestExtractPlaceholderKeys_IgnoresNumericFormatItems(t *testing.T) {
	cmd := `pwsh -Command "$row = \"{0}: {1}\" -f $a, $b"`
	keys := ExtractPlaceholderKeys(cmd)
	if len(keys) != 0 {
		t.Fatalf("numeric format items must not be placeholders, got %v", keys)
	}
}

func TestExtractPlaceholderKeys_IgnoresShellPositionals(t *testing.T) {
	cmd := `run.sh "$1" "${2}"`
	if keys := ExtractPlaceholderKeys(cmd); len(keys) != 0 {
		t.Fatalf("shell positional args must not be placeholders, got %v", keys)
	}
}

func TestExtractPlaceholderKeys_StillMatchesNamedKeys(t *testing.T) {
	keys := ExtractPlaceholderKeys("convert {{input}} -o {output} --fmt ${format}")
	want := []string{"input", "output", "format"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
}

func TestSubstituteVariables_LeavesFormatItemsAlone(t *testing.T) {
	cmd := `"{0}: {1}" -f $a, $b`
	out := SubstituteVariables(cmd, map[string]string{"0": "x", "1": "y"})
	if out != cmd {
		t.Fatalf("format item must survive substitution, got %q", out)
	}
	stripped := StripUnresolvedPlaceholders(cmd)
	if stripped != cmd {
		t.Fatalf("format item must survive placeholder stripping, got %q", stripped)
	}
}

func TestExtractPlaceholderKeysWithForm_ReportsExplicitForm(t *testing.T) {
	keys := ExtractPlaceholderKeysWithForm("run {{input}} {output} ${extra}")
	want := []PlaceholderKey{
		{Key: "input", Explicit: true},
		{Key: "output", Explicit: false},
		{Key: "extra", Explicit: false},
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("got %+v, want %+v", keys, want)
	}
}

func TestExtractPlaceholderKeysWithForm_ExplicitIsSticky(t *testing.T) {
	// The ambiguous ${input} appears first, but the author also declared the
	// same key via the explicit grammar — the key must count as Explicit so
	// SynthesizeParams trusts it (e.g. a single-letter explicit declaration
	// must not be dropped just because of occurrence order).
	keys := ExtractPlaceholderKeysWithForm(`sort "${input}" --by {{input}}`)
	if len(keys) != 1 || keys[0].Key != "input" || !keys[0].Explicit {
		t.Fatalf("expected sticky explicit input, got %+v", keys)
	}
}

func TestSynthesizeParams_ExplicitSingleLetterStillSynthesized(t *testing.T) {
	// {{q}} is the authoring grammar — always trusted, even single-letter.
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": `echo {{q}}`}},
	}
	params := SynthesizeParams(steps, nil)
	if len(params) != 1 || params[0].Name != "q" {
		t.Fatalf("explicit {{q}} must synthesize param, got %v", paramNames(params))
	}
	if params[0].Required {
		t.Error("synthesized param must not be required without requiredArgs")
	}
}

func TestSynthesizeParams_AmbiguousSingleLetterDropped(t *testing.T) {
	// ${i}/{t} are shell loop variables captured verbatim from a recording.
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": `for f in *.md; do pandoc "${i}" -o "{t}.pdf"; done`,
		}},
	}
	if params := SynthesizeParams(steps, nil); len(params) != 0 {
		t.Fatalf("ambiguous single-letter keys must not synthesize params, got %v", paramNames(params))
	}
}

func TestSynthesizeParams_NumericKeysNeverSynthesized(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": `"{0}: {1}" -f $a, $b`,
		}},
	}
	params := SynthesizeParams(steps, []string{"0", "1"})
	if len(params) != 0 {
		t.Fatalf("numeric keys must never synthesize params even if listed in requiredArgs, got %v", paramNames(params))
	}
}

func TestSynthesizeParams_MultiLetterContentTokenSynthesizedButOptional(t *testing.T) {
	// {p_theta}: a multi-char key from an ambiguous form — synthesized (it
	// could be a real template var) but never required without explicit
	// requiredArgs.
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": `plot "{p_theta}"`,
		}},
	}
	params := SynthesizeParams(steps, nil)
	if len(params) != 1 || params[0].Name != "p_theta" {
		t.Fatalf("got %v", paramNames(params))
	}
	if params[0].Required {
		t.Error("ambiguously-formed synthetic param must be optional")
	}
}

func TestDetectImplicitRequiredArgs_SkipsShellLoopVars(t *testing.T) {
	// A bash loop variable must not be demanded from the LLM as a missing
	// run argument — that is the retry-burning failure mode. Explicit and
	// parameter-shaped placeholders still are.
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": `for f in *.md; do pandoc "${i}" -o "out-{{output_name}}.pdf"; done`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `mv out-{{output_name}}.pdf {final_name}.pdf`,
		}},
	}
	missing := DetectImplicitRequiredArgs(steps, map[string]string{})
	sort.Strings(missing)
	want := []string{"final_name", "output_name"}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("got %v, want %v", missing, want)
	}
}

func TestShouldQuarantineLearnedSkill(t *testing.T) {
	cases := []struct {
		name  string
		entry *corelib.NLSkillEntry
		want  bool
	}{
		{"nil", nil, false},
		{"builtin never succeeds", &corelib.NLSkillEntry{Source: "manual", FailureCount: 9}, false},
		{"learned with prior success", &corelib.NLSkillEntry{Source: "learned", SuccessCount: 1, FailureCount: 9}, false},
		{"learned under threshold", &corelib.NLSkillEntry{Source: "learned", FailureCount: 2}, false},
		{"learned at threshold", &corelib.NLSkillEntry{Source: "learned", FailureCount: LearnedSkillQuarantineThreshold}, true},
		{"crafted at threshold", &corelib.NLSkillEntry{Source: "crafted", FailureCount: LearnedSkillQuarantineThreshold}, true},
	}
	for _, tc := range cases {
		if got := ShouldQuarantineLearnedSkill(tc.entry); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
