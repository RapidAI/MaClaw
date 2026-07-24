package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// fakeNamingLLM implements skillNamingLLM for tests.
type fakeNamingLLM struct {
	configured bool
	response   string
	err        error
	gotPrompt  string
}

func (f *fakeNamingLLM) ChatCall(messages []map[string]string) (string, error) {
	for _, m := range messages {
		if m["role"] == "user" {
			f.gotPrompt = m["content"]
		}
	}
	return f.response, f.err
}

func (f *fakeNamingLLM) IsConfigured() bool { return f.configured }

func namingTestSteps() []skill.SkillYAMLStep {
	return []skill.SkillYAMLStep{
		{Action: "http_request"},
		{Action: "parse_json"},
		{Action: "http_request"}, // duplicate should be deduped
	}
}

func TestSkillNaming_LLMNameUsed(t *testing.T) {
	llm := &fakeNamingLLM{configured: true, response: "Fetch_Weather_Data\n"}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "帮我查一下北京天气", namingTestSteps(), nil)
	if !usedLLM {
		t.Fatalf("expected LLM name to be used, got fallback %q", name)
	}
	if name != "craft_fetch_weather_data" {
		t.Fatalf("unexpected name: %q", name)
	}
	// Prompt should carry description and deduped tool sequence.
	if !strings.Contains(llm.gotPrompt, "北京天气") {
		t.Errorf("prompt missing description: %q", llm.gotPrompt)
	}
	if !strings.Contains(llm.gotPrompt, "http_request -> parse_json") {
		t.Errorf("prompt missing deduped tool sequence: %q", llm.gotPrompt)
	}
}

func TestSkillNaming_StripsQuotesAndCraftPrefix(t *testing.T) {
	llm := &fakeNamingLLM{configured: true, response: "\"craft_export_excel_report\""}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "export report", namingTestSteps(), nil)
	if !usedLLM {
		t.Fatalf("expected LLM name, got fallback %q", name)
	}
	if name != "craft_export_excel_report" {
		t.Fatalf("double prefix or quotes not stripped: %q", name)
	}
}

func TestSkillNaming_FallbackWhenNotConfigured(t *testing.T) {
	llm := &fakeNamingLLM{configured: false}
	desc := "fetch weather data from API"
	name, usedLLM := GenerateSkillNameWithLLM(llm, desc, namingTestSteps(), nil)
	if usedLLM {
		t.Fatalf("expected fallback, got LLM name %q", name)
	}
	if want := tool.GenerateSkillName(desc); name != want {
		t.Fatalf("fallback mismatch: got %q want %q", name, want)
	}
}

func TestSkillNaming_FallbackOnNilLLM(t *testing.T) {
	desc := "fetch weather data"
	name, usedLLM := GenerateSkillNameWithLLM(nil, desc, namingTestSteps(), nil)
	if usedLLM {
		t.Fatalf("expected fallback with nil LLM")
	}
	if want := tool.GenerateSkillName(desc); name != want {
		t.Fatalf("fallback mismatch: got %q want %q", name, want)
	}
}

func TestSkillNaming_FallbackOnError(t *testing.T) {
	llm := &fakeNamingLLM{configured: true, err: errors.New("boom")}
	desc := "fetch weather data"
	name, usedLLM := GenerateSkillNameWithLLM(llm, desc, namingTestSteps(), nil)
	if usedLLM {
		t.Fatalf("expected fallback on error")
	}
	if want := tool.GenerateSkillName(desc); name != want {
		t.Fatalf("fallback mismatch: got %q want %q", name, want)
	}
}

func TestSkillNaming_FallbackOnUnusableOutput(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"whitespace":   "   \n  ",
		"chinese only": "查天气",
		"punctuation":  "!!!???",
	}
	desc := "fetch weather data"
	for label, resp := range cases {
		llm := &fakeNamingLLM{configured: true, response: resp}
		name, usedLLM := GenerateSkillNameWithLLM(llm, desc, namingTestSteps(), nil)
		if usedLLM {
			t.Errorf("%s: expected fallback, got %q", label, name)
		}
		if want := tool.GenerateSkillName(desc); name != want {
			t.Errorf("%s: fallback mismatch: got %q want %q", label, name, want)
		}
	}
}

func TestSkillNaming_TruncatesLongName(t *testing.T) {
	long := strings.Repeat("averylongword_", 10) + "end" // ~130 chars
	llm := &fakeNamingLLM{configured: true, response: long}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "task", namingTestSteps(), nil)
	if !usedLLM {
		t.Fatalf("expected truncated LLM name, got fallback %q", name)
	}
	base := strings.TrimPrefix(name, "craft_")
	if len(base) > maxLLMSkillNameLen {
		t.Fatalf("base name too long: %d chars (%q)", len(base), name)
	}
	if strings.HasSuffix(base, "_") || strings.HasSuffix(base, "-") {
		t.Fatalf("truncated name has trailing separator: %q", name)
	}
}

func TestSkillNaming_CollisionAppendsSuffix(t *testing.T) {
	llm := &fakeNamingLLM{configured: true, response: "fetch_weather_data"}
	existing := map[string]bool{
		"craft_fetch_weather_data":   true,
		"craft_fetch_weather_data_2": true,
	}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "查天气", namingTestSteps(), existing)
	if !usedLLM {
		t.Fatalf("expected LLM name, got fallback %q", name)
	}
	if name != "craft_fetch_weather_data_3" {
		t.Fatalf("expected _3 suffix, got %q", name)
	}
}

func TestSkillNaming_PromptListsExistingNames(t *testing.T) {
	llm := &fakeNamingLLM{configured: true, response: "new_skill"}
	existing := map[string]bool{"craft_old_skill": true}
	_, _ = GenerateSkillNameWithLLM(llm, "task", namingTestSteps(), existing)
	if !strings.Contains(llm.gotPrompt, "craft_old_skill") {
		t.Errorf("prompt should list existing names to avoid: %q", llm.gotPrompt)
	}
}

func TestSkillNaming_LegitTaskPrefixAccepted(t *testing.T) {
	// A real name starting with "task_" must not be mistaken for
	// SanitizeFilename's task_<hash> fallback.
	llm := &fakeNamingLLM{configured: true, response: "task_scheduler"}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "schedule daily tasks", namingTestSteps(), nil)
	if !usedLLM {
		t.Fatalf("task_scheduler wrongly rejected, got fallback %q", name)
	}
	if name != "craft_task_scheduler" {
		t.Fatalf("unexpected name: %q", name)
	}
}

func TestSkillNaming_KebabOutputNormalized(t *testing.T) {
	// LLM outputs kebab-case despite instructions: dashes must become
	// underscores so the name can't collide with a snake_case twin on disk
	// (directory names are derived via toKebabCase).
	llm := &fakeNamingLLM{configured: true, response: "Fetch-Weather-Data"}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "查天气", namingTestSteps(), nil)
	if !usedLLM {
		t.Fatalf("expected LLM name, got fallback %q", name)
	}
	if name != "craft_fetch_weather_data" {
		t.Fatalf("dashes not normalized: %q", name)
	}
}

func TestSkillNaming_DashCraftPrefixStripped(t *testing.T) {
	llm := &fakeNamingLLM{configured: true, response: "craft-foo-bar"}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "task", namingTestSteps(), nil)
	if !usedLLM {
		t.Fatalf("expected LLM name, got fallback %q", name)
	}
	if name != "craft_foo_bar" {
		t.Fatalf("craft- prefix not stripped: %q", name)
	}
}

func TestSkillNaming_KebabDirectoryCollisionAvoided(t *testing.T) {
	// Existing skill registered as craft_fetch_weather_data; the pipeline
	// seeds the set with both raw and toKebabCase forms. An LLM name that
	// only differs by _ vs - must still be treated as a collision.
	llm := &fakeNamingLLM{configured: true, response: "fetch-weather-data"}
	existing := map[string]bool{
		"craft_fetch_weather_data": true,
		"craft-fetch-weather-data": true, // toKebabCase form, as the pipeline adds
	}
	name, usedLLM := GenerateSkillNameWithLLM(llm, "查天气", namingTestSteps(), existing)
	if !usedLLM {
		t.Fatalf("expected LLM name, got fallback %q", name)
	}
	if name != "craft_fetch_weather_data_2" {
		t.Fatalf("kebab collision not avoided: %q", name)
	}
}

func TestSkillNaming_NameWithinValidateLimit(t *testing.T) {
	// Pin the implicit contract: every generated name (prefix + truncated
	// base + collision suffix) must satisfy ValidateSkillDraft's <=60 limit.
	long := strings.Repeat("verylongword_", 10) + "end"
	llm := &fakeNamingLLM{configured: true, response: long}
	existing := map[string]bool{}
	base := "craft_" + strings.TrimRight(long[:maxLLMSkillNameLen], "_-")
	existing[base] = true
	name, usedLLM := GenerateSkillNameWithLLM(llm, "task", namingTestSteps(), existing)
	if !usedLLM {
		t.Fatalf("expected LLM name, got fallback %q", name)
	}
	if len(name) > 60 {
		t.Fatalf("name exceeds ValidateSkillDraft limit: %d chars (%q)", len(name), name)
	}
	if !strings.HasSuffix(name, "_2") {
		t.Fatalf("expected collision suffix, got %q", name)
	}
}

func TestSkillNaming_PromptCapsAndFiltersExistingNames(t *testing.T) {
	existing := map[string]bool{"other_skill": true} // non-craft name must be filtered
	for i := 0; i < 60; i++ {
		existing[fmt.Sprintf("craft_n%03d", i)] = true
	}
	llm := &fakeNamingLLM{configured: true, response: "new_skill"}
	_, _ = GenerateSkillNameWithLLM(llm, "task", namingTestSteps(), existing)
	if strings.Contains(llm.gotPrompt, "other_skill") {
		t.Errorf("non-craft name should be filtered from prompt: %q", llm.gotPrompt)
	}
	if strings.Contains(llm.gotPrompt, "craft_n050") {
		t.Errorf("existing names list should be capped at 50: %q", llm.gotPrompt)
	}
	if !strings.Contains(llm.gotPrompt, "craft_n049") {
		t.Errorf("first 50 sorted craft_ names should be listed: %q", llm.gotPrompt)
	}
}

func TestSkillNaming_ChineseFallbackUnchanged(t *testing.T) {
	// With no LLM, a pure-Chinese description keeps the historical heuristic
	// behavior (hash-based name), so existing deployments are unaffected.
	desc := "帮我整理桌面上的文件"
	name, usedLLM := GenerateSkillNameWithLLM(nil, desc, namingTestSteps(), nil)
	if usedLLM {
		t.Fatalf("expected fallback")
	}
	if want := tool.GenerateSkillName(desc); name != want {
		t.Fatalf("fallback mismatch: got %q want %q", name, want)
	}
}
