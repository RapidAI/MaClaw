package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalRoundTripAndTamperDetection(t *testing.T) {
	root := t.TempDir()
	source := Journal{JobID: "job-a", PackageID: "package", PackageSHA256: "sha256:x", State: JournalPrepared}
	if err := WriteJournal(root, source); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJournal(root, "job-a")
	if err != nil || got.State != JournalPrepared {
		t.Fatalf("journal=%+v err=%v", got, err)
	}
	if err := WriteJournal(root, Journal{JobID: "../x", PackageID: "x"}); err == nil {
		t.Fatal("unsafe ID accepted")
	}
	if err := WriteJournal(root, Journal{JobID: "unknown", PackageID: "x", State: "invented"}); err == nil {
		t.Fatal("unknown journal state accepted")
	}
}

func TestReadJournalRejectsUnknownButChecksummedState(t *testing.T) {
	root := t.TempDir()
	known := Journal{Schema: JournalSchema, JobID: "job-unknown", PackageID: "package", State: "invented", UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal(known)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	known.Checksum = "sha256:" + hex.EncodeToString(sum[:])
	final, err := json.Marshal(known)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, known.JobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), final, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJournal(root, known.JobID); err == nil {
		t.Fatal("unknown but checksummed state accepted")
	}
}

func TestListRecoveryRequiredExcludesOnlyVerifiedWrites(t *testing.T) {
	root := t.TempDir()
	for _, source := range []Journal{
		{JobID: "verified", PackageID: "ok", State: JournalVerified},
		{JobID: "prepared", PackageID: "pending", State: JournalPrepared},
		{JobID: "writing", PackageID: "interrupted", State: JournalWriting},
		{JobID: "failed", PackageID: "recovery", State: JournalRecoveryRequired, BootCriticalModified: true},
	} {
		if err := WriteJournal(root, source); err != nil {
			t.Fatal(err)
		}
	}
	items, err := ListRecoveryRequired(root)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	seen := map[string]RecoveryItem{}
	for _, item := range items {
		seen[item.JobID] = item
	}
	if _, ok := seen["verified"]; ok || seen["prepared"].JobID != "" || seen["writing"].State != JournalRecoveryRequired || !seen["failed"].BootCriticalModified {
		t.Fatalf("unexpected recovery items: %+v", items)
	}
}

func TestMarkRecoveryResolvedRequiresRecoveryState(t *testing.T) {
	root := t.TempDir()
	if err := WriteJournal(root, Journal{JobID: "interrupted", PackageID: "package", State: JournalRecoveryRequired}); err != nil {
		t.Fatal(err)
	}
	if err := MarkRecoveryResolved(root, "interrupted"); err != nil {
		t.Fatal(err)
	}
	items, err := ListRecoveryRequired(root)
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if err := WriteJournal(root, Journal{JobID: "prepared", PackageID: "package", State: JournalPrepared}); err != nil {
		t.Fatal(err)
	}
	if err := MarkRecoveryResolved(root, "prepared"); err == nil {
		t.Fatal("prepared job was incorrectly resolved")
	}
}

func TestListRecoveryRequiredIgnoresStrayInvalidDirectoryButBlocksWriteEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "unrelated"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated", "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "job-interrupted"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "job-interrupted", "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "job-interrupted", "events.jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := ListRecoveryRequired(root)
	if err != nil || len(items) != 1 || items[0].JobID != "job-interrupted" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}
