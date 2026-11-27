package guiapp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppUpdateNotifyVersion(t *testing.T) {
	cases := []struct {
		name        string
		result      UpdateResult
		last        string
		wantOK      bool
		wantVersion string
	}{
		{
			name:   "no update",
			result: UpdateResult{HasUpdate: false, LatestVersion: "V1.2.0"},
			wantOK: false,
		},
		{
			name:        "new version",
			result:      UpdateResult{HasUpdate: true, LatestVersion: "V1.2.0"},
			wantOK:      true,
			wantVersion: "V1.2.0",
		},
		{
			name:   "same version already notified",
			result: UpdateResult{HasUpdate: true, LatestVersion: "V1.2.0"},
			last:   "V1.2.0",
			wantOK: false,
		},
		{
			name:        "newer version after prior notice",
			result:      UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0"},
			last:        "V1.2.0",
			wantOK:      true,
			wantVersion: "V1.3.0",
		},
		{
			name:   "has update but empty version",
			result: UpdateResult{HasUpdate: true, LatestVersion: "  "},
			wantOK: false,
		},
		{
			name:        "whitespace normalized for notify and dedupe",
			result:      UpdateResult{HasUpdate: true, LatestVersion: " V2.0.0 "},
			wantOK:      true,
			wantVersion: "V2.0.0",
		},
		{
			name:   "whitespace-normalized version dedupes against stored",
			result: UpdateResult{HasUpdate: true, LatestVersion: " V1.2.0 "},
			last:   "V1.2.0",
			wantOK: false,
		},
		{
			name:   "whitespace-normalized last also dedupes",
			result: UpdateResult{HasUpdate: true, LatestVersion: "V1.2.0"},
			last:   " V1.2.0 ",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotOK := appUpdateNotifyVersion(tc.result, tc.last)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotVersion != tc.wantVersion {
				t.Fatalf("version = %q, want %q", gotVersion, tc.wantVersion)
			}
		})
	}
}

func TestSilentUpdateAttemptState(t *testing.T) {
	state := &backgroundUpdateCheckState{}
	if !state.beginSilentUpdate(" V1.2.0 ") {
		t.Fatal("first silent update attempt was rejected")
	}
	if state.beginSilentUpdate("V1.2.0") || state.beginSilentUpdate("V1.3.0") {
		t.Fatal("a second silent update started while one was in flight")
	}

	state.finishSilentUpdate("V1.2.0", false)
	if !state.beginSilentUpdate("V1.2.0") {
		t.Fatal("retryable silent update failure did not allow a retry")
	}

	state.finishSilentUpdate("V1.2.0", true)
	if state.beginSilentUpdate("V1.2.0") {
		t.Fatal("terminal silent update attempt restarted for the same version")
	}
	if !state.beginSilentUpdate("V1.3.0") {
		t.Fatal("terminal state for an older version blocked a newer version")
	}
}

func installSilentUpdateHooks(
	t *testing.T,
	download func(a *App, url, fileName, expectedSHA256 string) (string, error),
	install func(a *App, installerPath, targetVersion string) error,
) {
	t.Helper()
	previousDownload := silentUpdateDownloadHook
	previousInstall := silentUpdateInstallHook
	silentUpdateDownloadHook = download
	silentUpdateInstallHook = install
	t.Cleanup(func() {
		silentUpdateDownloadHook = previousDownload
		silentUpdateInstallHook = previousInstall
	})
}

func TestRunSilentUpdateDownloadsThenWaitsForUserConfirmation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }

	var gotURL, gotFileName, gotSHA256 string
	installSilentUpdateHooks(t,
		func(_ *App, url, fileName, expectedSHA256 string) (string, error) {
			gotURL, gotFileName, gotSHA256 = url, fileName, expectedSHA256
			return "C:/Downloads/MaClaw-Setup.exe", nil
		},
		func(_ *App, _, _ string) error {
			t.Fatal("silent download launched installer without user confirmation")
			return nil
		},
	)

	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", SHA256: "abc123"}
	outcome := app.runSilentUpdate(context.Background(), nil, result)
	if outcome.err != nil {
		t.Fatalf("runSilentUpdate() error = %v", outcome.err)
	}
	if !outcome.terminal {
		t.Fatal("completed silent download must close out the background attempt")
	}
	if gotURL != result.DownloadUrl || gotSHA256 != result.SHA256 || gotFileName != versionedInstallerFileName(updateTargetFileName(), result.LatestVersion) {
		t.Fatalf("download args = (%q, %q, %q)", gotURL, gotFileName, gotSHA256)
	}
	ready := app.downloadedUpdateForVersion(result.LatestVersion)
	if ready == nil || ready.InstallerPath != "C:/Downloads/MaClaw-Setup.exe" {
		t.Fatalf("downloaded update was not kept for confirmation: %#v", ready)
	}
	if len(events) != 1 || events[0] != EventAppUpdateDownloaded {
		t.Fatalf("events = %v, want only %s", events, EventAppUpdateDownloaded)
	}
}

func TestInstallDownloadedUpdateRequiresExplicitConfirmation(t *testing.T) {
	app := &App{}
	if err := app.InstallDownloadedUpdate("V9.9.9"); err == nil {
		t.Fatal("InstallDownloadedUpdate() succeeded without a downloaded ready update")
	}

	var gotInstallerPath, gotTargetVersion string
	installSilentUpdateHooks(t, nil, func(_ *App, installerPath, targetVersion string) error {
		gotInstallerPath, gotTargetVersion = installerPath, targetVersion
		return nil
	})
	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe"}
	app.storeDownloadedUpdate(result, "C:/Downloads/MaClaw-Setup.exe")
	if err := app.InstallDownloadedUpdate(result.LatestVersion); err != nil {
		t.Fatalf("InstallDownloadedUpdate() error = %v", err)
	}
	if gotInstallerPath != "C:/Downloads/MaClaw-Setup.exe" || gotTargetVersion != result.LatestVersion {
		t.Fatalf("install args = (%q, %q)", gotInstallerPath, gotTargetVersion)
	}
}

func TestRunSilentUpdateDoesNotInstallAfterSilentModeDisabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.SilentUpdateEnabled = false
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			return "C:\\Downloads\\MaClaw-Setup.exe", nil
		},
		func(_ *App, _, _ string) error {
			t.Fatal("installer launched after silent updates were disabled")
			return nil
		},
	)

	outcome := app.runSilentUpdate(context.Background(), &backgroundUpdateCheckState{}, UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe"})
	if outcome.err == nil {
		t.Fatal("runSilentUpdate() error = nil, want disabled-before-install error")
	}
	if outcome.terminal {
		t.Fatal("disabled silent update must remain retryable as a manual notification")
	}
	if len(events) != 1 || events[0] != EventAppUpdateAvailable {
		t.Fatalf("events = %v, want fallback %s notice", events, EventAppUpdateAvailable)
	}
}

func TestRunSilentUpdateDropsBetaDownloadAfterChannelDisabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.SilentUpdateEnabled = true
	cfg.PreferBetaChannel = false
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			return "C:/Downloads/MaClaw-Setup.exe", nil
		},
		func(_ *App, _, _ string) error {
			t.Fatal("beta installer offered after the beta channel was disabled")
			return nil
		},
	)

	outcome := app.runSilentUpdate(context.Background(), &backgroundUpdateCheckState{}, UpdateResult{
		HasUpdate: true, LatestVersion: "V9.9.9-beta.1", DownloadUrl: "https://example.invalid/setup.exe", Channel: "beta",
	})
	if outcome.err == nil || !strings.Contains(outcome.err.Error(), "beta channel was disabled") {
		t.Fatalf("runSilentUpdate() error = %v, want disabled-beta-channel failure", outcome.err)
	}
	if outcome.terminal {
		t.Fatal("channel-disabled beta download must not block a stable retry")
	}
	if len(events) != 0 {
		t.Fatalf("disabled beta channel emitted UI events: %v", events)
	}
}

func TestRunSilentUpdateDownloadFailureIsRetryable(t *testing.T) {
	app := &App{}
	wantErr := errors.New("network down")
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) { return "", wantErr },
		func(_ *App, _, _ string) error {
			t.Fatal("installer launched after a download failure")
			return nil
		},
	)

	outcome := app.runSilentUpdate(context.Background(), nil, UpdateResult{LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe"})
	if !errors.Is(outcome.err, wantErr) {
		t.Fatalf("runSilentUpdate() error = %v, want %v", outcome.err, wantErr)
	}
	if outcome.terminal {
		t.Fatal("download failure must remain retryable")
	}
}

func TestRunSilentUpdateDoesNotInstallSupersededVersion(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	state := &backgroundUpdateCheckState{}
	state.noteSilentUpdateVersion("V9.9.8", "stable")
	state.noteSilentUpdateVersion("V9.9.9", "stable")

	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			return "C:\\Downloads\\MaClaw-Setup.exe", nil
		},
		func(_ *App, _, _ string) error {
			t.Fatal("superseded installer launched")
			return nil
		},
	)

	outcome := app.runSilentUpdate(context.Background(), state, UpdateResult{LatestVersion: "V9.9.8", DownloadUrl: "https://example.invalid/setup.exe"})
	if outcome.err == nil || !strings.Contains(outcome.err.Error(), "superseded") {
		t.Fatalf("runSilentUpdate() error = %v, want superseded-version failure", outcome.err)
	}
	if outcome.terminal {
		t.Fatal("superseded update must leave the newer version retryable")
	}
}

func TestRunSilentUpdateDoesNotLetBetaChannelSupersedeStableChannel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	state := &backgroundUpdateCheckState{}
	// A beta build seen while the beta preference was enabled must not block
	// the lower-numbered stable line after the user switches back to stable.
	state.noteSilentUpdateVersion("V9.9.9", "beta")

	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			return "C:/Downloads/MaClaw-Setup.exe", nil
		},
		nil,
	)

	outcome := app.runSilentUpdate(context.Background(), state, UpdateResult{
		HasUpdate: true, LatestVersion: "V9.9.8", DownloadUrl: "https://example.invalid/setup.exe", Channel: "stable",
	})
	if outcome.err != nil {
		t.Fatalf("runSilentUpdate() error = %v, want stable channel to remain installable", outcome.err)
	}
	if !outcome.terminal {
		t.Fatal("completed stable download should be terminal for that version")
	}
}

func TestRunSilentUpdateSafelyRecoversPanics(t *testing.T) {
	app := &App{}
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) { panic("download boom") },
		nil,
	)

	outcome := app.runSilentUpdateSafely(context.Background(), nil, UpdateResult{LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe"})
	if outcome.err == nil || !strings.Contains(outcome.err.Error(), "download boom") {
		t.Fatalf("runSilentUpdateSafely() error = %v, want recovered panic", outcome.err)
	}
	if outcome.terminal {
		t.Fatal("panic before installer launch must remain retryable")
	}
}

func TestHandleBackgroundUpdateResultSilentStartsDownloadWithoutNotice(t *testing.T) {
	app := &App{}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }
	downloadStarted := make(chan struct{})
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			close(downloadStarted)
			return "", errors.New("stop before install")
		},
		func(_ *App, _, _ string) error {
			t.Fatal("installer launched by handler test")
			return nil
		},
	)

	state := &backgroundUpdateCheckState{lastNotifiedVersion: "V9.9.9"}
	app.handleBackgroundUpdateResult(context.Background(), state, UpdateResult{
		HasUpdate: true, LatestVersion: " V9.9.9 ", DownloadUrl: "https://example.invalid/setup.exe",
	}, true)

	select {
	case <-downloadStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("silent update download did not start")
	}
	if len(events) != 0 {
		t.Fatalf("silent update emitted UI events: %v", events)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.silentMu.Lock()
		inFlight := state.silentInFlight
		state.silentMu.Unlock()
		if !inFlight {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("silent update attempt did not finish")
}

func TestHandleBackgroundUpdateResultSilentDownloadUnavailableFallsBackToNotice(t *testing.T) {
	app := &App{}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			t.Fatal("download started without an installer URL")
			return "", nil
		},
		nil,
	)

	state := &backgroundUpdateCheckState{}
	app.handleBackgroundUpdateResult(context.Background(), state, UpdateResult{
		HasUpdate: true, LatestVersion: "V9.9.9", DownloadUnavailable: true,
	}, true)

	if len(events) != 1 || events[0] != EventAppUpdateAvailable {
		t.Fatalf("events = %v, want fallback %s notice", events, EventAppUpdateAvailable)
	}
}

func TestHandleBackgroundUpdateResultNonSilentKeepsCurrentNotice(t *testing.T) {
	app := &App{}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }
	state := &backgroundUpdateCheckState{}
	app.handleBackgroundUpdateResult(context.Background(), state, UpdateResult{
		HasUpdate: true, LatestVersion: " V9.9.9 ", DownloadUrl: "https://example.invalid/setup.exe",
	}, false)

	if len(events) != 1 || events[0] != EventAppUpdateAvailable {
		t.Fatalf("events = %v, want only %s", events, EventAppUpdateAvailable)
	}
	if state.lastNotifiedVersion != "V9.9.9" {
		t.Fatalf("lastNotifiedVersion = %q", state.lastNotifiedVersion)
	}
}

func TestRegisterDownloadedUpdateAllowsManualDeferChoice(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	if err := app.RegisterDownloadedUpdate(" V9.9.9 ", "C:/Downloads/MaClaw-Setup.exe", "beta"); err != nil {
		t.Fatalf("RegisterDownloadedUpdate() error = %v", err)
	}
	ready := app.downloadedUpdateForVersion("v9.9.9")
	if ready == nil || ready.InstallerPath != "C:/Downloads/MaClaw-Setup.exe" || ready.Result.Channel != "beta" {
		t.Fatalf("registered update = %#v", ready)
	}

	before := time.Now()
	if err := app.DeferDownloadedUpdateInstall("9.9.9", 2); err != nil {
		t.Fatalf("DeferDownloadedUpdateInstall() error = %v", err)
	}
	t.Cleanup(func() { _ = app.clearDeferredUpdateInstall() })
	record, ok := app.readDeferredUpdateInstall()
	if !ok {
		t.Fatal("deferred install record was not persisted")
	}
	if record.TargetVersion != "V9.9.9" || record.InstallerPath != "C:/Downloads/MaClaw-Setup.exe" || record.Channel != "beta" {
		t.Fatalf("deferred record = %#v", record)
	}
	if record.NextStartup {
		t.Fatal("hourly deferral incorrectly marked as next-startup install")
	}
	if due := record.DueAt.Sub(before); due < (119*time.Minute) || due > (121*time.Minute) {
		t.Fatalf("deferred due delta = %v, want about 2h", due)
	}
}

func TestDeferDownloadedUpdateInstallValidatesHoursAndReadyState(t *testing.T) {
	app := &App{}
	if err := app.DeferDownloadedUpdateInstall("V9.9.9", 3); err == nil {
		t.Fatal("unsupported deferral length accepted")
	}
	if err := app.DeferDownloadedUpdateInstall("V9.9.9", 1); err == nil {
		t.Fatal("deferral accepted without a downloaded ready update")
	}
	if err := app.DeferDownloadedUpdateInstallToNextStartup("V9.9.9"); err == nil {
		t.Fatal("next-startup deferral accepted without a downloaded ready update")
	}
}

func TestDeferDownloadedUpdateInstallToNextStartupPersistsChoice(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	if err := app.RegisterDownloadedUpdate("V9.9.9", "C:/Downloads/MaClaw-Setup.exe", "stable"); err != nil {
		t.Fatalf("RegisterDownloadedUpdate() error = %v", err)
	}
	if err := app.DeferDownloadedUpdateInstallToNextStartup("v9.9.9"); err != nil {
		t.Fatalf("DeferDownloadedUpdateInstallToNextStartup() error = %v", err)
	}
	record, ok := app.readDeferredUpdateInstall()
	if !ok {
		t.Fatal("next-startup install record was not persisted")
	}
	if !record.NextStartup || !record.DueAt.IsZero() {
		t.Fatalf("next-startup record = %#v", record)
	}
}

func TestDeferDownloadedUpdateInstallToNextStartupCancelsTimedTimer(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.RegisterDownloadedUpdate("V9.9.9", "C:/Downloads/MaClaw-Setup.exe", "stable"); err != nil {
		t.Fatalf("RegisterDownloadedUpdate() error = %v", err)
	}
	if err := app.DeferDownloadedUpdateInstall("V9.9.9", 1); err != nil {
		t.Fatalf("DeferDownloadedUpdateInstall() error = %v", err)
	}
	t.Cleanup(func() { _ = app.clearDeferredUpdateInstall() })
	app.downloadedUpdateMu.Lock()
	hadTimer := app.downloadedUpdateTimer != nil
	app.downloadedUpdateMu.Unlock()
	if !hadTimer {
		t.Fatal("timed deferral did not schedule its in-process timer")
	}

	if err := app.DeferDownloadedUpdateInstallToNextStartup("V9.9.9"); err != nil {
		t.Fatalf("DeferDownloadedUpdateInstallToNextStartup() error = %v", err)
	}
	app.downloadedUpdateMu.Lock()
	timer := app.downloadedUpdateTimer
	app.downloadedUpdateMu.Unlock()
	if timer != nil {
		t.Fatal("next-startup deferral kept the superseded timed-install timer")
	}
	record, ok := app.readDeferredUpdateInstall()
	if !ok || !record.NextStartup {
		t.Fatalf("next-startup record = %#v, ok=%v", record, ok)
	}
}

func TestDeferredNextStartupRecordDoesNotRunFromStaleTimedCallback(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")
	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: "V9.9.9",
		NextStartup:   true,
	})
	checked := false
	var checkResult UpdateResult
	installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
		checked = true
		return checkResult, nil
	})

	// A countdown timer can already be in flight when the user replaces a
	// timed deferral with next-startup. That stale callback must leave the
	// record untouched instead of installing during the current session.
	app.executeDeferredUpdateInstall("V9.9.9", false)
	if checked {
		t.Fatal("stale timed callback executed a next-startup install record")
	}
	if _, ok := app.readDeferredUpdateInstall(); !ok {
		t.Fatal("stale timed callback dropped the next-startup install choice")
	}

	// The same record remains executable when the next startup deliberately
	// runs it.
	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", Channel: "stable"}
	checkResult = result
	app.storeDownloadedUpdate(result, "C:/Downloads/MaClaw-Setup.exe")
	launches := 0
	installSilentUpdateHooks(t, nil, func(_ *App, installerPath, targetVersion string) error {
		launches++
		if installerPath != "C:/Downloads/MaClaw-Setup.exe" || targetVersion != result.LatestVersion {
			t.Fatalf("install args = (%q, %q)", installerPath, targetVersion)
		}
		return nil
	})
	app.executeDeferredUpdateInstall("V9.9.9", true)
	if launches != 1 {
		t.Fatalf("startup deferred install launches = %d, want 1", launches)
	}
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("successful next-startup install kept its durable record")
	}
}

func TestReadDeferredUpdateInstallDiscardsMalformedTiming(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	path := app.deferredUpdateInstallPath()
	if err := writeDeferredUpdateInstall(path, deferredUpdateInstall{TargetVersion: "V9.9.9"}); err != nil {
		t.Fatalf("write malformed deferred update record: %v", err)
	}
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("malformed deferred-install record without timing was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed deferred-install record was not removed, stat err=%v", err)
	}
}

func TestIgnoreUpdateVersionPersistsAndClearsPendingChoices(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	var events []string
	app.testEmit = func(name string, _ ...interface{}) { events = append(events, name) }

	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", Channel: "stable"}
	app.storeDownloadedUpdate(result, "C:/Downloads/MaClaw-Setup.exe")
	if err := app.DeferDownloadedUpdateInstall(result.LatestVersion, 1); err != nil {
		t.Fatalf("DeferDownloadedUpdateInstall() error = %v", err)
	}
	if err := app.IgnoreUpdateVersion("v9.9.9"); err != nil {
		t.Fatalf("IgnoreUpdateVersion() error = %v", err)
	}
	if !app.isUpdateVersionIgnored("V9.9.9") {
		t.Fatal("ignored version did not persist with v-prefix normalization")
	}
	ignored := readIgnoredUpdateVersions(app.ignoredUpdateVersionsPath())
	if _, ok := ignored.Versions["9.9.9"]; !ok {
		t.Fatalf("ignored versions = %#v", ignored.Versions)
	}
	if ready := app.downloadedUpdateForVersion(result.LatestVersion); ready != nil {
		t.Fatalf("ignored downloaded update is still ready: %#v", ready)
	}
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("ignored update kept its deferred-install record")
	}

	events = nil
	state := &backgroundUpdateCheckState{}
	app.handleBackgroundUpdateResult(context.Background(), state, result, false)
	if len(events) != 0 {
		t.Fatalf("ignored update emitted events: %v", events)
	}
	if state.lastNotifiedVersion != "" {
		t.Fatalf("ignored update was marked notified: %q", state.lastNotifiedVersion)
	}
}

func installDeferredUpdateCheckHook(t *testing.T, check func(a *App, currentVersion, channel string) (UpdateResult, error)) {
	t.Helper()
	previous := deferredUpdateCheckHook
	deferredUpdateCheckHook = check
	t.Cleanup(func() { deferredUpdateCheckHook = previous })
}

func withLinkedTestVersion(t *testing.T, value string) {
	t.Helper()
	previous := version
	version = value
	t.Cleanup(func() { version = previous })
}

func writeTestDeferredUpdateInstall(t *testing.T, app *App, record deferredUpdateInstall) {
	t.Helper()
	if record.TargetVersion == "" {
		record.TargetVersion = "V9.9.9"
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if err := writeDeferredUpdateInstall(app.deferredUpdateInstallPath(), record); err != nil {
		t.Fatalf("write deferred update record: %v", err)
	}
	t.Cleanup(func() { _ = app.clearDeferredUpdateInstall() })
}

func TestInstallDownloadedUpdateSupersedesDeferredChoice(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	if err := app.RegisterDownloadedUpdate("V9.9.9", "C:/Downloads/MaClaw-Setup.exe", "stable"); err != nil {
		t.Fatalf("RegisterDownloadedUpdate() error = %v", err)
	}
	if err := app.DeferDownloadedUpdateInstall("V9.9.9", 1); err != nil {
		t.Fatalf("DeferDownloadedUpdateInstall() error = %v", err)
	}
	installSilentUpdateHooks(t, nil, func(_ *App, installerPath, targetVersion string) error {
		if installerPath != "C:/Downloads/MaClaw-Setup.exe" || targetVersion != "V9.9.9" {
			t.Fatalf("install args = (%q, %q)", installerPath, targetVersion)
		}
		return nil
	})
	if err := app.InstallDownloadedUpdate("v9.9.9"); err != nil {
		t.Fatalf("InstallDownloadedUpdate() error = %v", err)
	}
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("install-now kept the deferred-install record")
	}
}

func TestUpdateInstallDecisionsAreSerialized(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.RegisterDownloadedUpdate("V9.9.9", "C:/Downloads/MaClaw-Setup.exe", "stable"); err != nil {
		t.Fatalf("RegisterDownloadedUpdate() error = %v", err)
	}

	installStarted := make(chan struct{})
	releaseInstall := make(chan struct{})
	var releaseOnce sync.Once
	releaseInstaller := func() { releaseOnce.Do(func() { close(releaseInstall) }) }
	defer releaseInstaller()
	installSilentUpdateHooks(t, nil, func(_ *App, _, _ string) error {
		close(installStarted)
		<-releaseInstall
		return nil
	})
	installDone := make(chan error, 1)
	go func() { installDone <- app.InstallDownloadedUpdate("V9.9.9") }()
	select {
	case <-installStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("test installer launch did not start")
	}

	if err := app.DeferDownloadedUpdateInstall("V9.9.9", 1); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("DeferDownloadedUpdateInstall() during install error = %v, want already-in-progress failure", err)
	}
	if err := app.DeferDownloadedUpdateInstallToNextStartup("V9.9.9"); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("DeferDownloadedUpdateInstallToNextStartup() during install error = %v, want already-in-progress failure", err)
	}
	releaseInstaller()
	if err := <-installDone; err != nil {
		t.Fatalf("InstallDownloadedUpdate() error = %v", err)
	}
}

func TestExecuteDeferredUpdateInstallLaunchesAuthorizedDownload(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")

	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", Channel: "beta"}
	app.storeDownloadedUpdate(result, "C:/Downloads/MaClaw-Setup.exe")
	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: result.LatestVersion,
		InstallerPath: "C:/Downloads/MaClaw-Setup.exe",
		Channel:       "beta",
		DueAt:         time.Now().Add(-time.Minute),
	})

	installDeferredUpdateCheckHook(t, func(_ *App, currentVersion, channel string) (UpdateResult, error) {
		if currentVersion != "9.9.8" || channel != "beta" {
			t.Fatalf("deferred check args = (%q, %q)", currentVersion, channel)
		}
		return result, nil
	})
	launches := 0
	installSilentUpdateHooks(t, nil, func(_ *App, installerPath, targetVersion string) error {
		launches++
		if installerPath != "C:/Downloads/MaClaw-Setup.exe" || targetVersion != result.LatestVersion {
			t.Fatalf("install args = (%q, %q)", installerPath, targetVersion)
		}
		return nil
	})

	app.executeDeferredUpdateInstall("v9.9.9", false)
	if launches != 1 {
		t.Fatalf("deferred installer launches = %d, want 1", launches)
	}
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("successful deferred install kept its durable record")
	}
}

func TestExecuteDeferredUpdateInstallRetriesCheckFailure(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")

	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: "V9.9.9",
		InstallerPath: "C:/Downloads/MaClaw-Setup.exe",
		Channel:       "stable",
		DueAt:         time.Now().Add(-time.Minute),
	})
	checkErr := errors.New("manifest unavailable")
	installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
		return UpdateResult{}, checkErr
	})
	installSilentUpdateHooks(t, nil, func(_ *App, _, _ string) error {
		t.Fatal("installer launched when the deferred update check failed")
		return nil
	})

	before := time.Now()
	app.executeDeferredUpdateInstall("V9.9.9", false)
	record, ok := app.readDeferredUpdateInstall()
	if !ok {
		t.Fatal("transient deferred check failure dropped the user's install choice")
	}
	if record.NextStartup || record.DueAt.Before(before.Add(29*time.Minute)) {
		t.Fatalf("retry record = %#v, want due about 30 minutes later", record)
	}
}

func TestExecuteDeferredUpdateInstallKeepsNextStartupRetryAtSessionBoundary(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")

	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: "V9.9.9",
		InstallerPath: "C:/Downloads/MaClaw-Setup.exe",
		Channel:       "stable",
		NextStartup:   true,
	})
	installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
		return UpdateResult{}, errors.New("manifest unavailable")
	})
	installSilentUpdateHooks(t, nil, func(_ *App, _, _ string) error {
		t.Fatal("installer launched when the deferred update check failed")
		return nil
	})

	app.executeDeferredUpdateInstall("V9.9.9", true)
	record, ok := app.readDeferredUpdateInstall()
	if !ok {
		t.Fatal("next-startup install choice was dropped after a transient check failure")
	}
	if !record.NextStartup || !record.DueAt.IsZero() {
		t.Fatalf("next-startup retry record = %#v, want the unchanged session-boundary choice", record)
	}
}

func TestExecuteDeferredUpdateInstallRetriesLaunchFailureButNotDeclinedElevation(t *testing.T) {
	newReadyApp := func(t *testing.T) *App {
		t.Helper()
		tmpHome := t.TempDir()
		t.Setenv("USERPROFILE", tmpHome)
		t.Setenv("HOME", tmpHome)
		app := &App{testHomeDir: tmpHome}
		withLinkedTestVersion(t, "9.9.8")
		result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", Channel: "stable"}
		app.storeDownloadedUpdate(result, "C:/Downloads/MaClaw-Setup.exe")
		writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
			TargetVersion: result.LatestVersion,
			InstallerPath: "C:/Downloads/MaClaw-Setup.exe",
			Channel:       "stable",
			DueAt:         time.Now().Add(-time.Minute),
		})
		installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
			return result, nil
		})
		return app
	}

	t.Run("transient launch failure keeps deferred choice", func(t *testing.T) {
		app := newReadyApp(t)
		installSilentUpdateHooks(t, nil, func(_ *App, _, _ string) error {
			return errors.New("shell busy")
		})
		app.executeDeferredUpdateInstall("V9.9.9", false)
		record, ok := app.readDeferredUpdateInstall()
		if !ok {
			t.Fatal("transient launch failure dropped the deferred install choice")
		}
		if record.NextStartup || time.Until(record.DueAt) < 29*time.Minute {
			t.Fatalf("retry record = %#v, want due about 30 minutes later", record)
		}
	})

	t.Run("declined elevation clears deferred choice", func(t *testing.T) {
		app := newReadyApp(t)
		installSilentUpdateHooks(t, nil, func(_ *App, _, _ string) error {
			return errInstallerElevationCancelled
		})
		app.executeDeferredUpdateInstall("V9.9.9", false)
		if _, ok := app.readDeferredUpdateInstall(); ok {
			t.Fatal("declined elevation kept a deferred install that would prompt again")
		}
	})
}

func TestExecuteDeferredUpdateInstallRebindsReusedInstallerToFreshHash(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")

	installerPath := filepath.Join(tmpHome, "MaClaw-Setup.exe")
	installerBytes := []byte("test installer bytes")
	if err := os.WriteFile(installerPath, installerBytes, 0o600); err != nil {
		t.Fatalf("write test installer: %v", err)
	}
	installerSHA256 := fmt.Sprintf("%x", sha256.Sum256(installerBytes))
	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", SHA256: installerSHA256, Channel: "stable"}

	// Simulate an installer downloaded before the fresh manifest exposed a
	// hash. Once the deferred execution verifies that hash, the launch
	// authorization must use it rather than falling back to file identity.
	app.storeDownloadedUpdate(UpdateResult{HasUpdate: true, LatestVersion: result.LatestVersion, Channel: result.Channel}, installerPath)
	app.rememberVerifiedInstaller(installerPath, "")
	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: result.LatestVersion,
		InstallerPath: installerPath,
		Channel:       result.Channel,
		DueAt:         time.Now().Add(-time.Minute),
	})
	installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
		return result, nil
	})
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			t.Fatal("reused installer was downloaded again")
			return "", nil
		},
		func(_ *App, gotInstallerPath, gotTargetVersion string) error {
			if gotInstallerPath != installerPath || gotTargetVersion != result.LatestVersion {
				t.Fatalf("install args = (%q, %q)", gotInstallerPath, gotTargetVersion)
			}
			return nil
		},
	)

	app.executeDeferredUpdateInstall(result.LatestVersion, false)
	app.verifiedInstallerMu.Lock()
	verified := app.verifiedInstallers[verifiedInstallerKey(installerPath)]
	app.verifiedInstallerMu.Unlock()
	if verified == nil || verified.SHA256 != installerSHA256 {
		t.Fatalf("verified installer hash after reuse = %#v, want %q", verified, installerSHA256)
	}
}

func TestExecuteDeferredUpdateInstallDownloadsFreshWhenPersistedPathIsUnverified(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")

	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", SHA256: "abc123", Channel: "stable"}
	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: result.LatestVersion,
		InstallerPath: filepath.Join(tmpHome, "missing-or-tampered-setup.exe"),
		Channel:       "stable",
		DueAt:         time.Now().Add(-time.Minute),
	})
	installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
		return result, nil
	})
	installSilentUpdateHooks(t,
		func(_ *App, url, fileName, expectedSHA256 string) (string, error) {
			if url != result.DownloadUrl || expectedSHA256 != result.SHA256 || fileName != versionedInstallerFileName(updateTargetFileName(), result.LatestVersion) {
				t.Fatalf("download args = (%q, %q, %q)", url, fileName, expectedSHA256)
			}
			return "C:/Downloads/fresh-MaClaw-Setup.exe", nil
		},
		func(_ *App, installerPath, targetVersion string) error {
			if installerPath != "C:/Downloads/fresh-MaClaw-Setup.exe" || targetVersion != result.LatestVersion {
				t.Fatalf("install args = (%q, %q)", installerPath, targetVersion)
			}
			return nil
		},
	)

	app.executeDeferredUpdateInstall(result.LatestVersion, false)
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("successful fresh deferred install kept its durable record")
	}
}

func TestExecuteDeferredUpdateInstallRetryKeepsFreshlyDownloadedPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "9.9.8")

	// The durable record still points at the stale unversioned file (written
	// before versioned naming, or simply replaced). The execution re-downloads
	// to the versioned destination; when that launch fails transiently the
	// retry record must keep the FRESH path so the next attempt can verify and
	// reuse those bytes instead of downloading the same installer again.
	stalePath := filepath.Join(tmpHome, "MaClaw-Setup.exe")
	freshPath := filepath.Join(tmpHome, "MaClaw-Setup-9.9.9.exe")
	result := UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", DownloadUrl: "https://example.invalid/setup.exe", SHA256: "abc123", Channel: "stable"}
	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: result.LatestVersion,
		InstallerPath: stalePath,
		Channel:       "stable",
		DueAt:         time.Now().Add(-time.Minute),
	})
	installDeferredUpdateCheckHook(t, func(_ *App, _, _ string) (UpdateResult, error) {
		return result, nil
	})
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			return freshPath, nil
		},
		func(_ *App, installerPath, _ string) error {
			if installerPath != freshPath {
				t.Fatalf("install path = %q, want freshly downloaded %q", installerPath, freshPath)
			}
			return errors.New("shell busy")
		},
	)

	app.executeDeferredUpdateInstall(result.LatestVersion, false)
	record, ok := app.readDeferredUpdateInstall()
	if !ok {
		t.Fatal("transient launch failure dropped the deferred install choice")
	}
	if record.InstallerPath != freshPath {
		t.Fatalf("retry record installer path = %q, want fresh %q", record.InstallerPath, freshPath)
	}
}

func installDeferredRollbackCatalogueHook(t *testing.T, catalogue func(a *App) ([]RollbackRelease, error)) {
	t.Helper()
	previous := deferredRollbackCatalogueHook
	deferredRollbackCatalogueHook = catalogue
	t.Cleanup(func() { deferredRollbackCatalogueHook = previous })
}

func TestResolveDeferredRollbackTarget(t *testing.T) {
	catalogue := []RollbackRelease{
		{Build: "11970", DownloadUrl: "https://example.invalid/releases/11970/MaClaw-Setup.exe", SHA256: "abc123"},
		{Build: "11960", DownloadUrl: "https://example.invalid/releases/11960/MaClaw-Setup.exe", SHA256: "def456", DownloadUnavailable: true},
	}

	t.Run("installable build keeps fresh url and hash", func(t *testing.T) {
		result := resolveDeferredRollbackTarget(catalogue, "8.1.0.11950", "11970")
		if !result.HasUpdate || result.LatestVersion != "11970" || result.SHA256 != "abc123" || result.Channel != deferredRollbackChannel {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("build missing from catalogue cannot be honored", func(t *testing.T) {
		if result := resolveDeferredRollbackTarget(catalogue, "8.1.0.11950", "11940"); result.HasUpdate {
			t.Fatalf("result = %#v, want no update", result)
		}
	})

	t.Run("unavailable or unsigned build cannot be honored", func(t *testing.T) {
		if result := resolveDeferredRollbackTarget(catalogue, "8.1.0.11950", "11960"); result.HasUpdate {
			t.Fatalf("result = %#v, want no update for unavailable build", result)
		}
		unsigned := []RollbackRelease{{Build: "11970", DownloadUrl: "https://example.invalid/x.exe"}}
		if result := resolveDeferredRollbackTarget(unsigned, "8.1.0.11950", "11970"); result.HasUpdate {
			t.Fatalf("result = %#v, want no update without a hash", result)
		}
	})

	t.Run("already installed build has nothing left to do", func(t *testing.T) {
		if result := resolveDeferredRollbackTarget(catalogue, "8.1.0.11970", "11970"); result.HasUpdate {
			t.Fatalf("result = %#v, want no update when the rollback already landed", result)
		}
	})
}

func TestExecuteDeferredUpdateInstallRollbackUsesCatalogue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "8.1.0.11950")

	// The deferred record names the rollback build. Unlike a channel release,
	// the execution must validate it against the rollback catalogue: a
	// deliberate rollback is never the latest version CheckUpdate would find.
	release := RollbackRelease{Build: "11970", DownloadUrl: "https://example.invalid/releases/11970/MaClaw-Setup.exe", SHA256: "abc123"}
	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: "11970",
		InstallerPath: filepath.Join(tmpHome, "missing.exe"),
		Channel:       deferredRollbackChannel,
		DueAt:         time.Now().Add(-time.Minute),
	})
	installDeferredRollbackCatalogueHook(t, func(_ *App) ([]RollbackRelease, error) {
		return []RollbackRelease{release}, nil
	})
	installSilentUpdateHooks(t,
		func(_ *App, url, fileName, expectedSHA256 string) (string, error) {
			if url != release.DownloadUrl || expectedSHA256 != release.SHA256 {
				t.Fatalf("download args = (%q, _, %q)", url, expectedSHA256)
			}
			if fileName != versionedInstallerFileName(updateTargetFileName(), "11970") {
				t.Fatalf("download file name = %q", fileName)
			}
			return "C:/Downloads/MaClaw-Setup-11970.exe", nil
		},
		func(_ *App, installerPath, targetVersion string) error {
			if installerPath != "C:/Downloads/MaClaw-Setup-11970.exe" || targetVersion != "11970" {
				t.Fatalf("install args = (%q, %q)", installerPath, targetVersion)
			}
			return nil
		},
	)

	app.executeDeferredUpdateInstall("11970", false)
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("successful deferred rollback install kept its durable record")
	}
}

func TestExecuteDeferredUpdateInstallRollbackDroppedWhenBuildLeavesCatalogue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	withLinkedTestVersion(t, "8.1.0.11950")

	writeTestDeferredUpdateInstall(t, app, deferredUpdateInstall{
		TargetVersion: "11970",
		InstallerPath: filepath.Join(tmpHome, "missing.exe"),
		Channel:       deferredRollbackChannel,
		DueAt:         time.Now().Add(-time.Minute),
	})
	installDeferredRollbackCatalogueHook(t, func(_ *App) ([]RollbackRelease, error) {
		return []RollbackRelease{{Build: "11980", DownloadUrl: "https://example.invalid/x.exe", SHA256: "abc123"}}, nil
	})
	installSilentUpdateHooks(t,
		func(_ *App, _, _, _ string) (string, error) {
			t.Fatal("a rollback build absent from the catalogue was downloaded")
			return "", nil
		},
		func(_ *App, _, _ string) error {
			t.Fatal("a rollback build absent from the catalogue was installed")
			return nil
		},
	)

	app.executeDeferredUpdateInstall("11970", false)
	if _, ok := app.readDeferredUpdateInstall(); ok {
		t.Fatal("unhonorable deferred rollback choice was kept")
	}
}

func TestRemoteAppVersion_UnlinkedBuildIsNotFakeOldRelease(t *testing.T) {
	previous := version
	version = "dev"
	t.Cleanup(func() { version = previous })
	if got := remoteAppVersion(); got != "1.0.0" {
		t.Fatalf("remoteAppVersion() = %q, want compatibility fallback", got)
	}
	if hasLinkedReleaseVersion() {
		t.Fatal("unlinked build reported as release version")
	}
}

func TestHasLinkedReleaseVersion(t *testing.T) {
	previous := version
	t.Cleanup(func() { version = previous })
	for _, candidate := range []string{"", "dev", " DEV ", "unknown", "7"} {
		version = candidate
		if hasLinkedReleaseVersion() {
			t.Fatalf("version %q reported as linked", candidate)
		}
	}
	version = "7.5.0.11973"
	if !hasLinkedReleaseVersion() {
		t.Fatal("release version reported as unlinked")
	}
}

func TestLinkedReleaseVersionTrimsValue(t *testing.T) {
	previous := version
	t.Cleanup(func() { version = previous })
	version = "  V7.5.0.11973  "
	got, ok := linkedReleaseVersion()
	if !ok || got != "V7.5.0.11973" {
		t.Fatalf("linkedReleaseVersion() = (%q, %v)", got, ok)
	}
}

func TestShouldFetchAppUpdateNow(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		next time.Time
		want bool
	}{
		{name: "never scheduled", next: time.Time{}, want: true},
		{name: "before next allowed", next: now.Add(10 * time.Minute), want: false},
		{name: "exactly at next allowed", next: now, want: true},
		{name: "after next allowed", next: now.Add(-time.Minute), want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldFetchAppUpdateNow(tc.next, now)
			if got != tc.want {
				t.Fatalf("shouldFetchAppUpdateNow() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextAppUpdateFetchAt(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	successInterval := 30 * time.Minute
	failureBackoff := 5 * time.Minute

	got := nextAppUpdateFetchAt(now, true, successInterval, failureBackoff)
	if want := now.Add(successInterval); !got.Equal(want) {
		t.Fatalf("success next = %v, want %v", got, want)
	}

	got = nextAppUpdateFetchAt(now, false, successInterval, failureBackoff)
	if want := now.Add(failureBackoff); !got.Equal(want) {
		t.Fatalf("failure next = %v, want %v", got, want)
	}

	// Non-positive intervals mean "allow immediately again".
	if got := nextAppUpdateFetchAt(now, true, 0, failureBackoff); !got.Equal(now) {
		t.Fatalf("zero success interval next = %v, want %v", got, now)
	}
	if got := nextAppUpdateFetchAt(now, false, successInterval, 0); !got.Equal(now) {
		t.Fatalf("zero failure backoff next = %v, want %v", got, now)
	}
}

func TestRunBackgroundUpdateCheckLoop_RunsFirstThenPeriodicAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var n atomic.Int32
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 5*time.Millisecond, 10*time.Millisecond, func() {
			if n.Add(1) >= 3 {
				cancel()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background update loop did not stop in time")
	}

	if got := n.Load(); got < 2 {
		t.Fatalf("expected at least first + one periodic tick, got %d", got)
	}
}

func TestRunBackgroundUpdateCheckLoop_TickPanicDoesNotKillLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var n atomic.Int32
	done := make(chan struct{})
	go func() {
		// Mirror production: recover inside the tick wrapper so panics do not
		// terminate the loop for the rest of the process lifetime.
		runBackgroundUpdateCheckLoop(ctx, 0, 5*time.Millisecond, func() {
			defer func() { _ = recover() }()
			i := n.Add(1)
			if i == 1 {
				panic("simulated check failure")
			}
			if i >= 3 {
				cancel()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not recover from tick panic")
	}
	if got := n.Load(); got < 3 {
		t.Fatalf("expected loop to continue after panic, ticks=%d", got)
	}
}

func TestRunBackgroundUpdateCheckLoop_ZeroInitialDelayStillRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called atomic.Bool
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 0, 50*time.Millisecond, func() {
			called.Store(true)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit with zero initial delay and cancelled ctx")
	}
	if called.Load() {
		t.Fatal("tick must not run when context is already cancelled")
	}
}

func TestRunBackgroundUpdateCheckLoop_CancelDuringInitialDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called atomic.Bool
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 200*time.Millisecond, 50*time.Millisecond, func() {
			called.Store(true)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit after ctx cancel during initial delay")
	}
	if called.Load() {
		t.Fatal("tick must not run when context is already cancelled")
	}
}

func TestRunBackgroundUpdateCheckLoop_NilContextNoPanic(t *testing.T) {
	runBackgroundUpdateCheckLoop(nil, 0, time.Millisecond, func() {
		t.Fatal("tick should not run with nil context")
	})
}

func TestRunBackgroundUpdateCheckLoop_ConcurrentCancelSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	ticks := 0
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 0, 5*time.Millisecond, func() {
			mu.Lock()
			ticks++
			n := ticks
			mu.Unlock()
			if n == 2 {
				cancel()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("loop did not stop")
	}
}

func TestStartBackgroundUpdateChecks_RequiresContextAndIsIdempotent(t *testing.T) {
	app := &App{}
	app.startBackgroundUpdateChecks()
	app.startBackgroundUpdateChecks()

	ctx, cancel := context.WithCancel(context.Background())
	app2 := &App{ctx: ctx, testHomeDir: t.TempDir()}
	app2.startBackgroundUpdateChecks()
	app2.startBackgroundUpdateChecks()
	cancel()
	// Allow the armed loop to observe cancel during the initial delay and exit.
	time.Sleep(20 * time.Millisecond)
}

func TestBackgroundUpdateBackoff_FailureShorterThanSuccess(t *testing.T) {
	now := time.Now()
	successNext := nextAppUpdateFetchAt(now, true, backgroundUpdateCheckInterval, backgroundUpdateCheckFailureBackoff)
	failureNext := nextAppUpdateFetchAt(now, false, backgroundUpdateCheckInterval, backgroundUpdateCheckFailureBackoff)
	if !failureNext.Before(successNext) {
		t.Fatalf("failure backoff %v should be sooner than success interval %v", failureNext.Sub(now), successNext.Sub(now))
	}
	if failureNext.Sub(now) != backgroundUpdateCheckFailureBackoff {
		t.Fatalf("failure backoff = %v, want %v", failureNext.Sub(now), backgroundUpdateCheckFailureBackoff)
	}
}

func TestDownloadedUpdatesCoexistAcrossVersions(t *testing.T) {
	// A manual rollback registration must not evict the silent download that
	// is still waiting for its own install decision, and vice versa.
	app := &App{}
	app.storeDownloadedUpdate(UpdateResult{HasUpdate: true, LatestVersion: "V9.9.9", Channel: "stable"}, "C:/Downloads/latest-setup.exe")
	if err := app.RegisterDownloadedUpdate("11970", "C:/Downloads/rollback-setup.exe", "stable"); err != nil {
		t.Fatalf("register rollback download: %v", err)
	}

	latest := app.downloadedUpdateForVersion("V9.9.9")
	if latest == nil || latest.InstallerPath != "C:/Downloads/latest-setup.exe" {
		t.Fatalf("latest ready update was evicted by the rollback registration: %#v", latest)
	}
	rollback := app.downloadedUpdateForVersion("11970")
	if rollback == nil || rollback.InstallerPath != "C:/Downloads/rollback-setup.exe" {
		t.Fatalf("rollback ready update missing: %#v", rollback)
	}

	app.clearDownloadedUpdateForVersion("11970")
	if ready := app.downloadedUpdateForVersion("11970"); ready != nil {
		t.Fatal("cleared rollback update still resolves")
	}
	if ready := app.downloadedUpdateForVersion("V9.9.9"); ready == nil {
		t.Fatal("clearing the rollback update evicted the latest download")
	}
}
