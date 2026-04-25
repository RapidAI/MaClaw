package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistCraftedSkill_Basic(t *testing.T) {
	root := t.TempDir()

	result, err := PersistCraftedSkill(root,
		"Convert markdown to PDF using weasyprint",
		"import weasyprint\nweasyprint.HTML('input.md').write_pdf('output.pdf')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkillName == "" {
		t.Error("SkillName should not be empty")
	}
	if result.IsUpdate {
		t.Error("first persist should not be an update")
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(result.SkillDir, "skill.yaml")); err != nil {
		t.Errorf("skill.yaml should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.SkillDir, "main.py")); err != nil {
		t.Errorf("main.py should exist: %v", err)
	}
}

func TestPersistCraftedSkill_Deduplication(t *testing.T) {
	root := t.TempDir()

	// First persist.
	r1, err := PersistCraftedSkill(root,
		"Convert markdown file to PDF document using weasyprint library",
		"print('v1')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Add a few more skills to make BM25 scoring meaningful (IDF needs corpus).
	PersistCraftedSkill(root, "Generate QR code from URL string", "print('qr')", "python")
	PersistCraftedSkill(root, "Resize image to thumbnail size", "print('img')", "python")

	// Second persist with very similar description — should update.
	r2, err := PersistCraftedSkill(root,
		"Convert markdown file to PDF document using weasyprint",
		"print('v2')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !r2.IsUpdate {
		t.Error("second persist with similar description should be an update")
	}
	if r2.SkillDir != r1.SkillDir {
		t.Errorf("update should use same dir: got %q, want %q", r2.SkillDir, r1.SkillDir)
	}

	// Verify script was updated.
	content, _ := os.ReadFile(filepath.Join(r2.SkillDir, "main.py"))
	if string(content) != "print('v2')" {
		t.Errorf("script should be updated to v2, got %q", string(content))
	}
}

func TestPersistCraftedSkill_DifferentTasks(t *testing.T) {
	root := t.TempDir()

	r1, err := PersistCraftedSkill(root,
		"Convert markdown to PDF",
		"print('pdf')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := PersistCraftedSkill(root,
		"Generate QR code from URL",
		"print('qr')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	if r2.IsUpdate {
		t.Error("completely different task should not be an update")
	}
	if r2.SkillDir == r1.SkillDir {
		t.Error("different tasks should have different dirs")
	}
}

func TestPersistCraftedSkill_EmptyInputs(t *testing.T) {
	root := t.TempDir()

	_, err := PersistCraftedSkill("", "desc", "code", "python")
	if err == nil {
		t.Error("empty skillsRoot should error")
	}

	_, err = PersistCraftedSkill(root, "", "code", "python")
	if err == nil {
		t.Error("empty taskDescription should error")
	}

	_, err = PersistCraftedSkill(root, "desc", "", "python")
	if err == nil {
		t.Error("empty scriptContent should error")
	}
}

func TestPersistCraftedSkill_ScriptLanguages(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		lang    string
		wantExt string
	}{
		{"python", ".py"},
		{"py", ".py"},
		{"node", ".js"},
		{"javascript", ".js"},
		{"bash", ".sh"},
		{"unknown", ".py"}, // default
	}

	for _, tt := range tests {
		result, err := PersistCraftedSkill(root,
			"task for "+tt.lang,
			"echo hello",
			tt.lang,
		)
		if err != nil {
			t.Errorf("lang=%s: %v", tt.lang, err)
			continue
		}
		scriptPath := filepath.Join(result.SkillDir, "main"+tt.wantExt)
		if _, err := os.Stat(scriptPath); err != nil {
			t.Errorf("lang=%s: expected main%s to exist", tt.lang, tt.wantExt)
		}
	}
}

func TestCraftedSkillName(t *testing.T) {
	tests := []struct {
		desc     string
		wantExact string // exact match when non-empty; empty means just check non-empty
	}{
		{"Convert markdown to PDF", "Convert-markdown-to-PDF"},
		{"", ""},  // empty desc → timestamp-based fallback, can't predict exact value
		{"a very long description that exceeds the forty character limit for skill names", "a-very-long-description-that-exceeds-the"},
	}

	for _, tt := range tests {
		got := craftedSkillName(tt.desc)
		if tt.desc == "" {
			if got == "" {
				t.Error("empty desc should produce timestamp-based name")
			}
			continue
		}
		if got == "" {
			t.Errorf("desc=%q: name should not be empty", tt.desc)
		}
		if tt.wantExact != "" && got != tt.wantExact {
			t.Errorf("craftedSkillName(%q) = %q, want %q", tt.desc, got, tt.wantExact)
		}
		if len([]rune(got)) > 80 {
			t.Errorf("desc=%q: name too long: %d runes", tt.desc, len([]rune(got)))
		}
	}
}

func TestIsRepairableError(t *testing.T) {
	tests := []struct {
		errorClass string
		want       bool
	}{
		{"file_not_found", true},
		{"command_not_found", true},
		{"timeout", true},
		{"session_not_found", true},
		{"unknown", true},
		{"", true},
		// External/transient errors — not fixable by modifying skill steps.
		{"rate_limit", false},
		{"network_error", false},
		{"auth_error", false},
		{"missing_env_var", false},
	}

	for _, tt := range tests {
		got := IsRepairableError(tt.errorClass)
		if got != tt.want {
			t.Errorf("IsRepairableError(%q) = %v, want %v", tt.errorClass, got, tt.want)
		}
	}
}
