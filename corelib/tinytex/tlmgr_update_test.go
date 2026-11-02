package tinytex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClassifyTlmgrSelfUpdate(t *testing.T) {
	banner := strings.Join([]string{
		"===============================================================================",
		"tlmgr itself needs to be updated.",
		"Please do this via either",
		"  tlmgr update --self",
		"or by getting the latest updater for Unix-ish systems:",
		"  https://mirror.ctan.org/systems/texlive/tlnet/update-tlmgr-latest.sh",
		"and/or Windows systems:",
		"  https://mirror.ctan.org/systems/texlive/tlnet/update-tlmgr-latest.exe",
		"Then continue with other updates as usual.",
		"===============================================================================",
		"tlmgr.pl: package repository https://ctan.math.illinois.edu/systems/texlive/tlnet (not verified: gpg unavailable)",
		"tlmgr.pl: Terminating; please see warning above!",
	}, "\n")
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 255"), banner); got != TlmgrOfficialUpdater {
		t.Fatalf("banner recovery = %v", got)
	}
	if got := ClassifyTlmgrSelfUpdate(nil, banner); got != TlmgrReady {
		t.Fatalf("zero exit = %v", got)
	}
	// updater-w32.bat :panic exits 666. The "failed self restore" lines go to
	// the console, so the captured transcript is only perl's preamble.
	panicOut := "tlmgr.pl: Preparing TeX Live infrastructure update...\n"
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 666"), panicOut); got != TlmgrOfficialUpdater {
		t.Fatalf("windows infra panic = %v", got)
	}
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("tlmgr update --self from https://example: exit status 666: %s", panicOut), panicOut); got != TlmgrOfficialUpdater {
		t.Fatalf("wrapped windows infra panic = %v", got)
	}
	// :rollback and the missing-tar path are exit /b 1. Perl has already
	// printed the preparing line and did not abort, so the script ran.
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 1"), panicOut); got != TlmgrOfficialUpdater {
		t.Fatalf("rollback exit = %v", got)
	}
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 1"), panicOut+"tlmgr.pl: Aborting infrastructure update.\n"); got != TlmgrNextMirror {
		t.Fatalf("perl abort = %v", got)
	}
	rename := panicOut + "tlmgr.bat: failed to rename \"C:\\tex\\temp\\updater-w32\"\n"
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 1"), rename); got != TlmgrNextMirror {
		t.Fatalf("rename refusal = %v", got)
	}
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 6660"), panicOut); got != TlmgrNextMirror {
		t.Fatalf("unrelated exit = %v", got)
	}
	// The gpg warning is not the failure. A dead mirror stays on the next mirror.
	gpg := "tlmgr.pl: package repository https://mirror.ctan.org/systems/texlive/tlnet (not verified: gpg unavailable)\ncannot contact mirror"
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 1"), gpg); got != TlmgrNextMirror {
		t.Fatalf("gpg warning = %v", got)
	}
	cross := "Local TeX Live (2025) is older than remote repository (2026).\nCross release updates are only supported with\n  update-tlmgr-latest(.sh/.exe) -- --upgrade\n"
	if got := ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status 1"), cross); got != TlmgrOfficialUpgrade {
		t.Fatalf("cross release = %v", got)
	}
	if got := ClassifyTlmgrInstallFailure("Unknown directive foo"); got != TlmgrOfficialUpdater {
		t.Fatalf("unknown directive = %v", got)
	}
	// A clipped UI tail can lose the first line of the banner.
	clipped := "https://mirror.ctan.org/systems/texlive/tlnet/update-tlmgr-latest.exe\nThen continue with other updates as usual.\n===============================================================================\ntlmgr.pl: Terminating; please see warning above!"
	if got := ClassifyTlmgrInstallFailure(clipped); got != TlmgrOfficialUpdater {
		t.Fatalf("clipped tail = %v", got)
	}
}

func TestTlmgrUpdaterURL(t *testing.T) {
	got, err := TlmgrUpdaterURL("https://mirror.ctan.org/systems/texlive/tlnet/", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://mirror.ctan.org/systems/texlive/tlnet/update-tlmgr-latest.exe" {
		t.Fatalf("url = %s", got)
	}
	if _, err := TlmgrUpdaterURL("http://mirror.ctan.org/systems/texlive/tlnet", "windows"); err == nil {
		t.Fatal("http repository was accepted")
	}
	if args := TlmgrUpdaterArgs(false); args != nil {
		t.Fatalf("same-year args = %#v", args)
	}
	args := TlmgrUpdaterArgs(true)
	if strings.Join(args, " ") != "-- --upgrade" {
		t.Fatalf("upgrade args = %#v", args)
	}
}

func TestUpdaterOKRejectsHTML(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "update-tlmgr-latest.sh")
	body := make([]byte, minArchiveBytes+8)
	copy(body, []byte("<html>not an updater</html>"))
	if err := os.WriteFile(html, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdaterOK(html); err == nil {
		t.Fatal("html updater was accepted")
	}
	script := filepath.Join(dir, "ok.sh")
	copy(body, []byte("#!/bin/sh\n"))
	if err := os.WriteFile(script, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdaterOK(script); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "update-tlmgr-latest.exe")
	exeBody := make([]byte, minArchiveBytes+8)
	exeBody[0], exeBody[1] = 'M', 'Z'
	if err := os.WriteFile(exe, exeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdaterOK(exe); err != nil {
		t.Fatal(err)
	}
}

func TestInstallPackageUpdatesTlmgrWhenTheGuardFires(t *testing.T) {
	tlmgr := filepath.Join("texlive", "bin", "tlmgr")
	var calls []string
	run := func(_ context.Context, _, dir string, args ...string) (string, int, error) {
		if dir != filepath.Dir(tlmgr) {
			t.Errorf("dir = %q", dir)
		}
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if line == "install amsmath" && len(calls) == 1 {
			return "tlmgr itself needs to be updated.\ntlmgr.pl: Terminating; please see warning above!", 255, nil
		}
		return "", 0, nil
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "|") != "install amsmath|update --self|install amsmath" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageLeavesACurrentTlmgrAlone(t *testing.T) {
	var calls []string
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		calls = append(calls, strings.Join(args, " "))
		return "installed", 0, nil
	}
	if err := InstallPackage(context.Background(), run, "tlmgr", "amsmath"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "|") != "install amsmath" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageDoesNotSelfUpdateOnAMirrorError(t *testing.T) {
	var calls []string
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		calls = append(calls, strings.Join(args, " "))
		return "cannot contact mirror\nnot verified: gpg unavailable", 1, nil
	}
	err := InstallPackage(context.Background(), run, "tlmgr", "amsmath")
	if err == nil || !strings.Contains(err.Error(), "exited 1") || !strings.Contains(err.Error(), "cannot contact mirror") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(calls, "|") != "install amsmath" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackagePinsTheSavedRepositoryBeforeSelfUpdate(t *testing.T) {
	prev := lookupTLNet
	t.Cleanup(func() { lookupTLNet = prev })
	const pinned = "https://ctan.math.illinois.edu/systems/texlive/tlnet"
	var seen string
	lookupTLNet = func(_ context.Context, repo string) string {
		seen = repo
		if strings.Contains(repo, "mirror.ctan.org") {
			return pinned
		}
		return repo
	}
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	if err := os.MkdirAll(filepath.Dir(tlmgr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tlmgr, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	tlpdb := filepath.Join(dist, "tlpkg", "texlive.tlpdb")
	if err := os.MkdirAll(filepath.Dir(tlpdb), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"name 00texlive.config",
		"name 00texlive.installation",
		"depend opt_location:https://mirror.ctan.org/systems/texlive/tlnet",
		"name next",
		"depend opt_location:https://evil.example/systems/texlive/tlnet",
		"",
	}, "\n")
	if err := os.WriteFile(tlpdb, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	run := func(_ context.Context, _, dir string, args ...string) (string, int, error) {
		if dir != filepath.Dir(tlmgr) {
			t.Errorf("dir = %s", dir)
		}
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if line == "install amsmath" && strings.Count(strings.Join(calls, "\n"), "install amsmath") == 1 {
			return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
		}
		return "", 0, nil
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if seen != "https://mirror.ctan.org/systems/texlive/tlnet" {
		t.Fatalf("saved repository = %s", seen)
	}
	want := "install amsmath|option repository " + pinned + "|update --self|install amsmath"
	if strings.Join(calls, "|") != want {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageLeavesAConcreteRepositoryAlone(t *testing.T) {
	prev := lookupTLNet
	t.Cleanup(func() { lookupTLNet = prev })
	lookupTLNet = func(_ context.Context, repo string) string { return repo }
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tlmgr), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := "https://mirrors.tuna.tsinghua.edu.cn/CTAN/systems/texlive/tlnet"
	body := "name 00texlive.installation\ndepend opt_location:" + repo + "\n"
	if err := os.WriteFile(filepath.Join(dist, "tlpkg", "texlive.tlpdb"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if line == "install amsmath" && len(calls) == 1 {
			return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
		}
		return "", 0, nil
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "|") != "install amsmath|update --self|install amsmath" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageReportsSelfUpdateWhenTheOfficialUpdaterIsUnset(t *testing.T) {
	prev := applyOfficialUpdater
	t.Cleanup(func() { applyOfficialUpdater = prev })
	applyOfficialUpdater = nil
	var calls []string
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if line == "update --self" {
			return "tlmgr.pl: Preparing TeX Live infrastructure update...\n", 1, nil
		}
		return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
	}
	err := InstallPackage(context.Background(), run, "tlmgr", "amsmath")
	if err == nil || !strings.Contains(err.Error(), "update --self exited 1") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(calls, "|") != "install amsmath|update --self" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageRunsTheOfficialUpdaterAfterWindowsRollback(t *testing.T) {
	prevLookup := lookupTLNet
	prevApply := applyOfficialUpdater
	t.Cleanup(func() {
		lookupTLNet = prevLookup
		applyOfficialUpdater = prevApply
	})
	const repo = "https://mirrors.tuna.tsinghua.edu.cn/CTAN/systems/texlive/tlnet"
	lookupTLNet = func(_ context.Context, got string) string { return got }
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tlmgr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tlmgr, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "name 00texlive.installation\ndepend opt_location:" + repo + "\n"
	if err := os.WriteFile(filepath.Join(dist, "tlpkg", "texlive.tlpdb"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotRepo string
	var upgrade bool
	applyOfficialUpdater = func(_ context.Context, gotDist, gotTlmgr, got string, up bool) error {
		if gotDist != dist || gotTlmgr != tlmgr {
			t.Errorf("updater target dist=%s tlmgr=%s", gotDist, gotTlmgr)
		}
		gotRepo = got
		upgrade = up
		return nil
	}
	var calls []string
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		switch {
		case line == "install amsmath" && strings.Count(strings.Join(calls, "\n"), "install amsmath") == 1:
			return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
		case line == "update --self":
			return "tlmgr.pl: Preparing TeX Live infrastructure update...\n", 1, nil
		default:
			return "", 0, nil
		}
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if upgrade || gotRepo != repo {
		t.Fatalf("upgrade=%v repo=%s", upgrade, gotRepo)
	}
	if strings.Join(calls, "|") != "install amsmath|update --self|install amsmath" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageRunsTheOfficialUpdaterWhenSelfUpdateDoesNotClearTheGuard(t *testing.T) {
	prevLookup := lookupTLNet
	prevApply := applyOfficialUpdater
	t.Cleanup(func() {
		lookupTLNet = prevLookup
		applyOfficialUpdater = prevApply
	})
	lookupTLNet = func(_ context.Context, repo string) string { return repo }
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tlmgr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tlmgr, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := "https://mirrors.tuna.tsinghua.edu.cn/CTAN/systems/texlive/tlnet"
	if err := os.WriteFile(filepath.Join(dist, "tlpkg", "texlive.tlpdb"), []byte("name 00texlive.installation\ndepend opt_location:"+repo+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var updates int
	applyOfficialUpdater = func(context.Context, string, string, string, bool) error {
		updates++
		return nil
	}
	var calls []string
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if line == "update --self" {
			return "", 0, nil
		}
		if strings.Count(strings.Join(calls, "\n"), "install amsmath") < 3 {
			return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
		}
		return "", 0, nil
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if updates != 1 {
		t.Fatalf("official updater runs = %d", updates)
	}
	if strings.Join(calls, "|") != "install amsmath|update --self|install amsmath|install amsmath" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestInstallPackageOfficialUpdaterKeepsThePinnedRepository(t *testing.T) {
	prevLookup := lookupTLNet
	prevApply := applyOfficialUpdater
	t.Cleanup(func() {
		lookupTLNet = prevLookup
		applyOfficialUpdater = prevApply
	})
	var lookups int
	lookupTLNet = func(_ context.Context, _ string) string {
		lookups++
		if lookups == 1 {
			return "https://ctan.math.illinois.edu/systems/texlive/tlnet"
		}
		return "https://mirrors.example/CTAN/systems/texlive/tlnet"
	}
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tlmgr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tlmgr, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := "https://mirror.ctan.org/systems/texlive/tlnet"
	if err := os.WriteFile(filepath.Join(dist, "tlpkg", "texlive.tlpdb"), []byte("name 00texlive.installation\ndepend opt_location:"+saved+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	applyOfficialUpdater = func(_ context.Context, _, _, repo string, _ bool) error {
		got = repo
		return nil
	}
	var installs int
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		line := strings.Join(args, " ")
		switch {
		case line == "install amsmath":
			installs++
			if installs == 1 {
				return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
			}
			return "", 0, nil
		case line == "update --self":
			return "tlmgr.pl: Preparing TeX Live infrastructure update...\n", 1, nil
		default:
			return "", 0, nil
		}
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if lookups != 1 || got != "https://ctan.math.illinois.edu/systems/texlive/tlnet" {
		t.Fatalf("lookups=%d repo=%s", lookups, got)
	}
}

func TestInstallPackageOfficialUpdaterKeepsTheSavedRepositoryWhenPinFails(t *testing.T) {
	prevLookup := lookupTLNet
	prevApply := applyOfficialUpdater
	t.Cleanup(func() {
		lookupTLNet = prevLookup
		applyOfficialUpdater = prevApply
	})
	const saved = "https://mirror.ctan.org/systems/texlive/tlnet"
	lookupTLNet = func(_ context.Context, _ string) string {
		return "https://ctan.math.illinois.edu/systems/texlive/tlnet"
	}
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tlmgr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tlmgr, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "tlpkg", "texlive.tlpdb"), []byte("name 00texlive.installation\ndepend opt_location:"+saved+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	applyOfficialUpdater = func(_ context.Context, _, _, repo string, _ bool) error {
		got = repo
		return nil
	}
	var installs int
	var pinned bool
	run := func(_ context.Context, _, _ string, args ...string) (string, int, error) {
		line := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(line, "option repository"):
			pinned = true
			return "cannot write tlpdb", 1, nil
		case line == "install amsmath":
			installs++
			if installs == 1 {
				return "tlmgr itself needs to be updated.\nTerminating; please see warning above!", 255, nil
			}
			return "", 0, nil
		case line == "update --self":
			return "tlmgr.pl: Preparing TeX Live infrastructure update...\n", 1, nil
		default:
			return "", 0, nil
		}
	}
	if err := InstallPackage(context.Background(), run, tlmgr, "amsmath"); err != nil {
		t.Fatal(err)
	}
	if !pinned || got != saved {
		t.Fatalf("pinned=%v repo=%s", pinned, got)
	}
}

func TestTlmgrOutputStaleUpdaterMatchesTheBatchRefusal(t *testing.T) {
	if !TlmgrOutputStaleUpdater("tlmgr.bat: failed to remove previous updater script") {
		t.Fatal("remove refusal was treated as a mirror error")
	}
	if !TlmgrOutputStaleUpdater(`tlmgr.bat: failed to rename "C:\TinyTeX\temp\updater-w32"`) {
		t.Fatal("rename refusal was treated as a mirror error")
	}
	if TlmgrOutputStaleUpdater("failed to rename package") {
		t.Fatal("unrelated rename was treated as the updater lock")
	}
}

func TestClearStaleTlmgrUpdaterRemovesAReadOnlyScript(t *testing.T) {
	dist := t.TempDir()
	temp := filepath.Join(dist, "temp")
	if err := os.MkdirAll(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"updater-w32", "updater-w32.bat"} {
		if err := os.WriteFile(filepath.Join(temp, name), []byte("stale"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearStaleTlmgrUpdater(dist); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"updater-w32", "updater-w32.bat"} {
		_, statErr := os.Stat(filepath.Join(temp, name))
		if runtime.GOOS == "windows" {
			if !os.IsNotExist(statErr) {
				t.Fatalf("read-only %s still present (%v)", name, statErr)
			}
			continue
		}
		if statErr != nil {
			t.Fatalf("non-windows clear removed %s: %v", name, statErr)
		}
	}
}
