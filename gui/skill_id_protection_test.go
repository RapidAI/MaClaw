package main

import "testing"

func TestCheckSkillIDChanged_Preserved(t *testing.T) {
	content := []byte("id: lovstudio.any2pdf\nname: Any2PDF\nsteps:\n  - action: bash\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result != "" {
		t.Errorf("expected empty (id preserved), got: %s", result)
	}
}

func TestCheckSkillIDChanged_Modified(t *testing.T) {
	content := []byte("id: hacker.evil\nname: Any2PDF\nsteps:\n  - action: bash\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result == "" {
		t.Fatal("expected error for modified id")
	}
}

func TestCheckSkillIDChanged_Removed(t *testing.T) {
	content := []byte("name: Any2PDF\nsteps:\n  - action: bash\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result == "" {
		t.Fatal("expected error for removed id")
	}
}

func TestCheckSkillIDChanged_EmptyValue(t *testing.T) {
	content := []byte("id: \nname: Any2PDF\nsteps:\n  - action: bash\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result == "" {
		t.Fatal("expected error for empty id value")
	}
}

func TestCheckSkillIDChanged_NestedIDIgnored(t *testing.T) {
	// Indented "id:" in a nested context should NOT be treated as the top-level id
	content := []byte("id: lovstudio.any2pdf\nname: Any2PDF\nsteps:\n  - action: bash\n    params:\n      id: some-internal-id\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result != "" {
		t.Errorf("nested id: should not trigger protection, got: %s", result)
	}
}

func TestCheckSkillIDChanged_QuotedValue(t *testing.T) {
	content := []byte("id: \"lovstudio.any2pdf\"\nname: Any2PDF\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result != "" {
		t.Errorf("quoted id should match, got: %s", result)
	}
}

func TestCheckSkillIDChanged_CaseInsensitive(t *testing.T) {
	content := []byte("id: Lovstudio.Any2PDF\nname: Any2PDF\n")
	result := checkSkillIDChanged(content, "lovstudio.any2pdf")
	if result != "" {
		t.Errorf("case-insensitive match should pass, got: %s", result)
	}
}
