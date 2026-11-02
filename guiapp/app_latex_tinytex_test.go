package guiapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tinytex"
)

func TestGetLatexTinyTeXStatusReportsSchemeAndVerifiedTree(t *testing.T) {
	invalidateLatexVerifiedCache()
	t.Cleanup(invalidateLatexVerifiedCache)
	latexRuntime.mu.Lock()
	latexRuntime.phase = ""
	latexRuntime.running = false
	latexRuntime.err = ""
	latexRuntime.percent = 0
	latexRuntime.mu.Unlock()

	app := &App{testHomeDir: t.TempDir()}
	app.configCacheValid = true
	app.configCache = corelib.AppConfigDefaults()

	status := app.GetLatexTinyTeXStatus()
	if status["enabled"] != true {
		t.Fatalf("enabled = %#v", status["enabled"])
	}
	if status["ready"] != false {
		t.Fatalf("ready = %#v", status["ready"])
	}
	if status["scheme"] != tinytex.Scheme || status["bundle"] != tinytex.Bundle {
		t.Fatalf("scheme/bundle = %#v %#v", status["scheme"], status["bundle"])
	}
	source, _ := status["source"].(string)
	if source == "" || status["phase"] != "idle" {
		t.Fatalf("status = %#v", status)
	}

	dist := filepath.Join(tinytex.DistDir(app.GetDataDir()), "TinyTeX")
	if err := os.MkdirAll(filepath.Join(dist, "bin", "windows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	engineName := "xelatex"
	if runtime.GOOS == "windows" {
		engineName = "xelatex.exe"
	}
	if err := os.WriteFile(filepath.Join(dist, "bin", "windows", engineName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	article := tinytex.ArticleClassPath(dist)
	if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(article, []byte("\\documentclass"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tinytex.WriteStamp(dist, "XeTeX test"); err != nil {
		t.Fatal(err)
	}

	status = app.GetLatexTinyTeXStatus()
	if status["ready"] != true || status["phase"] != "ready" || status["version"] != "XeTeX test" {
		t.Fatalf("verified status = %#v", status)
	}
	if err := tinytex.WriteStamp(dist, "XeTeX updated"); err != nil {
		t.Fatal(err)
	}
	status = app.GetLatexTinyTeXStatus()
	if status["version"] != "XeTeX test" {
		t.Fatalf("cached version = %#v", status["version"])
	}
	invalidateLatexVerifiedCache()
	status = app.GetLatexTinyTeXStatus()
	if status["version"] != "XeTeX updated" {
		t.Fatalf("version after invalidation = %#v", status["version"])
	}

	app.configCache.LatexTinyTeXEnabled = false
	app.configCacheValid = true
	app.configSnap.Store(&app.configCache)
	status = app.GetLatexTinyTeXStatus()
	if status["enabled"] != false {
		t.Fatalf("enabled after disable = %#v", status["enabled"])
	}
	if app.testHomeDir == "" || app.latexTreeVerified() != true {
		t.Fatal("verified tree should stay on disk when the switch is off")
	}
}

func TestInstallTinyTeXSchemeUpdatesTlmgrBeforeScheme(t *testing.T) {
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	logPath := filepath.Join(dist, "tlmgr-args.txt")
	article := tinytex.ArticleClassPath(dist)
	if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRecordingTlmgr(dist, logPath, article); err != nil {
		t.Fatal(err)
	}
	fetchLatexFile = func(_ *App, _ context.Context, url, _ string) error {
		t.Fatalf("official updater downloaded after a successful self-update: %s", url)
		return nil
	}
	app := latexInstallTestApp(t)
	if err := app.installTinyTeXScheme(dist); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	self := strings.Index(text, "update --self")
	install := strings.Index(text, "install "+tinytex.Scheme)
	if self < 0 || install < 0 || self > install {
		t.Fatalf("tlmgr log:\n%s", text)
	}
	if !tinytex.ArticlePresent(dist) {
		t.Fatal("scheme install did not provide article.cls")
	}
}

func TestInstallTinyTeXSchemeUsesOfficialUpdaterWhenSelfUpdateIsBlocked(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "same-year"
		if upgrade {
			name = "cross-release"
		}
		t.Run(name, func(t *testing.T) {
			restoreLatexInstallHooks(t)
			dist := t.TempDir()
			tlmgrPath, err := writePresentTlmgr(dist)
			if err != nil {
				t.Fatal(err)
			}
			var calls []string
			var updaterPrepend []string
			var updaterDest string
			invokeLatexTool = func(_ context.Context, bin, _ string, prepend []string, args ...string) ([]byte, error) {
				line := strings.Join(args, " ")
				calls = append(calls, filepath.Base(bin)+" "+line)
				switch {
				case strings.HasPrefix(line, "update --self"):
					if upgrade {
						return []byte("Local TeX Live (2025) is older than remote repository (2026).\nCross release updates are only supported with\n  update-tlmgr-latest(.sh/.exe) -- --upgrade\n"), fmt.Errorf("exit status 1")
					}
					return []byte("tlmgr itself needs to be updated.\ntlmgr.pl: Terminating; please see warning above!"), fmt.Errorf("exit status 255")
				case strings.Contains(bin, "update-tlmgr-latest") || (bin == "sh" && strings.Contains(line, "update-tlmgr-latest")):
					latexRuntime.mu.Lock()
					phase := latexRuntime.phase
					latexRuntime.mu.Unlock()
					if phase != "updating" {
						t.Errorf("phase during official updater = %q", phase)
					}
					updaterPrepend = append([]string{}, prepend...)
					joinedArgs := strings.Join(args, " ")
					if upgrade && !strings.Contains(joinedArgs, "-- --upgrade") {
						t.Errorf("updater args = %#v", args)
					}
					if !upgrade && strings.Contains(joinedArgs, "--upgrade") {
						t.Errorf("same-year updater got upgrade args %#v", args)
					}
					return []byte("updated"), nil
				case strings.HasPrefix(line, "install"):
					if err := os.MkdirAll(filepath.Dir(tinytex.ArticleClassPath(dist)), 0o755); err != nil {
						return nil, err
					}
					if err := os.WriteFile(tinytex.ArticleClassPath(dist), []byte("\\documentclass"), 0o644); err != nil {
						return nil, err
					}
					return []byte("installed"), nil
				default:
					return []byte("ok"), nil
				}
			}
			fetchLatexFile = func(_ *App, _ context.Context, url, dest string) error {
				if !strings.HasPrefix(url, "https://") || !strings.HasSuffix(url, "/"+tinytex.TlmgrUpdaterName(runtime.GOOS)) {
					t.Errorf("updater url = %s", url)
				}
				updaterDest = dest
				wantDir := filepath.Join(filepath.Dir(dist), "maclaw-tlmgr-updater")
				if filepath.Dir(dest) != wantDir {
					t.Errorf("updater dir = %s, want %s", filepath.Dir(dest), wantDir)
				}
				if strings.HasPrefix(filepath.Clean(dest), filepath.Clean(filepath.Join(dist, "temp"))) {
					t.Errorf("updater downloaded inside the TeX temp dir: %s", dest)
				}
				body := make([]byte, 300<<10)
				if runtime.GOOS == "windows" {
					body[0], body[1] = 'M', 'Z'
				} else {
					copy(body, []byte("#!/bin/sh\n"))
				}
				return os.WriteFile(dest, body, 0o755)
			}
			app := latexInstallTestApp(t)
			if err := app.installTinyTeXScheme(dist); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(calls, "\n")
			if !strings.Contains(joined, "update --self") || !strings.Contains(joined, "install "+tinytex.Scheme) {
				t.Fatalf("calls:\n%s", joined)
			}
			if len(updaterPrepend) == 0 || !strings.EqualFold(updaterPrepend[0], filepath.Dir(tlmgrPath)) {
				t.Fatalf("updater PATH prefix = %#v, tlmgr dir %s", updaterPrepend, filepath.Dir(tlmgrPath))
			}
			if !tinytex.ArticlePresent(dist) {
				t.Fatal("article.cls missing after updater")
			}
			if updaterDest == "" {
				t.Fatal("official updater was not downloaded")
			}
			if _, statErr := os.Stat(updaterDest); !os.IsNotExist(statErr) {
				t.Fatalf("updater left behind at %s (%v)", updaterDest, statErr)
			}
		})
	}
}

func TestInstallTinyTeXSchemeRunsOfficialUpdaterAfterWindowsInfraPanic(t *testing.T) {
	for _, code := range []int{666, 1} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			testInstallTinyTeXSchemeOfficialUpdaterAfterScriptExit(t, code)
		})
	}
}

func testInstallTinyTeXSchemeOfficialUpdaterAfterScriptExit(t *testing.T, code int) {
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	if _, err := writePresentTlmgr(dist); err != nil {
		t.Fatal(err)
	}
	var fetched string
	invokeLatexTool = func(_ context.Context, bin, _ string, _ []string, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(line, "update --self"):
			// Rollback and :panic write the explanation to the console.
			return []byte("tlmgr.pl: Preparing TeX Live infrastructure update...\n"), fmt.Errorf("exit status %d", code)
		case strings.Contains(bin, "update-tlmgr-latest") || (bin == "sh" && strings.Contains(line, "update-tlmgr-latest")):
			return []byte("updated"), nil
		case strings.HasPrefix(line, "install"):
			article := tinytex.ArticleClassPath(dist)
			if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(article, []byte("\\documentclass"), 0o644); err != nil {
				return nil, err
			}
			return []byte("installed"), nil
		default:
			return []byte("ok"), nil
		}
	}
	fetchLatexFile = func(_ *App, _ context.Context, _, dest string) error {
		fetched = dest
		body := make([]byte, 300<<10)
		if runtime.GOOS == "windows" {
			body[0], body[1] = 'M', 'Z'
		} else {
			copy(body, []byte("#!/bin/sh\n"))
		}
		return os.WriteFile(dest, body, 0o755)
	}
	app := latexInstallTestApp(t)
	if err := app.installTinyTeXScheme(dist); err != nil {
		t.Fatal(err)
	}
	if fetched == "" {
		t.Fatal("windows infra panic did not download the official updater")
	}
	if !tinytex.ArticlePresent(dist) {
		t.Fatal("article.cls missing after infra panic recovery")
	}
}

func TestInstallTinyTeXSchemeClassifiesTheFullInstallOutput(t *testing.T) {
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	if _, err := writePresentTlmgr(dist); err != nil {
		t.Fatal(err)
	}
	// The banner sits at the front. The settings error keeps the last 800
	// characters, which are only padding, so classifying err.Error() would
	// treat this as a dead mirror and never download the updater.
	full := "tlmgr itself needs to be updated.\nhttps://mirror.ctan.org/systems/texlive/tlnet/update-tlmgr-latest.exe\n" + strings.Repeat("n", 4000)
	var updated bool
	var fetched string
	invokeLatexTool = func(_ context.Context, bin, _ string, _ []string, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(line, "update --self"):
			return []byte("ok"), nil
		case strings.Contains(bin, "update-tlmgr-latest") || (bin == "sh" && strings.Contains(line, "update-tlmgr-latest")):
			updated = true
			return []byte("updated"), nil
		case strings.HasPrefix(line, "install"):
			if !updated {
				return []byte(full), fmt.Errorf("exit status 255")
			}
			article := tinytex.ArticleClassPath(dist)
			if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(article, []byte("\\documentclass"), 0o644); err != nil {
				return nil, err
			}
			return []byte("installed"), nil
		default:
			return []byte("ok"), nil
		}
	}
	fetchLatexFile = func(_ *App, _ context.Context, _, dest string) error {
		fetched = dest
		body := make([]byte, 300<<10)
		if runtime.GOOS == "windows" {
			body[0], body[1] = 'M', 'Z'
		} else {
			copy(body, []byte("#!/bin/sh\n"))
		}
		return os.WriteFile(dest, body, 0o755)
	}
	app := latexInstallTestApp(t)
	if err := app.installTinyTeXScheme(dist); err != nil {
		t.Fatal(err)
	}
	if fetched == "" || !updated {
		t.Fatal("clipped install error skipped the official updater")
	}
	if _, statErr := os.Stat(fetched); !os.IsNotExist(statErr) {
		t.Fatalf("updater left behind at %s (%v)", fetched, statErr)
	}
}

func TestInstallTinyTeXSchemePinsTheRepositoryRedirect(t *testing.T) {
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	logPath := filepath.Join(dist, "tlmgr-args.txt")
	article := tinytex.ArticleClassPath(dist)
	if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRecordingTlmgr(dist, logPath, article); err != nil {
		t.Fatal(err)
	}
	const pinned = "https://ctan.math.illinois.edu/systems/texlive/tlnet"
	resolveLatexRepo = func(_ context.Context, repo string) string {
		if strings.Contains(repo, "mirror.ctan.org") {
			return pinned
		}
		return repo
	}
	fetchLatexFile = func(_ *App, _ context.Context, url, _ string) error {
		t.Fatalf("official updater downloaded after a successful self-update: %s", url)
		return nil
	}
	app := latexInstallTestApp(t)
	if err := app.installTinyTeXScheme(dist); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.Contains(text, "option repository "+pinned) {
		t.Fatalf("pinned repository missing:\n%s", text)
	}
	if strings.Contains(text, "option repository https://mirror.ctan.org") {
		t.Fatalf("redirector was stored:\n%s", text)
	}
}

func TestInstallTinyTeXSchemeStopsWhenTheUpdaterScriptIsLocked(t *testing.T) {
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	if _, err := writePresentTlmgr(dist); err != nil {
		t.Fatal(err)
	}
	var repos []string
	invokeLatexTool = func(_ context.Context, _, _ string, _ []string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "option" && args[1] == "repository" {
			repos = append(repos, args[2])
			return nil, fmt.Errorf("remove %w", tinytex.ErrStaleUpdater)
		}
		t.Fatalf("tlmgr kept going after a locked updater: %s", strings.Join(args, " "))
		return nil, nil
	}
	app := latexInstallTestApp(t)
	err := app.installTinyTeXScheme(dist)
	if !errors.Is(err, tinytex.ErrStaleUpdater) {
		t.Fatalf("err = %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("repositories tried = %#v", repos)
	}
}

func TestInstallTinyTeXSchemeStopsWhenTheBatchCannotRemoveTheUpdater(t *testing.T) {
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	if _, err := writePresentTlmgr(dist); err != nil {
		t.Fatal(err)
	}
	var repos []string
	invokeLatexTool = func(_ context.Context, _, _ string, _ []string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "option" && args[1] == "repository" {
			repos = append(repos, args[2])
			return []byte("tlmgr.bat: failed to remove previous updater script"), fmt.Errorf("exit status 1")
		}
		t.Fatalf("tlmgr kept going after the batch updater lock: %s", strings.Join(args, " "))
		return nil, nil
	}
	app := latexInstallTestApp(t)
	err := app.installTinyTeXScheme(dist)
	if err == nil || !tinytex.TlmgrOutputStaleUpdater(err.Error()) {
		t.Fatalf("err = %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("repositories tried = %#v", repos)
	}
}

func TestInstallTinyTeXSchemeClearsAReadOnlyUpdaterScript(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("updater-w32 is the Windows tlmgr.bat lock")
	}
	restoreLatexInstallHooks(t)
	dist := t.TempDir()
	logPath := filepath.Join(dist, "tlmgr-args.txt")
	article := tinytex.ArticleClassPath(dist)
	if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRecordingTlmgr(dist, logPath, article); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dist, "temp", "updater-w32")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o444); err != nil {
		t.Fatal(err)
	}
	fetchLatexFile = func(_ *App, _ context.Context, url, _ string) error {
		t.Fatalf("official updater downloaded after a successful self-update: %s", url)
		return nil
	}
	app := latexInstallTestApp(t)
	if err := app.installTinyTeXScheme(dist); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Fatalf("read-only updater-w32 still present (%v)", statErr)
	}
}

func TestUpdaterDownloadStopsWhenTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (&App{}).downloadModelFromWithEventContext(ctx, "https://127.0.0.1:1/update-tlmgr-latest.exe", filepath.Join(t.TempDir(), "u.exe"), false, "latex-tinytex-updater")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestRecoverTlmgrAnnouncesOnlyForTheSettingsInstaller(t *testing.T) {
	restoreLatexInstallHooks(t)
	fetchLatexFile = func(_ *App, _ context.Context, _, _ string) error {
		return fmt.Errorf("stopped")
	}
	app := latexInstallTestApp(t)
	dist := t.TempDir()
	tlmgr := filepath.Join(dist, "bin", "windows", "tlmgr.bat")
	repo := "https://mirrors.tuna.tsinghua.edu.cn/CTAN/systems/texlive/tlnet"
	latexRuntime.mu.Lock()
	latexRuntime.phase = "ready"
	latexRuntime.running = false
	latexRuntime.mu.Unlock()
	_ = app.recoverTlmgr(context.Background(), dist, tlmgr, repo, false, false)
	latexRuntime.mu.Lock()
	phase := latexRuntime.phase
	latexRuntime.mu.Unlock()
	if phase != "ready" {
		t.Fatalf("package updater phase = %q", phase)
	}
	_ = app.recoverTlmgr(context.Background(), dist, tlmgr, repo, false, true)
	latexRuntime.mu.Lock()
	phase = latexRuntime.phase
	latexRuntime.mu.Unlock()
	if phase != "updating" {
		t.Fatalf("settings updater phase = %q", phase)
	}
}

func TestLatexDownloadProgressStaysOnTheSettingsPhase(t *testing.T) {
	restoreLatexInstallHooks(t)
	app := latexInstallTestApp(t)
	app.setLatexPhase("downloading", 40, 400, 1000, "")
	app.setLatexPhase("updating", 0, 0, 0, "")
	if phase, pct, got, total := latexRuntimeSnapshot(); phase != "updating" || pct != 0 || got != 0 || total != 0 {
		t.Fatalf("updating kept TinyTeX-0 sizes phase=%s pct=%d got=%d total=%d", phase, pct, got, total)
	}
	app.emitDownloadProgressNamed(latexUpdaterProgressEvent, 55, 550, 1000, "")
	if phase, pct, got, total := latexRuntimeSnapshot(); phase != "updating" || pct != 55 || got != 550 || total != 1000 {
		t.Fatalf("updater progress phase=%s pct=%d got=%d total=%d", phase, pct, got, total)
	}
	status := app.GetLatexTinyTeXStatus()
	if status["phase"] != "updating" || status["percent"] != 55 || status["downloaded"] != int64(550) || status["total"] != int64(1000) {
		t.Fatalf("status = %#v", status)
	}
	app.setLatexPhase("ready", 100, 0, 0, "")
	app.emitDownloadProgressNamed(latexUpdaterProgressEvent, 10, 10, 100, "")
	if phase, pct, got, total := latexRuntimeSnapshot(); phase != "ready" || pct != 100 || got != 0 || total != 0 {
		t.Fatalf("package updater moved settings phase=%s pct=%d got=%d total=%d", phase, pct, got, total)
	}
	app.setLatexPhase("downloading", 0, 0, 0, "")
	app.emitDownloadProgressNamed(latexProgressEvent, 12, 120, 800, "")
	if phase, pct, got, total := latexRuntimeSnapshot(); phase != "downloading" || pct != 12 || got != 120 || total != 800 {
		t.Fatalf("archive progress phase=%s pct=%d got=%d total=%d", phase, pct, got, total)
	}
}

func latexRuntimeSnapshot() (phase string, pct int, downloaded, total int64) {
	latexRuntime.mu.Lock()
	defer latexRuntime.mu.Unlock()
	return latexRuntime.phase, latexRuntime.percent, latexRuntime.downloaded, latexRuntime.total
}

func latexInstallTestApp(t *testing.T) *App {
	t.Helper()
	app := &App{testHomeDir: t.TempDir(), CurrentLanguage: "en"}
	app.configCacheValid = true
	app.configCache = corelib.AppConfigDefaults()
	app.configSnap.Store(&app.configCache)
	return app
}

func restoreLatexInstallHooks(t *testing.T) {
	t.Helper()
	prevRun := invokeLatexTool
	prevFetch := fetchLatexFile
	prevResolve := resolveLatexRepo
	resolveLatexRepo = func(_ context.Context, repo string) string { return repo }
	t.Cleanup(func() {
		invokeLatexTool = prevRun
		fetchLatexFile = prevFetch
		resolveLatexRepo = prevResolve
		latexRuntime.mu.Lock()
		latexRuntime.phase = ""
		latexRuntime.running = false
		latexRuntime.err = ""
		latexRuntime.percent = 0
		latexRuntime.downloaded = 0
		latexRuntime.total = 0
		latexRuntime.mu.Unlock()
	})
}

func writePresentTlmgr(dist string) (string, error) {
	name := "tlmgr"
	binDir := filepath.Join(dist, "bin", "unix")
	if runtime.GOOS == "windows" {
		name = "tlmgr.bat"
		binDir = filepath.Join(dist, "bin", "windows")
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(binDir, name)
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func writeRecordingTlmgr(dist, logPath, article string) error {
	path, err := writePresentTlmgr(dist)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// The real tlmgr.bat drops call's script name with shift, then reads
		// %0. Logging %* would hide a launcher that ate --self.
		body := strings.Join([]string{
			"@echo off",
			"setlocal enableextensions",
			"if x == x%1 goto noargs",
			"set args=",
			":rebuildargs",
			"shift",
			"if x == x%0 goto nomoreargs",
			"if gui == %0 goto guibailout",
			"if -gui == %0 goto guibailout",
			"if --gui == %0 goto guibailout",
			"set args=%args% %0",
			"goto rebuildargs",
			":nomoreargs",
			">>\"" + logPath + "\" echo %args%",
			"set probe=%args%",
			"if not \"%probe:install=%\"==\"%probe%\" echo x> \"" + article + "\"",
			"exit /b 0",
			":noargs",
			">>\"" + logPath + "\" echo NOARGS",
			"exit /b 1",
			":guibailout",
			">>\"" + logPath + "\" echo GUI",
			"exit /b 1",
			"",
		}, "\r\n")
		return os.WriteFile(path, []byte(body), 0o644)
	}
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shSingleQuote(logPath) + "\nif [ \"$1\" = \"install\" ]; then printf x > " + shSingleQuote(article) + "; fi\nexit 0\n"
	return os.WriteFile(path, []byte(body), 0o755)
}

func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
