package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func setLogDetailForTest(t *testing.T, enabled bool) {
	t.Helper()
	previous := corelib.IsLogDetailEnabled()
	corelib.SetLogDetailEnabled(enabled)
	t.Cleanup(func() { corelib.SetLogDetailEnabled(previous) })
}

func TestDetailAwareLogWriterCanDisableStderrMirror(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: nil}

	if _, err := writer.Write([]byte("warning: tui startup\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "warning: tui startup") {
		t.Fatalf("file log missing important line: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr mirror should stay disabled, got %q", got)
	}
}

func TestDetailAwareLogWriterDropsUnimportantLinesWhenDetailDisabled(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: &stderr}

	if _, err := writer.Write([]byte("routine startup note\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); got != "" {
		t.Fatalf("file log should drop unimportant line, got %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr mirror should drop unimportant line, got %q", got)
	}
}

// The user sees semantic_capability_unmet in the UI, and this line carries the
// only cause record. Losing it to the detail gate leaves the rejection
// undiagnosable after the fact.
func TestDetailAwareLogWriterKeepsSemanticPlanRejectWhenDetailDisabled(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: nil}

	line := `[semantic-routing] plan rejected user="desktop-user:x" reason=semantic route has unmet needs` + "\n"
	if _, err := writer.Write([]byte(line)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "unmet needs") {
		t.Fatalf("host reject reason was dropped: %q", got)
	}
}

func TestDetailAwareLogWriterIgnoresGenericPlanRejectedWording(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: nil}

	if _, err := writer.Write([]byte("the user said the plan rejected yesterday\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); got != "" {
		t.Fatalf("generic wording should not bypass the detail gate: %q", got)
	}
}

func TestUsableLogDirRejectsSymlinkAndNonDir(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing-logs")
	if err := usableLogDir(missing); err != nil {
		t.Fatalf("missing dir should be creatable: %v", err)
	}
	if err := usableLogDir(""); err == nil {
		t.Fatal("empty dir should be rejected")
	}
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := usableLogDir(filePath); err == nil {
		t.Fatal("file path should be rejected")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "logs")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := usableLogDir(link); err == nil {
		t.Fatal("symlink log dir should be rejected")
	}
}

func TestOpenLogSinksRefusesSymlinkFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "external.log")
	if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "maclaw.log")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Cleanup(closeLogSinks)
	if openLogSinks(dir, false) {
		t.Fatal("openLogSinks followed a maclaw.log symlink")
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "external" {
		t.Fatalf("symlink target was written: %q err=%v", body, err)
	}
}

func TestTruncateBugReportDiagnosticFileRefusesSymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "external.log")
	if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "active.log")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := truncateBugReportDiagnosticFile(link); err == nil {
		t.Fatal("truncate followed a symlink")
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "external" {
		t.Fatalf("symlink target was truncated: %q err=%v", body, err)
	}
}

func TestOpenLogSinksKeepsMaclawLogWhenRegistrationLogIsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "external-registration.log")
	if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "registration.log")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Cleanup(closeLogSinks)
	if !openLogSinks(dir, false) {
		t.Fatal("openLogSinks should keep maclaw.log when only registration.log is a symlink")
	}
	log.Printf("[semantic-routing] plan rejected user=%q reason=%v", "user-1", "semantic route has unmet needs")
	body, err := os.ReadFile(filepath.Join(dir, "maclaw.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "plan rejected") {
		t.Fatalf("maclaw.log missing reject line: %s", body)
	}
	external, err := os.ReadFile(target)
	if err != nil || string(external) != "external" {
		t.Fatalf("registration.log symlink target was written: %q err=%v", external, err)
	}
}

func TestOpenLogSinksRefusesSymlinkDir(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "logs")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Cleanup(closeLogSinks)
	if openLogSinks(link, false) {
		t.Fatal("openLogSinks followed a logs symlink")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatalf("symlink target was written: %v", entries)
	}
}

func TestDetailAwareLogWriterKeepsBugReportLinesWhenDetailDisabled(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: nil}

	line := `[bug-report] diagnostics cleared; log sinks reopened` + "\n"
	if _, err := writer.Write([]byte(line)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "log sinks reopened") {
		t.Fatalf("bug-report line was dropped: %q", got)
	}
}

func TestDetailAwareLogWriterAlwaysKeepsRegistrationLines(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var reg bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, regFile: &reg, stderr: &stderr}

	line := `[registration-contact] phone send rejected code=PHONE_REGISTRATION_DISABLED` + "\n"
	if _, err := writer.Write([]byte(line)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if !strings.Contains(file.String(), "PHONE_REGISTRATION_DISABLED") {
		t.Fatalf("maclaw.log missing registration line: %q", file.String())
	}
	if !strings.Contains(reg.String(), "PHONE_REGISTRATION_DISABLED") {
		t.Fatalf("registration.log missing registration line: %q", reg.String())
	}
	if !strings.Contains(stderr.String(), "PHONE_REGISTRATION_DISABLED") {
		t.Fatalf("stderr missing registration line: %q", stderr.String())
	}

	// Non-registration noise still dropped with detail off.
	if _, err := writer.Write([]byte("routine note\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if strings.Contains(file.String(), "routine note") || strings.Contains(reg.String(), "routine note") {
		t.Fatalf("routine noise leaked into logs: file=%q reg=%q", file.String(), reg.String())
	}
}

func TestIsRegistrationLogLine(t *testing.T) {
	if !isRegistrationLogLine(`[onboarding] ActivateRemote total=1s`) {
		t.Fatal("expected onboarding important")
	}
	if !isRegistrationLogLine(`[onboarding-sms] send_code_begin`) {
		t.Fatal("expected onboarding-sms")
	}
	if !isRegistrationLogLine(`[frontend-diagnostic] {"tag":"onboarding","stage":"x"}`) {
		t.Fatal("expected onboarding frontend diagnostic")
	}
	if !isRegistrationLogLine(`[frontend-diagnostic] {"tag": "onboarding", "stage":"x"}`) {
		t.Fatal("expected spaced JSON tag form")
	}
	if isRegistrationLogLine(`[frontend-diagnostic] {"stage":"app-render-begin"}`) {
		t.Fatal("non-onboarding frontend diagnostic must not be registration")
	}
	if isRegistrationLogLine(`routine startup note`) {
		t.Fatal("noise must not be registration")
	}
}

func TestDetailAwareLogWriterMirrorsStderrWhenConfigured(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: &stderr}

	if _, err := writer.Write([]byte("failed to start tui\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "failed to start tui") {
		t.Fatalf("file log missing important line: %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "failed to start tui") {
		t.Fatalf("stderr mirror missing important line: %q", got)
	}
}

func TestDetailAwareLogWriterWritesRoutineLinesWhenDetailEnabled(t *testing.T) {
	setLogDetailForTest(t, true)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: &stderr}

	if _, err := writer.Write([]byte("routine startup note\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "routine startup note") {
		t.Fatalf("file log missing detail line: %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "routine startup note") {
		t.Fatalf("stderr mirror missing detail line: %q", got)
	}
}

func TestDetailAwareLogWriterAllowsNilMirrorTargets(t *testing.T) {
	setLogDetailForTest(t, true)

	writer := &detailAwareLogWriter{}
	if n, err := writer.Write([]byte("detail line without sinks\n")); err != nil || n == 0 {
		t.Fatalf("Write() = (%d, %v), want successful discard", n, err)
	}
}

func TestSetLogFallbackSuppressesTUIStderr(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	var stderr bytes.Buffer
	log.SetOutput(&stderr)
	log.SetFlags(0)

	setLogFallback(false, &stderr)
	log.Print("hidden from terminal")
	if got := stderr.String(); got != "" {
		t.Fatalf("TUI fallback should suppress default stderr logging, got %q", got)
	}
}

func TestSetLogFallbackPreservesDesktopStderr(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	var stderr bytes.Buffer
	log.SetOutput(&stderr)
	log.SetFlags(0)

	setLogFallback(true, &stderr)
	log.Print("visible in desktop mode")
	if got := stderr.String(); !strings.Contains(got, "visible in desktop mode") {
		t.Fatalf("desktop fallback should keep stderr logging, got %q", got)
	}
}

func TestSetLogFallbackHandlesNilDesktopStderr(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	setLogFallback(true, nil)
	if log.Writer() == nil {
		t.Fatal("desktop fallback should install a non-nil writer")
	}
}

func TestIsTUISubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "standalone desktop", args: []string{"maclaw"}, want: false},
		{name: "tui", args: []string{"maclaw", "tui"}, want: true},
		{name: "ui alias", args: []string{"maclaw", "ui"}, want: true},
		{name: "later tui arg is not subcommand", args: []string{"maclaw", "remote-smoke", "tui"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTUISubcommand(tt.args); got != tt.want {
				t.Fatalf("isTUISubcommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
