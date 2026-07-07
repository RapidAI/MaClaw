package skill

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveDependency_ExactSkillIDMatch(t *testing.T) {
	installed := []corelib.NLSkillEntry{
		{SkillID: "lovstudio.any2pdf", Name: "Any2PDF", Version: "1.3.0"},
		{SkillID: "rapidai.ocr", Name: "OCR Tool", Version: "2.0.0"},
	}
	dep := SkillDependency{SkillID: "lovstudio.any2pdf", Version: ">=1.2.0", Required: true}
	result := ResolveDependency(dep, installed)

	if result.Resolved == nil {
		t.Fatal("expected resolved entry, got nil")
	}
	if result.Resolved.SkillID != "lovstudio.any2pdf" {
		t.Errorf("resolved wrong skill: %s", result.Resolved.SkillID)
	}
	if !result.Satisfied {
		t.Error("expected satisfied=true")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestResolveDependency_VersionTooOld(t *testing.T) {
	installed := []corelib.NLSkillEntry{
		{SkillID: "lovstudio.any2pdf", Name: "Any2PDF", Version: "1.0.0"},
	}
	dep := SkillDependency{SkillID: "lovstudio.any2pdf", Version: ">=1.2.0", Required: true}
	result := ResolveDependency(dep, installed)

	if result.Resolved == nil {
		t.Fatal("expected resolved entry")
	}
	if result.Satisfied {
		t.Error("expected satisfied=false (version too old)")
	}
	if !result.NeedsUpgrade {
		t.Error("expected NeedsUpgrade=true")
	}
	if result.Error == nil {
		t.Error("expected error for required dep with old version")
	}
}

func TestResolveDependency_NotFound(t *testing.T) {
	installed := []corelib.NLSkillEntry{
		{SkillID: "other.skill", Name: "Other", Version: "1.0.0"},
	}
	dep := SkillDependency{SkillID: "lovstudio.any2pdf", Required: true}
	result := ResolveDependency(dep, installed)

	if result.Resolved != nil {
		t.Error("expected nil resolved")
	}
	if result.Satisfied {
		t.Error("expected satisfied=false")
	}
	if result.Error == nil {
		t.Error("expected error for missing required dep")
	}
}

func TestResolveDependency_OptionalNotFound(t *testing.T) {
	installed := []corelib.NLSkillEntry{}
	dep := SkillDependency{SkillID: "lovstudio.any2pdf", Required: false}
	result := ResolveDependency(dep, installed)

	if result.Resolved != nil {
		t.Error("expected nil resolved")
	}
	if result.Error != nil {
		t.Error("expected no error for optional dep not found")
	}
}

func TestResolveDependency_AnyVersion(t *testing.T) {
	installed := []corelib.NLSkillEntry{
		{SkillID: "lovstudio.any2pdf", Name: "Any2PDF", Version: "0.1.0"},
	}
	dep := SkillDependency{SkillID: "lovstudio.any2pdf", Version: "*", Required: true}
	result := ResolveDependency(dep, installed)

	if !result.Satisfied {
		t.Error("wildcard version should be satisfied")
	}
}

func TestResolveDependency_FallbackToHubVersion(t *testing.T) {
	installed := []corelib.NLSkillEntry{
		{SkillID: "lovstudio.any2pdf", Name: "Any2PDF", Version: "", HubVersion: "1.5.0"},
	}
	dep := SkillDependency{SkillID: "lovstudio.any2pdf", Version: ">=1.2.0", Required: true}
	result := ResolveDependency(dep, installed)

	if !result.Satisfied {
		t.Error("should satisfy from HubVersion fallback")
	}
}

func TestResolveDependency_LegacyNameMatch(t *testing.T) {
	// Skill without SkillID but with matching Name
	installed := []corelib.NLSkillEntry{
		{Name: "any2pdf", Version: "1.0.0"},
	}
	dep := SkillDependency{SkillID: "any2pdf", Version: ">=1.0.0", Required: true}
	result := ResolveDependency(dep, installed)

	if result.Resolved == nil {
		t.Fatal("expected resolved via legacy name match")
	}
	if !result.Satisfied {
		t.Error("expected satisfied")
	}
}

func TestResolveDependencies_Multiple(t *testing.T) {
	installed := []corelib.NLSkillEntry{
		{SkillID: "lovstudio.any2pdf", Name: "Any2PDF", Version: "1.3.0"},
		{SkillID: "rapidai.ocr", Name: "OCR", Version: "2.1.0"},
	}
	deps := []SkillDependency{
		{SkillID: "lovstudio.any2pdf", Version: "^1.0.0", Required: true},
		{SkillID: "rapidai.ocr", Version: ">=2.0.0", Required: true},
		{SkillID: "missing.skill", Version: "*", Required: false},
	}
	results := ResolveDependencies(deps, installed)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Satisfied {
		t.Error("any2pdf should be satisfied")
	}
	if !results[1].Satisfied {
		t.Error("ocr should be satisfied")
	}
	if results[2].Satisfied {
		t.Error("missing should not be satisfied")
	}

	unresolved := UnresolvedDependencies(results)
	if len(unresolved) != 1 {
		t.Errorf("expected 1 unresolved, got %d", len(unresolved))
	}
}
