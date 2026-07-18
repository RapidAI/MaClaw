package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestInjectOwnerWorkDirEnvDoesNotOverrideCaller(t *testing.T) {
	r := &SkillRunner{}
	custom := filepath.Join(t.TempDir(), "custom")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	callerTemp := filepath.Join(t.TempDir(), "caller-temp")
	if err := os.MkdirAll(callerTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	got := r.injectOwnerWorkDirEnv("desktop-user", map[string]string{
		"MACLAW_WORKDIR": custom,
		"TEMP":           callerTemp,
	})
	if filepath.Clean(got["MACLAW_WORKDIR"]) != filepath.Clean(custom) {
		t.Fatalf("caller workdir override lost: %#v", got)
	}
	if got["TEMP"] != callerTemp {
		t.Fatalf("caller TEMP override lost: %#v", got)
	}
	if got["TMP"] != "" || got["TMPDIR"] != "" {
		t.Fatalf("expected TMP/TMPDIR left empty when caller set TEMP, got %#v", got)
	}
}

func TestInjectOwnerWorkDirEnvRedirectsTempUnderWorkdir(t *testing.T) {
	tempHome := t.TempDir()
	workDir := filepath.Join(tempHome, "workbench")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	corelib.SetWorkspaceDir(workDir)
	t.Cleanup(func() { corelib.SetWorkspaceDir("") })

	app := &App{testHomeDir: tempHome}
	r := &SkillRunner{executor: NewSkillExecutor(app, nil, nil)}
	got := r.injectOwnerWorkDirEnv(desktopUserID, nil)

	wantWork := filepath.Clean(workDir)
	if filepath.Clean(got["MACLAW_WORKDIR"]) != wantWork {
		t.Fatalf("MACLAW_WORKDIR=%q want %q", got["MACLAW_WORKDIR"], wantWork)
	}
	wantTmp := filepath.Join(wantWork, ".maclaw-tmp")
	for _, key := range []string{"TEMP", "TMP", "TMPDIR"} {
		if filepath.Clean(got[key]) != wantTmp {
			t.Fatalf("%s=%q want %q", key, got[key], wantTmp)
		}
	}
	if _, err := os.Stat(filepath.Join(wantTmp, ".gitignore")); err != nil {
		t.Fatalf(".maclaw-tmp/.gitignore missing: %v", err)
	}
}

func TestInjectOwnerWorkDirEnvCallerWorkdirGetsTempRedirect(t *testing.T) {
	r := &SkillRunner{}
	custom := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	got := r.injectOwnerWorkDirEnv("desktop-user", map[string]string{
		"MACLAW_WORKDIR": custom,
	})
	wantTmp := filepath.Join(filepath.Clean(custom), ".maclaw-tmp")
	for _, key := range []string{"TEMP", "TMP", "TMPDIR"} {
		if filepath.Clean(got[key]) != wantTmp {
			t.Fatalf("%s=%q want %q full=%#v", key, got[key], wantTmp, got)
		}
	}
}

func TestInjectOwnerWorkDirEnvIgnoresInvalidCallerWorkdir(t *testing.T) {
	r := &SkillRunner{}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got := r.injectOwnerWorkDirEnv("desktop-user", map[string]string{
		"MACLAW_WORKDIR": missing,
	})
	if got["TEMP"] != "" || got["TMP"] != "" || got["TMPDIR"] != "" {
		t.Fatalf("invalid workdir should not redirect TEMP: %#v", got)
	}
}

func TestSkillRunWorkspaceRootFromEnvUsesWorkdir(t *testing.T) {
	workDir := t.TempDir()
	// Ensure env has been through inject so keys are consistent.
	env := cskill.InjectSkillWorkDirEnv(workDir, nil)
	root := skillRunWorkspaceRootFromEnv(env)
	want := filepath.Join(workDir, ".maclaw-tmp", "skill-runs")
	if filepath.Clean(root) != filepath.Clean(want) {
		t.Fatalf("root=%q want %q", root, want)
	}
}

func TestSkillRunWorkspaceRootFromEnvFallsBackToSystemTemp(t *testing.T) {
	root := skillRunWorkspaceRootFromEnv(nil)
	sys := os.TempDir()
	if runtime.GOOS == "windows" {
		if filepath.VolumeName(root) != filepath.VolumeName(sys) {
			t.Fatalf("root=%q not on same volume as TempDir=%q", root, sys)
		}
	}
	if filepath.Base(root) != "maclaw-skill-runs" {
		t.Fatalf("unexpected fallback root %q", root)
	}
}
