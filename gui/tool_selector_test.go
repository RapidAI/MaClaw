package main

import (
	"strings"
	"testing"
)

func TestToolSelector_Recommend_Default(t *testing.T) {
	s := NewToolSelector()
	name, reason := s.Recommend("do something", nil)
	if name == "" {
		t.Error("expected a recommendation")
	}
	if reason == "" {
		t.Error("expected a reason")
	}
}

func TestToolSelector_Recommend_PythonTask(t *testing.T) {
	s := NewToolSelector()
	name, _ := s.Recommend("write a python script to process data", []string{"claude", "codex"})
	// Both claude and codex support python; either is acceptable
	if name != "claude" && name != "codex" {
		t.Errorf("expected claude or codex for python task, got %s", name)
	}
}

func TestToolSelector_Recommend_ReactTask(t *testing.T) {
	s := NewToolSelector()
	name, _ := s.Recommend("build a react component with typescript", []string{"cursor", "claude"})
	// Both support react+typescript
	if name == "" {
		t.Error("expected a recommendation")
	}
}

func TestToolSelector_Recommend_InstalledBonus(t *testing.T) {
	s := NewToolSelector()
	// Without installed, claude should win (highest base score)
	name1, _ := s.Recommend("general task", nil)
	// With only codex installed, codex gets +2 bonus
	name2, _ := s.Recommend("general task", []string{"codex"})
	// codex should now beat claude due to installed bonus
	if name1 == name2 && name2 != "codex" {
		// This is fine — just checking the bonus has an effect
	}
	if name2 != "codex" {
		t.Errorf("expected codex with installed bonus, got %s", name2)
	}
}

func TestToolSelector_Recommend_FlutterTask(t *testing.T) {
	s := NewToolSelector()
	name, _ := s.Recommend("create a flutter app for android", []string{"gemini"})
	if name != "gemini" {
		t.Errorf("expected gemini for flutter task, got %s", name)
	}
}

func TestToolSelector_Recommend_ReasonContainsMatches(t *testing.T) {
	s := NewToolSelector()
	_, reason := s.Recommend("refactor python code", []string{"claude"})
	if !strings.Contains(reason, "matches capabilities") {
		t.Errorf("reason should mention matches, got %q", reason)
	}
}
