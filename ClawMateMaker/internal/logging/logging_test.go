package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRawLogTruncationUpdatesSnapshot(t *testing.T) {
	root := t.TempDir()
	const jobID = "job-0123456789abcdef"
	w, err := New(root, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// AppendRaw redacts before applying the on-disk cap. Feed bounded chunks so
	// the test exercises the cumulative raw-log limit, not detail truncation.
	chunk := strings.Repeat("x\n", 1024)
	for written := int64(0); written <= maxRawLogBytes; written += int64(len(chunk)) {
		if err := w.AppendRaw("serial.log", chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadSnapshot(root, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LastEvent == nil || snapshot.LastEvent.Code != "LOG_TRUNCATED" || snapshot.LatestSequence != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestSafeFieldsAllowsOperationalBaudButNotDeviceSerial(t *testing.T) {
	fields := SafeFields(map[string]any{"baud": 115200, "fromBaud": 921600, "toBaud": 115200, "serial": "device-123"})
	if len(fields) != 3 || fields["baud"] != 115200 || fields["serial"] != nil {
		t.Fatalf("fields=%#v", fields)
	}
}

func TestSafeFieldsAllowsApplicationDescriptorMetadata(t *testing.T) {
	fields := SafeFields(map[string]any{"project": "maclaw_esp32s3_client", "version": "V7.0.0", "serial": "device-123"})
	if len(fields) != 2 || fields["project"] != "maclaw_esp32s3_client" || fields["version"] != "V7.0.0" || fields["serial"] != nil {
		t.Fatalf("fields=%#v", fields)
	}
}

func TestSafeJobIDRejectsPathsAndUntrustedValues(t *testing.T) {
	for _, value := range []string{"job-0123456789abcdef", "job-ffffffffffffffff"} {
		if !SafeJobID(value) {
			t.Fatalf("valid ID rejected: %q", value)
		}
	}
	for _, value := range []string{"", "job-0123", "job-0123456789abcdef/..", "../job-0123456789abcdef", "job-0123456789ABCDEf"} {
		if SafeJobID(value) {
			t.Fatalf("unsafe ID accepted: %q", value)
		}
	}
}

func TestReadRecentSummariesAcceptsOnlyBoundedValidJobDirectories(t *testing.T) {
	root := t.TempDir()
	newer := JobSummary{JobID: "job-0123456789abcdef", Status: "succeeded", FinishedAt: time.Now().UTC()}
	older := JobSummary{JobID: "job-fedcba9876543210", Status: "failed", ErrorMessage: "token=secret", FinishedAt: newer.FinishedAt.Add(-time.Minute)}
	for _, summary := range []JobSummary{newer, older} {
		dir := filepath.Join(root, summary.JobID)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(summary)
		if err := os.WriteFile(filepath.Join(dir, "summary.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "not-a-job"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-job", "summary.json"), []byte(`{"jobId":"not-a-job"}`), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := ReadRecentSummaries(root, 20)
	if err != nil || len(items) != 2 || items[0].JobID != newer.JobID || strings.Contains(items[1].ErrorMessage, "secret") {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestSnapshotTracksLastPersistedEventAndTerminalResult(t *testing.T) {
	root := t.TempDir()
	const jobID = "job-0123456789abcdef"
	w, err := New(root, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Event(Error, "flash", "engine", "WRITE_FAILED", "write.failed", "token=secret", nil)
	if err := w.WriteSummary(map[string]any{"jobId": jobID, "status": "failed", "errorCode": "WRITE_FAILED", "errorMessage": "token=secret"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadSnapshot(root, jobID)
	if err != nil || snapshot.Status != "failed" || snapshot.LatestSequence != 1 || snapshot.LastEvent == nil || snapshot.LastEvent.Code != "WRITE_FAILED" || strings.Contains(snapshot.ErrorMessage, "secret") {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	items, err := ReadRecentSnapshots(root, 20)
	if err != nil || len(items) != 1 || items[0].JobID != jobID {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}
