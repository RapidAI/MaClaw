package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveCreateSessionProjectID(t *testing.T) {
	cfg := corelib.AppConfig{Projects: []corelib.ProjectConfig{
		{Id: "p1", Name: "One", Path: "/repo/one"},
		{Id: "p2", Name: "Two", Path: "/repo/two"},
	}}

	got := resolveCreateSessionProjectID(cfg, "p1")
	if got.Error != "" {
		t.Fatalf("Error = %q, want empty", got.Error)
	}
	if got.ProjectPath != "/repo/one" {
		t.Fatalf("ProjectPath = %q, want /repo/one", got.ProjectPath)
	}
	if !strings.Contains(got.Hint, "p1") || !strings.Contains(got.Hint, "/repo/one") {
		t.Fatalf("Hint = %q, want project id and path", got.Hint)
	}
}

func TestResolveCreateSessionProjectIDNotFound(t *testing.T) {
	cfg := corelib.AppConfig{Projects: []corelib.ProjectConfig{
		{Id: "p1", Name: "One", Path: "/repo/one"},
	}}

	got := resolveCreateSessionProjectID(cfg, "missing")
	if !strings.Contains(got.Error, "missing") || !strings.Contains(got.Error, "p1(One)") {
		t.Fatalf("Error = %q, want missing id and available project", got.Error)
	}
}

func TestResolveCreateSessionProjectIDNoProjects(t *testing.T) {
	got := resolveCreateSessionProjectID(corelib.AppConfig{}, "missing")
	if !strings.Contains(got.Error, "当前没有已配置的项目") {
		t.Fatalf("Error = %q, want no projects message", got.Error)
	}
}

func TestResolveCreateSessionProjectIDEmptyNoop(t *testing.T) {
	got := resolveCreateSessionProjectID(corelib.AppConfig{}, "")
	if got != (createSessionProjectIDResolution{}) {
		t.Fatalf("empty project id resolution = %#v, want zero value", got)
	}
}

func TestResolveCreateSessionProjectSelectionKeepsExplicitPathWithoutProjectID(t *testing.T) {
	got := resolveCreateSessionProjectSelection(corelib.AppConfig{}, "", "/explicit")

	if got.Error != "" {
		t.Fatalf("Error = %q, want empty", got.Error)
	}
	if got.ProjectPath != "/explicit" {
		t.Fatalf("ProjectPath = %q, want /explicit", got.ProjectPath)
	}
	if len(got.Hints) != 0 {
		t.Fatalf("Hints = %#v, want empty", got.Hints)
	}
}

func TestResolveCreateSessionProjectSelectionProjectIDOverridesPath(t *testing.T) {
	cfg := corelib.AppConfig{Projects: []corelib.ProjectConfig{
		{Id: "p1", Name: "One", Path: "/resolved"},
	}}

	got := resolveCreateSessionProjectSelection(cfg, "p1", "/explicit")

	if got.Error != "" {
		t.Fatalf("Error = %q, want empty", got.Error)
	}
	if got.ProjectPath != "/resolved" {
		t.Fatalf("ProjectPath = %q, want /resolved", got.ProjectPath)
	}
	if len(got.Hints) != 1 {
		t.Fatalf("Hints = %#v, want one project_id hint", got.Hints)
	}
}
