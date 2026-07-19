package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestSkillAutoSummary_AnalyzeComplexity_NilSession(t *testing.T) {
	result := AnalyzeComplexity(nil)
	if result.Score != "too_simple" {
		t.Errorf("nil session: got Score=%q, want %q", result.Score, "too_simple")
	}
	if result.StepCount != 0 || result.ToolKindCount != 0 || result.TurnCount != 0 {
		t.Errorf("nil session: expected all counts 0, got step=%d tool=%d turn=%d",
			result.StepCount, result.ToolKindCount, result.TurnCount)
	}
}

func TestSkillAutoSummary_AnalyzeComplexity_EmptyEntries(t *testing.T) {
	session := &TrajectorySession{Entries: []TrajectoryEntry{}}
	result := AnalyzeComplexity(session)
	if result.Score != "too_simple" {
		t.Errorf("empty entries: got Score=%q, want %q", result.Score, "too_simple")
	}
}

func TestSkillAutoSummary_AnalyzeComplexity_TooSimple(t *testing.T) {
	// 2 steps, 1 tool kind, 3 turns — below all thresholds
	session := &TrajectorySession{
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "do something"},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file")},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file")},
		},
	}
	result := AnalyzeComplexity(session)
	if result.Score != "too_simple" {
		t.Errorf("simple session: got Score=%q, want %q", result.Score, "too_simple")
	}
	if result.StepCount != 2 {
		t.Errorf("StepCount: got %d, want 2", result.StepCount)
	}
	if result.ToolKindCount != 1 {
		t.Errorf("ToolKindCount: got %d, want 1", result.ToolKindCount)
	}
	if result.TurnCount != 3 {
		t.Errorf("TurnCount: got %d, want 3", result.TurnCount)
	}
}

func TestSkillAutoSummary_AnalyzeComplexity_WorthSummarizing(t *testing.T) {
	// 3 steps, 2 tool kinds, 5 turns — meets all thresholds
	session := &TrajectorySession{
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "complex task"},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file")},
			{Role: "tool", Content: "file content"},
			{Role: "assistant", ToolCalls: makeToolCalls("write_file")},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file")},
		},
	}
	result := AnalyzeComplexity(session)
	if result.Score != "worth_summarizing" {
		t.Errorf("complex session: got Score=%q, want %q", result.Score, "worth_summarizing")
	}
	if result.StepCount != 3 {
		t.Errorf("StepCount: got %d, want 3", result.StepCount)
	}
	if result.ToolKindCount != 2 {
		t.Errorf("ToolKindCount: got %d, want 2", result.ToolKindCount)
	}
	if result.TurnCount != 5 {
		t.Errorf("TurnCount: got %d, want 5", result.TurnCount)
	}
}

func TestSkillAutoSummary_AnalyzeComplexity_AssistantWithoutToolCalls(t *testing.T) {
	// assistant entries without ToolCalls should not count as steps
	session := &TrajectorySession{
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "do something"},
			{Role: "assistant", Content: "sure"},
			{Role: "assistant", Content: "done"},
		},
	}
	result := AnalyzeComplexity(session)
	if result.StepCount != 0 {
		t.Errorf("no tool calls: StepCount got %d, want 0", result.StepCount)
	}
	if result.Score != "too_simple" {
		t.Errorf("no tool calls: got Score=%q, want %q", result.Score, "too_simple")
	}
}

func TestSkillAutoSummary_AnalyzeComplexity_MultipleToolsPerCall(t *testing.T) {
	// A single assistant entry with multiple tool calls
	session := &TrajectorySession{
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "task"},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file", "write_file", "exec_cmd")},
			{Role: "tool", Content: "ok"},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file")},
			{Role: "assistant", ToolCalls: makeToolCalls("exec_cmd")},
		},
	}
	result := AnalyzeComplexity(session)
	if result.StepCount != 3 {
		t.Errorf("StepCount: got %d, want 3", result.StepCount)
	}
	if result.ToolKindCount != 3 {
		t.Errorf("ToolKindCount: got %d, want 3", result.ToolKindCount)
	}
}

// makeToolCalls creates a []interface{} mimicking the ToolCalls structure
// with the given tool names.
func makeToolCalls(names ...string) []interface{} {
	var calls []interface{}
	for _, name := range names {
		calls = append(calls, map[string]interface{}{
			"function": map[string]interface{}{
				"name": name,
			},
		})
	}
	return calls
}

// makeLLMToolCalls builds the live in-memory ToolCalls shape used by shared/legacy
// agent loops (and HistoryDelta) before any JSON round-trip.
func makeLLMToolCalls(pairs ...string) []llm.ToolCall {
	// pairs: id1, name1, id2, name2, ...
	out := make([]llm.ToolCall, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, llm.ToolCall{
			ID:   pairs[i],
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      pairs[i+1],
				Arguments: `{}`,
			},
		})
	}
	return out
}

func TestSkillAutoSummary_AnalyzeComplexity_LiveLLMToolCalls(t *testing.T) {
	// Pipeline receives in-memory sessions with []llm.ToolCall, not JSON maps.
	session := &TrajectorySession{
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "complex task"},
			{Role: "assistant", ToolCalls: makeLLMToolCalls("c1", "read_file")},
			{Role: "tool_result", ToolCallID: "c1", Content: "file content"},
			{Role: "assistant", ToolCalls: makeLLMToolCalls("c2", "write_file")},
			{Role: "tool_result", ToolCallID: "c2", Content: "ok"},
			{Role: "assistant", ToolCalls: makeLLMToolCalls("c3", "read_file")},
		},
	}
	result := AnalyzeComplexity(session)
	if result.Score != "worth_summarizing" {
		t.Fatalf("live llm tool calls: Score=%q steps=%d kinds=%d turns=%d",
			result.Score, result.StepCount, result.ToolKindCount, result.TurnCount)
	}
	if result.StepCount != 3 || result.ToolKindCount != 2 {
		t.Fatalf("counts steps=%d kinds=%d", result.StepCount, result.ToolKindCount)
	}
}

func TestSkillAutoSummary_DraftSkill_LiveLLMToolCalls(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "live-llm-tools",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "patch the bug"},
			{Role: "assistant", ToolCalls: makeLLMToolCalls("c1", "read_file")},
			{Role: "tool_result", ToolCallID: "c1", Content: "src"},
			{Role: "assistant", ToolCalls: makeLLMToolCalls("c2", "write_file")},
			{Role: "tool_result", ToolCallID: "c2", Content: "[error] permission denied"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("DraftSkill: %v", err)
	}
	if len(draft.Steps) != 2 {
		t.Fatalf("steps=%d want 2", len(draft.Steps))
	}
	if draft.Steps[0].Action != "read_file" || draft.Steps[1].Action != "write_file" {
		t.Fatalf("actions=%q %q", draft.Steps[0].Action, draft.Steps[1].Action)
	}
	if draft.Steps[1].OnError != "skip" {
		t.Fatalf("write_file OnError=%q want skip", draft.Steps[1].OnError)
	}
}

// makeToolCallsWithID creates a []interface{} mimicking ToolCalls with id, name, and arguments.
func makeToolCallsWithID(id, name, argsJSON string) []interface{} {
	call := map[string]interface{}{
		"id": id,
		"function": map[string]interface{}{
			"name":      name,
			"arguments": argsJSON,
		},
	}
	return []interface{}{call}
}

func TestSkillAutoSummary_DraftSkill_BasicExtraction(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "test-session-1",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "deploy the application"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c1", "read_file", `{"path":"main.go"}`)},
			{Role: "tool", ToolCallID: "c1", Content: "package main"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c2", "write_file", `{"path":"out.go","content":"done"}`)},
			{Role: "tool", ToolCallID: "c2", Content: "ok"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c3", "exec_cmd", `{"cmd":"go build"}`)},
			{Role: "tool", ToolCallID: "c3", Content: "success"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Steps) != 3 {
		t.Fatalf("Steps count: got %d, want 3", len(draft.Steps))
	}
	if draft.Steps[0].Action != "read_file" {
		t.Errorf("Step[0].Action: got %q, want %q", draft.Steps[0].Action, "read_file")
	}
	if draft.Steps[1].Action != "write_file" {
		t.Errorf("Step[1].Action: got %q, want %q", draft.Steps[1].Action, "write_file")
	}
	if draft.Steps[2].Action != "exec_cmd" {
		t.Errorf("Step[2].Action: got %q, want %q", draft.Steps[2].Action, "exec_cmd")
	}
	// Params should be parsed from JSON arguments.
	if p, ok := draft.Steps[0].Params["path"]; !ok || p != "main.go" {
		t.Errorf("Step[0].Params[path]: got %v", draft.Steps[0].Params)
	}
	if draft.Description != "deploy the application" {
		t.Errorf("Description: got %q, want %q", draft.Description, "deploy the application")
	}
	if draft.Status != "active" {
		t.Errorf("Status: got %q, want %q", draft.Status, "active")
	}
	if draft.Source != "learned" {
		t.Errorf("Source: got %q, want %q", draft.Source, "learned")
	}
	if draft.Name == "" {
		t.Error("Name should not be empty")
	}
	if len(draft.Triggers) == 0 {
		t.Error("Triggers should not be empty")
	}
}

func TestSkillAutoSummary_DraftSkill_MergeConsecutive(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "merge-test",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "read many files"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c1", "read_file", `{"path":"a.go"}`)},
			{Role: "tool", ToolCallID: "c1", Content: "content a"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c2", "read_file", `{"path":"b.go"}`)},
			{Role: "tool", ToolCallID: "c2", Content: "content b"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c3", "read_file", `{"path":"c.go"}`)},
			{Role: "tool", ToolCallID: "c3", Content: "content c"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c4", "write_file", `{"path":"out.go"}`)},
			{Role: "tool", ToolCallID: "c4", Content: "ok"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 consecutive read_file should merge into 1, plus 1 write_file = 2 steps.
	if len(draft.Steps) != 2 {
		t.Fatalf("Steps count: got %d, want 2", len(draft.Steps))
	}
	if draft.Steps[0].Action != "read_file" {
		t.Errorf("Step[0].Action: got %q, want %q", draft.Steps[0].Action, "read_file")
	}
	rc, ok := draft.Steps[0].Params["_repeat_count"]
	if !ok {
		t.Fatal("Step[0] should have _repeat_count")
	}
	if rc != 3 {
		t.Errorf("_repeat_count: got %v, want 3", rc)
	}
	if draft.Steps[1].Action != "write_file" {
		t.Errorf("Step[1].Action: got %q, want %q", draft.Steps[1].Action, "write_file")
	}
}

func TestSkillAutoSummary_DraftSkill_ErrorStepMarking(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "error-test",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "run commands"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c1", "exec_cmd", `{"cmd":"ls"}`)},
			{Role: "tool", ToolCallID: "c1", Content: "[error] command failed"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c2", "exec_cmd", `{"cmd":"cat"}`)},
			{Role: "tool", ToolCallID: "c2", Content: "[stderr] permission denied"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c3", "read_file", `{"path":"ok.go"}`)},
			{Role: "tool", ToolCallID: "c3", Content: "file content"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// exec_cmd x2 are consecutive and both have errors → merged with on_error="skip"
	if len(draft.Steps) != 2 {
		t.Fatalf("Steps count: got %d, want 2", len(draft.Steps))
	}
	if draft.Steps[0].OnError != "skip" {
		t.Errorf("Step[0].OnError: got %q, want %q", draft.Steps[0].OnError, "skip")
	}
	if draft.Steps[1].OnError != "" {
		t.Errorf("Step[1].OnError: got %q, want empty", draft.Steps[1].OnError)
	}
}

func TestSkillAutoSummary_DraftSkill_NoToolCalls(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "no-tools",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
	}
	_, err := DraftSkill(session)
	if err == nil {
		t.Fatal("expected error for session with no tool_calls")
	}
}

func TestSkillAutoSummary_DraftSkill_NilSession(t *testing.T) {
	_, err := DraftSkill(nil)
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestSkillAutoSummary_DraftSkill_NoUserMessage(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "fallback-desc",
		Entries: []TrajectoryEntry{
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c1", "read_file", `{"path":"a.go"}`)},
			{Role: "tool", ToolCallID: "c1", Content: "content"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if draft.Description != "fallback-desc" {
		t.Errorf("Description: got %q, want %q (session_id fallback)", draft.Description, "fallback-desc")
	}
}

func TestSkillAutoSummary_DraftSkill_DescriptionFromFirstUser(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "desc-test",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "first user message"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c1", "read_file", `{}`)},
			{Role: "tool", ToolCallID: "c1", Content: "ok"},
			{Role: "user", Content: "second user message"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if draft.Description != "first user message" {
		t.Errorf("Description: got %q, want %q", draft.Description, "first user message")
	}
}

// --- ValidateSkillDraft unit tests ---

func makeValidDraft() *skill.SkillYAMLFile {
	return &skill.SkillYAMLFile{
		Name:        "test_skill",
		Description: "A valid test skill",
		Triggers:    []string{"test"},
		Steps: []skill.SkillYAMLStep{
			{Action: "read_file", Params: map[string]interface{}{"path": "a.go"}},
		},
	}
}

func allowAllChecker() *SecurityPolicyChecker {
	return NewSecurityPolicyChecker(SkillSecurityPolicy{
		NetworkAccess:    SecurityAllow,
		FileSystemAccess: SecurityAllow,
		ShellExec:        SecurityAllow,
		DatabaseAccess:   SecurityAllow,
	}, nil)
}

func TestSkillAutoSummary_ValidateSkillDraft_ValidDraft(t *testing.T) {
	draft := makeValidDraft()
	result, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Name != "test_skill" {
		t.Errorf("Name: got %q, want %q", result.Name, "test_skill")
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_EmptyName(t *testing.T) {
	draft := makeValidDraft()
	draft.Name = ""
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "name") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason about name, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_NameTooLong(t *testing.T) {
	draft := makeValidDraft()
	draft.Name = strings.Repeat("a", 61)
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for name > 60 chars")
	}
	ve := err.(*ValidationError)
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "60") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason about 60 char limit, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_EmptyDescription(t *testing.T) {
	draft := makeValidDraft()
	draft.Description = ""
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for empty description")
	}
	ve := err.(*ValidationError)
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "description") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason about description, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_DescriptionTooLong(t *testing.T) {
	draft := makeValidDraft()
	draft.Description = strings.Repeat("x", 501)
	result, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err != nil {
		t.Fatalf("expected soft-truncate, got error: %v", err)
	}
	if len(result.Description) > maxSkillDescriptionBytes {
		t.Fatalf("description still too long: %d bytes", len(result.Description))
	}
	if len(result.Description) == 0 {
		t.Fatal("description was emptied by truncate")
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_NoSteps(t *testing.T) {
	draft := makeValidDraft()
	draft.Steps = nil
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for no steps")
	}
	ve := err.(*ValidationError)
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "step") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason about steps, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_EmptyAction(t *testing.T) {
	draft := makeValidDraft()
	draft.Steps = []skill.SkillYAMLStep{{Action: ""}}
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for empty action")
	}
	ve := err.(*ValidationError)
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "action") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason about action, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_NoTriggers(t *testing.T) {
	draft := makeValidDraft()
	draft.Triggers = nil
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for no triggers")
	}
	ve := err.(*ValidationError)
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "trigger") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason about triggers, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_MultipleFailures(t *testing.T) {
	draft := &skill.SkillYAMLFile{
		Name:        "",
		Description: "",
		Triggers:    nil,
		Steps:       nil,
	}
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err == nil {
		t.Fatal("expected error for multiple failures")
	}
	ve := err.(*ValidationError)
	// Should have at least 4 reasons: name, description, steps, triggers
	if len(ve.Reasons) < 4 {
		t.Errorf("expected at least 4 reasons, got %d: %v", len(ve.Reasons), ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_NameDedup(t *testing.T) {
	draft := makeValidDraft()
	existing := map[string]bool{"test_skill": true}
	result, err := ValidateSkillDraft(draft, allowAllChecker(), existing)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Name == "test_skill" {
		t.Error("expected name to be modified for dedup")
	}
	if !strings.HasPrefix(result.Name, "test_skill_") {
		t.Errorf("expected name to start with 'test_skill_', got %q", result.Name)
	}
	// Should have a timestamp suffix like _20060102150405
	suffix := strings.TrimPrefix(result.Name, "test_skill_")
	if len(suffix) != 14 {
		t.Errorf("expected 14-char timestamp suffix, got %q (len=%d)", suffix, len(suffix))
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_SecurityDenied(t *testing.T) {
	draft := makeValidDraft()
	draft.Steps = []skill.SkillYAMLStep{
		{Action: "exec_cmd", Params: map[string]interface{}{"cmd": "ls"}},
	}
	checker := NewSecurityPolicyChecker(SkillSecurityPolicy{
		ShellExec:        SecurityDeny,
		NetworkAccess:    SecurityAllow,
		FileSystemAccess: SecurityAllow,
		DatabaseAccess:   SecurityAllow,
	}, nil)
	_, err := ValidateSkillDraft(draft, checker, nil)
	if err == nil {
		t.Fatal("expected error when all steps require denied capabilities")
	}
	ve := err.(*ValidationError)
	found := false
	for _, r := range ve.Reasons {
		if strings.Contains(r, "security") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected security-related reason, got: %v", ve.Reasons)
	}
}

func TestSkillAutoSummary_ValidateSkillDraft_SecurityStripKeepsSafeSteps(t *testing.T) {
	draft := makeValidDraft()
	draft.Steps = []skill.SkillYAMLStep{
		{Action: "read_file", Params: map[string]interface{}{"path": "a.go"}},
		{Action: "exec_cmd", Params: map[string]interface{}{"cmd": "ls"}},
		{Action: "http_fetch", Params: map[string]interface{}{"url": "https://example.com"}},
	}
	checker := NewSecurityPolicyChecker(SkillSecurityPolicy{
		ShellExec:        SecurityDeny,
		NetworkAccess:    SecurityDeny,
		FileSystemAccess: SecurityAllow,
		DatabaseAccess:   SecurityAllow,
	}, nil)
	result, err := ValidateSkillDraft(draft, checker, nil)
	if err != nil {
		t.Fatalf("expected soft-strip of blocked steps, got: %v", err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 kept step, got %d: %+v", len(result.Steps), result.Steps)
	}
	if result.Steps[0].Action != "read_file" {
		t.Fatalf("kept step action = %q, want read_file", result.Steps[0].Action)
	}
}

func TestInferSecurityLabels_NoFalsePositivesOnBenignNames(t *testing.T) {
	labels := inferSecurityLabels([]skill.SkillYAMLStep{
		{Action: "runtime_info"},
		{Action: "capital_summary"},
		{Action: "search_query"},
	})
	for _, l := range labels {
		if l == "shell_exec" || l == "network_access" || l == "database_access" {
			t.Fatalf("unexpected label %q for benign actions: %v", l, labels)
		}
	}
}

func TestTruncateSkillDescription_UTF8Safe(t *testing.T) {
	// 200 Chinese runes (~600 bytes) must truncate without splitting code points.
	desc := strings.Repeat("测", 200)
	got := truncateSkillDescription(desc, 50)
	if len(got) > 50 {
		t.Fatalf("truncated length %d > 50", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncated produced invalid UTF-8")
	}
	if got == "" {
		t.Fatal("expected non-empty truncate")
	}
}

// --- Additional edge case tests (Task 6.1) ---

// TestSkillAutoSummary_AnalyzeComplexity_SingleStepSimple tests a single-step
// task that doesn't meet any threshold (1 step, 1 tool, 2 turns).
// Validates: Requirement 1.6
func TestSkillAutoSummary_AnalyzeComplexity_SingleStepSimple(t *testing.T) {
	session := &TrajectorySession{
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "quick task"},
			{Role: "assistant", ToolCalls: makeToolCalls("read_file")},
		},
	}
	result := AnalyzeComplexity(session)
	if result.Score != "too_simple" {
		t.Errorf("single step: got Score=%q, want %q", result.Score, "too_simple")
	}
	if result.StepCount != 1 {
		t.Errorf("StepCount: got %d, want 1", result.StepCount)
	}
	if result.ToolKindCount != 1 {
		t.Errorf("ToolKindCount: got %d, want 1", result.ToolKindCount)
	}
	if result.TurnCount != 2 {
		t.Errorf("TurnCount: got %d, want 2", result.TurnCount)
	}
}

// TestSkillAutoSummary_EvaluateSkillExecution_Scoring tests the scoring
// function directly with various inputs.
// Validates: Requirement 2.7
func TestSkillAutoSummary_EvaluateSkillExecution_Scoring(t *testing.T) {
	tests := []struct {
		name   string
		result *SkillExecutionResult
		want   int
	}{
		{"nil result", nil, 0},
		{"security alert", &SkillExecutionResult{HasSecAlert: true}, -2},
		{"error", &SkillExecutionResult{HasError: true}, -1},
		{"not success", &SkillExecutionResult{Success: false}, 0},
		{"success basic", &SkillExecutionResult{Success: true, OutputQuality: "basic"}, 1},
		{"success good", &SkillExecutionResult{Success: true, OutputQuality: "good"}, 1},
		{"success excellent", &SkillExecutionResult{Success: true, OutputQuality: "excellent"}, 2},
		{"success none quality", &SkillExecutionResult{Success: true, OutputQuality: "none"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateSkillExecution(tt.result)
			if got != tt.want {
				t.Errorf("EvaluateSkillExecution(%v) = %d, want %d", tt.result, got, tt.want)
			}
		})
	}
}

// TestSkillAutoSummary_DraftSkill_ErrorToolCallWithStderr tests that a tool
// result starting with "[stderr]" marks on_error="skip" for a non-merged step.
// Validates: Requirement 2.7
func TestSkillAutoSummary_DraftSkill_ErrorToolCallWithStderr(t *testing.T) {
	session := &TrajectorySession{
		SessionID: "stderr-test",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "run a command"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c1", "exec_cmd", `{"cmd":"ls"}`)},
			{Role: "tool", ToolCallID: "c1", Content: "[stderr] permission denied"},
			{Role: "assistant", ToolCalls: makeToolCallsWithID("c2", "read_file", `{"path":"a.go"}`)},
			{Role: "tool", ToolCallID: "c2", Content: "file content"},
		},
	}
	draft, err := DraftSkill(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Steps) != 2 {
		t.Fatalf("Steps count: got %d, want 2", len(draft.Steps))
	}
	if draft.Steps[0].Action != "exec_cmd" {
		t.Errorf("Step[0].Action: got %q, want %q", draft.Steps[0].Action, "exec_cmd")
	}
	if draft.Steps[0].OnError != "skip" {
		t.Errorf("Step[0].OnError: got %q, want %q", draft.Steps[0].OnError, "skip")
	}
	if draft.Steps[1].OnError != "" {
		t.Errorf("Step[1].OnError: got %q, want empty", draft.Steps[1].OnError)
	}
}

// TestSkillAutoSummary_ValidateSkillDraft_NameExactly60Chars tests that a name
// of exactly 60 characters passes validation.
// Validates: Requirement 3.6
func TestSkillAutoSummary_ValidateSkillDraft_NameExactly60Chars(t *testing.T) {
	draft := makeValidDraft()
	draft.Name = strings.Repeat("n", 60)
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err != nil {
		t.Fatalf("name with exactly 60 chars should pass, got error: %v", err)
	}
}

// TestSkillAutoSummary_ValidateSkillDraft_DescExactly500Chars tests that a
// description of exactly 500 characters passes validation.
// Validates: Requirement 3.6
func TestSkillAutoSummary_ValidateSkillDraft_DescExactly500Chars(t *testing.T) {
	draft := makeValidDraft()
	draft.Description = strings.Repeat("d", 500)
	_, err := ValidateSkillDraft(draft, allowAllChecker(), nil)
	if err != nil {
		t.Fatalf("description with exactly 500 chars should pass, got error: %v", err)
	}
}

// TestSkillAutoSummary_Pipeline_Idempotency tests that calling RunPipeline
// twice with the same session_id is a no-op on the second call.
// Validates: Requirements 5.6, 5.7, 6.5
func TestSkillAutoSummary_Pipeline_Idempotency(t *testing.T) {
	// Create a pipeline with only the processed map initialized.
	// All other dependencies are nil — the session is "too_simple" so
	// the pipeline will mark it processed without touching other deps.
	p := &SkillAutoSummaryPipeline{
		processed: make(map[string]bool),
	}

	session := &TrajectorySession{
		SessionID: "idempotent-test",
		Entries: []TrajectoryEntry{
			{Role: "user", Content: "simple"},
		},
	}

	// First call — should process and mark as done.
	p.RunPipeline(session)

	p.mu.Lock()
	firstSeen := p.processed["idempotent-test"]
	p.mu.Unlock()
	if !firstSeen {
		t.Fatal("session should be in processed map after first call")
	}

	// Second call — should be a no-op (idempotent).
	p.RunPipeline(session)

	p.mu.Lock()
	count := 0
	for k := range p.processed {
		if k == "idempotent-test" {
			count++
		}
	}
	p.mu.Unlock()
	if count != 1 {
		t.Errorf("expected session_id to appear once in processed map, got %d", count)
	}
}

// --- shouldUpdateSkill tests (P1: Skill auto-iteration) ---

func TestShouldUpdateSkill_FewerSteps(t *testing.T) {
	newDraft := &skill.SkillYAMLFile{
		Steps: []skill.SkillYAMLStep{
			{Action: "read_file"},
			{Action: "write_file"},
		},
	}
	existing := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "read_file"},
			{Action: "write_file"},
			{Action: "exec_cmd"},
		},
	}
	if !shouldUpdateSkill(newDraft, existing) {
		t.Error("new draft with fewer steps should trigger update")
	}
}

func TestShouldUpdateSkill_FewerErrors(t *testing.T) {
	newDraft := &skill.SkillYAMLFile{
		Steps: []skill.SkillYAMLStep{
			{Action: "read_file"},
			{Action: "write_file"},
			{Action: "exec_cmd"},
		},
	}
	existing := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "read_file"},
			{Action: "write_file", OnError: "skip"},
			{Action: "exec_cmd", OnError: "continue"},
		},
	}
	if !shouldUpdateSkill(newDraft, existing) {
		t.Error("new draft with fewer error steps should trigger update")
	}
}

func TestShouldUpdateSkill_NotBetter(t *testing.T) {
	newDraft := &skill.SkillYAMLFile{
		Steps: []skill.SkillYAMLStep{
			{Action: "read_file"},
			{Action: "write_file"},
			{Action: "exec_cmd"},
			{Action: "deploy", OnError: "skip"},
		},
	}
	existing := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "read_file"},
			{Action: "write_file"},
			{Action: "exec_cmd"},
		},
	}
	if shouldUpdateSkill(newDraft, existing) {
		t.Error("new draft with more steps and more errors should NOT trigger update")
	}
}

func TestShouldUpdateSkill_SameStepsSameErrors(t *testing.T) {
	newDraft := &skill.SkillYAMLFile{
		Steps: []skill.SkillYAMLStep{
			{Action: "read_file"},
			{Action: "write_file"},
		},
	}
	existing := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "read_file"},
			{Action: "write_file"},
		},
	}
	if shouldUpdateSkill(newDraft, existing) {
		t.Error("same steps and errors should NOT trigger update")
	}
}

func TestRunAutoUpload_FailsOverToBackupHubCenter(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL, "https://upload-backup.example"}})
		case "/api/v1/skills/submit":
			_ = json.NewEncoder(w).Encode(map[string]any{"submission_id": "sub-123"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tempHome}
	skillExec := NewSkillExecutor(app, nil, nil)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteHubCenterURL = "http://127.0.0.1:1"
	cfg.RemoteHubCenterURLs = []string{backup.URL}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "auto-upload-skill",
		Description: "Uploads a learned skill through the lifecycle quality gate.",
		Triggers:    []string{"auto upload skill"},
		Source:      "learned",
		Status:      "active",
		Steps:       []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "hello"}}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := NewSkillMarketClient(app)
	trigger := NewAutoUploadTrigger(client, func() string { return "user@example.com" })

	skillDir := filepath.Join(tempHome, "auto-upload-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := RunAutoUpload(context.Background(), "auto-upload-skill", skillDir, 1, trigger, skillExec, client); err != nil {
		t.Fatalf("RunAutoUpload() error = %v", err)
	}

	items, err := app.skillLifecycle.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusUploaded || items[0].SubmissionID != "sub-123" {
		t.Fatalf("upload queue = %+v", items)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
	// MarkUploaded persists only a stable package identity as HubSkillID;
	// a bare upload tracking id ("sub-<digits>…") is rejected by design
	// (corelib.StableSkillIdentityFromRef) and falls back to the skill name.
	// The submission id itself is tracked in the upload queue (asserted above).
	if len(saved.NLSkills) != 1 || saved.NLSkills[0].HubSkillID != "auto-upload-skill" {
		t.Fatalf("HubSkillID not recorded in config: %+v", saved.NLSkills)
	}
	if !containsString(saved.RemoteHubCenterURLs, "https://upload-backup.example") {
		t.Fatalf("RemoteHubCenterURLs = %#v", saved.RemoteHubCenterURLs)
	}
}

func TestRunAutoUpload_SkipsWhenNoHubCenterReachable(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	app := &App{testHomeDir: tempHome}
	skillExec := NewSkillExecutor(app, nil, nil)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteHubCenterURL = "http://127.0.0.1:1"
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "skip-upload-skill",
		Description: "demo",
		Source:      "learned",
		Status:      "active",
		Steps:       []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "hello"}}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := NewSkillMarketClient(app)
	trigger := NewAutoUploadTrigger(client, func() string { return "user@example.com" })

	skillDir := filepath.Join(tempHome, "skip-upload-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := RunAutoUpload(context.Background(), "skip-upload-skill", skillDir, 1, trigger, skillExec, client); err != nil {
		t.Fatalf("RunAutoUpload() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "upload_status.json")); !os.IsNotExist(err) {
		t.Fatalf("upload_status.json should not exist, stat err = %v", err)
	}
}

func TestSkillDefinitionExistsIgnoresJSONAndAcceptsReadme(t *testing.T) {
	jsonDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(jsonDir, "skill.json"), []byte(`{"name":"json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if skillDefinitionExists(jsonDir) {
		t.Fatal("skillDefinitionExists should ignore retired skill.json definitions")
	}

	readmeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# Readme skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !skillDefinitionExists(readmeDir) {
		t.Fatal("skillDefinitionExists should accept README.md")
	}
}
