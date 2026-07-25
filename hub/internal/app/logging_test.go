package app

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrefixFilterWriterPassesMatchingLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := prefixFilterWriter{
		w:       &buf,
		needles: []string{"[onboarding-sms]", "[onboarding-email]"},
	}
	if _, err := w.Write([]byte("2026/01/01 [onboarding-sms] send_code_begin phone=187***8637\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "onboarding-sms") {
		t.Fatalf("expected match written, got %q", buf.String())
	}
}

func TestPrefixFilterWriterDropsNonMatchingLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := prefixFilterWriter{
		w:       &buf,
		needles: []string{"[onboarding-sms]"},
	}
	n, err := w.Write([]byte("2026/01/01 [hub] listening on :9399\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("filter writer must report full write length")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected drop, got %q", buf.String())
	}
}

func TestIsHubRegistrationLogLine(t *testing.T) {
	t.Parallel()
	if !isHubRegistrationLogLine(`[onboarding-sms] send_code_begin`) {
		t.Fatal("expected onboarding-sms")
	}
	if !isHubRegistrationLogLine(`2026/01/01 12:00:00.000000 [ONBOARDING-EMAIL] send_code_rejected`) {
		t.Fatal("case-insensitive match expected")
	}
	if isHubRegistrationLogLine(`[hub] listening on :9399`) {
		t.Fatal("hub listen must not be registration")
	}
}

func TestHubLogWriterTeesRegistrationOnly(t *testing.T) {
	t.Parallel()
	var hub, reg, stderr bytes.Buffer
	w := &hubLogWriter{file: &hub, regFile: &reg, stderr: &stderr}

	if _, err := w.Write([]byte("[hub] listening\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hub.String(), "[hub] listening") || reg.Len() != 0 {
		t.Fatalf("hub=%q reg=%q", hub.String(), reg.String())
	}

	if _, err := w.Write([]byte("[onboarding-sms] send_code_begin phone=187***8637\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reg.String(), "onboarding-sms") {
		t.Fatalf("registration tee missing: %q", reg.String())
	}
	if !strings.Contains(stderr.String(), "onboarding-sms") {
		t.Fatalf("stderr missing: %q", stderr.String())
	}
}

func TestConfigureLoggingWritesFiles(t *testing.T) {
	dir := t.TempDir()
	prev := log.Writer()
	prevFlags := log.Flags()
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(prevFlags)
	})

	if err := ConfigureLogging(dir); err != nil {
		t.Fatal(err)
	}
	log.Printf("[onboarding-sms] send_code_begin phone=187***8637 tenant_id=tenant_default")
	log.Printf("[hub] other line")

	hubBody, err := os.ReadFile(filepath.Join(dir, "hub.log"))
	if err != nil {
		t.Fatal(err)
	}
	regBody, err := os.ReadFile(filepath.Join(dir, "registration.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hubBody), "onboarding-sms") || !strings.Contains(string(hubBody), "[hub] other line") {
		t.Fatalf("hub.log incomplete: %s", hubBody)
	}
	if !strings.Contains(string(regBody), "onboarding-sms") {
		t.Fatalf("registration.log missing onboarding line: %s", regBody)
	}
	if strings.Contains(string(regBody), "[hub] other line") {
		t.Fatalf("registration.log should not contain general hub lines: %s", regBody)
	}
}

func TestConfigureLoggingEmptyDirNoop(t *testing.T) {
	prev := log.Writer()
	t.Cleanup(func() { log.SetOutput(prev) })
	if err := ConfigureLogging(""); err != nil {
		t.Fatal(err)
	}
}
