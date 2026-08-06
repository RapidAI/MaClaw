package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactRemovesCredentialsAndMAC(t *testing.T) {
	given := "token=hello password=secret mac=aa:bb:cc:dd:ee:ff https://a.test/x?token=abc"
	got := Redact(given)
	for _, secret := range []string{"hello", "secret", "aa:bb:cc:dd:ee:ff", "token=abc"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret leaked: %q", got)
		}
	}
}

func TestWriterWritesReadableEvents(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "job-1", "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Event(Info, "probe", "engine", "STAGE_STARTED", "stage.started", "", nil)
	w.Event(Error, "probe", "engine", "FAILED", "failed", "token=secret", nil)
	if err := w.WriteSummary(map[string]string{"status": "failed"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPage(filepath.Join(dir, "job-1"), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Events) != 2 || strings.Contains(p.Events[1].Detail, "secret") {
		t.Fatalf("unexpected events: %#v", p.Events)
	}
	if _, err := os.Stat(filepath.Join(dir, "job-1", "log-meta.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRawLogsAreRedacted(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, "job-raw", "attempt-raw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AppendRaw("sidecar.log", "authorization=secret aa:bb:cc:dd:ee:ff\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "job-raw", "sidecar.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "aa:bb:cc:dd:ee:ff") {
		t.Fatalf("raw log leaked: %q", b)
	}
}

func TestSafeFieldsAllowsOperationalBaudButNotDeviceSerial(t *testing.T) {
	fields := SafeFields(map[string]any{"baud": 115200, "fromBaud": 921600, "toBaud": 115200, "serial": "device-123"})
	if len(fields) != 3 || fields["baud"] != 115200 || fields["serial"] != nil {
		t.Fatalf("fields=%#v", fields)
	}
}
