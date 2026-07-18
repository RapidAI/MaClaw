package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInjectSkillWorkDirEnvRedirectsTemp(t *testing.T) {
	wd := t.TempDir()
	got := InjectSkillWorkDirEnv(wd, nil)
	if filepath.Clean(got["MACLAW_WORKDIR"]) != filepath.Clean(wd) {
		t.Fatalf("WORKDIR=%q want %q", got["MACLAW_WORKDIR"], wd)
	}
	wantTmp := filepath.Join(filepath.Clean(wd), ".maclaw-tmp")
	for _, key := range []string{"TEMP", "TMP", "TMPDIR"} {
		if filepath.Clean(got[key]) != wantTmp {
			t.Fatalf("%s=%q want %q", key, got[key], wantTmp)
		}
	}
	if _, err := os.Stat(filepath.Join(wantTmp, ".gitignore")); err != nil {
		t.Fatalf("gitignore: %v", err)
	}
}

func TestInjectSkillWorkDirEnvPreservesCallerTempAtomically(t *testing.T) {
	wd := t.TempDir()
	callerTemp := t.TempDir()
	got := InjectSkillWorkDirEnv(wd, map[string]string{
		"Temp": callerTemp, // Windows-style case
	})
	if got["TEMP"] != callerTemp && got["Temp"] != callerTemp {
		// After set, key is normalized to TEMP via envMapSet only when we set —
		// caller key may remain "Temp".
		if envMapGet(got, "TEMP") != callerTemp {
			t.Fatalf("caller TEMP lost: %#v", got)
		}
	}
	if envMapGet(got, "TMP") != "" || envMapGet(got, "TMPDIR") != "" {
		t.Fatalf("must not partial-fill TMP/TMPDIR: %#v", got)
	}
}

func TestInjectSkillWorkDirEnvIgnoresMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	got := InjectSkillWorkDirEnv(missing, nil)
	if envMapGet(got, "TEMP") != "" {
		t.Fatalf("missing workdir must not set TEMP: %#v", got)
	}
}

func TestSkillWorkdirTmpSubdir(t *testing.T) {
	wd := t.TempDir()
	root := SkillWorkdirTmpSubdir(map[string]string{"MACLAW_WORKDIR": wd}, "skill-runs")
	want := filepath.Join(wd, MaclawSkillTmpDirName, "skill-runs")
	if filepath.Clean(root) != filepath.Clean(want) {
		t.Fatalf("root=%q want %q", root, want)
	}
	// Creating skill-runs should also ensure parent gitignore.
	if _, err := os.Stat(filepath.Join(wd, MaclawSkillTmpDirName, ".gitignore")); err != nil {
		t.Fatalf("gitignore via SkillWorkdirTmpSubdir: %v", err)
	}
}

func TestInjectSkillWorkDirEnvPrefersValidEnvOverArg(t *testing.T) {
	fromEnv := t.TempDir()
	fromArg := t.TempDir()
	got := InjectSkillWorkDirEnv(fromArg, map[string]string{
		"MACLAW_WORKDIR": fromEnv,
	})
	if filepath.Clean(got["MACLAW_WORKDIR"]) != filepath.Clean(fromEnv) {
		t.Fatalf("preferred env workdir lost: %#v", got)
	}
}

func TestInjectSkillWorkDirEnvInvalidEnvFallsBackToArg(t *testing.T) {
	fromArg := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	got := InjectSkillWorkDirEnv(fromArg, map[string]string{
		"MACLAW_WORKDIR": missing,
	})
	if filepath.Clean(got["MACLAW_WORKDIR"]) != filepath.Clean(fromArg) {
		t.Fatalf("should fall back to arg workdir: %#v", got)
	}
}

func TestInjectSkillWorkDirEnvIdempotent(t *testing.T) {
	wd := t.TempDir()
	first := InjectSkillWorkDirEnv(wd, nil)
	second := InjectSkillWorkDirEnv(wd, first)
	if filepath.Clean(first["TEMP"]) != filepath.Clean(second["TEMP"]) {
		t.Fatalf("idempotent TEMP mismatch: %#v vs %#v", first, second)
	}
	if filepath.Clean(second["MACLAW_WORKDIR"]) != filepath.Clean(wd) {
		t.Fatalf("WORKDIR lost on re-inject: %#v", second)
	}
}
