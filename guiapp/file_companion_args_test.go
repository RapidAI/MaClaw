package guiapp

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestClassifyDesktopLaunchEmptyArgvAndPSNStayOnMainLock(t *testing.T) {
	if singleInstanceUniqueID() != "maclaw-lock" {
		t.Fatalf("historical main lock changed: %q", singleInstanceUniqueID())
	}
	if fileCompanionLockID() != "maclaw-lock-file-companion" {
		t.Fatalf("companion lock = %q, want maclaw-lock-file-companion", fileCompanionLockID())
	}
	cases := [][]string{
		{"MaClaw"},
		{"MaClaw", "-psn_0_12345"},
		{"MaClaw", "", "-psn_0_1", ""},
	}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for _, args := range cases {
			got := classifyDesktopLaunchForOS(args, goos)
			if got.Mode != desktopLaunchMain || got.LockID != "maclaw-lock" {
				t.Fatalf("args=%q goos=%s stayed off the main lock: %#v", args, goos, got)
			}
			if len(got.Paths) != 0 || got.Explicit {
				t.Fatalf("args=%q goos=%s unexpected paths: %#v", args, goos, got)
			}
			// Darwin latch: empty filtered args hide the main window. Other OS do not.
			wantHidden := goos == "darwin"
			if got.StartHidden != wantHidden {
				t.Fatalf("args=%q goos=%s StartHidden=%v, want %v for the Darwin latch", args, goos, got.StartHidden, wantHidden)
			}
		}
	}
}

func TestClassifyDesktopLaunchOpenFileKeepsEveryPath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(real, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.md")
	got := classifyDesktopLaunchForOS([]string{"MaClaw", "open-file", real, missing}, "windows")
	if got.Mode != desktopLaunchFileCompanion || got.LockID != fileCompanionLockID() || !got.Explicit {
		t.Fatalf("open-file launch = %#v", got)
	}
	if got.StartHidden {
		t.Fatal("explicit open-file must not use the Darwin hidden latch")
	}
	if len(got.Paths) != 2 || got.Paths[0] != real || got.Paths[1] != missing {
		t.Fatalf("paths = %#v", got.Paths)
	}

	empty := classifyDesktopLaunchForOS([]string{"MaClaw", "-psn_0_9", "open-file"}, "darwin")
	if empty.Mode != desktopLaunchFileCompanion || empty.StartHidden || len(empty.Paths) != 0 {
		t.Fatalf("empty open-file = %#v", empty)
	}

	// A bad path still enters the companion so one failure does not drop the batch.
	bad := classifyDesktopLaunchForOS([]string{"MaClaw", "open-file", dir}, "linux")
	if bad.Mode != desktopLaunchFileCompanion || len(bad.Paths) != 1 || bad.Paths[0] != dir {
		t.Fatalf("directory open-file = %#v", bad)
	}
}

func TestClassifyDesktopLaunchDoesNotStealKnownSubcommands(t *testing.T) {
	dir := t.TempDir()
	initFile := filepath.Join(dir, "init")
	if err := os.WriteFile(initFile, []byte("not a subcommand file"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	cases := [][]string{
		{"MaClaw", "init"},
		{"MaClaw", "autostart"},
		{"MaClaw", "tui"},
		{"MaClaw", "UI"},
		{"MaClaw", "remote-smoke"},
		{"MaClaw", "generate-mobile-pwa-shell"},
		{"MaClaw", "generate-android-pwa-shell"},
		{"MaClaw", "maclaw://onboarding?referral_handoff=1"},
		{"MaClaw", "init", initFile},
	}
	for _, args := range cases {
		got := classifyDesktopLaunchForOS(args, "darwin")
		if got.Mode != desktopLaunchMain || got.LockID != singleInstanceUniqueID() || got.StartHidden {
			t.Fatalf("%q classified as %#v; known subcommand must stay on the main lock without the Darwin latch", args, got)
		}
	}
	// The relative token "init" is the subcommand even though ./init exists.
	if _, err := os.Stat("init"); err != nil {
		t.Fatalf("relative init file missing: %v", err)
	}
}

func TestClassifyDesktopLaunchImplicitFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(other, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirLaunch := classifyDesktopLaunchForOS([]string{"MaClaw", dir}, "linux")
	if dirLaunch.Mode != desktopLaunchMain {
		t.Fatalf("directory launch = %#v", dirLaunch)
	}
	mixed := classifyDesktopLaunchForOS([]string{"MaClaw", file, dir}, "darwin")
	if mixed.Mode != desktopLaunchMain || mixed.StartHidden {
		t.Fatalf("mixed file+dir launch = %#v", mixed)
	}
	missing := classifyDesktopLaunchForOS([]string{"MaClaw", filepath.Join(dir, "nope.txt")}, "windows")
	if missing.Mode != desktopLaunchMain {
		t.Fatalf("missing file launch = %#v", missing)
	}
	bare := classifyDesktopLaunchForOS([]string{"MaClaw", file, other}, "darwin")
	if bare.Mode != desktopLaunchFileCompanion || bare.Explicit || bare.StartHidden || bare.LockID != fileCompanionLockID() {
		t.Fatalf("darwin bare files = %#v", bare)
	}
	if len(bare.Paths) != 2 || bare.Paths[0] != file || bare.Paths[1] != other {
		t.Fatalf("bare paths = %#v", bare.Paths)
	}
	// open-file that is not the first user arg does not steal a real file launch
	// when a later token is not itself an existing file.
	notFirst := classifyDesktopLaunchForOS([]string{"MaClaw", file, "open-file"}, "windows")
	if notFirst.Mode != desktopLaunchMain {
		t.Fatalf("open-file not first = %#v", notFirst)
	}
}

func TestFileCompanionCanonicalPathFoldsWindowsCase(t *testing.T) {
	if goruntime.GOOS != "windows" {
		t.Skip("full-path case fold is Windows-only")
	}
	upper, err := fileCompanionCanonicalPath(`D:\Notes\A.md`)
	if err != nil {
		t.Fatal(err)
	}
	lower, err := fileCompanionCanonicalPath(`d:\notes\a.md`)
	if err != nil {
		t.Fatal(err)
	}
	if upper != lower {
		t.Fatalf("canonical %q != %q", upper, lower)
	}
	if upper != strings.ToLower(upper) {
		t.Fatalf("canonical %q is not fully folded", upper)
	}
	if fileCompanionSessionID(upper) != fileCompanionSessionID(lower) {
		t.Fatalf("session ids differ for one Windows path")
	}
	session := fileCompanionSessionID(upper)
	if !strings.HasPrefix(session, "fc_") || len(session) != 3+32 {
		t.Fatalf("session id %q", session)
	}

	dir := t.TempDir()
	nested := filepath.Join(dir, "Notes")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "A.md")
	if err := os.WriteFile(file, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromUpper, err := fileCompanionCanonicalPath(file)
	if err != nil {
		t.Fatal(err)
	}
	fromLower, err := fileCompanionCanonicalPath(strings.ToLower(file))
	if err != nil {
		t.Fatal(err)
	}
	if fromUpper != fromLower || fromUpper != strings.ToLower(fromUpper) {
		t.Fatalf("existing file canonical upper=%q lower=%q", fromUpper, fromLower)
	}
	if fileCompanionSessionID(fromUpper) != fileCompanionSessionID(fromLower) {
		t.Fatal("existing file session ids differ")
	}

	if _, err := fileCompanionCanonicalPath("has\x00nul"); err == nil {
		t.Fatal("NUL path accepted")
	}
}
