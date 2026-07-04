package maclawpath

import (
	"path/filepath"
	"testing"
)

func TestDerivedDirsUseEffectiveBaseDir(t *testing.T) {
	oldBase := BaseDir()
	base := t.TempDir()
	SetBaseDir(base)
	t.Cleanup(func() { SetBaseDir(oldBase) })

	if got := DataDir(); got != filepath.Join(base, "data") {
		t.Fatalf("DataDir() = %q, want %q", got, filepath.Join(base, "data"))
	}
	if got := LogsDir(); got != filepath.Join(base, "logs") {
		t.Fatalf("LogsDir() = %q, want %q", got, filepath.Join(base, "logs"))
	}
	if got := SkillsDir(); got != filepath.Join(base, "data", "skills") {
		t.Fatalf("SkillsDir() = %q, want %q", got, filepath.Join(base, "data", "skills"))
	}
	if got := RuntimeDir(); got != filepath.Join(base, "data", "runtimes") {
		t.Fatalf("RuntimeDir() = %q, want %q", got, filepath.Join(base, "data", "runtimes"))
	}
	if got := AppOutputsDir(); got != filepath.Join(base, "data", "app-outputs") {
		t.Fatalf("AppOutputsDir() = %q, want %q", got, filepath.Join(base, "data", "app-outputs"))
	}
}
