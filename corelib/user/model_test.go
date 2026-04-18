package user

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewModel_FileNotExist(t *testing.T) {
	m, err := NewModel("/tmp/nonexistent_user_model_test.json")
	if err != nil {
		t.Fatalf("NewModel should not error for missing file: %v", err)
	}
	p := m.GetProfile()
	if p.CommunicationStyle.Value != "" {
		t.Errorf("expected empty communication_style, got %q", p.CommunicationStyle.Value)
	}
	if p.TechnicalLevel.Confidence != 0 {
		t.Errorf("expected 0 confidence, got %f", p.TechnicalLevel.Confidence)
	}
}

func TestNewModel_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "model.json")

	// Write a valid profile JSON
	content := `{
		"communication_style": {"value": "concise", "confidence": 0.8, "evidence": [], "user_confirmed": false},
		"technical_level": {"value": "senior", "confidence": 0.9, "evidence": [], "user_confirmed": true},
		"preferred_languages": {"value": "", "confidence": 0, "evidence": null, "user_confirmed": false},
		"domain_expertise": {"value": "", "confidence": 0, "evidence": null, "user_confirmed": false},
		"work_patterns": {"value": "", "confidence": 0, "evidence": null, "user_confirmed": false},
		"tool_preferences": {"value": "", "confidence": 0, "evidence": null, "user_confirmed": false}
	}`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewModel(fp)
	if err != nil {
		t.Fatalf("NewModel error: %v", err)
	}
	p := m.GetProfile()
	if p.CommunicationStyle.Value != "concise" {
		t.Errorf("expected 'concise', got %q", p.CommunicationStyle.Value)
	}
	if p.CommunicationStyle.Confidence != 0.8 {
		t.Errorf("expected 0.8, got %f", p.CommunicationStyle.Confidence)
	}
	if p.TechnicalLevel.UserConfirmed != true {
		t.Error("expected UserConfirmed=true for technical_level")
	}
}

func TestNewModel_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "model.json")
	if err := os.WriteFile(fp, []byte("not valid json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewModel(fp)
	if err != nil {
		t.Fatalf("NewModel should not error for corrupted JSON: %v", err)
	}
	p := m.GetProfile()
	if p.CommunicationStyle.Value != "" {
		t.Error("expected empty profile after corrupted JSON")
	}
}

func TestUpdateDimension_NewValue(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	ev := Evidence{Observation: "uses Go", Timestamp: time.Now(), Source: "pattern"}

	err := m.UpdateDimension("preferred_languages", "Go", ev)
	if err != nil {
		t.Fatal(err)
	}

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Go" {
		t.Errorf("expected 'Go', got %q", p.PreferredLanguages.Value)
	}
	if p.PreferredLanguages.Confidence != 0.5 {
		t.Errorf("expected 0.5, got %f", p.PreferredLanguages.Confidence)
	}
	if len(p.PreferredLanguages.Evidence) != 1 {
		t.Errorf("expected 1 evidence, got %d", len(p.PreferredLanguages.Evidence))
	}
}

func TestUpdateDimension_Reinforcement(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	ev1 := Evidence{Observation: "uses Go", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("preferred_languages", "Go", ev1)

	ev2 := Evidence{Observation: "Go again", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("preferred_languages", "Go", ev2)

	p := m.GetProfile()
	if p.PreferredLanguages.Confidence <= 0.5 {
		t.Errorf("expected confidence > 0.5 after reinforcement, got %f", p.PreferredLanguages.Confidence)
	}
	if len(p.PreferredLanguages.Evidence) != 2 {
		t.Errorf("expected 2 evidences, got %d", len(p.PreferredLanguages.Evidence))
	}
}

func TestUpdateDimension_Contradiction(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	ev1 := Evidence{Observation: "uses Go", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("preferred_languages", "Go", ev1)

	initialConf := m.GetProfile().PreferredLanguages.Confidence

	ev2 := Evidence{Observation: "uses Python", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("preferred_languages", "Python", ev2)

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Errorf("expected 'Python', got %q", p.PreferredLanguages.Value)
	}
	if p.PreferredLanguages.Confidence >= initialConf {
		t.Errorf("expected confidence < %f after contradiction, got %f", initialConf, p.PreferredLanguages.Confidence)
	}
	if len(p.PreferredLanguages.Evidence) != 2 {
		t.Errorf("expected 2 evidences, got %d", len(p.PreferredLanguages.Evidence))
	}
}

func TestUpdateDimension_UnknownDimension(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	ev := Evidence{Observation: "test", Timestamp: time.Now(), Source: "pattern"}
	err := m.UpdateDimension("nonexistent_dimension", "value", ev)
	if err == nil {
		t.Error("expected error for unknown dimension")
	}
}

func TestCorrectDimension(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	// Set initial value
	ev := Evidence{Observation: "inferred", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("technical_level", "junior", ev)

	// User correction
	err := m.CorrectDimension("technical_level", "senior")
	if err != nil {
		t.Fatal(err)
	}

	p := m.GetProfile()
	if p.TechnicalLevel.Value != "senior" {
		t.Errorf("expected 'senior', got %q", p.TechnicalLevel.Value)
	}
	if p.TechnicalLevel.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", p.TechnicalLevel.Confidence)
	}
	if !p.TechnicalLevel.UserConfirmed {
		t.Error("expected UserConfirmed=true")
	}
}

func TestResetDimension(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	ev := Evidence{Observation: "test", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("domain_expertise", "backend", ev)

	err := m.ResetDimension("domain_expertise")
	if err != nil {
		t.Fatal(err)
	}

	p := m.GetProfile()
	if p.DomainExpertise.Value != "" {
		t.Errorf("expected empty value, got %q", p.DomainExpertise.Value)
	}
	if p.DomainExpertise.Confidence != 0 {
		t.Errorf("expected 0 confidence, got %f", p.DomainExpertise.Confidence)
	}
	if p.DomainExpertise.Evidence != nil {
		t.Errorf("expected nil evidence, got %v", p.DomainExpertise.Evidence)
	}
	if p.DomainExpertise.UserConfirmed {
		t.Error("expected UserConfirmed=false")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "subdir", "model.json")

	m, _ := NewModel(fp)
	ev := Evidence{Observation: "uses vim", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("tool_preferences", "vim", ev)
	m.CorrectDimension("communication_style", "verbose")

	err := m.Save()
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Load from same file
	m2, err := NewModel(fp)
	if err != nil {
		t.Fatalf("NewModel error: %v", err)
	}

	p := m2.GetProfile()
	if p.ToolPreferences.Value != "vim" {
		t.Errorf("expected 'vim', got %q", p.ToolPreferences.Value)
	}
	if p.CommunicationStyle.Value != "verbose" {
		t.Errorf("expected 'verbose', got %q", p.CommunicationStyle.Value)
	}
	if p.CommunicationStyle.Confidence != 1.0 {
		t.Errorf("expected 1.0, got %f", p.CommunicationStyle.Confidence)
	}
	if !p.CommunicationStyle.UserConfirmed {
		t.Error("expected UserConfirmed=true")
	}
}

func TestFormatForPrompt_Empty(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	result := m.FormatForPrompt()
	if result != "" {
		t.Errorf("expected empty string for empty profile, got %q", result)
	}
}

func TestFormatForPrompt_WithValues(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	m.CorrectDimension("communication_style", "concise")
	ev := Evidence{Observation: "test", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("technical_level", "senior", ev)

	result := m.FormatForPrompt()
	if result == "" {
		t.Error("expected non-empty prompt")
	}
	if !contains(result, "Communication Style") {
		t.Error("expected 'Communication Style' in output")
	}
	if !contains(result, "concise") {
		t.Error("expected 'concise' in output")
	}
	if !contains(result, "[confirmed]") {
		t.Error("expected '[confirmed]' for user-confirmed dimension")
	}
	if !contains(result, "Technical Level") {
		t.Error("expected 'Technical Level' in output")
	}
	if !contains(result, "senior") {
		t.Error("expected 'senior' in output")
	}
	// Empty dimensions should not appear
	if contains(result, "Domain Expertise") {
		t.Error("empty dimension should not appear in prompt")
	}
}

func TestGetProfile_ReturnsCopy(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	ev := Evidence{Observation: "test", Timestamp: time.Now(), Source: "pattern"}
	m.UpdateDimension("technical_level", "senior", ev)

	p := m.GetProfile()
	// Mutate the copy
	p.TechnicalLevel.Value = "mutated"
	p.TechnicalLevel.Evidence = append(p.TechnicalLevel.Evidence, Evidence{Observation: "extra"})

	// Original should be unchanged
	p2 := m.GetProfile()
	if p2.TechnicalLevel.Value != "senior" {
		t.Errorf("original was mutated: got %q", p2.TechnicalLevel.Value)
	}
	if len(p2.TechnicalLevel.Evidence) != 1 {
		t.Errorf("original evidence was mutated: got %d", len(p2.TechnicalLevel.Evidence))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
