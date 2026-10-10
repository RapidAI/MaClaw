package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPackageNamesRejectShellAndKeepDebianNames(t *testing.T) {
	names, err := PackageNames([]any{"libreoffice", "libreoffice", "g++"})
	if err != nil || len(names) != 2 || names[0] != "libreoffice" || names[1] != "g++" {
		t.Fatalf("names=%v err=%v", names, err)
	}
	fromText, err := PackageNames("vim, wget")
	if err != nil || len(fromText) != 2 || fromText[0] != "vim" || fromText[1] != "wget" {
		t.Fatalf("text=%v err=%v", fromText, err)
	}
	for _, bad := range []any{"LibreOffice", "libreoffice;rm", "foo/bar", "../evil", "", []any{"ok", 1}} {
		if _, err := PackageNames(bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	argv := AptInstallArgs("maclaw-desktop-alice", []string{"libreoffice"})
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "apt-get install -y") || strings.Contains(joined, "apt-get -f install") || !strings.HasSuffix(joined, "libreoffice") {
		t.Fatalf("argv=%s", joined)
	}
	fixArgs := AptFixArgs("maclaw-desktop-alice")
	fix := strings.Join(fixArgs, " ")
	if !strings.Contains(fix, "apt-get -f install -y") || strings.Contains(fix, "apt-get install -y") || strings.Contains(fix, "sh ") || !strings.Contains(fix, "--force-confdef") || fixArgs[len(fixArgs)-1] != "Dpkg::Options::=--force-confold" {
		t.Fatalf("fix=%s", fix)
	}
	repair := strings.Join(DpkgConfigureArgs("maclaw-desktop-alice"), " ")
	if !strings.Contains(repair, "python3") || !strings.Contains(repair, "lockf") || !strings.Contains(repair, "set_inheritable") || !strings.Contains(repair, "execvp") || !strings.Contains(repair, "'dpkg'") || !strings.Contains(repair, "'--configure'") || !strings.Contains(repair, "'-a'") || !strings.Contains(repair, "'--force-confold'") || strings.Contains(repair, "sh ") || strings.Contains(repair, "apt-get") {
		t.Fatalf("repair=%s", repair)
	}
	wait := strings.Join(AptMirrorWaitArgs("maclaw-desktop-alice"), " ")
	if !strings.Contains(wait, "python3") || !strings.Contains(wait, "flock") || !strings.Contains(wait, "LOCK_NB") || !strings.Contains(wait, "configure_apt") || !strings.Contains(wait, "apt_config_current") || strings.Contains(wait, "apt-get") {
		t.Fatalf("wait=%s", wait)
	}
	ending := "E: Unable to locate package libreoffice"
	failure := AptFailureText("PROGRESS-HEAD"+strings.Repeat("x", 4000)+"\n"+ending, nil)
	if !strings.Contains(failure, ending) || strings.Contains(failure, "PROGRESS-HEAD") || len([]rune(failure)) > AptFailureRunes {
		t.Fatalf("failure=%q", failure)
	}
	if AptFailureText("", nil) != "apt-get failed" {
		t.Fatal("empty apt failure was blank")
	}
	if _, err := PackageNames(strings.Repeat("vim ", MaxInstallPackages+1)); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("too many err=%v", err)
	}
	if InstallClientTimeout <= InstallBudget {
		t.Fatalf("client timeout %s does not outlast apt %s", InstallClientTimeout, InstallBudget)
	}
}

func TestRunPackageInstallRepairsHalfInstalledWhenUpdateWorks(t *testing.T) {
	var calls []string
	out, err := RunPackageInstall(context.Background(), func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "execvp") {
			return "package is half-installed", errors.New("exit status 1")
		}
		if strings.Contains(joined, "apt-get -f install") {
			return "Setting up broken", nil
		}
		if strings.Contains(joined, "apt-get install -y") {
			return "Setting up vim", nil
		}
		return "", nil
	}, "maclaw-desktop-alice", []string{"vim"})
	if err != nil || out != "Setting up vim" || len(calls) != 4 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, len(calls))
	}
	if !strings.Contains(calls[0], "execvp") || !strings.Contains(calls[1], "apt-get update") || !strings.Contains(calls[2], "apt-get -f install") || !strings.Contains(calls[3], "apt-get install -y") || strings.Contains(calls[3], "apt-get -f install") {
		t.Fatalf("calls=%v", calls)
	}
}

func TestRunPackageInstallKeepsConfigureErrorWhenUpdateFails(t *testing.T) {
	calls := 0
	out, err := RunPackageInstall(context.Background(), func(args []string) (string, error) {
		calls++
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "execvp") {
			return "configure failed", errors.New("exit status 1")
		}
		if strings.Contains(joined, "apt-get update") {
			return "E: dpkg was interrupted", errors.New("exit status 100")
		}
		t.Fatalf("unexpected %s", joined)
		return "", nil
	}, "box", []string{"vim"})
	if err == nil || out != "configure failed" || !strings.Contains(err.Error(), "exit status 1") || calls != 2 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
}

func TestRunPackageInstallStopsWhenFixFails(t *testing.T) {
	calls := 0
	out, err := RunPackageInstall(context.Background(), func(args []string) (string, error) {
		calls++
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "apt-get -f install") {
			return "E: Unmet dependencies", errors.New("exit status 100")
		}
		if strings.Contains(joined, "apt-get install -y") {
			t.Fatal("install ran after fix failed")
		}
		return "", nil
	}, "box", []string{"vim"})
	if err == nil || out != "E: Unmet dependencies" || calls != 3 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
}

func TestRunPackageInstallStopsOnDeadlineBeforeFix(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	calls := 0
	out, err := RunPackageInstall(ctx, func(args []string) (string, error) {
		calls++
		return "Get:1 progress", ctx.Err()
	}, "box", []string{"vim"})
	if out != "" || err == nil || !strings.Contains(err.Error(), "time budget") || strings.Contains(err.Error(), "Get:1") || calls != 1 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
}

func TestRunPackageInstallReturnsCancelWithoutProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := RunPackageInstall(ctx, func(args []string) (string, error) {
		return "Get:1 progress", ctx.Err()
	}, "box", []string{"vim"})
	if out != "" || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "time budget") || strings.Contains(err.Error(), "Get:1") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestUserKeyIgnoresInstanceAndRejectsEmptyUser(t *testing.T) {
	first, err := UserKey("tenant", "alice")
	if err != nil || !ValidUserKey(first) {
		t.Fatalf("key=%q err=%v", first, err)
	}
	second, err := UserKey("tenant", "alice")
	if err != nil || second != first {
		t.Fatalf("same user key changed: %q %q", first, second)
	}
	other, err := UserKey("tenant", "bob")
	if err != nil || other == first {
		t.Fatalf("different user reused %q", other)
	}
	if _, err := UserKey("tenant", "  "); err == nil {
		t.Fatal("empty user was accepted")
	}
	aliceMounts, err := PrivateMounts("tenant", "alice")
	if err != nil || len(aliceMounts) != 4 {
		t.Fatalf("mounts=%#v err=%v", aliceMounts, err)
	}
	bobMounts, err := PrivateMounts("tenant", "bob")
	if err != nil {
		t.Fatal(err)
	}
	for i := range aliceMounts {
		if aliceMounts[i].Volume == bobMounts[i].Volume {
			t.Fatalf("users share volume %s", aliceMounts[i].Volume)
		}
		if !strings.Contains(aliceMounts[i].Volume, first) {
			t.Fatalf("volume %s is not bound to alice", aliceMounts[i].Volume)
		}
	}
	image, err := StateImage("tenant", "alice")
	if err != nil || image != "maclaw-desktop-user-"+first+":state" || image == DefaultImage {
		t.Fatalf("state image=%q", image)
	}
}

func TestNormalizeResourcesUsesContainerDefaults(t *testing.T) {
	got, err := NormalizeResources("", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != DefaultImage || got.Memory != DefaultMemory || got.CPUs != DefaultCPUs || got.ShmSize != DefaultShmSize {
		t.Fatalf("defaults=%#v", got)
	}
	got, err = NormalizeResources("maclaw-gui:1", "4G", "2", "1G")
	if err != nil || got.Memory != "4g" || got.CPUs != "2" || got.ShmSize != "1g" {
		t.Fatalf("resources=%#v err=%v", got, err)
	}
	if _, err := NormalizeResources("bad image", "4g", "2", "1g"); err == nil {
		t.Fatal("invalid image was accepted")
	}
}

func TestParseEnsureReadsSupervisorLine(t *testing.T) {
	got, err := ParseEnsure("starting\n{\"display\":\":20\",\"cdp_port\":19020}\n")
	if err != nil || got.Display != ":20" || got.CDPPort != 19020 {
		t.Fatalf("status=%#v err=%v", got, err)
	}
	if _, err := ParseEnsure("no json"); err == nil {
		t.Fatal("missing status was accepted")
	}
}

func TestDefaultsAreTheXfceImage(t *testing.T) {
	// Hub's admin page (hub/web/admin/desktop-tab.js) shows the same values.
	if DefaultImage != "maclaw-gui:2" || LegacyImage != "maclaw-gui:1" || DefaultMemory != "3g" || DefaultShmSize != "1g" || DefaultDisplay != ":20" {
		t.Fatalf("defaults image=%s memory=%s shm=%s display=%s", DefaultImage, DefaultMemory, DefaultShmSize, DefaultDisplay)
	}
	if _, err := NormalizeResources(DefaultImage, DefaultMemory, DefaultCPUs, DefaultShmSize); err != nil {
		t.Fatal(err)
	}
	if !ValidDisplay(DefaultDisplay) {
		t.Fatal("default display is invalid")
	}
}

func TestOpenArgvStartsAWindowAndRejectsAShell(t *testing.T) {
	argv, err := OpenArgv("xterm", []string{"-geometry", "100x30"})
	if err != nil || len(argv) != 3 || argv[0] != "xterm" || argv[2] != "100x30" {
		t.Fatalf("argv=%v err=%v", argv, err)
	}
	absolute, err := OpenArgv("/usr/bin/xfce4-terminal", nil)
	if err != nil || len(absolute) != 1 || absolute[0] != "/usr/bin/xfce4-terminal" {
		t.Fatalf("absolute=%v err=%v", absolute, err)
	}
	command, err := OpenExecArgs("maclaw-desktop-alice", ":20", "xterm", []string{"-geometry", "100x30"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command, " ")
	if command[0] != "exec" || command[1] != "-d" || !strings.Contains(joined, "DISPLAY=:20") || !strings.Contains(joined, "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/.maclaw-dbus-20") || !strings.Contains(joined, "HOME="+DesktopHome) || !strings.Contains(joined, "-w "+DesktopHome) || !strings.Contains(joined, " maclaw-desktop-alice xterm ") || command[len(command)-1] != "100x30" {
		t.Fatalf("command=%v", command)
	}
	workDirAt := -1
	containerAt := -1
	for i, arg := range command {
		if arg == "-w" && i+1 < len(command) && command[i+1] == DesktopHome && workDirAt < 0 {
			workDirAt = i
		}
		if arg == "maclaw-desktop-alice" {
			containerAt = i
		}
	}
	if workDirAt < 0 || containerAt < 0 || workDirAt > containerAt {
		t.Fatalf("working directory was not set before the container: %v", command)
	}
	for _, want := range []string{"XMODIFIERS=@im=none", "GTK_IM_MODULE=gtk-im-context-simple", "QT_IM_MODULE=compose", "SDL_IM_MODULE="} {
		at := -1
		for i, arg := range command {
			if arg == want {
				at = i
				break
			}
		}
		if at < 0 || at > containerAt || strings.Contains(joined, "GTK_IM_MODULE=fcitx") || strings.Contains(joined, "GTK_IM_MODULE=xim") || strings.Contains(joined, "@im=fcitx") || strings.Contains(joined, "--disable-server") {
			t.Fatalf("input method %s was not overridden before the container: %v", want, command)
		}
	}
	term, err := OpenExecArgs("maclaw-desktop-alice", ":20", "/usr/bin/xfce4-terminal", []string{"--geometry", "80x24"})
	if err != nil {
		t.Fatal(err)
	}
	termJoined := strings.Join(term, " ")
	if !strings.Contains(termJoined, "/usr/bin/xfce4-terminal --disable-server --geometry 80x24") || strings.Contains(termJoined, "GTK_IM_MODULE=xim") || strings.Contains(termJoined, "QT_IM_MODULE=xim") {
		t.Fatalf("terminal was not detached from the session server: %v", term)
	}
	again, err := OpenExecArgs("maclaw-desktop-alice", ":20", "xfce4-terminal", []string{"--disable-server"})
	if err != nil || strings.Count(strings.Join(again, " "), "--disable-server") != 1 {
		t.Fatalf("disable-server was repeated: %v err=%v", again, err)
	}
	for _, arg := range command {
		if arg == "sh" || arg == "bash" || arg == "xdotool" || arg == "-c" {
			t.Fatalf("launch used a shell: %v", command)
		}
	}
	for _, program := range []string{"bash", "/bin/bash", "sh", "python3", "chromium", "chromium-browser", "apt-get", "xdg-open", "../xterm", "bin/xterm", "-e", "xterm;rm", "", "xterm foo"} {
		if _, err := OpenArgv(program, nil); err == nil {
			t.Fatalf("accepted %q", program)
		}
	}
	if _, err := OpenArgv("xterm", []string{"-e", "ok", "a", "b", "c", "d", "e", "f", "g"}); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("too many err=%v", err)
	}
	if _, err := OpenArgv("xterm", []string{"line\nbreak"}); err == nil {
		t.Fatal("a newline argument was accepted")
	}
	if _, err := ProgramArgs("bash -c echo"); err == nil {
		t.Fatal("a shell line was accepted as argv")
	}
	fromList, err := ProgramArgs([]any{"-title", "Hello"})
	if err != nil || len(fromList) != 2 || fromList[1] != "Hello" {
		t.Fatalf("args=%v err=%v", fromList, err)
	}
	if AppFailureText("", errors.New("exit status 1")) != AppWindowMiss || AppFailureText("desktop app command failed: exit status 1", nil) != AppWindowMiss {
		t.Fatal("exit status 1 was not explained")
	}
	for _, other := range []string{"exit status 127", "exit status 10"} {
		if got := AppFailureText("", errors.New(other)); got == AppWindowMiss || !strings.Contains(got, other) {
			t.Fatalf("failure %q became %q", other, got)
		}
	}
	display := "Error: Can't open display: :20"
	if got := AppFailureText(display, errors.New("exit status 1")); got == AppWindowMiss || !strings.Contains(got, "Can't open display") {
		t.Fatalf("display failure became %q", got)
	}
	if !strings.Contains(OpenFailureText("xterm", `exec: "xterm": executable file not found in $PATH`, nil), "not installed") || !strings.Contains(OpenFailureText("xterm", "", nil), "app_open failed") {
		t.Fatal("a missing program was not named")
	}
	missingDir := `chdir to cwd ("/home/desktop") failed: no such file or directory`
	if got := OpenFailureText("xterm", missingDir, nil); strings.Contains(got, "not installed") || !strings.Contains(got, "no such file") {
		t.Fatalf("a missing directory was called a missing program: %q", got)
	}
	bus, err := SessionBusAddress(":20")
	if err != nil || bus != "unix:path=/tmp/.maclaw-dbus-20" {
		t.Fatalf("bus=%s err=%v", bus, err)
	}
	padded, err := OpenExecArgs("maclaw-desktop-alice", ":020", "xterm", nil)
	if err != nil || !strings.Contains(strings.Join(padded, " "), "DISPLAY=:20") || !strings.Contains(strings.Join(padded, " "), "unix:path=/tmp/.maclaw-dbus-20") || strings.Contains(strings.Join(padded, " "), "dbus-020") {
		t.Fatalf("padded display=%v err=%v", padded, err)
	}
	if _, err := SessionBusAddress("20"); err == nil {
		t.Fatal("a display without a colon was accepted")
	}
}

func TestScreenshotScriptSavesOnlyAPlainPngName(t *testing.T) {
	plain, err := ScreenshotScriptFor("")
	if err != nil || plain != ScreenshotScript || strings.Contains(plain, "cp ") {
		t.Fatalf("plain script changed: err=%v", err)
	}
	script, err := ScreenshotScriptFor("baidu_screenshot.png")
	if err != nil || !strings.Contains(script, `cp "$f" /home/desktop/Desktop/baidu_screenshot.png`) || !strings.Contains(script, "mkdir -p /home/desktop/Desktop") || !strings.Contains(script, "chmod 644 /home/desktop/Desktop/baidu_screenshot.png") || !strings.Contains(script, "base64 -w0") {
		t.Fatalf("script=%s err=%v", script, err)
	}
	if DesktopShotPath("baidu_screenshot.png") != "/home/desktop/Desktop/baidu_screenshot.png" {
		t.Fatal(DesktopShotPath("baidu_screenshot.png"))
	}
	for _, name := range []string{"../secret.png", "/tmp/x.png", "shot.jpg", ".hidden.png", "a b.png", "baidu.png;rm", "shot.PNG"} {
		if _, err := ScreenshotScriptFor(name); err == nil || DesktopShotPath(name) != "" {
			t.Fatalf("%s was accepted: %v", name, err)
		}
	}
}
